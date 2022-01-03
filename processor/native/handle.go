package native

import (
	"sync"

	"github.com/google/uuid"
	"github.com/pulchre/wingman"
)

type handle struct {
	id      uuid.UUID
	process *process

	resultChan chan wingman.ResultMessage
	doneChan   chan struct{}

	status   wingman.ProcessorStatus
	statusMu sync.Mutex
}

func newHandle(process *process) *handle {
	return &handle{
		id:      uuid.New(),
		process: process,
	}
}

func (h *handle) Id() string { return h.id.String() }

func (h *handle) SendJob(job wingman.InternalJob) error {
	h.resultChan = make(chan wingman.ResultMessage)
	h.doneChan = make(chan struct{})
	return h.process.sendJob(job, h)
}

func (h *handle) Results() <-chan wingman.ResultMessage {
	return h.resultChan
}

func (h *handle) Kill() error {
	// TODO: Implement canceling specific handles
	return nil
}

func (h *handle) Done() <-chan struct{} {
	return h.doneChan
}

func (h *handle) sendResult(msg wingman.ResultMessage) {
	h.resultChan <- msg
}

func (h *handle) setStatus(s wingman.ProcessorStatus) {
	h.statusMu.Lock()
	defer h.statusMu.Unlock()

	h.status = s
	wingman.Log.Printf("Processor id=%v status=%v", h.Id(), wingman.ProcessorStatuses[s])
}
