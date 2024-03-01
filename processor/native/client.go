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

	"github.com/pulchre/wingman"
	pb "github.com/pulchre/wingman/grpc"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
)

type Client struct {
	port int
	pid  int

	clientStream   pb.Processor_InitializeClient
	clientStreamMu sync.Mutex

	signalChan chan os.Signal

	wg       sync.WaitGroup
	signalWg sync.WaitGroup
}

func NewClient() (*Client, error) {
	c := &Client{
		pid:  os.Getpid(),
		port: DefaultPort,
	}

	portEnv := os.Getenv(PortEnvironmentName)

	if portEnv != "" {
		port, err := strconv.Atoi(portEnv)
		if err != nil {
			return nil, fmt.Errorf("Unable to parse port: %v", err)
		}

		c.port = port
	}

	return c, nil
}

func (c *Client) Start() error {
	wingman.Log.Info().Msg("Client process connecting")

	// TODO: Add timeout
	conn, err := grpc.Dial(fmt.Sprintf("localhost:%d", c.port), grpc.WithInsecure())
	if err != nil {
		return err
	}
	defer conn.Close()
	connClient := pb.NewProcessorClient(conn)

	ctx := context.Background()
	c.clientStreamMu.Lock()
	c.clientStream, err = connClient.Initialize(ctx)
	if err != nil {
		return err
	}

	c.signalChan = make(chan os.Signal, 32)
	signal.Notify(c.signalChan, unix.SIGTERM, unix.SIGINT)
	c.signalWg.Add(1)
	go c.signalWatcher()

	msg := &pb.Message{
		Type: pb.Type_CONNECT,
		PID:  int32(c.pid),
	}

	err = c.clientStream.Send(msg)
	if err != nil {
		return err
	}
	c.clientStreamMu.Unlock()

	for {
		in, err := c.clientStream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		switch in.Type {
		case pb.Type_JOB:
			c.wg.Add(1)
			go c.handleJob(ctx, in)
		case pb.Type_SHUTDOWN:
			wingman.Log.Info().Msg("Subprocess received shutdown message")

			c.wg.Wait()
			c.closeConnection()
			c.shutdownSignalWatcher()
			c.signalWg.Wait()
		default:
		}
	}
}

func (c *Client) signalWatcher() {
	defer c.signalWg.Done()

	for sig := range c.signalChan {
		wingman.Log.Info().Str("signal", sig.String()).Msg(`Subprocessor received ignoring`)
	}
}

func (c *Client) shutdownSignalWatcher() {
	signal.Reset()
	close(c.signalChan)
	c.signalWg.Wait()
}

func (c *Client) closeConnection() {
	c.clientStreamMu.Lock()
	defer c.clientStreamMu.Unlock()

	err := c.clientStream.CloseSend()
	if err != nil {
		wingman.Log.Err(err).Msg("Error closing client send stream")
	}
}

func (c *Client) handleJob(ctx context.Context, in *pb.Message) {
	defer c.wg.Done()

	var err error

	id := in.Job.ID

	job := wingman.InternalJob{
		ID:       in.Job.ID,
		TypeName: in.Job.TypeName,
		LockID:   wingman.LockID(in.Job.LockID),
	}

	job.Job, err = wingman.DeserializeJob(job.TypeName, in.Job.Payload)
	if err != nil {
		wingman.Log.Err(err).Str("job_id", job.ID).Msg("Failed to deserialize job")
	}

	defer func() {
		var errMsg *pb.Error
		if err != nil {
			errMsg = &pb.Error{
				Message: err.Error(),
			}
		}

		result := &pb.Message{
			Type:  pb.Type_RESULT,
			Job:   in.Job,
			Error: errMsg,
		}

		c.clientStreamMu.Lock()
		senderr := c.clientStream.SendMsg(result)
		if senderr != nil {
			wingman.Log.Err(senderr).Str("job_err", err.Error()).Str("job_id", id).Msg("Failed to send job result")
		}
		c.clientStreamMu.Unlock()
	}()

	defer func() {
		if recoveredErr := recover(); recoveredErr != nil {
			err = wingman.NewError(recoveredErr)
		}
	}()

	err = job.Job.Handle(context.WithValue(ctx, wingman.ContextJobIDKey, in.Job.ID))
}
