package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pulchre/wingman"
	"github.com/redis/go-redis/v9"
)

const successfulJobsCount = "wingman:successful_total"
const failedJobsCount = "wingman:failed_total"
const stagingQueueFmt = "wingman:staging:%v"
const heldQueueFmt = "wingman:held:%v"
const lockKeyFmt = "wingman:lock:%s:%s"
const processorFmt = "wingman:processing:%v"
const failedFmt = "wingman:failed:%v"

type response struct {
	Job   wingman.InternalJob
	Raw   []byte
	Error error
}

type Backend struct {
	*redis.Client

	timeout time.Duration
}

var _ wingman.Backend = &Backend{}

type Options struct {
	redis.Options

	BlockingTimeout time.Duration
}

func Init(options Options) (*Backend, error) {
	var backend *Backend

	backend = &Backend{
		redis.NewClient(&options.Options),
		options.BlockingTimeout,
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

	raw, err := conn.BLMove(context.Background(), queue, fmtStagingKey(stagingID.String()), "LEFT", "RIGHT", b.timeout).Result()
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

func (b Backend) LockJob(job wingman.InternalJob) (bool, error) {
	lockKey := job.Job.LockKey()
	concurrency := job.Job.Concurrency()

	if lockKey == "" || concurrency < 1 {
		return true, nil
	}

	serializedJob, err := json.Marshal(job)
	if err != nil {
		return false, err
	}

	redisLockKey := fmtLockKey(lockKey, job.ID)

	err = b.Set(context.Background(), redisLockKey, serializedJob, 5*time.Minute).Err()
	if err != nil {
		return false, err
	}

	count, err := b.lockCount(lockKey)
	if err != nil {
		return false, err
	}

	if count > concurrency {
		err = b.LMove(context.Background(), fmtStagingKey(job.StagingID), fmtHeldKey(lockKey), "RIGHT", "RIGHT").Err()
		err = errors.Join(err, b.Del(context.Background(), redisLockKey).Err())
		if err != nil {
			return false, err
		}

		count, err = b.lockCount(lockKey)
		if err != nil {
			return false, err
		}

		// We check one more time if a lock has freed since we may not have
		// tried to pull a held job before the above job was moved to the held
		// list.
		if count < concurrency {
			err = b.LMove(context.Background(), fmtHeldKey(lockKey), job.Queue(), "RIGHT", "RIGHT").Err()
			if err != nil && err != redis.Nil {
				return false, err
			}
		}

		return false, nil
	}

	return true, err
}

func (b Backend) ReleaseJob(job wingman.InternalJob) error {
	lockKey := job.Job.LockKey()
	concurrency := job.Job.Concurrency()

	if lockKey == "" || concurrency < 1 {
		return nil
	}

	redisLockKey := fmtLockKey(lockKey, job.ID)
	err := b.LMove(context.Background(), fmtHeldKey(lockKey), job.Queue(), "LEFT", "LEFT").Err()
	if err == redis.Nil {
		err = nil
	}

	return errors.Join(err, b.Del(context.Background(), redisLockKey).Err())
}

func (b Backend) ProcessJob(stagingID string) error {
	raw, err := b.LRange(context.Background(), fmtStagingKey(stagingID), 0, 1).Result()
	if err != nil {
		return err
	} else if len(raw) == 0 {
		return wingman.ErrorJobNotStaged
	}

	job, err := wingman.InternalJobFromJSON([]byte(raw[0]))
	if err != nil {
		return wingman.ErrorJobNotStaged
	}

	err = b.LMove(context.Background(), fmtStagingKey(stagingID), fmtProcessingKey(job.ID), "LEFT", "RIGHT").Err()
	if err != nil {
		return err
	}
	return nil
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
	keys, err := b.Keys(context.Background(), fmtStagingKey("*")).Result()
	if err != nil {
		return nil, err
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
func (b Backend) Close() (err error) {
	if b.Client != nil {
		err = b.Client.Close()
		b.Client = nil
	}

	return
}

func (b Backend) lockCount(key string) (int, error) {
	redisLockKeyMatch := fmtLockKey(key, "*")
	var count int
	var cursor uint64
	var err error
	for {
		var keys []string

		keys, cursor, err = b.Scan(context.Background(), cursor, redisLockKeyMatch, 0).Result()
		if err != nil {
			return 0, err
		}

		count += len(keys)
		if cursor == 0 {
			break
		}
	}

	return count, nil
}

func fmtStagingKey(id string) string {
	return fmt.Sprintf(stagingQueueFmt, id)
}

func fmtLockKey(key, id string) string {
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
