package native

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/pulchre/wingman"
	pb "github.com/pulchre/wingman/grpc"
	"github.com/pulchre/wingman/mock"
	"google.golang.org/grpc"
)

func init() {
	// Call signal.Notify starts a goroutine which cannot be shut down
	// (nor can the program deadlock). Therefore we start it here so when
	// we compare the number of goroutines in tests we won't be off by one.
	c := make(chan os.Signal)
	signal.Notify(c)
	signal.Reset()
	close(c)
}

func TestSubprocessStart(t *testing.T) {
	table := []struct {
		port string
		err1 error
		err2 error
	}{
		{strconv.FormatInt(DefaultPort, 10), nil, nil},
		{"34567", nil, fmt.Errorf(`rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing dial tcp [::1]:34567: connect: connection refused"`)},
		{"notaport", fmt.Errorf(`Unable to parse port: strconv.Atoi: parsing "notaport": invalid syntax`), nil},
	}

	var sWg sync.WaitGroup
	var cWg sync.WaitGroup

	for _, c := range table {
		func() {
			// This seems to help prevent circumstances where there
			// are more goroutines at the start than the end.
			time.Sleep(10 * time.Millisecond)

			goroutines := runtime.NumGoroutine()
			defer func() {
				if err := recover(); err != nil {
					panic(err)
				}

				// This seems to help prevent circumstances
				// where there are more goroutines at the end
				// than the start.
				time.Sleep(10 * time.Millisecond)
				now := runtime.NumGoroutine()

				if goroutines != now {
					pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
					t.Fatalf("Started with %d goroutines. Ended with: %d", goroutines, now)
				}
			}()

			var err error
			mock.TestLog.Reset()

			server := newTestServer(t)
			sWg.Add(1)
			go func() {
				defer server.CloseReady()
				defer sWg.Done()

				server.Start()
			}()

			// Wait for the server to begin listening
			<-server.Ready()

			defer func() {
				if err := recover(); err != nil {
					panic(err)
				}
				sWg.Wait()
			}()
			defer func() {
				server.ForceClose()
			}()
			defer func() {
				if err := recover(); err != nil {
					panic(err)
				}
				cWg.Wait()
			}()

			os.Setenv("WINGMAN_NATIVE_PORT", c.port)

			s, err := NewSubprocess()
			CheckError(c.err1, err, t)
			if err != nil {
				return
			}

			cWg.Add(1)
			go func() {
				defer server.ForceClose()
				defer cWg.Done()

				err = s.Start()
				CheckError(c.err2, err, t)
			}()

			// Wait for the subprocess to connect
			<-server.Ready()

			if server.stream == nil {
				return
			}

			job := mock.NewWrappedJob()

			server.SendJob(job)

			cWg.Add(1)
			go func() {
				defer cWg.Done()

				time.Sleep(10 * time.Millisecond)
				server.Heartbeat()
				time.Sleep(10 * time.Millisecond)
				server.Shutdown()
			}()
		}()
	}
}

type TestServer struct {
	pb.UnimplementedProcessorServer

	l      sync.Mutex
	server *grpc.Server
	stream pb.Processor_InitializeServer
	t      *testing.T

	readyChan chan struct{}
}

func newTestServer(t *testing.T) *TestServer {
	return &TestServer{
		t:         t,
		readyChan: make(chan struct{}),
	}
}

func (s *TestServer) Start() {
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", DefaultHost, DefaultPort))
	if err != nil {
		panic(err)
	}

	s.server = grpc.NewServer()
	pb.RegisterProcessorServer(s.server, s)

	s.readyChan <- struct{}{}
	err = s.server.Serve(lis)
	if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		panic(err)
	}
}

func (s *TestServer) Initialize(stream pb.Processor_InitializeServer) error {
	in, err := stream.Recv()
	if err != nil {
		panic(err)
	}

	if in.GetType() != pb.Type_CONNECT {
		s.t.Fatalf("Expected first message to be connect. Got: %v", in.Job.GetTypeName())
		return nil
	}

	s.l.Lock()
	s.stream = stream
	s.l.Unlock()

	s.readyChan <- struct{}{}

	for {
		in, err = stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		switch in.GetType() {
		case pb.Type_RESULT:
		case pb.Type_DONE:
			err := stream.Send(&pb.Message{
				Type: pb.Type_DONE,
			})
			if err != nil {
				panic(err)
			}
		default:
			s.t.Fatalf("Received unexpected message type: %s", in.Job.GetTypeName())
		}
	}
}

func (s *TestServer) Heartbeat() {
	s.l.Lock()
	defer s.l.Unlock()

	if s.stream == nil {
		return
	}

	s.stream.Send(&pb.Message{
		Type: pb.Type_HEARTBEAT,
	})
}

func (s *TestServer) Shutdown() {
	defer s.ForceClose()

	s.l.Lock()
	defer s.l.Unlock()

	if s.stream == nil {
		return
	}

	s.stream.Send(&pb.Message{
		Type: pb.Type_SHUTDOWN,
	})

	time.Sleep(10 * time.Millisecond)
}

func (s *TestServer) ForceClose() {
	defer s.CloseReady()

	s.l.Lock()
	defer s.l.Unlock()

	if s.server == nil {
		return
	}

	s.server.Stop()

	s.server = nil
}

func (s *TestServer) SendJob(job wingman.InternalJob) {
	s.l.Lock()
	defer s.l.Unlock()

	if s.server == nil {
		return
	}

	id, err := job.ID.MarshalBinary()
	if err != nil {
		panic(err)
	}

	payload, err := json.Marshal(job.Job)
	if err != nil {
		panic(err)
	}

	s.stream.Send(&pb.Message{
		Type: pb.Type_JOB,
		Job: &pb.Job{
			ID:       id,
			TypeName: job.TypeName,
			Payload:  payload,
		},
	})
}

func (s *TestServer) Ready() <-chan struct{} {
	return s.readyChan
}

func (s *TestServer) CloseReady() {
	s.l.Lock()
	defer s.l.Unlock()

	if s.readyChan == nil {
		return
	}

	close(s.readyChan)
	s.readyChan = nil
}
