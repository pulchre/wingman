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

type NewProcessorFunc func() Processor

var NewProcessor NewProcessorFunc

type Processor interface {
	Id() string
	Start() error
	SendJob(InternalJob) error
	Results() <-chan ResultMessage
	Stop() error
	Kill() error
	StatusChange() <-chan ProcessorStatus
}
