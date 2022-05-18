package goroutine

import (
	"fmt"
	"testing"
	"time"

	"github.com/pulchre/wingman/mock"
)

func TestStart(t *testing.T) {
	mock.TestLog.Reset()
	p := NewProcessor()

	p.Start()
	if !mock.TestLog.PrintReceived(fmt.Sprintf("Processor id=%v status=starting", p.ID())) {
		t.Errorf("Expected to receive `starting` status for processor")
	}

	if !mock.TestLog.PrintReceived(fmt.Sprintf("Processor id=%v status=idle", p.ID())) {
		t.Errorf("Expected to receive `idle` status for processor")
	}
}

func TestStop(t *testing.T) {
	mock.TestLog.Reset()
	p := NewProcessor()

	p.Start()
	p.Stop()
	if !mock.TestLog.PrintReceived(fmt.Sprintf("Processor id=%v status=stopping", p.ID())) {
		t.Errorf("Expected to receive `stopping` status for processor")
	}

	time.Sleep(10 * time.Millisecond)
	if !mock.TestLog.PrintReceived(fmt.Sprintf("Processor id=%v status=dead", p.ID())) {
		t.Errorf("Expected to receive `dead` status for processor")
	}
}

func TestSendJob(t *testing.T) {
	mock.TestLog.Reset()
	p := NewProcessor()

	p.Start()

	job := mock.NewWrappedJob()
	err := p.SendJob(job)
	if err != nil {
		t.Fatalf("goroutine SendJob should never return a non-nil error: %v", err)
	}

	if !mock.TestLog.PrintReceived(fmt.Sprintf("Processor id=%v status=working", p.ID())) {
		t.Errorf("Expected to receive `working` status for processor")
	}

	res := <-p.Results()
	time.Sleep(10 * time.Millisecond)

	if !mock.TestLog.PrintReceived(fmt.Sprintf("Job %s started...", job.ID)) {
		t.Errorf("Expected to receive `working` status for processor")
	}

	if res.Error != nil {
		t.Errorf("Expected nil error got: %v", res.Error)
	}

	if res.Job.ID != job.ID {
		t.Errorf("Expected job in results %v to match sent job %v", res.Job.ID, job.ID)
	}
}
