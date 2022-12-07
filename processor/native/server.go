package native

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/pulchre/wingman"
	pb "github.com/pulchre/wingman/grpc"
	"google.golang.org/grpc"
)

const PortEnvironmentName = "WINGMAN_NATIVE_PORT"

var ErrorNoAvailableSubprocessor = errors.New("No available subprocessor")
var ErrorShuttingDown = errors.New("Server is shutting down")

type Server struct {
	pb.UnimplementedProcessorServer
	server *grpc.Server
	opts   ProcessorOptions
	wg     wingman.WaitGroup

	resultsChan chan wingman.ResultMessage
	doneChan    chan struct{}

	capacityCond  *sync.Cond
	totalCapacity int
	shuttingDown  bool
	working       map[string]workingJob

	subprocesses    map[int]*subprocess
	newSubprocesses map[int]*subprocess
}

type workingJob struct {
	ID        string
	PID       int
	StartTime time.Time
}

func init() {
	wingman.NewProcessor = NewServer
}

func NewServer(opts interface{}) (wingman.Processor, error) {
	server := newServer(opts.(ProcessorOptions))
	return server, nil
}

func (s *Server) Start() error {
	err := s.startGRPCServer()
	if err != nil {
		return err
	}

	err = s.startSubprocesses()
	if err != nil {
		s.server.Stop()
		return err
	}

	return nil
}

func (s *Server) Close() {
	var err error

	s.capacityCond.L.Lock()
	s.shuttingDown = true

	for _, subproc := range s.subprocesses {
		err = subproc.sendShutdown()
		if err != nil {
			wingman.Log.Err(err).Msg("Failed to send shutdown to subprocessor with error")
		}

		s.wg.Add(1)
		go func(subproc *subprocess) {
			defer s.wg.Done()

			<-subproc.Done()
		}(subproc)
	}
	s.capacityCond.L.Unlock()

	s.server.GracefulStop()
	s.wg.Wait()
	close(s.resultsChan)
	close(s.doneChan)
}

func (s *Server) ForceClose() {
	s.server.Stop()
	s.wg.Clear()
}

func (s *Server) Wait() bool {
	s.capacityCond.L.Lock()
	defer s.capacityCond.L.Unlock()

	if len(s.working) < s.totalCapacity {
		return true
	}

	s.capacityCond.Wait()

	return len(s.working) < s.totalCapacity
}

func (s *Server) Cancel() {
	s.capacityCond.Signal()
}

func (s *Server) Working() int {
	s.capacityCond.L.Lock()
	defer s.capacityCond.L.Unlock()

	return len(s.working)
}

func (s *Server) SendJob(job *wingman.InternalJob) error {
	for _, subproc := range s.subprocesses {
		subproc.workingMu.Lock()

		if subproc.working < s.opts.Concurrency {
			err := subproc.sendJob(job)
			if err != nil {
				subproc.workingMu.Unlock()
				return err
			}

			s.capacityCond.L.Lock()

			subproc.working++
			s.working[job.ID] = workingJob{
				ID:        job.ID,
				PID:       subproc.Pid(),
				StartTime: time.Now(),
			}

			s.capacityCond.L.Unlock()
			subproc.workingMu.Unlock()

			return err
		}

		subproc.workingMu.Unlock()
	}

	return ErrorNoAvailableSubprocessor
}

func (s *Server) Initialize(stream pb.Processor_InitializeServer) error {
	in, err := stream.Recv()
	if err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}

	if in.GetType() != pb.Type_CONNECT {
		return errors.New("First message must be CONNECT")
	}

	s.capacityCond.L.Lock()
	proc, ok := s.newSubprocesses[int(in.PID)]

	if ok {
		proc.setStream(stream)
		delete(s.newSubprocesses, proc.Pid())

		if s.shuttingDown {
			err := proc.cmd.Process.Kill()
			if err != nil {
				wingman.Log.Err(err).Send()
			}

			s.capacityCond.L.Unlock()
			return nil
		}

		s.subprocesses[proc.Pid()] = proc
		s.totalCapacity += s.opts.Concurrency

		s.capacityCond.Signal()
		s.capacityCond.L.Unlock()

		err = proc.handleStream()
		if err != nil {
			wingman.Log.Err(err).Send()
		}

		s.capacityCond.L.Lock()
		delete(s.subprocesses, int(in.PID))
		s.totalCapacity -= s.opts.Concurrency
		s.capacityCond.L.Unlock()

		err2 := s.startSubprocess()
		if err2 != nil && err2 != ErrorShuttingDown {
			wingman.Log.Err(err2).Msg("Failed to start replacement subprocess")
		}

		return err
	} else {
		s.capacityCond.L.Unlock()
		return fmt.Errorf("Process with pid=%v is unknown", in.PID)
	}
}

func (s *Server) Results() <-chan wingman.ResultMessage { return s.resultsChan }
func (s *Server) Done() <-chan struct{}                 { return s.doneChan }

func newServer(opts ProcessorOptions) *Server {
	if opts.Processes < 1 {
		opts.Processes = 1
	}

	if opts.Concurrency < 1 {
		opts.Concurrency = 1
	}

	return &Server{
		opts:            opts,
		capacityCond:    sync.NewCond(&sync.Mutex{}),
		resultsChan:     make(chan wingman.ResultMessage, 64),
		doneChan:        make(chan struct{}),
		subprocesses:    make(map[int]*subprocess),
		newSubprocesses: make(map[int]*subprocess),
		working:         make(map[string]workingJob),
	}
}

func (s *Server) startGRPCServer() error {
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.opts.Bind, s.opts.Port))
	if err != nil {
		return err
	}

	s.server = grpc.NewServer(s.opts.GRPCOptions...)
	pb.RegisterProcessorServer(s.server, s)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		err := s.server.Serve(lis)
		if err != nil {
			wingman.Log.Err(err).Msg("GRPC server returned an error")
		}
	}()

	return nil
}

func (s *Server) startSubprocesses() error {
	for i := 0; i < s.opts.Processes; i++ {
		err := s.startSubprocess()
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) startSubprocess() error {
	s.capacityCond.L.Lock()
	defer s.capacityCond.L.Unlock()

	if s.shuttingDown {
		return ErrorShuttingDown
	}

	subprocess := newSubprocess(s)
	err := subprocess.start()
	if err != nil {
		return err
	}

	s.newSubprocesses[subprocess.Pid()] = subprocess

	return nil
}

func (s *Server) finishJob(job *wingman.InternalJob, err error) {
	s.capacityCond.L.Lock()
	defer s.capacityCond.L.Unlock()

	delete(s.working, job.ID)
	s.capacityCond.Signal()

	s.resultsChan <- wingman.ResultMessage{
		Job:   job,
		Error: err,
	}
}
