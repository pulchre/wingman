package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/google/uuid"
	"github.com/pulchre/wingman"
)

const successfulJobsCount = "wingman:successful_total"
const failedJobsCount = "wingman:failed_total"
const stagingQueueFmt = "wingman:staging:%v"
const processorFmt = "wingman:processing:%v"
const failedFmt = "wingman:failed:%v"

type response struct {
	Job   wingman.InternalJob
	Raw   []byte
	Error error
}

type Backend struct {
	*redis.Pool

	timeout float32
}

type clientCanceller struct {
	conn redis.Conn
	cncl redis.Conn

	clientID int
}

var WrongOptionsTypeError = errors.New("Must be type redis.Options")

type Options struct {
	MaxIdle         int
	MaxActive       int
	IdleTimeout     time.Duration
	MaxConnLifetime time.Duration
	Dial            func() (redis.Conn, error)

	BlockingTimeout float32
}

func Init(options Options) (*Backend, error) {
	var backend *Backend

	backend = &Backend{&redis.Pool{
		MaxIdle:         options.MaxIdle,
		MaxActive:       options.MaxActive,
		IdleTimeout:     options.IdleTimeout,
		MaxConnLifetime: options.MaxConnLifetime,
		Dial:            options.Dial,
	},
		options.BlockingTimeout,
	}

	_, err := backend.do("PING")
	if err != nil {
		backend.Close()
		return nil, err
	}

	return backend, err
}

func (b Backend) PushJob(job wingman.Job) error {
	intJob, err := wingman.WrapJob(job)
	if err != nil {
		return err
	}

	client, err := b.getClient()
	if err != nil {
		return err
	}
	defer client.Close()

	serializedJob, err := json.Marshal(intJob)
	if err != nil {
		return err
	}

	_, err = client.conn.Do("RPUSH", job.Queue(), string(serializedJob))

	return err
}

func (b Backend) PopAndStageJob(ctx context.Context, queue string) (*wingman.InternalJob, error) {
	client, err := b.getClient()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	stagingID, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			wingman.Log.Info().Int("redis_id", client.clientID).Msg("Unblocking redis client")
			client.cncl.Do("CLIENT", "UNBLOCK", client.clientID)
		case <-done:
			// Ensure we don't leak this goroutine when the parent
			// method exits normally.
		}
	}()

	raw, err := redis.Bytes(client.conn.Do("BLMOVE", queue, fmtStagingKey(stagingID.String()), "LEFT", "RIGHT", b.timeout))
	if err != nil {
		if err == redis.ErrNil {
			return nil, wingman.ErrCanceled
		} else {
			return nil, err
		}
	}

	job, err := wingman.InternalJobFromJSON(raw)
	if err != nil {
		return nil, err
	}

	job.StagingID = stagingID.String()

	return job, nil
}

func (b Backend) ProcessJob(stagingID string) error {
	client, err := b.getClient()
	if err != nil {
		return err
	}
	defer client.Close()

	raw, err := redis.ByteSlices(client.conn.Do("LRANGE", fmtStagingKey(stagingID), 0, 1))
	if err != nil {
		return err
	} else if len(raw) == 0 {
		return wingman.ErrorJobNotStaged
	}

	job, err := wingman.InternalJobFromJSON(raw[0])
	if err != nil {
		return wingman.ErrorJobNotStaged
	}

	_, err = client.conn.Do("LMOVE", fmtStagingKey(stagingID), fmtProcessingKey(job.ID), "LEFT", "RIGHT")
	if err != nil {
		return err
	}
	return nil
}

func (b Backend) ReenqueueStagedJob(stagingID string) error {
	client, err := b.getClient()
	if err != nil {
		return err
	}
	defer client.Close()

	raw, err := redis.ByteSlices(client.conn.Do("LRANGE", fmtStagingKey(stagingID), 0, 1))
	if err != nil {
		return err
	} else if len(raw) < 1 {
		return wingman.ErrorJobNotStaged
	}

	job, err := wingman.InternalJobFromJSON(raw[0])
	if err != nil {
		return err
	}

	_, err = client.conn.Do("LMOVE", fmtStagingKey(stagingID), job.Queue(), "LEFT", "RIGHT")
	if err != nil {
		return err
	}

	return nil
}

func (b Backend) ClearJob(jobID string) error {
	client, err := b.getClient()
	if err != nil {
		return err
	}
	defer client.Close()

	_, err = client.conn.Do("DEL", fmtProcessingKey(jobID))

	return err
}

func (b Backend) FailJob(jobID string) error {
	client, err := b.getClient()
	if err != nil {
		return err
	}
	defer client.Close()

	_, err = client.conn.Do("LMOVE", fmtProcessingKey(jobID), fmtFailedKey(jobID), "RIGHT", "RIGHT")
	return err
}

func (b Backend) StagedJobs() ([]*wingman.InternalJob, error) {
	client, err := b.getClient()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	keys, err := redis.Strings(client.conn.Do("KEYS", fmtStagingKey("*")))
	if err != nil {
		return nil, err
	}

	jobs := make([]*wingman.InternalJob, len(keys))

	for i, key := range keys {
		raw, err := redis.ByteSlices(client.conn.Do("LRANGE", key, 0, 0))
		if err != nil {
			return nil, err
		}

		job, err := wingman.InternalJobFromJSON(raw[0])
		if err != nil {
			return nil, wingman.NewBackendIDError(key, err)
		}

		split := strings.Split(key, ":")
		job.StagingID = split[2]

		jobs[i] = job
	}

	return jobs, nil
}

func (b Backend) ClearStagedJob(stagingID string) error {
	client, err := b.getClient()
	if err != nil {
		return err
	}
	defer client.Close()

	_, err = client.conn.Do("DEL", fmtStagingKey(stagingID))

	return err
}

func (b Backend) Peek(queue string) (*wingman.InternalJob, error) {
	client, err := b.getClient()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	raw, err := redis.ByteSlices(client.conn.Do("LRANGE", queue, 0, 0))
	if err != nil {
		return nil, err
	}

	if len(raw) > 0 {
		return wingman.InternalJobFromJSON(raw[0])
	} else {
		return nil, nil
	}
}

func (b Backend) Size(queue string) int {
	client, err := b.getClient()
	if err != nil {
		return 0
	}
	defer client.Close()

	size, _ := redis.Int(client.conn.Do("LLEN", queue))

	return int(size)
}

func (b Backend) SuccessfulJobs() int {
	client, err := b.getClient()
	if err != nil {
		return 0
	}
	defer client.Close()

	count, _ := redis.Int(client.conn.Do("GET", successfulJobsCount))

	return count
}

func (b Backend) IncSuccessfulJobs() {
	client, err := b.getClient()
	if err != nil {
		return
	}
	defer client.Close()

	client.conn.Do("INCR", successfulJobsCount)
}

func (b Backend) FailedJobs() int {
	client, err := b.getClient()
	if err != nil {
		return 0
	}
	defer client.Close()

	count, _ := redis.Int(client.conn.Do("GET", failedJobsCount))

	return count
}

func (b Backend) IncFailedJobs() {
	client, err := b.getClient()
	if err != nil {
		return
	}
	defer client.Close()

	client.conn.Do("INCR", failedJobsCount)
}

// Release any resources used
func (b Backend) Close() error {
	var err error

	if b.Pool != nil {
		err = b.Pool.Close()
		b.Pool = nil
	}

	return err
}

func (b Backend) do(cmd string, args ...interface{}) (interface{}, error) {
	conn := b.Pool.Get()
	defer conn.Close()

	return conn.Do(cmd, args...)
}

func (b Backend) getClient() (clientCanceller, error) {
	var err error

	c := clientCanceller{}
	c.conn = b.Pool.Get()
	c.clientID, err = redis.Int(c.conn.Do("CLIENT", "ID"))
	if err != nil {
		c.conn.Close()
		return c, err
	}

	c.cncl = b.Pool.Get()
	_, err = c.cncl.Do("CLIENT", "ID")
	if err != nil {
		c.conn.Close()
		c.cncl.Close()
		return c, err
	}

	return c, nil
}

func (c clientCanceller) Close() error {
	err := c.conn.Close()
	err2 := c.cncl.Close()

	c.conn = nil
	c.cncl = nil

	if err != nil {
		return err
	} else if err2 != nil {
		return err2
	}

	return nil
}

func fmtStagingKey(id string) string {
	return fmt.Sprintf(stagingQueueFmt, id)
}

func fmtProcessingKey(id string) string {
	return fmt.Sprintf(processorFmt, id)
}

func fmtFailedKey(id string) string {
	return fmt.Sprintf(failedFmt, id)
}
