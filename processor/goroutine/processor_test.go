package goroutine

import (
	"fmt"
	"testing"

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
