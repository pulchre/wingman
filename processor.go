package wingman

type NewProcessorFunc func(interface{}) (Processor, error)

var NewProcessor NewProcessorFunc

type Processor interface {
	Start() error
	SendJob(*InternalJob) error
	Working() int
	Results() <-chan ResultMessage
	Done() <-chan struct{}

	Wait() bool
	Cancel()
	Close()
	ForceClose()
}
