package goroutine

import (
	"context"
	"sync"
	"time"

	"github.com/pulchre/wingman"
)

func init() {
	wingman.NewProcessor = NewProcessor
}

type Processor struct {
	options ProcessorOptions

	capacityCond *sync.Cond
	working      map[string]workingJob
	stopping     bool

	wg wingman.WaitGroup

	baseCtx context.Context

	resultMu      sync.Mutex
	resultsClosed bool
	resultsChan   chan wingman.ResultMessage
	doneChan      chan struct{}
}

type workingJob struct {
	ID        string
	Cancel    context.CancelFunc
	StartTime time.Time
}

func NewProcessor(options interface{}) (wingman.Processor, error) {
	opts := options.(ProcessorOptions)
	if opts.Concurrency < 1 {
		opts.Concurrency = 1
	}

	p := &Processor{
		options:      opts,
		capacityCond: sync.NewCond(new(sync.Mutex)),
		working:      make(map[string]workingJob),
		baseCtx:      context.Background(),
		resultsChan:  make(chan wingman.ResultMessage, 64),
		doneChan:     make(chan struct{}),
	}

	return p, nil
}

func (p *Processor) Start() error {
	return nil
}

func (p *Processor) Close() {
	p.wg.Wait()

	p.resultMu.Lock()
	defer p.resultMu.Unlock()
	p.resultsClosed = true
	close(p.resultsChan)
	close(p.doneChan)
}

func (p *Processor) Wait() bool {
	p.capacityCond.L.Lock()
	defer p.capacityCond.L.Unlock()

	if len(p.working) < p.options.Concurrency {
		return true
	}

	p.capacityCond.Wait()

	return len(p.working) < p.options.Concurrency
}

func (p *Processor) Cancel() {
	p.capacityCond.Signal()
}

func (p *Processor) Working() int {
	p.capacityCond.L.Lock()
	defer p.capacityCond.L.Unlock()

	return len(p.working)
}

func (p *Processor) SendJob(job *wingman.InternalJob) error {
	var ctx context.Context

	p.capacityCond.L.Lock()
	ctx, cancel := context.WithCancel(context.WithValue(p.baseCtx, wingman.ContextJobIDKey, job.ID))
	p.working[job.ID] = workingJob{
		ID:        job.ID,
		Cancel:    cancel,
		StartTime: time.Now(),
	}
	p.capacityCond.L.Unlock()

	p.wg.Add(1)
	go func() {
		var err error

		defer p.wg.Done()

		defer func() {
			if recoveredErr := recover(); recoveredErr != nil {
				err = wingman.NewError(recoveredErr)
			}

			p.capacityCond.L.Lock()
			defer p.capacityCond.L.Unlock()

			delete(p.working, job.ID)
			p.capacityCond.Signal()

			p.resultMu.Lock()
			defer p.resultMu.Unlock()

			if p.resultsClosed {
				return
			}

			p.resultsChan <- wingman.ResultMessage{
				Job:   job,
				Error: err,
			}
		}()

		err = job.Job.Handle(ctx)
	}()

	return nil
}

func (p *Processor) Results() <-chan wingman.ResultMessage { return p.resultsChan }
func (p *Processor) Done() <-chan struct{}                 { return p.doneChan }
func (p *Processor) ForceClose()                           { p.wg.Clear() }
