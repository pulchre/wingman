package wingman

import (
	"context"
	"errors"
)

var (
	ErrCanceled  = errors.New("backend: operation canceled")
	ErrUnmarshal = errors.New("backend: failed to unmarshal job")
)

type Backend interface {
	// PushJob takes a job and generates an InternalJob ready for
	// serialization, then pushes it onto the queue.
	PushJob(Job) error

	// PushInternalJob takes an already wrapped InternalJob then pushes it
	// onto the queue.
	PushInternalJob(*InternalJob) error

	// PopJob retrieves the next job from the queue. The context is be used
	// for canceling this call as it may block indefinitely. ErrCanceled
	// will not be treated by the manager as a failure.
	PopJob(ctx context.Context, queue string) (*InternalJob, error)

	// LockJob attempts to lock the given job. If successful, a LockID
	// greater than 0 is returned. If JobHeld is returned, the job has been
	// place in a separate holding queue. If LockNotRequired is returned,
	// the job has not acquired a lock and should still be processed.
	LockJob(job *InternalJob) (LockID, error)

	// ReleaseJob release a single lock for the given job. If there is job
	// with the same lock key that was previously held, it should be
	// inserted back onto the front of the queue.
	ReleaseJob(job *InternalJob) error

	// ProcessJob sets a job under a processing key. This is merely marks
	// the job in the backend as processing. The real processing is handled
	// by a processor.
	ProcessJob(job *InternalJob) error

	// ClearJob removes the job from processing.
	ClearJob(jobID string) error

	// FailJob marks a removes the processing key and marks the job as
	// failed.
	FailJob(job *InternalJob) error

	// Peek returns the next job on the queue if present. This should not
	// block and wait for a job to be enqueued.
	Peek(queue string) (*InternalJob, error)

	// Size returns the length of the queue.
	Size(queue string) uint64

	// SuccessfulJobs returns the number of jobs successfully processed.
	SuccessfulJobs() uint64

	// IncSuccessfulJobs increments the successful jobs count.
	IncSuccessfulJobs()

	// FailedJobs returns the number of jobs which failed during
	// processing.
	FailedJobs() uint64

	// IncFailedJobs increments the failed jobs count.
	IncFailedJobs()

	// Close cleans up any resources that may be necessary for its
	// operation.
	Close() error
}
