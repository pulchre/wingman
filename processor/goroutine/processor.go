package goroutine

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pulchre/wingman"
)

type Processor struct {
	id      uuid.UUID
	cancel  context.CancelFunc
	working bool
	mu      sync.Mutex
	wg      sync.WaitGroup

	status     wingman.ProcessorStatus
	statusMu   sync.Mutex
	statusChan chan wingman.ProcessorStatus
	stopping   bool

	resultChan chan wingman.ResultMessage
}

func newProcessor() wingman.Processor {
	return &Processor{
		id:         uuid.New(),
		resultChan: make(chan wingman.ResultMessage, 64),
		statusChan: make(chan wingman.ProcessorStatus, 64),
	}
}

func init() {
	wingman.NewProcessor = newProcessor
}

func (p *Processor) Id() string {
	return p.id.String()
}

func (p *Processor) Start() error {
	p.setStatus(wingman.ProcessorStatusStarting)
	p.setStatus(wingman.ProcessorStatusIdle)
	return nil
}

func (p *Processor) Stop() error {
	p.mu.Lock()
	p.setStatus(wingman.ProcessorStatusStopping)
	p.stopping = true
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Unlock()

	go func() {
		p.wg.Wait()
		close(p.resultChan)
		p.setStatus(wingman.ProcessorStatusDead)

	}()

	return nil
}

func (p *Processor) Kill() error {
	return nil
}

func (p *Processor) Working() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.working
}

func (p *Processor) SendJob(job wingman.InternalJob) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var ctx context.Context

	p.wg.Add(1)
	p.working = true
	ctx, p.cancel = context.WithCancel(context.Background())

	p.setStatus(wingman.ProcessorStatusWorking)

	go func() {
		defer p.wg.Done()
		defer func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			if !p.stopping {
				p.setStatus(wingman.ProcessorStatusIdle)
			}
		}()
		var err error

		start := time.Now()
		defer func() {
			end := time.Now()
			wingman.Log.Printf("Job %s done after %s", job.ID, end.Sub(start))

			if recoveredErr := recover(); recoveredErr != nil {
				err = wingman.NewError(recoveredErr)
			}

			p.mu.Lock()
			p.working = false
			p.cancel = nil
			p.mu.Unlock()

			p.resultChan <- wingman.ResultMessage{
				Job:   job,
				Error: err,
			}
		}()

		wingman.Log.Printf("Job %s started...", job.ID)
		err = job.Job.Handle(ctx)
	}()

	return nil
}

func (p *Processor) Results() <-chan wingman.ResultMessage        { return p.resultChan }
func (p *Processor) StatusChange() <-chan wingman.ProcessorStatus { return p.statusChan }

func (p *Processor) setStatus(s wingman.ProcessorStatus) {
	p.statusMu.Lock()
	defer p.statusMu.Unlock()

	p.status = s
	p.statusChan <- p.status

	if s == wingman.ProcessorStatusDead {
		close(p.statusChan)
	}
}
