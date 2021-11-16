package wingman_test

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/pulchre/wingman"
	"github.com/pulchre/wingman/mock"
	_ "github.com/pulchre/wingman/processor/goroutine"
)

const concurrencyCount = 2

func init() {
	// Call signal.Notify starts a goroutine which cannot be shut down
	// (nor can the program deadlock). Therefore we start it here so when
	// we compare the number of goroutines in tests we won't be off by one.
	c := make(chan os.Signal)
	signal.Notify(c)
	signal.Reset()
	close(c)
}

func TestNewManagerConcurrencyLessThanZero(t *testing.T) {
	s := wingman.NewManager(
		wingman.ManagerOptions{
			Backend:     mock.NewBackend(),
			Queues:      []string{"*"},
			Concurrency: -1,
		},
	)

	if wingman.ManagerConcurrency(s) != 1 {
		t.Error("Concurrency should be 1, got: ", wingman.ManagerConcurrency(s))
	}
}

func TestStartAlreadyRunning(t *testing.T) {
	s := setup()
	run(t, s, nil)

	err := s.Start()
	if err != wingman.ErrorAlreadyRunning {
		t.Errorf("Expected %v, got %v", wingman.ErrorAlreadyRunning, err)
	}

	s.Stop()
}

func TestStartJobProcessSuccessfully(t *testing.T) {
	wingman.TestLog.Reset()

	handlerName := "handler"
	mockJob := mock.NewJob()
	mockJob.HandlerOverride = handlerName

	var jobWg sync.WaitGroup

	mock.RegisterHandler(handlerName, func(ctx context.Context, job wingman.Job) error {
		j := job.(mock.Job)

		if j != mockJob {
			t.Errorf("Wrong job passed to handler. Got: %v, Expected: %v", j, mockJob)
		}
		jobWg.Done()

		return nil
	})

	s := setup()
	wg := run(t, s, nil)
	jobWg.Add(1)
	wingman.ManagerBackend(s).PushJob(mockJob)
	jobWg.Wait()

	mock.CleanupHandlers()

	s.Stop()
	wg.Wait()

	if !wingman.TestLog.PrintReceived("Job %s started...", backendToMockBackend(s).LastAddedId) {
		t.Errorf("Expected log when job processing starts. Got: %v", wingman.TestLog.PrintVars)
	}
}

func TestStartJobProcessError(t *testing.T) {
	wingman.TestLog.Reset()

	handlerName := "handler"
	mockJob := mock.NewJob()
	mockJob.HandlerOverride = handlerName

	var jobWg sync.WaitGroup

	jobErr := errors.New("Failed to process job")

	mock.RegisterHandler(handlerName, func(ctx context.Context, job wingman.Job) error {
		j := job.(mock.Job)

		if j != mockJob {
			t.Errorf("Wrong job passed to handler. Got: %v, Expected: %v", j, mockJob)
		}
		jobWg.Done()

		return jobErr
	})

	s := setup()
	wg := run(t, s, nil)
	jobWg.Add(1)
	wingman.ManagerBackend(s).PushJob(mockJob)
	jobWg.Wait()

	mock.CleanupHandlers()

	s.Stop()
	wg.Wait()

	if !wingman.TestLog.PrintReceived("Job %v failed with error: %v", backendToMockBackend(s).LastAddedId, jobErr) {
		t.Errorf("Expected log when job processing fails. Got: %v", wingman.TestLog.PrintVars)
	}
}

func TestStartJobProcessPanic(t *testing.T) {
	wingman.TestLog.Reset()

	handlerName := "handler"
	mockJob := mock.NewJob()
	mockJob.HandlerOverride = handlerName

	var jobWg sync.WaitGroup

	panicMsg := "Job panicked"

	mock.RegisterHandler(handlerName, func(ctx context.Context, job wingman.Job) error {
		j := job.(mock.Job)

		if j != mockJob {
			t.Errorf("Wrong job passed to handler. Got: %v, Expected: %v", j, mockJob)
		}
		jobWg.Done()

		panic(panicMsg)
	})

	s := setup()
	wg := run(t, s, nil)
	jobWg.Add(1)
	wingman.ManagerBackend(s).PushJob(mockJob)
	jobWg.Wait()

	mock.CleanupHandlers()

	s.Stop()
	wg.Wait()

	if !wingman.TestLog.PrintReceived("Job %v failed with error: %v", backendToMockBackend(s).LastAddedId, wingman.NewError(panicMsg)) {
		t.Errorf("Expected log when job panics. Got: %v", wingman.TestLog.PrintVars)
	}
}

func TestStartNextJobFails(t *testing.T) {
	wingman.TestLog.Reset()

	s := setup()
	err := errors.New("Test error")

	backendToMockBackend(s).NextJobCanceledErr = err
	run(t, s, nil)

	s.Stop()

	if !wingman.TestLog.PrintReceived("Failed to retrieve next job: ", err) {
		t.Errorf("Erroneously logged: `%v`", wingman.TestLog.PrintVars)
	}
}

func TestStopAlreadyStopped(t *testing.T) {
	s := setup()

	timer := time.NewTimer(1 * time.Second)

	go func() {
		if _, ok := <-timer.C; ok {
			t.Error("Stop call to non-running manager should not hang")
		}
	}()

	// This should not panic or hang
	s.Stop()

	// We don't want to leave the goroutine above running
	timer.Stop()
}

func TestStopByOneSignal(t *testing.T) {
	wingman.TestLog.Reset()

	s := setup()
	wg := run(t, s, nil)

	pid := os.Getpid()
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal("Could not get test process: ", err)
	}

	err = process.Signal(syscall.SIGUSR2)
	if err != nil {
		t.Fatal("Could not send signal to test process: ", err)
	}

	wg.Wait()

	if !wingman.TestLog.PrintReceived(wingman.SignalReceivedMsg()) {
		t.Errorf("Expected signal received message: %s", wingman.TestLog.PrintVars)
	}
}

func TestStopByManySignals(t *testing.T) {
	wingman.TestLog.Reset()

	s := setup()
	wg := run(t, s, nil)

	pid := os.Getpid()
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal("Could not get test process: ", err)
	}

	handlerName := "handler"

	mockJob := mock.NewJob()
	mockJob.HandlerOverride = handlerName

	mock.RegisterHandler(handlerName, func(ctx context.Context, job wingman.Job) error {
		time.Sleep(1500 * time.Millisecond)
		return nil
	})

	wingman.ManagerBackend(s).PushJob(mockJob)

	err = process.Signal(syscall.SIGUSR2)
	if err != nil {
		t.Fatal("Could not send first signal to test process: ", err)
	}

	time.Sleep(500 * time.Millisecond)
	err = process.Signal(syscall.SIGUSR2)
	if err != nil {
		t.Fatal("Could not send second signal to test process: ", err)
	}

	wg.Wait()

	if !wingman.TestLog.FatalReceived(wingman.SignalHardShutdownMsg()) {
		t.Errorf("Expected signal received message: %s", wingman.TestLog.PrintVars)
	}

	mock.CleanupHandlers()
}

func TestMaintainCorrectProcessorCount(t *testing.T) {
	s := setup()
	run(t, s, nil)

	count := wingman.ProcessorCount(s)
	if count != concurrencyCount {
		t.Fatalf("Correct number of processors not started. Got: %d, expect: %v", count, concurrencyCount)
	}

	p := wingman.PopIdleProcessor(s)
	p.Stop()

	time.Sleep(100 * time.Millisecond)
	count = wingman.ProcessorCount(s)
	if count != concurrencyCount {
		t.Fatalf("Correct number of processors not maintained. Got: %d, expect: %v", count, concurrencyCount)
	}
}

func run(t *testing.T, s *wingman.Manager, expectedErr error) *sync.WaitGroup {
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		err := s.Start()
		if err != expectedErr {
			t.Errorf("Expected manager.Start to return %v, got: %v", expectedErr, err)
		}

		wg.Done()
	}()

	// We want to ensure the call to Start in the goroutine above is
	// successful in entering a running state before other checks are run.
	time.Sleep(10 * time.Millisecond)
	return &wg
}

func setup() *wingman.Manager {
	backend := mock.NewBackend()
	return wingman.NewManager(
		wingman.ManagerOptions{
			Backend:     backend,
			Queues:      []string{"*"},
			Concurrency: concurrencyCount,
			Signals:     []os.Signal{syscall.SIGUSR2},
		},
	)
}

func backendToMockBackend(s *wingman.Manager) *mock.Backend {
	return wingman.ManagerBackend(s).(*mock.Backend)
}
