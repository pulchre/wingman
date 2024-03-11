package mock

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"

	"github.com/pulchre/wingman"
)

var NoQueueError = errors.New("Job must specify a queue")

type Backend struct {
	LastAddedID        string
	NextJobCanceledErr error

	queues  map[string][]wingman.InternalJob
	working map[string]wingman.InternalJob
	failed  map[string]wingman.InternalJob
	locks   map[string]*atomic.Int64
	held    map[string][]wingman.InternalJob
	heldMu  sync.Mutex

	stopping bool
	notifier *sync.Cond

	successfulCount uint64
	failedCount     uint64
}

func NewBackend() *Backend {
	return &Backend{
		NextJobCanceledErr: wingman.ErrCanceled,
		queues:             make(map[string][]wingman.InternalJob),
		working:            make(map[string]wingman.InternalJob),
		failed:             make(map[string]wingman.InternalJob),
		locks:              make(map[string]*atomic.Int64),
		held:               make(map[string][]wingman.InternalJob),
		notifier:           sync.NewCond(&sync.Mutex{}),
	}
}

func (b *Backend) PushJob(job wingman.Job) error {
	if job.Queue() == "" {
		return NoQueueError
	}

	intJob, err := wingman.WrapJob(job)
	if err != nil {
		panic(err)
	}

	return b.PushInternalJob(intJob)
}

func (b *Backend) PushInternalJob(job *wingman.InternalJob) error {
	if job.Queue() == "" {
		return NoQueueError
	}

	b.notifier.L.Lock()
	defer b.notifier.L.Unlock()

	b.queues[job.Queue()] = append(b.queues[job.Queue()], *job)

	b.notifier.Broadcast()

	b.LastAddedID = job.ID
	return nil
}

// PopJob assumes that only one goroutine is waiting on the notifier
func (b *Backend) PopJob(ctx context.Context, queue string) (*wingman.InternalJob, error) {
	b.notifier.L.Lock()
	defer b.notifier.L.Unlock()

	var canceled bool
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
		case <-stop:
		}

		b.notifier.L.Lock()
		defer b.notifier.L.Unlock()

		canceled = true
		b.notifier.Broadcast()
	}()

	for !canceled {
		queue = b.resolveSplatQueue(queue)

		if len(b.queues[queue]) > 0 {
			job := b.queues[queue][0]
			b.queues[queue] = b.queues[queue][1:]

			return &job, nil
		}

		b.notifier.Wait()
	}

	return nil, b.NextJobCanceledErr
}

func (b *Backend) LockJob(job *wingman.InternalJob) (wingman.LockID, error) {
	lockKey := job.Job.LockKey()
	concurrency := job.Job.Concurrency()

	if lockKey == "" || concurrency < 1 {
		return wingman.LockNotRequired, nil
	}

	if _, ok := b.locks[lockKey]; !ok {
		b.locks[lockKey] = &atomic.Int64{}
	}

	locks := b.locks[lockKey].Add(1)
	if locks > int64(concurrency) {
		b.locks[lockKey].Add(-1)
		b.heldMu.Lock()
		defer b.heldMu.Unlock()

		b.held[lockKey] = append(b.held[lockKey], *job)
		return wingman.JobHeld, nil
	}

	return wingman.LockID(rand.IntN(concurrency) + 1), nil
}

func (b *Backend) ReleaseJob(job *wingman.InternalJob) error {
	lockKey := job.Job.LockKey()

	if _, ok := b.locks[lockKey]; !ok {
		b.locks[lockKey] = &atomic.Int64{}
		return nil
	}

	b.locks[lockKey].Add(-1)

	b.heldMu.Lock()
	defer b.heldMu.Unlock()
	b.notifier.L.Lock()
	defer b.notifier.L.Unlock()

	if len(b.held[lockKey]) == 0 {
		return nil
	}

	heldJob := b.held[lockKey][0]
	b.held[lockKey] = b.held[lockKey][1:]
	b.queues[job.Queue()] = append([]wingman.InternalJob{heldJob}, b.queues[job.Queue()]...)

	b.notifier.Broadcast()

	return nil
}

func (b *Backend) ProcessJob(job *wingman.InternalJob) error {
	b.notifier.L.Lock()
	defer b.notifier.L.Unlock()

	b.working[job.ID] = *job

	return nil
}

func (b *Backend) ClearJob(jobID string) error {
	b.notifier.L.Lock()
	defer b.notifier.L.Unlock()

	delete(b.working, jobID)

	return nil
}

func (b *Backend) FailJob(job *wingman.InternalJob) error {
	b.notifier.L.Lock()
	defer b.notifier.L.Unlock()

	b.failed[job.ID] = *job
	delete(b.working, job.ID)

	return nil
}

func (b *Backend) Peek(queue string) (*wingman.InternalJob, error) {
	b.notifier.L.Lock()
	defer b.notifier.L.Unlock()

	queue = b.resolveSplatQueue(queue)
	if len(b.queues[queue]) > 0 {
		return &b.queues[queue][0], nil
	}

	return nil, nil
}

func (b *Backend) Size(queue string) uint64 { return uint64(len(b.queues[queue])) }

func (b *Backend) SuccessfulJobs() uint64 { return b.successfulCount }
func (b *Backend) IncSuccessfulJobs()     { b.successfulCount++ }
func (b *Backend) FailedJobs() uint64     { return b.failedCount }
func (b *Backend) IncFailedJobs()         { b.failedCount++ }

func (b *Backend) Close() error { return nil }

func (b *Backend) HasProcessingJob(processorID string) bool {
	_, ok := b.working[processorID]
	return ok
}

func (b Backend) resolveSplatQueue(queue string) string {
	if queue == "*" {
		for k, v := range b.queues {
			if len(v) > 0 {
				queue = k
				break
			}
		}
	}

	return queue
}
