package goroutine

import (
	"testing"
	"time"
)

func TestNewPool(t *testing.T) {
	options := PoolOptions{Concurrency: 0}

	p, err := NewPool(options)
	if err != nil {
		t.Error("Expected nil error. Got: ", err)

	}

	pool := p.(*Pool)
	if pool.options.Concurrency != 1 {
		t.Error("Expected Concurrency to be set to 1, got: ", pool.options.Concurrency)
	}
}

func TestPoolGet(t *testing.T) {
	p, _ := NewPool(PoolOptions{})

	proc := p.Get()

	if proc == nil {
		t.Error("Expected returned processor. Got nil")
	}

	go func() { p.Cancel() }()

	proc2 := p.Get()
	if proc2 != nil {
		t.Error("Expected nil returned processor")
	}
}

func TestPoolPut(t *testing.T) {
	p, _ := NewPool(PoolOptions{})

	proc := p.Get()
	if proc == nil {
		t.Error("Expected returned processor. Got nil")
	}

	p.Put(proc.ID())

	proc2 := p.Get()
	if proc.ID() != proc2.ID() {
		t.Errorf("Expected processor to be reused with id: %s. Got: %s", proc.ID(), proc2.ID())
	}

	// Should not panic!
	p.Put("invalid")
}

// This tests that the close method doesn't hang.
func TestPoolClose(t *testing.T) {
	p, _ := NewPool(PoolOptions{Concurrency: 5})

	proc1 := p.Get()
	proc2 := p.Get()
	proc3 := p.Get()
	proc4 := p.Get()

	go func() {
		p.Put(proc1.ID())
		p.Put(proc2.ID())
		p.Put(proc3.ID())
		p.Put(proc4.ID())
	}()

	p.Close()
}

// This tests that the close method doesn't hang.
func TestPoolForceClose(t *testing.T) {
	p, _ := NewPool(PoolOptions{Concurrency: 5})

	p.Get()
	p.Get()
	p.Get()
	p.Get()

	go func() { p.Close() }()
	time.Sleep(25 * time.Millisecond)

	p.ForceClose()
}
