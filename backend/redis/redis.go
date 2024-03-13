package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/pulchre/wingman"
	"github.com/redis/go-redis/v9"
)

const (
	successfulJobsCount = "wingman:successful_total"
	failedJobsCount     = "wingman:failed_total"
	queueFmt            = "wingman:queue:%s"
	lockKeyFmt          = "wingman:lock:%s:%d"
	processorFmt        = "wingman:processing:%v"
	failedFmt           = "wingman:failed:%v"
)

var (
	ErrCouldNotLock = errors.New("could not lock job")
	ctx             = context.Background()
)

type response struct {
	Job   wingman.InternalJob
	Error error
	Raw   []byte
}

type Backend struct {
	*redis.Client
	config Config
}

var _ wingman.Backend = &Backend{}

type Options struct {
	redis.Options
	Config
}

type Config struct {
	BlockingTimeout time.Duration
}

func Init(options Options) (*Backend, error) {
	backend := &Backend{
		redis.NewClient(&options.Options),
		options.Config,
	}

	err := backend.Client.Ping(context.Background()).Err()
	if err != nil {
		err = errors.Join(err, backend.Close())
		return nil, err
	}

	return backend, err
}

func (b Backend) PushJob(job wingman.Job) error {
	intJob, err := wingman.WrapJob(job)
	if err != nil {
		return err
	}

	return b.PushInternalJob(intJob)
}

func (b Backend) PushInternalJob(job *wingman.InternalJob) error {
	serializedJob, err := json.Marshal(job)
	if err != nil {
		return err
	}

	return b.RPush(context.Background(), fmtQueueKey(job.Queue()), serializedJob).Err()
}

func (b Backend) PopJob(ctx context.Context, queue string) (*wingman.InternalJob, error) {
	conn := b.Conn()
	clientID, err := conn.ClientID(context.Background()).Result()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			wingman.Log.Info().Msg("Unblocking Redis client")
			err := b.ClientUnblock(context.Background(), clientID).Err()
			if err != nil {
				wingman.Log.Err(err).Msg("Failed to unblock Redis client")
			}
		case <-done:
			// Ensure we don't leak this goroutine when the parent
			// method exits normally.
		}
	}()

	raw, err := conn.BLPop(ctx, b.config.BlockingTimeout, fmtQueueKey(queue)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, wingman.ErrCanceled
		} else {
			return nil, err
		}
	}

	job, err := wingman.InternalJobFromJSON([]byte(raw[1]))
	if err != nil {
		return nil, err
	}

	return job, nil
}

func (b Backend) LockJob(job *wingman.InternalJob) (wingman.LockID, error) {
	concurrency := job.Job.Concurrency()

	if job.Job.LockKey() == "" || concurrency < 1 {
		return wingman.LockNotRequired, nil
	}

	serialzedJob, err := json.Marshal(job)
	if err != nil {
		return 0, err
	}

	checkedLocks := make(map[int]any, concurrency)
	for len(checkedLocks) < concurrency {
		// We add 1 here so we can reserve 0 for
		// wingman.LockNotRequired
		lockID := rand.IntN(concurrency) + 1

		if _, ok := checkedLocks[lockID]; ok {
			continue
		}
		checkedLocks[lockID] = nil

		lockKey := fmtLockKey(job.Job.LockKey(), lockID)
		res := b.SetNX(ctx, lockKey, 1, 5*time.Minute)
		if res.Err() != nil {
			err = errors.Join(res.Err())
			wingman.Log.Err(err).Msg("Failed attempt to lock")
			continue
		}

		if !res.Val() {
			continue
		}

		return wingman.LockID(lockID), nil
	}

	err = errors.Join(b.RPush(ctx, fmtQueueKey(job.Queue()), serialzedJob).Err())
	if err != nil {
		wingman.Log.Err(err).Msg("Failed to lock or hold job")
	}

	return wingman.JobHeld, err
}

func (b Backend) ReleaseJob(job *wingman.InternalJob) error {
	concurrency := job.Job.Concurrency()
	ctx := context.Background()

	if job.Job.LockKey() == "" || concurrency < 1 || job.LockID == wingman.LockNotRequired {
		return nil
	}

	lockKey := fmtLockKey(job.Job.LockKey(), int(job.LockID))
	err := b.Del(ctx, lockKey).Err()
	if err == redis.Nil {
		return nil
	}

	return err
}

func (b Backend) ProcessJob(job *wingman.InternalJob) error {
	serialzedJob, err := json.Marshal(job)
	if err != nil {
		return err
	}

	return b.Set(ctx, fmtProcessingKey(job.ID), serialzedJob, 0).Err()
}

func (b Backend) ClearJob(jobID string) error {
	return b.Del(context.Background(), fmtProcessingKey(jobID)).Err()
}

func (b Backend) FailJob(job *wingman.InternalJob) error {
	serialzedJob, err := json.Marshal(job)
	if err != nil {
		return err
	}

	_, err = b.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, fmtProcessingKey(job.ID))
		pipe.Set(ctx, fmtFailedKey(job.ID), serialzedJob, 0)
		return nil
	})
	return err
}

func (b Backend) Peek(queue string) (*wingman.InternalJob, error) {
	raw, err := b.LRange(context.Background(), fmtQueueKey(queue), 0, 0).Result()
	if err != nil {
		return nil, err
	}

	if len(raw) > 0 {
		return wingman.InternalJobFromJSON([]byte(raw[0]))
	} else {
		return nil, nil
	}
}

func (b Backend) Size(queue string) uint64 {
	size, _ := b.LLen(context.Background(), fmtQueueKey(queue)).Uint64()
	return size
}

func (b Backend) SuccessfulJobs() uint64 {
	count, _ := b.Get(context.Background(), successfulJobsCount).Uint64()
	return count
}

func (b Backend) IncSuccessfulJobs() {
	b.Incr(context.Background(), successfulJobsCount)
}

func (b Backend) FailedJobs() uint64 {
	count, _ := b.Get(context.Background(), failedJobsCount).Uint64()
	return count
}

func (b Backend) IncFailedJobs() {
	b.Incr(context.Background(), failedJobsCount)
}

// Release any resources used
func (b *Backend) Close() (err error) {
	if b.Client != nil {
		err = b.Client.Close()
		b.Client = nil
	}

	return
}

func fmtQueueKey(queue string) string {
	return fmt.Sprintf(queueFmt, queue)
}

func fmtLockKey(key string, id int) string {
	return fmt.Sprintf(lockKeyFmt, key, id)
}

func fmtProcessingKey(id string) string {
	return fmt.Sprintf(processorFmt, id)
}

func fmtFailedKey(id string) string {
	return fmt.Sprintf(failedFmt, id)
}
