package native

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/google/uuid"
	pb "github.com/pulchre/wingman/grpc"
	"google.golang.org/grpc"
)

const DefaultHost = "localhost"
const DefaultPort = 11456

var ProcessorServer *Server

var processors = make(map[uuid.UUID]*Processor)
var processorsMu sync.Mutex

type Server struct {
	pb.UnimplementedProcessorServer
	server *grpc.Server
	opts   []grpc.ServerOption

	err   error
	errMu sync.Mutex
}

func NewServer(opts ...grpc.ServerOption) *Server {
	return &Server{opts: opts}
}

func (s *Server) Start(host string, port int) error {
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return err
	}

	s.server = grpc.NewServer(s.opts...)
	pb.RegisterProcessorServer(s.server, s)
	go func() {
		err := s.server.Serve(lis)
		s.errMu.Lock()
		defer s.errMu.Unlock()

		s.err = err
	}()

	return nil
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

	id, err := uuid.FromBytes(in.GetProcessorID())
	if err != nil {
		return err
	}

	processorsMu.Lock()
	if proc, ok := processors[id]; ok {
		processorsMu.Unlock()
		return proc.handleStream(stream)
	} else {
		processorsMu.Unlock()
		return fmt.Errorf("Processor with ID %v not registered", id)
	}
}

func (s *Server) Error() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()

	return s.err
}

func initServer() error {
	ProcessorServer = NewServer()

	return ProcessorServer.Start(DefaultHost, DefaultPort)
}
