package wingman

type ProcessorPool interface {
	// Get locks and returns a processor from the pool.
	Get() Processor

	// Put releases the processor with the given back to the pool.
	Put(string)

	// Wait blocks until a processor is available in the pool.
	Wait()

	// Cancel unblocks a call to wait.
	Cancel()
}
