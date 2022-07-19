package native

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/pulchre/wingman"
	pb "github.com/pulchre/wingman/grpc"
	"google.golang.org/grpc"
)

func GetBinPath() string {
	return bin
}

func SetBinPath(binPath string) {
	bin = binPath
}

type TestServer struct {
	pb.UnimplementedProcessorServer

	l      sync.Mutex
	server *grpc.Server
	stream pb.Processor_InitializeServer
	t      *testing.T

	syncChan chan struct{}
	doneChan chan struct{}
}

func newTestServer(t *testing.T) *TestServer {
	return &TestServer{
		t:        t,
		syncChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
}

func (s *TestServer) Start() {
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", DefaultBind, DefaultPort))
	if err != nil {
		panic(err)
	}

	s.server = grpc.NewServer()
	pb.RegisterProcessorServer(s.server, s)

	s.syncChan <- struct{}{}
	err = s.server.Serve(lis)
	if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		panic(err)
	}

	s.l.Lock()
	defer s.l.Unlock()

	close(s.syncChan)
	s.doneChan <- struct{}{}
	close(s.doneChan)
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

	if s.stream != nil {
		s.t.Fatal("TestServer only supports one client")
	}

	s.stream = stream
	s.l.Unlock()

	s.syncChan <- struct{}{}

	for {
		in, err = stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}

		switch in.GetType() {
		case pb.Type_RESULT:
			s.syncChan <- struct{}{}
		default:
			s.t.Fatalf("Received unexpected message type: %s", in.Job.GetTypeName())
		}
	}
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

	s.server.GracefulStop()
}

func (s *TestServer) ForceClose() {
	s.l.Lock()
	defer s.l.Unlock()

	if s.server == nil {
		return
	}

	s.server.Stop()

	s.server = nil
	s.stream = nil
}

func (s *TestServer) SendJob(job *wingman.InternalJob) {
	s.l.Lock()
	defer s.l.Unlock()

	if s.server == nil || s.stream == nil {
		return
	}

	payload, err := json.Marshal(job.Job)
	if err != nil {
		panic(err)
	}

	s.stream.Send(&pb.Message{
		Type: pb.Type_JOB,
		Job: &pb.Job{
			ID:       job.ID,
			TypeName: job.TypeName,
			Payload:  payload,
		},
	})
}

func (s *TestServer) Sync() <-chan struct{} { return s.syncChan }
func (s *TestServer) Done() <-chan struct{} { return s.doneChan }
