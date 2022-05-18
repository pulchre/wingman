package mock

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
	"github.com/pulchre/wingman"
)

var NoQueueError = errors.New("Job must specify a queue")

type Backend struct {
	LastAddedID        uuid.UUID
	NextJobCanceledErr error

	queues  map[string][]*wingman.InternalJob
	staging map[uuid.UUID]*wingman.InternalJob
	working map[string]*wingman.InternalJob
	failed  map[string]*wingman.InternalJob

	stopping bool
	notifier *sync.Cond
}

func NewBackend() *Backend {
	return &Backend{
		NextJobCanceledErr: wingman.ErrCanceled,
		queues:             make(map[string][]*wingman.InternalJob),
		staging:            make(map[uuid.UUID]*wingman.InternalJob),
		working:            make(map[string]*wingman.InternalJob),
		failed:             make(map[string]*wingman.InternalJob),
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

	b.notifier.L.Lock()
	defer b.notifier.L.Unlock()

	if b.queues[job.Queue()] == nil {
		b.queues[job.Queue()] = make([]*wingman.InternalJob, 0)
	}

	b.queues[job.Queue()] = append(b.queues[job.Queue()], &intJob)

	b.notifier.Broadcast()

	b.LastAddedID = intJob.ID
	return nil
}

// PopAndStageJob assumes that only one goroutine is waiting on the notifier
func (b *Backend) PopAndStageJob(context context.Context, queue string) (*wingman.InternalJob, error) {
	var err error

	b.notifier.L.Lock()

	stop := make(chan struct{}, 1)
	defer close(stop)
	go func() {
		select {
		case <-context.Done():
		case <-stop:
		}

		b.notifier.L.Lock()
		defer b.notifier.L.Unlock()

		b.stopping = true
		b.notifier.Broadcast()
	}()
	defer b.notifier.L.Unlock()

	for !b.stopping {
		queue = b.resolveSplatQueue(queue)

		if len(b.queues[queue]) > 0 {
			job := b.queues[queue][0]
			job.StagingID, err = uuid.NewRandom()
			if err != nil {
				return nil, err
			}

			b.staging[job.StagingID] = job
			b.queues[queue] = b.queues[queue][1:]

			return job, nil
		}

		b.notifier.Wait()
	}

	return nil, b.NextJobCanceledErr
}

func (b *Backend) ProcessJob(stagingID, processorID string) error {
	b.notifier.L.Lock()
	defer b.notifier.L.Unlock()

	id := uuid.Must(uuid.Parse(stagingID))

	job, ok := b.staging[id]
	if !ok {
		return wingman.ErrorJobNotStaged
	}

	b.working[processorID] = job
	delete(b.staging, id)

	return nil
}
func (b *Backend) ClearProcessor(processorID string) error {
	b.notifier.L.Lock()
	defer b.notifier.L.Unlock()

	delete(b.working, processorID)

	return nil
}

func (b *Backend) FailJob(processorID string) error {
	b.notifier.L.Lock()
	defer b.notifier.L.Unlock()

	job, ok := b.working[processorID]
	if !ok {
		return wingman.ErrorJobNotFound
	}

	b.failed[job.ID.String()] = job
	delete(b.working, processorID)

	return nil
}

func (b *Backend) ReenqueueStagedJob(stagingID string) error {
	b.notifier.L.Lock()
	defer b.notifier.L.Unlock()

	id := uuid.Must(uuid.Parse(stagingID))

	job, ok := b.staging[id]
	if !ok {
		return wingman.ErrorJobNotStaged
	}

	b.queues[job.Queue()] = append(b.queues[job.Queue()], job)
	delete(b.staging, id)

	return nil
}

func (b *Backend) StagedJobs() ([]*wingman.InternalJob, error) {
	b.notifier.L.Lock()
	defer b.notifier.L.Unlock()

	jobs := make([]*wingman.InternalJob, len(b.staging))

	var i int
	for _, v := range b.staging {
		jobs[i] = v
		i++
	}

	return jobs, nil
}

func (b *Backend) ClearStagedJob(stagingID string) error {
	b.notifier.L.Lock()
	defer b.notifier.L.Unlock()

	delete(b.staging, uuid.Must(uuid.Parse(stagingID)))

	return nil
}

func (b *Backend) Peek(queue string) (*wingman.InternalJob, error) {
	b.notifier.L.Lock()
	defer b.notifier.L.Unlock()

	queue = b.resolveSplatQueue(queue)
	if len(b.queues[queue]) > 0 {
		return b.queues[queue][0], nil
	}

	return nil, nil
}

func (b *Backend) Size(queue string) int { return len(b.queues[queue]) }

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
