package native

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/pulchre/wingman"
	pb "github.com/pulchre/wingman/grpc"
)

const (
	handleStateAlive = iota
	handleStateDead
)

var bin, binErr = os.Executable()

type handleState int

type process struct {
	opts             PoolOptions
	handleUpdateChan chan handleUpdate
	cmd              *exec.Cmd

	serverStream   pb.Processor_InitializeServer
	serverStreamMu sync.Mutex

	shutdownSent bool

	handles  map[uuid.UUID]*handle
	handleMu sync.Mutex

	wg wingman.WaitGroup

	signalChan chan os.Signal
}

type handleUpdate struct {
	Handle *handle
	State  handleState
}

func init() {
	if binErr != nil {
		panic(fmt.Sprintf("Could not find binary path %v", binErr))
	}

	binOverride := strings.TrimSpace(os.Getenv("WINGMAN_NATIVE_BIN"))
	if binOverride != "" {
		bin = binOverride
	}
}

func newProcess(opts PoolOptions) *process {
	return &process{
		opts:             opts,
		handleUpdateChan: make(chan handleUpdate, opts.Concurrency*2),
		handles:          make(map[uuid.UUID]*handle),
		signalChan:       make(chan os.Signal, 32),
	}
}

func (p *process) updates() <-chan handleUpdate {
	return p.handleUpdateChan
}

func (p *process) sendJob(job wingman.InternalJob, h *handle) error {
	if p.serverStream == nil {
		return fmt.Errorf("Processor not connected")
	}

	id, err := job.ID.MarshalBinary()
	if err != nil {
		return err
	}

	payload, err := json.Marshal(job.Job)
	if err != nil {
		return err
	}

	processorId, err := h.id.MarshalBinary()
	if err != nil {
		return err
	}

	msg := &pb.Message{
		Type:        pb.Type_JOB,
		ProcessorID: processorId,
		Job: &pb.Job{
			ID:       id,
			TypeName: job.TypeName,
			Payload:  payload,
		},
	}

	wingman.Log.Printf("Sending job id=%s processorid=%s", job.ID, h.id.String())
	err = p.serverStream.Send(msg)
	if err != nil {
		return err
	}

	h.setStatus(wingman.ProcessorStatusWorking)
	return nil
}

func (p *process) Start() error {
	var err error

	p.handleMu.Lock()
	for i := 0; i < p.opts.Concurrency; i++ {
		h := newHandle(p)
		p.handles[h.id] = h
		h.setStatus(wingman.ProcessorStatusStarting)
	}
	p.handleMu.Unlock()

	defer func() {
		if err != nil {
			if p.cmd.Process != nil {
				p.cmd.Process.Kill()
			}

			p.cmd = nil
		}
	}()

	p.cmd = exec.Command(bin)
	p.cmd.Stdout = os.Stdout
	p.cmd.Stderr = os.Stderr

	p.cmd.Env = make([]string, len(os.Environ())+2)
	p.cmd.Env[0] = "WINGMAN_NATIVE_SUBPROCESS=1"
	p.cmd.Env[1] = fmt.Sprintf("WINGMAN_NATIVE_PORT=%d", p.opts.Port)
	for i, e := range os.Environ() {
		p.cmd.Env[i+2] = e
	}

	err = p.cmd.Start()
	if err != nil {
		return err
	}

	wingman.Log.Printf("Starting subprocess pid=%d", p.cmd.Process.Pid)

	p.wg.Add(1)
	go func() {
		p.cmd.Wait()

		p.handleMu.Lock()
		for _, h := range p.handles {
			h.setStatus(wingman.ProcessorStatusDead)
		}
		p.handleMu.Unlock()

		p.wg.Done()
	}()

	return nil
}

func (p *process) Stop() error {
	p.serverStreamMu.Lock()
	if p.shutdownSent {
		return nil
	}

	p.shutdownSent = true

	if p.serverStream == nil {
		return fmt.Errorf("Cannot stop unstarted process not started")
	}
	p.serverStreamMu.Unlock()

	err := p.serverStream.Send(&pb.Message{
		Type: pb.Type_SHUTDOWN,
	})
	if err != nil {
		wingman.Log.Print(`Failed to send shutdown signal to pid=%v err="%v"`, p.cmd.Process.Pid, err)
	}

	if p.cmd == nil {
		return nil
	}

	defer func() {
		p.cmd = nil
	}()

	// TODO: we wait for the process to stop in case we are working and
	// need to send the results. Sending a signal to the parent process
	// also sends it to the sub processes, at least on Linux. So we need
	// handle that properly in the subprocess before we can fix this as
	// the process will die and we will be waiting for it to finish
	// working.
	p.wg.Wait()

	return nil
}

func (p *process) Kill() error {
	err := p.cmd.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		err = nil
	}

	if p.handleUpdateChan != nil {
		close(p.handleUpdateChan)
		p.handleUpdateChan = nil
	}

	p.wg.Clear()

	return err
}

func (p *process) handleStream(stream pb.Processor_InitializeServer) error {
	p.wg.Add(1)
	defer p.wg.Done()

	wingman.Log.Printf("Subprocess connected pid=%v", p.cmd.Process.Pid)

	p.serverStreamMu.Lock()
	p.serverStream = stream
	p.serverStreamMu.Unlock()

	p.handleMu.Lock()
	for _, h := range p.handles {
		h.setStatus(wingman.ProcessorStatusIdle)
		p.handleUpdateChan <- handleUpdate{
			Handle: h,
			State:  handleStateAlive,
		}
	}
	p.handleMu.Unlock()

	wingman.Log.Print("Starting Server Stream goroutine")
	for {
		in, err := p.serverStream.Recv()
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}

		switch in.GetType() {
		case pb.Type_RESULT:
			processorID, err := uuid.FromBytes(in.GetProcessorID())
			if err != nil {
				wingman.Log.Printf("Error parsing processor id in response message: %v", err)
				continue
			}

			parsedJob, err := deserializeJob(in.GetJob())
			if err != nil {
				wingman.Log.Printf("Error parsing job in response message: %v, %v", err, in.GetJob())
				continue
			}

			handle := p.handleFromID(processorID)
			if handle == nil {
				wingman.Log.Printf("Error finding processor for job id=%v jobid=%v", processorID, parsedJob.ID)
				continue
			}

			var processErr error
			if in.GetError() != nil {
				processErr = errors.New(in.GetError().Message)
			}

			handle.sendResult(wingman.ResultMessage{
				Job:   parsedJob,
				Error: processErr,
			})
			close(handle.resultChan)

			handle.setStatus(wingman.ProcessorStatusIdle)
			close(handle.doneChan)
		default:
			wingman.Log.Print(in.GetType().String())
		}
	}
}

func (p *process) Pid() int {
	if p.cmd == nil || p.cmd.Process == nil {
		return -1
	}

	return p.cmd.Process.Pid
}

func (p *process) handleFromID(id uuid.UUID) *handle {
	p.handleMu.Lock()
	defer p.handleMu.Unlock()

	return p.handles[id]

}

// TODO: This should be moved to the GRPC package
func deserializeJob(msg *pb.Job) (wingman.InternalJob, error) {
	id, err := uuid.FromBytes(msg.ID)
	if err != nil {
		return wingman.InternalJob{}, err
	}

	job := wingman.InternalJob{
		ID:       id,
		TypeName: msg.TypeName,
	}

	job.Job, err = wingman.DeserializeJob(job.TypeName, msg.Payload)
	if err != nil {
		return wingman.InternalJob{}, err
	}

	return job, nil
}
