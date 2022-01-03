package goroutine

import (
	"sync"

	"github.com/pulchre/wingman"
)

type Pool struct {
	options PoolOptions

	cond      *sync.Cond
	available []*Processor
	busy      []*Processor
	dead      []*Processor

	shutdownCond *sync.Cond
	shutdown     bool
}

type PoolOptions struct {
	Concurrency int
}

func init() {
	wingman.NewProcessorPool = NewPool
}

func NewPool(options interface{}) (wingman.ProcessorPool, error) {
	opts := options.(PoolOptions)
	if opts.Concurrency < 1 {
		opts.Concurrency = 1
	}

	pool := &Pool{
		options:      opts,
		cond:         sync.NewCond(new(sync.Mutex)),
		available:    make([]*Processor, opts.Concurrency),
		busy:         make([]*Processor, 0),
		dead:         make([]*Processor, 0),
		shutdownCond: sync.NewCond(new(sync.Mutex)),
	}

	for i := 0; i < opts.Concurrency; i++ {
		pool.available[i] = NewProcessor()
	}

	return pool, nil
}

func (p *Pool) Get() wingman.Processor {
	p.cond.L.Lock()
	defer p.cond.L.Unlock()

	if len(p.available) == 0 {
		p.cond.Wait()
	}

	if len(p.available) == 0 {
		return nil
	}

	processor := p.available[0]
	p.available = p.available[1:]
	p.busy = append(p.busy, processor)

	return processor
}

func (p *Pool) Put(id string) {
	p.cond.L.Lock()
	defer p.cond.L.Unlock()

	i, ok := index(id, p.busy)
	if !ok {
		return
	}

	if p.shutdown {
		if len(p.busy) == 1 {
			p.shutdownCond.Broadcast()
		}
	} else {
		p.available = append(p.available, p.busy[i])
	}
	p.busy = append(p.busy[:i], p.busy[i+1:]...)

	p.Cancel()
}

func (p *Pool) Wait() {
	p.cond.L.Lock()
	defer p.cond.L.Unlock()

	if len(p.available) > 0 {
		return
	}

	p.cond.Wait()
}

func (p *Pool) Cancel() {
	p.cond.Signal()
}

func (p *Pool) Close() {
	p.closeProcessors(true)
}

func (p *Pool) ForceClose() {
	p.shutdownCond.Broadcast()
	p.closeProcessors(false)
}

func (p *Pool) closeProcessors(block bool) {
	p.shutdownCond.L.Lock()
	p.shutdown = true
	for _, processor := range p.available {
		processor.Kill()
	}
	p.available = make([]*Processor, 0)
	p.dead = make([]*Processor, 0)

	for _, processor := range p.busy {
		processor.Kill()
	}

	if block && len(p.busy) > 0 {
		p.shutdownCond.Wait()
	}
}

func index(id string, a []*Processor) (int, bool) {
	for i, p := range a {
		if p.Id() == id {
			return i, true
		}
	}

	return 0, false
}
