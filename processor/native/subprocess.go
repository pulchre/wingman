package native

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pulchre/wingman"
	pb "github.com/pulchre/wingman/grpc"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
)

type Subprocess struct {
	port          int
	pid           int
	lastHeartbeat time.Time

	clientStream   pb.Processor_InitializeClient
	clientStreamMu sync.Mutex

	signalChan chan os.Signal

	wg sync.WaitGroup
}

func NewSubprocess() (*Subprocess, error) {
	s := &Subprocess{
		pid:  os.Getpid(),
		port: DefaultPort,
	}

	portEnv := os.Getenv("WINGMAN_NATIVE_PORT")

	if portEnv != "" {
		port, err := strconv.Atoi(portEnv)
		if err != nil {
			return nil, fmt.Errorf("Unable to parse port: %v", err)
		}

		s.port = port
	}

	return s, nil
}

func (s *Subprocess) Start() error {
	wingman.Log.Printf("Subprocess connecting pid=%v", s.pid)

	// TODO: Add timeout
	conn, err := grpc.Dial(fmt.Sprintf("localhost:%d", s.port), grpc.WithInsecure())
	if err != nil {
		return err
	}
	defer conn.Close()
	connClient := pb.NewProcessorClient(conn)

	ctx := context.Background()
	s.clientStreamMu.Lock()
	s.clientStream, err = connClient.Initialize(ctx)
	if err != nil {
		return err
	}

	s.signalChan = make(chan os.Signal, 32)
	signal.Notify(s.signalChan, unix.SIGTERM, unix.SIGINT)
	s.wg.Add(1)
	go s.signalWatcher()

	msg := &pb.Message{
		Type:      pb.Type_CONNECT,
		ProcessID: int64(s.pid),
	}

	err = s.clientStream.Send(msg)
	if err != nil {
		return err
	}
	s.clientStreamMu.Unlock()

	for {
		in, err := s.clientStream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		switch in.GetType() {
		case pb.Type_HEARTBEAT:
			s.lastHeartbeat = time.Now()
		case pb.Type_JOB:
			id, err := uuid.FromBytes(in.GetJob().GetID())
			if err != nil {
				wingman.Log.Print("Failed to deserialize job id=%v", id)
				continue
			}

			job := wingman.InternalJob{
				ID:       id,
				TypeName: in.GetJob().TypeName,
			}

			job.Job, err = wingman.DeserializeJob(job.TypeName, in.GetJob().Payload)
			if err != nil {
				wingman.Log.Printf("Failed to deserialize job jobid=%v", job.ID)
			}

			// TODO: This needs to be done in the background so we
			// can properly handle greater than 1 concurrency.
			handleJob(ctx, job, &err)
			var errMsg *pb.Error
			if err != nil {
				errMsg = &pb.Error{
					Message: err.Error(),
				}
			}

			result := &pb.Message{
				Type:        pb.Type_RESULT,
				ProcessorID: in.GetProcessorID(),
				Job:         in.GetJob(),
				Error:       errMsg,
			}

			senderr := s.clientStream.SendMsg(result)
			if senderr != nil {
				wingman.Log.Print("Failed to send job result jobid=%v joberr=%v err=%v",
					id, err, senderr)
			}
		case pb.Type_SHUTDOWN:
			wingman.Log.Printf("Subprocess pid=%v received shutdown message", s.pid)
			close(s.signalChan)

			s.wg.Wait()

			s.clientStreamMu.Lock()
			s.clientStream.CloseSend()
			s.clientStreamMu.Unlock()

			return nil
		default:
		}
	}
}

func (s *Subprocess) signalWatcher() {
	defer s.wg.Done()

	for sig := range s.signalChan {
		wingman.Log.Printf(`Subprocessor pid=%v received signal="%v" ignoring`, s.pid, sig)
	}
}

func handleJob(ctx context.Context, job wingman.InternalJob, err *error) {
	defer func() {
		if recoveredErr := recover(); recoveredErr != nil {
			*err = wingman.NewError(recoveredErr)
		}
	}()

	*err = job.Job.Handle(ctx)
}
