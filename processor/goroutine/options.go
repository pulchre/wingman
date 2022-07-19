package goroutine

type ProcessorOptions struct {
	Concurrency int
}

func DefaultOptions() ProcessorOptions {
	return ProcessorOptions{}
}

func (opts ProcessorOptions) SetConcurrency(concurrency int) ProcessorOptions {
	opts.Concurrency = concurrency
	return opts
}
