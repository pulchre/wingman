package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pulchre/wingman"
	pb "github.com/pulchre/wingman/grpc"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
)

var bin, binErr = os.Executable()

func init() {
	if binErr != nil {
		panic(fmt.Sprintf("Could not find binary path %v", binErr))
	}

	binOverride := strings.TrimSpace(os.Getenv("WINGMAN_NATIVE_BIN"))
	if binOverride != "" {
		bin = binOverride
	}

	wingman.NewProcessor = NewProcessor
}

type Processor struct {
	id            uuid.UUID
	working       int
	cmd           *exec.Cmd
	lastHeartbeat time.Time

	serverStream   pb.Processor_InitializeServer
	serverStreamMu sync.Mutex

	clientStream   pb.Processor_InitializeClient
	clientStreamMu sync.Mutex

	status     wingman.ProcessorStatus
	statusMu   sync.Mutex
	statusChan chan wingman.ProcessorStatus

	resultChan chan wingman.ResultMessage
	wg         wingman.WaitGroup

	signalChan chan os.Signal
}

func NewProcessor() wingman.Processor {
	return NewSubprocessor(uuid.New())
}

func NewSubprocessor(id uuid.UUID) *Processor {
	return &Processor{
		id:         id,
		resultChan: make(chan wingman.ResultMessage),
		statusChan: make(chan wingman.ProcessorStatus, 8),
	}
}

func (p *Processor) Id() string {
	return p.id.String()
}

func (p *Processor) Pid() int {
	if p.cmd == nil || p.cmd.Process == nil {
		return -1
	}

	return p.cmd.Process.Pid
}

func (p *Processor) Start() error {
	var err error

	p.setStatus(wingman.ProcessorStatusStarting)

	processorsMu.Lock()
	processors[p.id] = p
	processorsMu.Unlock()

	if ProcessorServer == nil {
		err = initServer()
		if err != nil {
			return err
		}
	}

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
	p.cmd.Env[1] = fmt.Sprintf("WINGMAN_NATIVE_SUBPROCESS_ID=%s", p.id)
	for i, e := range os.Environ() {
		p.cmd.Env[i+2] = e
	}

	err = p.cmd.Start()
	if err != nil {
		return err
	}

	wingman.Log.Printf("Starting Subprocess pid=%d", p.cmd.Process.Pid)

	p.wg.Add(1)
	go func() {
		defer p.setStatus(wingman.ProcessorStatusDead)
		p.cmd.Wait()

		processorsMu.Lock()
		defer processorsMu.Unlock()
		delete(processors, p.id)
		p.wg.Done()
	}()

	return nil
}

func (p *Processor) StartSubprocess() error {
	wingman.Log.Printf("Subprocesses connecting id=%v", p.id)

	conn, err := grpc.Dial("localhost:11456", grpc.WithInsecure())
	if err != nil {
		return err
	}
	defer conn.Close()
	connClient := pb.NewProcessorClient(conn)

	ctx := context.Background()
	p.clientStreamMu.Lock()
	p.clientStream, err = connClient.Initialize(ctx)
	p.clientStreamMu.Unlock()
	if err != nil {
		return err
	}

	p.signalChan = make(chan os.Signal, 32)
	signal.Notify(p.signalChan, unix.SIGTERM, unix.SIGINT)
	go p.signalWatcher()

	binaryId, err := p.id.MarshalBinary()
	if err != nil {
		return err
	}

	msg := &pb.Message{
		Type:        pb.Type_CONNECT,
		ProcessorID: binaryId,
	}

	err = p.clientStream.Send(msg)
	if err != nil {
		return err
	}

	for {
		in, err := p.clientStream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		switch in.GetType() {
		case pb.Type_HEARTBEAT:
			p.lastHeartbeat = time.Now()
		case pb.Type_JOB:
			id, err := uuid.FromBytes(in.GetJob().GetID())
			if err != nil {
				wingman.Log.Print("Failed to deserialize job id")
			}

			job := wingman.InternalJob{
				ID:       id,
				TypeName: in.GetJob().TypeName,
			}

			job.Job, err = wingman.DeserializeJob(job.TypeName, in.GetJob().Payload)
			if err != nil {
				wingman.Log.Printf("Failed to deserialize job %v", job.ID)
			}

			handleJob(ctx, job, &err)
			var errMsg *pb.Error
			if err != nil {
				errMsg = &pb.Error{
					Message: err.Error(),
				}
			}

			result := &pb.Message{
				Type:  pb.Type_RESULT,
				Job:   in.GetJob(),
				Error: errMsg,
			}

			senderr := p.clientStream.SendMsg(result)
			if senderr != nil {
				wingman.Log.Print("Failed to send job result jobid=%v joberr=%v err=%v",
					id, err, senderr)
			}
		case pb.Type_SHUTDOWN:
			wingman.Log.Printf("Subprocess received shutdown message id=%v", p.id)
			return nil
		default:
		}
	}
}

func (p *Processor) SendJob(job wingman.InternalJob) error {
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

	msg := &pb.Message{
		Type: pb.Type_JOB,
		Job: &pb.Job{
			ID:       id,
			TypeName: job.TypeName,
			Payload:  payload,
		},
	}

	wingman.Log.Printf("Sending job id=%s", job.ID)
	err = p.serverStream.Send(msg)
	if err != nil {
		return err
	}

	p.setStatus(wingman.ProcessorStatusWorking)
	return nil
}

func (p *Processor) Results() <-chan wingman.ResultMessage        { return p.resultChan }
func (p *Processor) StatusChange() <-chan wingman.ProcessorStatus { return p.statusChan }

func (p *Processor) Stop() error {
	p.setStatus(wingman.ProcessorStatusStopping)

	err := p.serverStream.Send(&pb.Message{
		Type: pb.Type_SHUTDOWN,
	})
	if err != nil {
		wingman.Log.Print(`Failed to send shutdown signal to id=%v err="%v"`, p.id, err)
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
	// handle that properly in the subprocess be fore we can fix this as
	// the process will die and we will be waiting for it to finish
	// working.
	p.wg.Wait()

	if p.signalChan != nil {
		close(p.signalChan)
		signal.Reset()
	}

	close(p.resultChan)
	return nil
}

func (p *Processor) Kill() error {
	err := p.cmd.Process.Kill()
	if err == os.ErrProcessDone {
		err = nil
	}

	p.wg.Clear()

	return err
}

func handleJob(ctx context.Context, job wingman.InternalJob, err *error) {
	defer func() {
		if recoveredErr := recover(); recoveredErr != nil {
			*err = wingman.NewError(recoveredErr)
		}
	}()

	*err = job.Job.Handle(ctx)
}

func (p *Processor) handleStream(stream pb.Processor_InitializeServer) error {
	p.wg.Add(1)
	defer p.wg.Done()

	wingman.Log.Printf("Subprocess connected id=%v", p.id)

	p.serverStreamMu.Lock()
	p.serverStream = stream
	p.serverStreamMu.Unlock()

	p.setStatus(wingman.ProcessorStatusIdle)
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
			parsedJob, err := deserializeJob(in.GetJob())
			if err != nil {
				wingman.Log.Printf("Error parsing job in response message: %v, %v", err, in.GetJob())
			} else {
				var processErr error

				if in.GetError() != nil {
					processErr = errors.New(in.GetError().Message)
				}

				p.resultChan <- wingman.ResultMessage{
					Job:   parsedJob,
					Error: processErr,
				}
			}

			p.setStatus(wingman.ProcessorStatusIdle)
		default:
			wingman.Log.Print(in.GetType().String())
		}
	}
}

func (p *Processor) setStatus(s wingman.ProcessorStatus) {
	p.statusMu.Lock()
	defer p.statusMu.Unlock()

	p.status = s
	p.statusChan <- p.status

	if s == wingman.ProcessorStatusDead {
		close(p.statusChan)
	}
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

func (p *Processor) signalWatcher() {
	for sig := range p.signalChan {
		wingman.Log.Printf(`Subprocessor id=%v received signal="%v" ignoring`, p.id, sig)
	}
}
