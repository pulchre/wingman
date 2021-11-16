package wingman

import "sync"

type WaitGroup struct {
	wg    sync.WaitGroup
	mu    sync.Mutex
	count int
}

func (w *WaitGroup) Add(i int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.count+i < 0 {
		i = -1 * w.count
	}

	w.wg.Add(i)
	w.count += i
}

func (w *WaitGroup) Done() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.count == 0 {
		return
	}

	w.wg.Done()
	w.count -= 1
}

func (w *WaitGroup) Clear() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.wg.Add(-1 * w.count)
	w.count = 0
}

func (w *WaitGroup) Wait() {
	w.wg.Wait()
}
