package native

import (
	"testing"

	"github.com/google/uuid"
	"github.com/pulchre/wingman/mock"
)

func TestHandle(t *testing.T) {
	pool, err := NewPool(DefaultOptions())
	if err != nil {
		panic(err)
	}
	defer pool.ForceClose()

	p := pool.Get()
	if p == nil {
		t.Fatalf("Expected processor got nil")
	}
	defer pool.Put(p.ID())

	if p.Results() != nil {
		t.Error("Expected nil Results chan when no job is sent")
	}

	if p.Done() != nil {
		t.Error("Expected nil Done chan when no job is sent")
	}

	j := mock.NewWrappedJob()
	p.SendJob(j)

	if p.Results() == nil {
		t.Error("Expected non-nil Results chan when job is sent")
	}

	if p.Done() == nil {
		t.Error("Expected non-nil Done chan when job is sent")
	}

	results := <-p.Results()

	if results.Job.ID == uuid.Nil {
		t.Error("Expected results got nil")
	}

	<-p.Done()
}
