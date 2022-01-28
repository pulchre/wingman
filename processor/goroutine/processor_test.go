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
	if !mock.TestLog.PrintReceived(fmt.Sprintf("Processor id=%v status=starting", p.Id())) {
		t.Errorf("Expected to receive `starting` status for processor")
	}

	if !mock.TestLog.PrintReceived(fmt.Sprintf("Processor id=%v status=idle", p.Id())) {
		t.Errorf("Expected to receive `idle` status for processor")
	}
}

func TestStop(t *testing.T) {
	mock.TestLog.Reset()
	p := NewProcessor()

	p.Start()
	p.Stop()
	if !mock.TestLog.PrintReceived(fmt.Sprintf("Processor id=%v status=stopping", p.Id())) {
		t.Errorf("Expected to receive `stopping` status for processor")
	}

	time.Sleep(10 * time.Millisecond)
	if !mock.TestLog.PrintReceived(fmt.Sprintf("Processor id=%v status=dead", p.Id())) {
		t.Errorf("Expected to receive `dead` status for processor")
	}
}
