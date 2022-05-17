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

// ContextKey defines the type for our context value keys. We need this per the
// documentation for Context that states that the keys should not be a standard
// type.
type ContextKey string

// ContextProcessorIDKey is the key under which the processor ID is stored.
const ContextProcessorIDKey = ContextKey("processorID")

type Processor interface {
	Id() string
	SendJob(InternalJob) error
	Results() <-chan ResultMessage
	Kill() error
	Done() <-chan struct{}
}
