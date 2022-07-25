# Wingman

A resilient background job manager for Go.

# Usage

Please see [INSTALL.md](https://github.com/pulchre/wingman/blob/master/INSTALL.md)

# Backend

Provided backends

- Redis

To create a new backend,  implement the [wingman.Backend interface](https://github.com/pulchre/wingman/blob/master/backend.go#L13).

# Processors

Provided processors

- Goroutine
- Native Process (Linux, Unix only)

To create a new processor, implement the [wingman.Processor interface](https://github.com/pulchre/wingman/blob/master/processor.go#L7).

# License

Code is licensed under the [AGPL](https://github.com/pulchre/wingman/blob/master/COPYING). A license for commercial use is available for purchase at https://wingman.fyi
