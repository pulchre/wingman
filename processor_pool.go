package wingman

type NewProcessorPoolFunc func(interface{}) (ProcessorPool, error)

var NewProcessorPool NewProcessorPoolFunc

type ProcessorPool interface {
	// Get blocks until a processor is available then returns one from the
	// pool.
	Get() Processor

	// Put releases the processor with the given back to the pool.
	Put(string)

	// Wait blocks until a processor is available in the pool.
	Wait()

	// Cancel unblocks a call to wait.
	Cancel()

	// Close shuts down all available processors, delete all dead ones, and
	// blocks until all busy ones are Put back into the pool.
	Close()

	// ForceClose shuts down all available processors, delete all dead,
	// ones, unblocks any previous call to Close, and does not block even
	// if there are still busy processors.
	ForceClose()
}
