package wingman_test

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/pulchre/wingman"
	"github.com/pulchre/wingman/mock"
	"github.com/pulchre/wingman/processor/goroutine"
)

const concurrencyCount = 2
const timestampExpr = `[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}([-|\+][0-9]{2}:[0-9]{2}|Z)`
const durationExpr = `[0-9]+(\.[0-9]+)?(ns|µs|ms|s|m|h)`

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
	mock.ResetLog()

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

	mock.RegisterHandler(handlerName, func(ctx context.Context, job wingman.Job) error {
		defer jobWg.Done()

		j := job.(*mock.Job)

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

	mock.CleanupHandlers()
	s.Stop()
	wg.Wait()

	e, ok := mock.TestLog.EventByMessage("Job finished")
	if ok {
		if !e.HasStr("job_id", backendToMockBackend(s).LastAddedID) {
			t.Errorf("Expected log to have job_id %v, got %v", backendToMockBackend(s).LastAddedID, e["job_id"])
		}

		now := time.Now()
		if !e.HasTime("start", now, 500*time.Millisecond) {
			t.Errorf("Expected log to have start within 500 millisecond of %v, got %v", now.Format(time.RFC3339Nano), e["start"])
		}

		if !e.HasTime("end", now, 500*time.Millisecond) {
			t.Errorf("Expected log to have start within 500 millisecond of %v, got %v", now.Format(time.RFC3339Nano), e["end"])
		}

		if !e.HasField("duration") {
			t.Error("Expected log to have duration")
		}

		if _, ok = e["error"]; ok {
			t.Errorf("Expected log to not have error, got %v", e["error"])
		}
	} else {
		t.Error("Expected log")
	}
}

func TestStartLockJob(t *testing.T) {
	mock.ResetLog()

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
	mockJob1 := mock.NewJob()
	mockJob1.HandlerOverride = handlerName
	mockJob1.LockKeyOverride = "lock"
	mockJob1.ConcurrencyOverride = 1

	handler2Name := "handler2"
	mockJob2 := mock.NewJob()
	mockJob2.HandlerOverride = handler2Name
	mockJob2.LockKeyOverride = "lock"
	mockJob2.ConcurrencyOverride = 1
	var processed bool

	var wg1 sync.WaitGroup
	wg1.Add(1)
	busy1 := make(chan struct{})
	mock.RegisterHandler(handlerName, func(ctx context.Context, job wingman.Job) error {
		defer wg1.Done()

		j := job.(*mock.Job)

		if j != mockJob1 {
			t.Errorf("Wrong job passed to handler. Got: %v, Expected: %v", j, mockJob1)
		}

		<-busy1
		return nil
	})

	var wg2 sync.WaitGroup
	wg2.Add(1)
	busy2 := make(chan struct{})
	mock.RegisterHandler(handler2Name, func(ctx context.Context, job wingman.Job) error {
		defer wg2.Done()

		j := job.(*mock.Job)

		if j != mockJob2 {
			t.Errorf("Wrong job passed to handler. Got: %v, Expected: %v", j, mockJob1)
		}

		processed = true
		<-busy2
		return nil
	})

	s := setupWithConcurrency(2)
	wg := run(t, s, nil)
	wingman.ManagerBackend(s).PushJob(mockJob1)
	wingman.ManagerBackend(s).PushJob(mockJob2)

	time.Sleep(10 * time.Millisecond)

	close(busy1)

	if processed {
		t.Errorf("Expected job to be held")
	}

	wg1.Wait()

	close(busy2)
	wg2.Wait()
	if !processed {
		t.Errorf("Expected job to be processed")
	}

	mock.CleanupHandlers()
	s.Stop()
	wg.Wait()
}

func TestStartJobProcessError(t *testing.T) {
	mock.ResetLog()

	handlerName := "handler"
	mockJob := mock.NewJob()
	mockJob.HandlerOverride = handlerName

	var jobWg sync.WaitGroup

	jobErr := errors.New("Failed to process job")

	mock.RegisterHandler(handlerName, func(ctx context.Context, job wingman.Job) error {
		defer jobWg.Done()

		j := job.(*mock.Job)

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
	time.Sleep(50 * time.Millisecond)

	mock.CleanupHandlers()

	s.Stop()
	wg.Wait()

	e, ok := mock.TestLog.EventByMessage("Job finished")
	if ok {
		if !e.HasStr("job_id", backendToMockBackend(s).LastAddedID) {
			t.Errorf("Expected log to have job_id %v, got %v", backendToMockBackend(s).LastAddedID, e["job_id"])
		}

		now := time.Now()
		if !e.HasTime("start", now, 500*time.Millisecond) {
			t.Errorf("Expected log to have start within 500 millisecond of %v, got %v", now.Format(time.RFC3339Nano), e["start"])
		}

		if !e.HasTime("end", now, 500*time.Millisecond) {
			t.Errorf("Expected log to have start within 500 millisecond of %v, got %v", now.Format(time.RFC3339Nano), e["end"])
		}

		if !e.HasField("duration") {
			t.Error("Expected log to have duration")
		}

		if !e.HasErr(jobErr) {
			t.Errorf("Expected log to have error %v, got %v", jobErr, e["error"])
		}
	} else {
		t.Error("Expected log")
	}
}

func TestStartJobProcessPanic(t *testing.T) {
	mock.ResetLog()

	handlerName := "handler"
	mockJob := mock.NewJob()
	mockJob.HandlerOverride = handlerName

	var jobWg sync.WaitGroup

	panicMsg := "Job panicked"

	mock.RegisterHandler(handlerName, func(ctx context.Context, job wingman.Job) error {
		j := job.(*mock.Job)

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
	time.Sleep(150 * time.Millisecond)

	mock.CleanupHandlers()

	s.Stop()
	wg.Wait()

	e, ok := mock.TestLog.EventByMessage("Job finished")
	if ok {
		if !e.HasStr("job_id", backendToMockBackend(s).LastAddedID) {
			t.Errorf("Expected log to have job_id %v, got %v", backendToMockBackend(s).LastAddedID, e["job_id"])
		}

		now := time.Now()
		if !e.HasTime("start", now, 500*time.Millisecond) {
			t.Errorf("Expected log to have start within 500 millisecond of %v, got %v", now.Format(time.RFC3339Nano), e["start"])
		}

		if !e.HasTime("end", now, 500*time.Millisecond) {
			t.Errorf("Expected log to have start within 500 millisecond of %v, got %v", now.Format(time.RFC3339Nano), e["end"])
		}

		if !e.HasField("duration") {
			t.Error("Expected log to have duration")
		}

	} else {
		t.Error("Expected log")
	}
}

func TestStartNextJobFails(t *testing.T) {
	mock.ResetLog()

	s := setup()
	err := errors.New("Test error")

	backendToMockBackend(s).NextJobCanceledErr = err
	run(t, s, nil)

	s.Stop()

	e, ok := mock.TestLog.EventByMessage("Failed to retrieve next job")
	if ok {
		if !e.HasErr(err) {
			t.Errorf("Expected log to have error %v, got %v", err, e["error"])
		}
	} else {
		t.Error("Expected log")
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
	mock.ResetLog()

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

	_, ok := mock.TestLog.EventByMessage(wingman.SignalReceivedMsg())
	if !ok {
		t.Error("Expected log")
	}
}

func TestStopByManySignals(t *testing.T) {
	mock.ResetLog()

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

	_, ok := mock.TestLog.EventByMessage(wingman.SignalHardShutdownMsg())
	if !ok {
		t.Error("Expected log")
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
	return setupWithConcurrency(1)
}

func setupWithConcurrency(concurrency int) *wingman.Manager {
	backend := mock.NewBackend()
	mgr, err := wingman.NewManager(
		wingman.ManagerOptions{
			Backend: backend,
			Queues:  []string{"*"},
			Signals: []os.Signal{syscall.SIGUSR2},
			ProcessorOptions: goroutine.ProcessorOptions{
				Concurrency: concurrency,
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
