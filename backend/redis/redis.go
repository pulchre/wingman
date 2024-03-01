package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pulchre/wingman"
	"github.com/redis/go-redis/v9"
)

const (
	successfulJobsCount = "wingman:successful_total"
	failedJobsCount     = "wingman:failed_total"
	stagingQueueFmt     = "wingman:staging:%v"
	heldQueueFmt        = "wingman:held:%v"
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

	serializedJob, err := json.Marshal(intJob)
	if err != nil {
		return err
	}

	return b.RPush(context.Background(), job.Queue(), serializedJob).Err()
}

func (b Backend) PopAndStageJob(ctx context.Context, queue string) (*wingman.InternalJob, error) {
	stagingID := uuid.New()

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

	raw, err := conn.BLMove(context.Background(), queue, fmtStagingKey(stagingID.String()), "LEFT", "RIGHT", b.config.BlockingTimeout).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, wingman.ErrCanceled
		} else {
			return nil, err
		}
	}

	job, err := wingman.InternalJobFromJSON([]byte(raw))
	if err != nil {
		return nil, err
	}

	job.StagingID = stagingID.String()

	return job, nil
}

func (b Backend) LockJob(job *wingman.InternalJob) (wingman.LockID, error) {
	concurrency := job.Job.Concurrency()

	if job.Job.LockKey() == "" || concurrency < 1 {
		return wingman.LockNotRequired, nil
	}

	var err error
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

	stagingKey := fmtStagingKey(job.StagingID)
	heldKey := fmtHeldKey(job.Job.LockKey())
	err = errors.Join(b.LMove(ctx, stagingKey, heldKey, "LEFT", "RIGHT").Err())
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
	_, err := b.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, lockKey)
		pipe.LMove(ctx, fmtHeldKey(job.Job.LockKey()), job.Queue(), "LEFT", "LEFT")
		return nil
	})

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

	_, err = b.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.RPush(ctx, fmtProcessingKey(job.ID), serialzedJob)
		pipe.Del(ctx, fmtStagingKey(job.StagingID))
		return nil
	})

	return err
}

func (b Backend) ReenqueueStagedJob(stagingID string) error {
	raw, err := b.LRange(context.Background(), fmtStagingKey(stagingID), 0, 1).Result()
	if err != nil {
		return err
	} else if len(raw) < 1 {
		return wingman.ErrorJobNotStaged
	}

	job, err := wingman.InternalJobFromJSON([]byte(raw[0]))
	if err != nil {
		return err
	}

	err = b.LMove(context.Background(), fmtStagingKey(stagingID), job.Queue(), "LEFT", "RIGHT").Err()
	if err != nil {
		return err
	}

	return nil
}

func (b Backend) ClearJob(jobID string) error {
	return b.Del(context.Background(), fmtProcessingKey(jobID)).Err()
}

func (b Backend) FailJob(jobID string) error {
	return b.LMove(context.Background(), fmtProcessingKey(jobID), fmtFailedKey(jobID), "RIGHT", "RIGHT").Err()
}

func (b Backend) StagedJobs() ([]*wingman.InternalJob, error) {
	var cursor uint64
	var keys []string
	var err error

	for {
		var nextKeys []string

		nextKeys, cursor, err = b.Scan(context.Background(), cursor, fmtStagingKey("*"), 0).Result()
		if err != nil {
			return nil, err
		}

		keys = append(keys, nextKeys...)

		if cursor == 0 {
			break
		}
	}

	jobs := make([]*wingman.InternalJob, len(keys))

	for i, key := range keys {
		raw, err := b.LRange(context.Background(), key, 0, 0).Result()
		if err != nil {
			return nil, err
		}

		job, err := wingman.InternalJobFromJSON([]byte(raw[0]))
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
	return b.Del(context.Background(), fmtStagingKey(stagingID)).Err()
}

func (b Backend) Peek(queue string) (*wingman.InternalJob, error) {
	raw, err := b.LRange(context.Background(), queue, 0, 0).Result()
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
	size, _ := b.LLen(context.Background(), queue).Uint64()
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

func fmtStagingKey(id string) string {
	return fmt.Sprintf(stagingQueueFmt, id)
}

func fmtLockKey(key string, id int) string {
	return fmt.Sprintf(lockKeyFmt, key, id)
}

func fmtHeldKey(key string) string {
	return fmt.Sprintf(heldQueueFmt, key)
}

func fmtProcessingKey(id string) string {
	return fmt.Sprintf(processorFmt, id)
}

func fmtFailedKey(id string) string {
	return fmt.Sprintf(failedFmt, id)
}
