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

	mu       sync.Mutex
	running  bool
	stopping bool
	cancel   context.CancelFunc

	signalChan chan os.Signal
	signals    []os.Signal
	signalWg   sync.WaitGroup
	signalMu   sync.Mutex

	backend Backend

	pool ProcessorPool
}

type ManagerOptions struct {
	Backend     Backend
	Queues      []string
	Concurrency int
	Signals     []os.Signal
	PoolOptions interface{}
}

// NewManager returns a new manager with the given backend, watching the
// given queues. It is recommended that a manager be used only in a
// standalone program as it starts subprocesses of itself to handle
// parallelism.
//
// Concurrency is the number of goroutines that will process work in each
// process. A value of zero or less will be treated as unlimited.
func NewManager(options ManagerOptions) (*Manager, error) {
	pool, err := NewProcessorPool(options.PoolOptions)
	if err != nil {
		return nil, err
	}

	return &Manager{
		backend: options.Backend,
		queues:  options.Queues,
		signals: options.Signals,
		pool:    pool,
	}, nil
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
	s.pool.Cancel()

	s.mu.Unlock()

	s.pool.Close()

	s.wg.Wait()

	<-s.pool.Done()

	s.shutdownSignalHandler()
	s.signalWg.Wait()
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
		select {
		case <-ctx.Done():
			return
		default:
		}

		select {
		case job, ok := <-s.retrieveJob(ctx, queue):
			if !ok {
				continue
			}

			p := s.pool.Get()

			if p == nil {
				s.backend.ReenqueueStagedJob(job.StagingID.String())
				continue
			}

			s.handleJob(ctx, p, job)
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

func (s *Manager) handleJob(ctx context.Context, p Processor, job InternalJob) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopping {
		return
	}

	err := s.backend.ProcessJob(job.StagingID.String(), p.Id())
	if err != nil {
		Log.Printf("Failed to move job id=%v to processing on the backend err=%v", job.ID, err)
		s.pool.Put(p.Id())
		return
	}

	Log.Printf("Handling job id=%v", job.ID)
	err = p.SendJob(job)
	if err != nil {
		s.pool.Put(p.Id())
		Log.Printf("Failed to send job to processor: %v", err)
		return
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

	<-p.Done()
	s.pool.Put(p.Id())
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
					s.pool.ForceClose()
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
