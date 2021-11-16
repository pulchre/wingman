package mock

import (
	"context"

	"github.com/google/uuid"
	"github.com/pulchre/wingman"
)

const DefaultQueue = "queue"
const DefaultJobType = "important job"

type HandlerSig func(context.Context, wingman.Job) error

var handlers = make(map[string]HandlerSig)

type Job struct {
	Data            string
	HandlerOverride string
	QueueOverride   string
}

func init() {
	wingman.RegisterJobType(Job{})
}

func RegisterHandler(name string, h HandlerSig) {
	handlers[name] = h
}

func CleanupHandlers() {
	handlers = make(map[string]HandlerSig)
}

func NewJob() Job { return Job{Data: uuid.Must(uuid.NewRandom()).String()} }

func NewWrappedJob() wingman.InternalJob {
	job, err := wingman.WrapJob(NewJob())
	if err != nil {
		panic(err)
	}

	return job
}

func (j Job) InternalJob(queue string) wingman.InternalJob {
	job, err := wingman.WrapJob(j)
	if err != nil {
		panic(err)
	}
	return job
}

func (j Job) TypeName() string { return DefaultJobType }

func (j Job) Handle(ctx context.Context) error {
	if j.HandlerOverride != "" {
		handler := handlers[j.HandlerOverride]
		return handler(ctx, j)
	}

	return nil
}

func (j Job) Queue() string {
	if j.QueueOverride == "" {

		return DefaultQueue
	} else {
		return j.QueueOverride
	}
}
