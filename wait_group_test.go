package wingman_test

import (
	"testing"

	"github.com/pulchre/wingman"
)

func TestWaitGroupAddNegative(t *testing.T) {
	defer func() {
		if msg := recover(); msg != nil {
			t.Error("Should not panic: ", msg)
		}
	}()

	wg := &wingman.WaitGroup{}

	wg.Add(1)
	wg.Add(-21)
	wg.Wait()
}

func TestWaitGroupClear(t *testing.T) {
	defer func() {
		if msg := recover(); msg != nil {
			t.Error("Should not panic: ", msg)
		}
	}()

	wg := &wingman.WaitGroup{}

	wg.Add(23)
	wg.Clear()
	wg.Wait()
}

func TestWaitGroupDone(t *testing.T) {
	defer func() {
		if msg := recover(); msg != nil {
			t.Error("Should not panic: ", msg)
		}
	}()

	wg := &wingman.WaitGroup{}

	wg.Add(2)
	wg.Done()
	wg.Done()
	wg.Done()
	wg.Wait()
}
