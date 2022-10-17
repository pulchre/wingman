package wingman

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/rs/zerolog"
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

	backend   Backend
	processor Processor

	workingJobs map[string]InternalJob
}

type ManagerOptions struct {
	Backend          Backend
	Queues           []string
	Signals          []os.Signal
	ProcessorOptions interface{}
}

// NewManager returns a new manager with the given backend, watching the
// given queues. It is recommended that a manager be used only in a
// standalone program as it starts subprocesses of itself to handle
// parallelism.
func NewManager(options ManagerOptions) (*Manager, error) {
	processor, err := NewProcessor(options.ProcessorOptions)
	if err != nil {
		return nil, err
	}

	return &Manager{
		backend:     options.Backend,
		queues:      options.Queues,
		signals:     options.Signals,
		processor:   processor,
		workingJobs: make(map[string]InternalJob),
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

	err := s.processor.Start()
	if err != nil {
		s.running = false
		return err
	}

	// This will throw any jobs that were popped off their queue but for
	// which processing had not began.
	err = s.reenqueueStagedJobs()
	if err != nil {
		s.cancel()
		s.running = false
		Log.Err(err).Msg("Failed to reenqueue staged jobs")
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

	s.wg.Add(1)
	go s.waitForResults()

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

	<-s.processor.Done()

	Log.Info().Msg("Wingman shutdown gracefully")

	s.shutdownSignalHandler()
	s.signalWg.Wait()

	return nil
}

func (s *Manager) Stop() {
	s.mu.Lock()

	if !s.running {
		return
	}

	s.stopping = true
	s.cancel()
	s.processor.Cancel()

	s.mu.Unlock()

	s.processor.Close()

	s.wg.Wait()
	s.signalWg.Wait()
}

func (s *Manager) shutdownSignalHandler() {
	if s.signalChan != nil {
		signal.Reset()
		close(s.signalChan)
		s.signalWg.Wait()
	}
}

func (s *Manager) watchQueue(ctx context.Context, queue string) {
	defer s.wg.Done()

	Log.Info().Str("queue", queue).Msg("Watching queue")
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

			ok = s.processor.Wait()

			if !ok {
				s.backend.ReenqueueStagedJob(job.StagingID)
				continue
			}

			s.handleJob(ctx, job)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Manager) retrieveJob(ctx context.Context, queue string) <-chan *InternalJob {
	jobChan := make(chan *InternalJob, 1)

	s.wg.Add(1)
	go func() {
		Log.Info().Str("queue", queue).Msg("Retrieving job from queue")
		defer close(jobChan)
		defer s.wg.Done()

		job, err := s.backend.PopAndStageJob(ctx, queue)
		if err != nil {
			if err != ErrCanceled {
				Log.Err(err).Msg("Failed to retrieve next job")
			}
			return
		}

		jobChan <- job
	}()

	return jobChan
}

func (s *Manager) handleJob(ctx context.Context, job *InternalJob) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopping {
		return
	}

	err := s.backend.ProcessJob(job.StagingID)
	if err != nil {
		Log.Err(err).Str("job_id", job.ID).Msg("Failed to move job to processing on the backend")
		return
	}

	job.StartTime = time.Now()
	s.workingJobs[job.ID] = *job

	Log.Info().Str("job_id", job.ID).Msg("Handling job")
	err = s.processor.SendJob(job)
	if err != nil {
		s.failJob(job)
		Log.Err(err).Str("job_id", job.ID).Msg("Failed to send job to processor")
		return
	}
}

func (s *Manager) waitForResults() {
	defer s.wg.Done()

	for res := range s.processor.Results() {
		s.mu.Lock()
		res.Job.StartTime = s.workingJobs[res.Job.ID].StartTime
		res.Job.EndTime = time.Now()
		delete(s.workingJobs, res.Job.ID)
		s.mu.Unlock()

		var event *zerolog.Event
		if res.Error == nil {
			event = Log.Info()
		} else {
			event = Log.Err(res.Error)
		}
		event.Str("job_id", res.Job.ID).
			Str("start", res.Job.StartTime.Format(time.RFC3339Nano)).
			Str("end", res.Job.EndTime.Format(time.RFC3339Nano)).
			Str("duration", res.Job.EndTime.Sub(res.Job.StartTime).String()).
			Msg("Job finished")

		if res.Error == nil {
			s.backend.ClearJob(res.Job.ID)
			s.backend.IncSuccessfulJobs()
		} else {
			s.failJob(res.Job)
		}
	}

}

func (s *Manager) failJob(job *InternalJob) {
	err := s.backend.FailJob(job.ID)
	if err != nil {
		Log.Err(err).Str("job_id", job.ID).Msg("Failed to move job status to failed with error")
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
					Log.Info().Msg(signalReceivedMsg)
					go s.Stop()
				} else if i > 1 {
					s.processor.ForceClose()
					done = true
				}
			} else {
				done = true
			}

		}
	}

	if i > 1 {
		Log.Fatal().Msg(signalHardShutdownMsg)
	}
}

func (s *Manager) reenqueueStagedJobs() error {
	jobs, err := s.backend.StagedJobs()
	if err != nil {
		return err
	}

	for _, j := range jobs {
		err = s.backend.ReenqueueStagedJob(j.StagingID)
		if err == ErrorJobNotStaged {
			Log.Err(err).Str("staging_id", j.StagingID).Msg("Failed to reenqueue staged job")
		} else if err != nil {
			return err
		}
	}

	return nil
}
