package wingman_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/pulchre/wingman"
	"github.com/pulchre/wingman/mock"
	"github.com/pulchre/wingman/processor/goroutine"
)

const concurrencyCount = 2

func init() {
	// Call signal.Notify starts a goroutine which cannot be shut down
	// (nor can the program deadlock). Therefore we start it here so when
	// we compare the number of goroutines in tests we won't be off by one.
	c := make(chan os.Signal)
	signal.Notify(c)
	signal.Stop(c)
	close(c)
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
	mock.TestLog.Reset()

	// This seems to help prevent circumstances where there
	// are more goroutines at the start than the end.
	time.Sleep(10 * time.Millisecond)

	goroutines := runtime.NumGoroutine()
	defer func() {
		if err := recover(); err != nil {
			panic(err)
		}

		// This seems to help prevent circumstances
		// where there are more goroutines at the end
		// than the start.
		time.Sleep(10 * time.Millisecond)
		now := runtime.NumGoroutine()

		if goroutines != now {
			pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			t.Fatalf("Started with %d goroutines. Ended with: %d", goroutines, now)
		}
	}()

	handlerName := "handler"
	mockJob := mock.NewJob()
	mockJob.HandlerOverride = handlerName

	var jobWg sync.WaitGroup
	var processed bool

	var processorID string

	mock.RegisterHandler(handlerName, func(ctx context.Context, job wingman.Job) error {
		defer jobWg.Done()
		processorID = ctx.Value(wingman.ContextProcessorIDKey).(string)

		j := job.(mock.Job)

		if j != mockJob {
			t.Errorf("Wrong job passed to handler. Got: %v, Expected: %v", j, mockJob)
		}

		processed = true

		return nil
	})

	s := setup()
	wg := run(t, s, nil)
	jobWg.Add(1)
	wingman.ManagerBackend(s).PushJob(mockJob)

	jobWg.Wait()
	time.Sleep(100 * time.Millisecond)

	if !processed {
		t.Errorf("Expected job to process")
	}

	backend := backendToMockBackend(s)

	if backend.HasProcessingJob(processorID) {
		t.Errorf("Expected processing marker to be removed from the backend")
	}

	mock.CleanupHandlers()
	s.Stop()
	wg.Wait()

	for _, v := range mock.TestLog.PrintVars {
		if strings.Contains(v, "failed with error:") {
			t.Errorf("Expected successful job to not log error")
		}
	}
}

func TestStartJobProcessError(t *testing.T) {
	mock.TestLog.Reset()

	handlerName := "handler"
	mockJob := mock.NewJob()
	mockJob.HandlerOverride = handlerName

	var jobWg sync.WaitGroup

	jobErr := errors.New("Failed to process job")

	mock.RegisterHandler(handlerName, func(ctx context.Context, job wingman.Job) error {
		defer jobWg.Done()

		j := job.(mock.Job)

		if j != mockJob {
			t.Errorf("Wrong job passed to handler. Got: %v, Expected: %v", j, mockJob)
		}

		return jobErr
	})

	s := setup()
	wg := run(t, s, nil)
	jobWg.Add(1)
	wingman.ManagerBackend(s).PushJob(mockJob)
	jobWg.Wait()
	time.Sleep(100 * time.Millisecond)

	mock.CleanupHandlers()

	s.Stop()
	wg.Wait()

	if !mock.TestLog.PrintReceived(fmt.Sprintf("Job %v failed with error: %v", backendToMockBackend(s).LastAddedID, jobErr)) {
		t.Errorf("Expected log when job processing fails. Got: %v", mock.TestLog.PrintVars)
	}
}

func TestStartJobProcessPanic(t *testing.T) {
	mock.TestLog.Reset()

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

	// Make sure we don't miss the panic
	time.Sleep(250 * time.Millisecond)

	mock.CleanupHandlers()

	s.Stop()
	wg.Wait()

	if !mock.TestLog.PrintReceived(fmt.Sprintf("Job %v failed with error: %v", backendToMockBackend(s).LastAddedID, wingman.NewError(panicMsg))) {
		t.Errorf("Expected log when job panics. Got: %v", mock.TestLog.PrintVars)
	}
}

func TestStartNextJobFails(t *testing.T) {
	mock.TestLog.Reset()

	s := setup()
	err := errors.New("Test error")

	backendToMockBackend(s).NextJobCanceledErr = err
	run(t, s, nil)

	s.Stop()

	if !mock.TestLog.PrintReceived(fmt.Sprint("Failed to retrieve next job: ", err)) {
		t.Errorf("Erroneously logged: `%v`", mock.TestLog.PrintVars)
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
	mock.TestLog.Reset()

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

	if !mock.TestLog.PrintReceived(wingman.SignalReceivedMsg()) {
		t.Errorf("Expected signal received message: %s", mock.TestLog.PrintVars)
	}
}

func TestStopByManySignals(t *testing.T) {
	mock.TestLog.Reset()

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
		time.Sleep(500 * time.Millisecond)
		return nil
	})

	wingman.ManagerBackend(s).PushJob(mockJob)

	time.Sleep(10 * time.Millisecond)
	err = process.Signal(syscall.SIGUSR2)
	if err != nil {
		t.Fatal("Could not send first signal to test process: ", err)
	}

	time.Sleep(10 * time.Millisecond)
	err = process.Signal(syscall.SIGUSR2)
	if err != nil {
		t.Fatal("Could not send second signal to test process: ", err)
	}

	wg.Wait()

	time.Sleep(10 * time.Millisecond)
	if !mock.TestLog.FatalReceived(wingman.SignalHardShutdownMsg()) {
		t.Errorf("Expected signal received message: %s", mock.TestLog.PrintVars)
	}

	mock.CleanupHandlers()
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
	mgr, err := wingman.NewManager(
		wingman.ManagerOptions{
			Backend: backend,
			Queues:  []string{"*"},
			Signals: []os.Signal{syscall.SIGUSR2},
			PoolOptions: goroutine.PoolOptions{
				Concurrency: 1,
			},
		},
	)
	if err != nil {
		panic(err)
	}

	return mgr
}

func backendToMockBackend(s *wingman.Manager) *mock.Backend {
	return wingman.ManagerBackend(s).(*mock.Backend)
}
