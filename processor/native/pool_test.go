package native

import (
	"testing"
	"time"
)

func TestNewPool(t *testing.T) {
	table := []struct {
		in          PoolOptions
		err         error
		processes   int
		concurrency int
	}{
		{DefaultOptions(), nil, 1, 1},
		{DefaultOptions().SetConcurrency(0).SetProcesses(0), nil, 1, 1},
	}

	for _, c := range table {
		pool, err := NewPool(c.in)
		CheckError(c.err, err, t)

		p := pool.(*Pool)

		if len(p.processes) != c.processes {
			t.Fatalf("Expected %d processes. Got: %d", c.processes, len(p.processes))

		}

		// Sleep so that the spawned subprocess has time to connect
		time.Sleep(250 * time.Millisecond)

		p.ForceClose()
	}
}

func TestGet(t *testing.T) {
	pool, err := NewPool(DefaultOptions())
	if err != nil {
		panic(err)
	}

	p := pool.Get()
	if p == nil {
		t.Fatalf("Expected processor got nil")
	}

	go func() {
		pool.Cancel()
	}()

	p2 := pool.Get()
	if p2 != nil {
		t.Fatalf("Expected nil processor, got processor with id: %s", p2.ID())
	}

	pool.ForceClose()
}

func TestPut(t *testing.T) {
	// We assume that DefaultOptions has 1 processors and 1 concurrency.
	pool, err := NewPool(DefaultOptions())
	if err != nil {
		panic(err)
	}

	p := pool.Get()
	if p == nil {
		t.Fatal("Failed to get processor")
	}

	pool.Put(p.ID())

	// Nothing should happen
	pool.Put(p.ID())

	p2 := pool.Get()
	if p2.ID() != p.ID() {
		t.Errorf("Unknown processor returned: %s. Expected: %s", p2.ID(), p.ID())
	}

	pool.ForceClose()
}

func CheckError(expect, got error, t *testing.T) {
	if expect == nil {
		if got != nil {
			t.Fatalf("Expected nil error. Got: `%v`", got)
		}
	} else {
		if got == nil {
			t.Fatalf("Expected `%v` error. Got nil", expect)
		} else if got.Error() != expect.Error() {
			t.Fatalf("Expected error `%v`. Got: `%v`", expect, got)
		}
	}
}
