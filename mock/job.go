package mock

import (
	"context"
	"time"

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
	Processed       bool
}

func init() {
	wingman.RegisterJobType(&Job{})
	addDefaultHandlers()
}

func addDefaultHandlers() {
	RegisterHandler("10millisecondsleep", func(ctx context.Context, j wingman.Job) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})
}

func RegisterHandler(name string, h HandlerSig) {
	handlers[name] = h
}

func CleanupHandlers() {
	handlers = make(map[string]HandlerSig)
	addDefaultHandlers()
}

func NewJob() *Job { return &Job{Data: uuid.Must(uuid.NewRandom()).String()} }

func NewWrappedJob() *wingman.InternalJob {
	job, err := wingman.WrapJob(NewJob())
	if err != nil {
		panic(err)
	}

	return job
}

func (j *Job) InternalJob(queue string) *wingman.InternalJob {
	job, err := wingman.WrapJob(j)
	if err != nil {
		panic(err)
	}
	return job
}

func (j Job) TypeName() string { return DefaultJobType }

func (j *Job) Handle(ctx context.Context) error {
	if j.HandlerOverride != "" {
		handler := handlers[j.HandlerOverride]
		return handler(ctx, j)
	}

	j.Processed = true
	return nil
}

func (j Job) Queue() string {
	if j.QueueOverride == "" {

		return DefaultQueue
	} else {
		return j.QueueOverride
	}
}
