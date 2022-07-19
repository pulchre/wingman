package goroutine

import (
	"testing"
	"time"

	"github.com/pulchre/wingman"
	"github.com/pulchre/wingman/mock"
)

func TestSendJob(t *testing.T) {
	p, _ := NewProcessor(DefaultOptions().SetConcurrency(2))

	job := mock.NewWrappedJob()
	err := p.SendJob(job)
	if err != nil {
		t.Fatalf("Expected nil error, got: %v", err)
	}

	res := <-p.Results()

	if res.Error != nil {
		t.Errorf("Expected nil error got: %v", res.Error)
	}

	if res.Job.ID != job.ID {
		t.Errorf("Expected job in results %v to match sent job %v", res.Job.ID, job.ID)
	}

	if !job.Job.(*mock.Job).Processed {
		t.Error("Expected job to be processed")
	}
}

func TestWait(t *testing.T) {
	concurrency := 2
	p, _ := NewProcessor(DefaultOptions().SetConcurrency(concurrency))

	jobs := []*wingman.InternalJob{
		mock.NewWrappedJob(),
		mock.NewWrappedJob(),
		mock.NewWrappedJob(),
		mock.NewWrappedJob(),
	}

	for _, j := range jobs {
		j.Job.(*mock.Job).HandlerOverride = "10millisecondsleep"

		count := p.Working()
		if count > concurrency {
			t.Errorf("Working jobs should not exceed concurrency  = %d. %d", concurrency, count)
		}

		p.Wait()
		p.SendJob(j)
	}

	p.Close()

	responseCount := 0
	for range p.Results() {
		responseCount++
	}

	if responseCount != len(jobs) {
		t.Error("Response count does not match job count: ", responseCount)
	}

	<-p.Done()
}

func TestClose(t *testing.T) {
	p, _ := NewProcessor(DefaultOptions())

	job := mock.NewWrappedJob()
	job.Job.(*mock.Job).HandlerOverride = "10millisecondsleep"
	err := p.SendJob(job)
	if err != nil {
		t.Fatalf("goroutine SendJob should never return a non-nil error: %v", err)
	}

	p.Close()
	res := <-p.Results()
	if res.Job.ID != job.ID {
		t.Errorf("Expected correct job result %v, got: %v", job.ID, res.Job.ID)
	}
	<-p.Done()
}

func TestForceClose(t *testing.T) {
	p, _ := NewProcessor(DefaultOptions())

	job := mock.NewWrappedJob()
	job.Job.(*mock.Job).HandlerOverride = "10millisecondsleep"
	err := p.SendJob(job)
	if err != nil {
		t.Fatalf("goroutine SendJob should never return a non-nil error: %v", err)
	}

	c := make(chan struct{}, 0)
	go func() {
		defer close(c)
		p.Close()
	}()

	p.ForceClose()

	res := <-p.Results()
	if res.Job != nil {
		t.Errorf("Expected nil result, got: %v", res)
	}
	<-p.Done()
	<-c
	time.Sleep(200 * time.Millisecond)
}
