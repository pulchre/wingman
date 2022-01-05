package native

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"

	"github.com/pulchre/wingman"
	pb "github.com/pulchre/wingman/grpc"
	"google.golang.org/grpc"
)

const DefaultHost = "localhost"
const DefaultPort = 11456

type Pool struct {
	pb.UnimplementedProcessorServer
	server *grpc.Server
	opts   PoolOptions

	cond      *sync.Cond
	available []*handle
	busy      []*handle
	dead      []*handle

	processes map[int]*process

	shutdownCond *sync.Cond
	shutdown     bool
	wg           sync.WaitGroup

	doneMu     sync.Mutex
	doneChan   chan struct{}
	doneClosed bool

	err   error
	errMu sync.Mutex
}

type PoolOptions struct {
	Processes   int
	Concurrency int
	Host        string
	Port        int
	GRPCOptions []grpc.ServerOption
}

func DefaultOptions() PoolOptions {
	return PoolOptions{
		Processes:   1,
		Concurrency: 1,
		Host:        DefaultHost,
		Port:        DefaultPort,
		GRPCOptions: make([]grpc.ServerOption, 0),
	}
}

func (o PoolOptions) SetProcesses(v int) PoolOptions {
	o.Processes = v
	return o
}

func (o PoolOptions) SetConcurrency(v int) PoolOptions {
	o.Concurrency = v
	return o
}

func init() {
	wingman.NewProcessorPool = NewPool
}

func NewPool(opts interface{}) (wingman.ProcessorPool, error) {
	p := newPool(opts.(PoolOptions))

	err := p.start()
	if err != nil {
		return nil, err
	}

	return p, nil
}

func newPool(opts PoolOptions) *Pool {
	if opts.Processes < 1 {
		opts.Processes = 1
	}

	if opts.Concurrency < 1 {
		opts.Concurrency = 1
	}

	return &Pool{
		opts:         opts,
		cond:         sync.NewCond(new(sync.Mutex)),
		available:    make([]*handle, 0),
		busy:         make([]*handle, 0),
		dead:         make([]*handle, 0),
		processes:    make(map[int]*process),
		shutdownCond: sync.NewCond(new(sync.Mutex)),
		doneChan:     make(chan struct{}),
	}
}

func (p *Pool) start() error {
	err := p.startGRPCServer()
	if err != nil {
		return err
	}

	for i := 0; i < p.opts.Processes; i++ {
		process := newProcess(p.opts)
		err = process.Start()
		if err != nil {
			// TODO: Make sure we shutdown any started processes
			return err
		}

		p.processes[process.Pid()] = process

		p.wg.Add(1)
		go p.watchHandleUpdates(process)
	}

	return nil
}

func (p *Pool) startGRPCServer() error {
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", p.opts.Host, p.opts.Port))
	if err != nil {
		return err
	}

	p.server = grpc.NewServer(p.opts.GRPCOptions...)
	pb.RegisterProcessorServer(p.server, p)
	go func() {
		err := p.server.Serve(lis)
		p.errMu.Lock()
		defer p.errMu.Unlock()

		p.err = err
	}()

	return nil
}

func (p *Pool) Get() wingman.Processor {
	p.cond.L.Lock()
	defer p.cond.L.Unlock()

	if len(p.available) == 0 {
		p.cond.Wait()
	}

	if len(p.available) == 0 {
		return nil
	}

	processor := p.available[0]
	p.available = p.available[1:]
	p.busy = append(p.busy, processor)

	return processor
}

func (p *Pool) Put(id string) {
	p.cond.L.Lock()
	defer p.cond.L.Unlock()

	p.shutdownCond.L.Lock()
	defer p.shutdownCond.L.Unlock()

	i, ok := index(id, p.busy)
	if !ok {
		return
	}

	if p.shutdown {
		if len(p.busy) == 1 {
			p.shutdownCond.Broadcast()
		}
	} else {
		p.available = append(p.available, p.busy[i])
	}
	p.busy = append(p.busy[:i], p.busy[i+1:]...)

	p.Cancel()
}

func (p *Pool) Wait() {
	p.cond.L.Lock()
	defer p.cond.L.Unlock()

	if len(p.available) > 0 {
		return
	}

	p.cond.Wait()
}

func (p *Pool) Cancel() {
	p.cond.Broadcast()
}

func (p *Pool) Close() {
	p.closeProcessors(true)
	p.wg.Wait()
	p.server.Stop()
	p.sendDone()

}

func (p *Pool) ForceClose() {
	p.cond.L.Lock()
	defer p.cond.L.Unlock()

	for _, proc := range p.processes {
		// This is for tests.
		if proc.cmd == nil || proc.cmd.Process.Pid == os.Getpid() {
			continue
		}

		proc.Kill()
	}

	p.server.Stop()
	p.sendDone()
}

func (p *Pool) Done() <-chan struct{} { return p.doneChan }

func (p *Pool) Initialize(stream pb.Processor_InitializeServer) error {
	p.wg.Add(1)
	defer p.wg.Done()

	in, err := stream.Recv()
	if err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}

	if in.GetType() != pb.Type_CONNECT {
		return errors.New("First message must be CONNECT")
	}

	p.cond.L.Lock()
	proc, ok := p.processes[int(in.GetProcessID())]
	p.cond.L.Unlock()

	if ok {
		return proc.handleStream(stream)
	} else {
		return fmt.Errorf("Process with PID %v is unknown", in.GetProcessID())
	}
}

func (p *Pool) Error() error {
	p.errMu.Lock()
	defer p.errMu.Unlock()

	return p.err
}

func (p *Pool) watchHandleUpdates(process *process) {
	defer p.wg.Done()

	for msg := range process.updates() {
		p.cond.L.Lock()
		switch msg.State {
		case handleStateAlive:
			p.available = append(p.available, msg.Handle)
			p.cond.Broadcast()
		case handleStateDead:
			if i, ok := index(msg.Handle.Id(), p.available); ok {
				p.available = append(p.available[:i], p.available[i+1:]...)
			}

			p.dead = append(p.dead, msg.Handle)
		}
		p.cond.L.Unlock()
	}
}

func (p *Pool) sendDone() {
	p.doneMu.Lock()
	defer p.doneMu.Unlock()

	if p.doneClosed {
		return
	}

	close(p.doneChan)
	p.doneClosed = true
}

// TODO: review handling of handles and processes
func (p *Pool) closeProcessors(block bool) {
	p.shutdownCond.L.Lock()
	defer p.shutdownCond.L.Unlock()

	p.shutdown = true

	p.cond.L.Lock()
	for _, h := range p.available {
		p.dead = append(p.dead, h)
	}

	p.available = make([]*handle, 0)
	p.dead = make([]*handle, 0)
	p.cond.L.Unlock()

	for _, p := range p.processes {
		p.Stop()
	}

	if block && len(p.busy) > 0 {
		p.shutdownCond.Wait()
	}
}

func index(id string, a []*handle) (int, bool) {
	for i, p := range a {
		if p.Id() == id {
			return i, true
		}
	}

	return 0, false
}
