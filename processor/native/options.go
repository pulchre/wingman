package native

import "google.golang.org/grpc"

const DefaultBind = "127.0.0.1"
const DefaultPort = 11456

type ProcessorOptions struct {
	Processes   int
	Concurrency int
	Bind        string
	Port        int
	GRPCOptions []grpc.ServerOption
}

func DefaultOptions() ProcessorOptions {
	return ProcessorOptions{
		Processes:   1,
		Concurrency: 2,
		Bind:        DefaultBind,
		Port:        DefaultPort,
		GRPCOptions: make([]grpc.ServerOption, 0),
	}
}

func (o ProcessorOptions) SetProcesses(n int) ProcessorOptions {
	o.Processes = n
	return o
}

func (o ProcessorOptions) SetConcurrency(n int) ProcessorOptions {
	o.Concurrency = n
	return o
}

func (o ProcessorOptions) SetBind(bind string) ProcessorOptions {
	o.Bind = bind
	return o
}

func (o ProcessorOptions) SetPort(port int) ProcessorOptions {
	o.Port = port
	return o
}

func (o ProcessorOptions) AddGRPCOption(opt grpc.ServerOption) ProcessorOptions {
	o.GRPCOptions = append(o.GRPCOptions, opt)
	return o
}
