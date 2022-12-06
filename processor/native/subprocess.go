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

	"github.com/pulchre/wingman"
	pb "github.com/pulchre/wingman/grpc"
)

const ClientEnvironmentName = "WINGMAN_NATIVE_CLIENT"

var ErrorSubprocessShutdownCalled = errors.New("Shutdown called on subprocessor")

var bin, binErr = os.Executable()

type subprocess struct {
	cmd      *exec.Cmd
	doneChan chan struct{}
	errChan  chan error

	serverStream   pb.Processor_InitializeServer
	serverStreamMu sync.Mutex

	server *Server

	working   int
	workingMu sync.Mutex

	startupComplete bool
	startupCond     sync.Cond

	wg wingman.WaitGroup
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

func newSubprocess(server *Server) *subprocess {
	return &subprocess{
		server:      server,
		doneChan:    make(chan struct{}),
		errChan:     make(chan error, 1),
		startupCond: *sync.NewCond(&sync.Mutex{}),
	}
}

func (p *subprocess) start() error {
	var err error

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
	p.cmd.Env[0] = fmt.Sprintf("%s=1", ClientEnvironmentName)
	p.cmd.Env[1] = fmt.Sprintf("%s=%d", PortEnvironmentName, p.server.opts.Port)
	for i, e := range os.Environ() {
		p.cmd.Env[i+2] = e
	}

	err = p.cmd.Start()
	if err != nil {
		return err
	}
	wingman.Log.Info().
		Int("subprocess_pid", p.Pid()).
		Msg("Starting subprocess")

	p.wg.Add(1)
	go func() {
		p.cmd.Wait()
		wingman.Log.Info().
			Int("subprocess_pid", p.Pid()).
			Msg("Dead subprocess")

		if !p.cmd.ProcessState.Success() {
			p.errChan <- errors.New("Processed exited with failure")
		}

		close(p.errChan)

		p.wg.Done()

		close(p.doneChan)
	}()

	return nil
}

func (p *subprocess) sendJob(job *wingman.InternalJob) error {
	p.serverStreamMu.Lock()
	defer p.serverStreamMu.Unlock()

	if p.serverStream == nil {
		return fmt.Errorf("Processor not connected")
	}

	payload, err := json.Marshal(job.Job)
	if err != nil {
		return err
	}

	msg := &pb.Message{
		Type: pb.Type_JOB,
		Job: &pb.Job{
			ID:       job.ID,
			TypeName: job.TypeName,
			Payload:  payload,
		},
	}

	wingman.Log.Info().
		Str("job_id", job.ID).
		Int("subprocess_pid", p.Pid()).
		Msg("Sending job to subprocess")
	err = p.serverStream.Send(msg)
	if err != nil {
		return err
	}

	return nil
}

func (s *subprocess) handleStream(stream pb.Processor_InitializeServer) error {
	defer func() {
		s.serverStreamMu.Lock()
		s.serverStream = nil
		s.serverStreamMu.Unlock()
	}()

	wingman.Log.Info().
		Int("subprocess_pid", s.Pid()).
		Msg("Subprocess connected")

	s.serverStreamMu.Lock()
	s.serverStream = stream
	s.serverStreamMu.Unlock()

	s.startupCond.L.Lock()
	s.startupComplete = true
	s.startupCond.L.Unlock()
	s.startupCond.Broadcast()

	for {
		in, err := s.serverStream.Recv()
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}

		switch in.Type {
		case pb.Type_RESULT:
			parsedJob, err := deserializeJob(in.Job)
			if err != nil {
				wingman.Log.Err(err).
					Str("job", in.Job.String()).
					Int("subprocess_pid", s.Pid()).
					Msg("Error parsing job in response message")
				continue
			}

			var processErr error
			if in.Error != nil {
				processErr = errors.New(in.Error.Message)
			}

			s.workingMu.Lock()
			s.working -= 1
			s.workingMu.Unlock()

			s.server.finishJob(parsedJob, processErr)
		default:
			wingman.Log.Info().
				Str("type", in.Type.String()).
				Msg("Received unhandled message type")
		}
	}
}

func (s *subprocess) sendShutdown() error {
	s.startupCond.L.Lock()
	for !s.startupComplete {
		s.startupCond.Wait()
	}
	s.startupCond.L.Unlock()

	s.serverStreamMu.Lock()
	defer s.serverStreamMu.Unlock()

	if s.serverStream == nil {
		return nil
	}

	return s.serverStream.Send(&pb.Message{
		Type: pb.Type_SHUTDOWN,
	})
}

func (p *subprocess) Pid() int {
	if p.cmd == nil || p.cmd.Process == nil {
		return -1
	}

	return p.cmd.Process.Pid
}

func (p *subprocess) Done() <-chan struct{} { return p.doneChan }
func (p *subprocess) Error() <-chan error   { return p.errChan }

// TODO: This should be moved to the GRPC package
func deserializeJob(msg *pb.Job) (*wingman.InternalJob, error) {
	var err error

	job := wingman.InternalJob{
		ID:       msg.ID,
		TypeName: msg.TypeName,
	}

	job.Job, err = wingman.DeserializeJob(job.TypeName, msg.Payload)
	if err != nil {
		return &wingman.InternalJob{}, err
	}

	return &job, nil
}
