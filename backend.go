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

	// PopAndStageJob retrieves the next job from the queue, copies it to
	// the staging area, and returns it. This should stage the job so that
	// it can be recovered in the event of a hard shutdown.
	//
	// The context is be used for canceling this call as it may block
	// indefinitely. ErrCanceled will not be treated by the manager as a
	// failure.
	PopAndStageJob(ctx context.Context, queue string) (*InternalJob, error)

	// LockJob attempts to lock the given job. If successful, true is
	// returned. The manager will then process the job. If false, the job
	// should be held until ReleaseJob is called after another job with
	// the same lock key completes.
	LockJob(job InternalJob) (bool, error)

	// ReleaseJob release a single lock for the given job. If there is job
	// with the same lock key that was previously held, it should be
	// inserted back onto the front of the queue.
	ReleaseJob(job InternalJob) error

	// ProcessJob moves a job from staging to processing. This is merely
	// marks the job in the backend as processing. The real processing is
	// handled by a processor.
	ProcessJob(stagingID string) error

	// ClearJob removes the job from processing.
	ClearJob(jobID string) error

	// FailJob marks a currently marked processing job as failed.
	FailJob(jobID string) error

	// ReenqueueStagedJob pushes a staged job back onto the queue.
	ReenqueueStagedJob(stagingID string) error

	// StagedJobs returns a list of staged jobs. Staged jobs are jobs that
	// have been popped off the queue to be processed but have not yet been
	// sent to a processor. This is so we can minimize data loss in case of
	// a hard shutdown.
	//
	// If the data retrieved from the backend is invalid, the error
	// returned should be of type BackendIDError. This will allow the
	// manager to safely work around it.
	StagedJobs() ([]*InternalJob, error)

	// ClearStagedJob takes a staging ID string to delete the staged value
	// at that key.
	ClearStagedJob(stagingID string) error

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
