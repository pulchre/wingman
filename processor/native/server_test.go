package native

import (
	"testing"
	"time"

	"github.com/pulchre/wingman"
	"github.com/pulchre/wingman/mock"
)

func TestNewServer(t *testing.T) {
	table := []struct {
		in          ProcessorOptions
		processes   int
		concurrency int
	}{
		{
			in:          DefaultOptions().SetProcesses(0).SetConcurrency(0),
			processes:   1,
			concurrency: 1,
		},
		{
			in:          DefaultOptions().SetProcesses(1).SetConcurrency(1),
			processes:   1,
			concurrency: 1,
		},
		{
			in:          DefaultOptions().SetProcesses(2).SetConcurrency(1),
			processes:   2,
			concurrency: 1,
		},
		{
			in:          DefaultOptions().SetProcesses(3).SetConcurrency(3),
			processes:   3,
			concurrency: 3,
		},
		{
			in:          DefaultOptions().SetProcesses(3).SetConcurrency(4),
			processes:   3,
			concurrency: 4,
		},
	}

	for _, c := range table {
		processor, err := NewServer(c.in)
		server := processor.(*Server)

		if err != nil {
			t.Fatal("Expected nil error, got: ", err)
		}

		if server.opts.Processes != c.processes {
			t.Errorf("Expected %d processors, got: %d", server.opts.Processes, c.processes)
		}

		if server.opts.Concurrency != c.concurrency {
			t.Errorf("Expected %d concurrency, got: %d", server.opts.Concurrency, c.concurrency)
		}
	}
}

func TestServerStart(t *testing.T) {
	table := []struct {
		in  ProcessorOptions
		err bool
	}{
		{
			in:  DefaultOptions(),
			err: false,
		},
		{
			in:  DefaultOptions().SetBind("invalid"),
			err: true,
		},
		{
			in:  DefaultOptions().SetPort(999999),
			err: true,
		},
	}

	for _, c := range table {
		s, err := NewServer(c.in)
		if err != nil {
			t.Fatal("Expected nil error, got: ", err)
		}

		err = s.Start()
		if err == nil && c.err {
			t.Fatal("Expected error, got nil")
		} else if err != nil {
			if !c.err {
				t.Fatal("Expected nil error, got: ", err)
			}

			continue
		}

		s.Close()
		<-s.Done()
	}
}

func TestServerSendMessage(t *testing.T) {
	s, err := NewServer(DefaultOptions())
	if err != nil {
		t.Fatal("Expected nil error, got: ", err)
	}

	err = s.Start()
	if err != nil {
		t.Fatal("Expected error, got nil")
	}

	// Give time for the client to connect
	time.Sleep(10 * time.Millisecond)

	job := mock.NewWrappedJob()

	err = s.SendJob(job)
	if err != nil {
		t.Error("Expected nil error, got: ", err)
	}

	res := <-s.Results()
	if res.Job.ID != job.ID {
		t.Errorf("Expected results for job with id %s, got: %s", job.ID, res.Job.ID)
	}

	s.Close()
	<-s.Done()
}

func TestServerWait(t *testing.T) {
	concurrency := 1
	processes := 1

	s, err := NewServer(DefaultOptions().SetConcurrency(concurrency).SetProcesses(processes))
	if err != nil {
		t.Fatal("Expected nil error, got: ", err)
	}

	err = s.Start()
	if err != nil {
		t.Fatal("Expected error, got nil")
	}

	jobs := []*wingman.InternalJob{
		mock.NewWrappedJob(),
		mock.NewWrappedJob(),
		mock.NewWrappedJob(),
		mock.NewWrappedJob(),
	}

	for _, j := range jobs {
		count := s.Working()
		if count > concurrency*processes {
			t.Errorf("Working jobs should not exceed concurrency * processes = %d. %d", concurrency*processes, count)
		}

		s.Wait()
		err := s.SendJob(j)
		if err != nil {
			t.Fatal("Got error sending job: ", err)
		}
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		s.Close()
	}()

	responseCount := 0
	for range s.Results() {
		responseCount++
	}

	if responseCount != len(jobs) {
		t.Error("Response count does not match job count: ", responseCount)
	}

	<-s.Done()
}
