package wingman

const (
	ProcessorStatusStarting = iota
	ProcessorStatusIdle
	ProcessorStatusWorking
	ProcessorStatusStopping
	ProcessorStatusDead
)

var ProcessorStatuses = map[ProcessorStatus]string{
	ProcessorStatusStarting: "starting",
	ProcessorStatusIdle:     "idle",
	ProcessorStatusWorking:  "working",
	ProcessorStatusStopping: "stopping",
	ProcessorStatusDead:     "dead",
}

type ProcessorStatus int

type Processor interface {
	Id() string
	SendJob(InternalJob) error
	Results() <-chan ResultMessage
	Kill() error
	Done() <-chan struct{}
}
