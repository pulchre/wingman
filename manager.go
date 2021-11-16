package wingman

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
)

const signalReceivedMsg = "Shutting down safely. Send signal again to shutdown immediately. Warning: data loss possible."
const signalHardShutdownMsg = "Hard shutdown"

var ErrorAlreadyRunning = errors.New("Manager is already running")

var wg sync.WaitGroup

type Manager struct {
	queues []string
	wg     sync.WaitGroup

	concurrency int

	mu       sync.Mutex
	running  bool
	stopping bool
	cancel   context.CancelFunc

	signalChan chan os.Signal
	signals    []os.Signal
	signalWg   sync.WaitGroup
	signalMu   sync.Mutex

	backend Backend

	processorCond       *sync.Cond
	busyProcessors      map[string]Processor
	availableProcessors map[string]Processor
}

type ManagerOptions struct {
	Backend     Backend
	Queues      []string
	Concurrency int
	Signals     []os.Signal
}

// NewManager returns a new manager with the given backend, watching the
// given queues. It is recommended that a manager be used only in a
// standalone program as it starts subprocesses of itself to handle
// parallelism.
//
// Concurrency is the number of goroutines that will process work in each
// process. A value of zero or less will be treated as unlimited.
func NewManager(options ManagerOptions) *Manager {
	if options.Concurrency < 1 {
		options.Concurrency = 1
	}

	manager := &Manager{
		backend:             options.Backend,
		queues:              options.Queues,
		signals:             options.Signals,
		concurrency:         options.Concurrency,
		processorCond:       sync.NewCond(&sync.Mutex{}),
		availableProcessors: make(map[string]Processor),
		busyProcessors:      make(map[string]Processor),
	}

	return manager
}

// Start starts the manager which begins watching the specified queues for
// jobs. When a job is received, it is processed.
//
// Start blocks until Stop is called and child goroutines exit.
func (s *Manager) Start() error {
	var ctx context.Context

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ErrorAlreadyRunning
	}

	s.running = true
	ctx, s.cancel = context.WithCancel(context.Background())

	err := s.reenqueueStagedJobs()
	if err != nil {
		s.cancel()
		s.running = false
		Log.Print("Failed to reenqueue staged jobs: ", err)
		return err
	}

	// Signal handling
	if len(s.signals) > 0 {
		s.signalMu.Lock()
		s.signalChan = make(chan os.Signal, 2)
		signal.Notify(s.signalChan, s.signals...)

		s.signalWg.Add(1)
		go s.waitForSignal()
		s.signalMu.Unlock()
	}
	s.mu.Unlock()

	// Processor count manager
	s.wg.Add(1)
	go s.concurrencyManager(ctx)

	// Queue watcher
	s.wg.Add(len(s.queues))
	for _, q := range s.queues {
		go s.watchQueue(ctx, q)
	}

	// Block
	s.wg.Wait()

	s.mu.Lock()
	s.cancel = nil
	s.running = false
	s.mu.Unlock()

	Log.Print("Wingman shutdown gracefully")

	return nil
}

func (s *Manager) Stop() {
	s.mu.Lock()

	if !s.running {
		return
	}

	s.stopping = true
	s.cancel()

	s.mu.Unlock()

	s.shutdownProcessors()

	s.wg.Wait()

	s.shutdownSignalHandler()
	s.signalWg.Wait()
}

func (s *Manager) concurrencyManager(ctx context.Context) {
	defer s.wg.Done()

	for {
		s.processorCond.L.Lock()
		processorCount := len(s.availableProcessors) + len(s.busyProcessors)
		if processorCount == s.concurrency {
			s.processorCond.Wait()
		}

		for i := processorCount; i < s.concurrency; i++ {
			s.startNewProcessor()
		}
		s.processorCond.L.Unlock()

		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// This method assumes that the caller has a lock on processorCond.L
func (s *Manager) startNewProcessor() {
	p := NewProcessor()
	err := p.Start()
	if err != nil {
		Log.Print("Error starting processor error=%v", err)
		return
	}

	s.busyProcessors[p.Id()] = p

	s.wg.Add(1)
	go s.processorStatusWatch(p)
}

func (s *Manager) processorStatusWatch(p Processor) {
	defer s.wg.Done()

	for status := range p.StatusChange() {
		s.handleProcessorStatusChange(p, status)
	}
}

func (s *Manager) handleProcessorStatusChange(p Processor, status ProcessorStatus) {
	s.processorCond.L.Lock()
	defer s.processorCond.L.Unlock()

	Log.Printf("Processor id=%v status=%v", p.Id(), ProcessorStatuses[status])

	switch status {
	case ProcessorStatusStarting, ProcessorStatusWorking, ProcessorStatusStopping:
		delete(s.availableProcessors, p.Id())
		s.busyProcessors[p.Id()] = p
	case ProcessorStatusIdle:
		delete(s.busyProcessors, p.Id())

		err := s.backend.ClearProcessor(p.Id())
		if err != nil {
			Log.Print(`Failed to clear working processor=%v err="%v"`, p.Id(), err)
		}

		s.availableProcessors[p.Id()] = p
		s.processorCond.Broadcast()
	case ProcessorStatusDead:
		delete(s.availableProcessors, p.Id())
		delete(s.busyProcessors, p.Id())
		s.processorCond.Broadcast()
	}
}

func (s *Manager) shutdownProcessors() {
	s.processorCond.L.Lock()
	defer s.processorCond.L.Unlock()

	for _, p := range s.availableProcessors {
		go func(p Processor) {
			err := p.Stop()

			if err != nil {
				Log.Printf("Error shutting down processor (%s): %v", p.Id(), err)
			}
		}(p)
	}

	for _, p := range s.busyProcessors {
		go func(p Processor) {
			err := p.Stop()

			if err != nil {
				Log.Printf("Error shutting down processor (%s): %v", p.Id(), err)
			}
		}(p)
	}
}

func (s *Manager) killProcessors() {
	s.processorCond.L.Lock()
	defer s.processorCond.L.Unlock()

	for _, p := range s.availableProcessors {
		err := p.Kill()

		if err != nil {
			Log.Printf("Error kill processor (%s): %v", p.Id(), err)
		}
	}

	for _, p := range s.busyProcessors {
		err := p.Kill()

		if err != nil {
			Log.Printf("Error kill processor (%s): %v", p.Id(), err)
		}
	}
}

func (s *Manager) shutdownSignalHandler() {
	if s.signalChan != nil {
		signal.Reset(s.signals...)
		close(s.signalChan)
		s.signalWg.Wait()
	}
}

func (s *Manager) watchQueue(ctx context.Context, queue string) {
	defer s.wg.Done()

	Log.Printf("Watching queue=%v", queue)
	for {
		s.processorCond.L.Lock()
		s.mu.Lock()
		if s.stopping {
			s.mu.Unlock()
			s.processorCond.L.Unlock()
			return
		}
		s.mu.Unlock()

		if len(s.availableProcessors) == 0 {
			s.processorCond.Wait()
			s.processorCond.L.Unlock()
			continue
		}
		s.processorCond.L.Unlock()

		// Check if we're shutting down before creating the retrieveJob
		// channel/goroutine. Since select chooses a random channel,
		// we want to ensure we don't start working on a new job.
		select {
		case <-ctx.Done():
			return
		default:
		}

		select {
		case job, ok := <-s.retrieveJob(ctx, queue):
			if ok {
				s.handleJob(ctx, job)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *Manager) retrieveJob(ctx context.Context, queue string) <-chan InternalJob {
	jobChan := make(chan InternalJob, 1)

	s.wg.Add(1)
	go func() {
		Log.Print("Retrieving job from queue=", queue)
		defer close(jobChan)
		defer s.wg.Done()

		job, err := s.backend.PopAndStageJob(ctx, queue)
		if err != nil {
			if err != ErrCanceled {
				Log.Print("Failed to retrieve next job: ", err)
			}
			return
		}

		jobChan <- *job
	}()

	return jobChan
}

func (s *Manager) handleJob(ctx context.Context, job InternalJob) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopping {
		return
	}

	p := s.popIdleProcessor()

	err := s.backend.ProcessJob(job.StagingID.String(), p.Id())
	if err != nil {
		Log.Printf("Failed to move job id=%v to processing on the backend err=%v", job.ID, err)
		s.pushIdleProcessor(p)
		return
	}

	Log.Printf("Handling job id=%v", job.ID)
	err = p.SendJob(job)
	if err != nil {
		s.pushIdleProcessor(p)
		Log.Printf("Failed to send job to processor: %v", err)
	}

	s.wg.Add(1)
	go s.waitForResults(p)
}

func (s *Manager) waitForResults(p Processor) {
	defer s.wg.Done()

	for res := range p.Results() {
		if res.Error != nil {
			Log.Printf("Job %v failed with error: %v", res.Job.ID, res.Error)
		}
	}

}

func (s *Manager) waitForSignal() {
	defer s.signalWg.Done()

	var i int
	var done bool

	for !done {
		select {
		case _, ok := <-s.signalChan:
			if ok {
				i++
				if i == 1 {
					Log.Print(signalReceivedMsg)
					go s.Stop()
				} else if i > 1 {
					s.killProcessors()
					done = true
				}
			} else {
				done = true
			}

		}
	}

	if i > 1 {
		Log.Fatal(signalHardShutdownMsg)
	}
}

func (s *Manager) reenqueueStagedJobs() error {
	jobs, err := s.backend.StagedJobs()
	if err != nil {
		return err
	}

	for _, j := range jobs {
		err = s.backend.ReenqueueStagedJob(j.StagingID.String())
		if err == ErrorJobNotStaged {
			Log.Print("Failed to reenqueue staged job staging_id=%v", j.StagingID)
		} else if err != nil {
			return err
		}
	}

	return nil
}

func (s *Manager) popIdleProcessor() Processor {
	s.processorCond.L.Lock()
	defer s.processorCond.L.Unlock()

	for id, p := range s.availableProcessors {
		delete(s.availableProcessors, id)
		s.busyProcessors[p.Id()] = p
		return p
	}

	return nil
}

func (s *Manager) pushIdleProcessor(p Processor) {
	s.processorCond.L.Lock()
	defer s.processorCond.L.Unlock()
	defer s.processorCond.Signal()

	delete(s.busyProcessors, p.Id())
	s.availableProcessors[p.Id()] = p
}
