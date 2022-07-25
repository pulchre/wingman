# Wingman Installation

Simply import the base package into your code

`import "github.com/pulchre/wingman"`

# Suggested structure

We recommend that each job type be registered in a single package with
`wingman.RegisterJobType` in the init function. This package will need to be
imported by both the application and the manager.

# Application

To enqueue jobs:
1. Import `github.com/pulchre/wingman`
1. Register job types
1. Import the backend package (e.g., github.com/wingman/backend/redis)
1. Import the backend support package (e.g., github.com/gomodule/redigo/redis)
1. Initialize the backend support package
1. To enqueue a job, call `backend.PushJob(job)`

# Manager

To build a manager binary:
1. Import `github.com/pulchre/wingman`
1. Register job types
1. Import the backend package (e.g., github.com/wingman/backend/redis)
1. Import the backend support package (e.g., github.com/gomodule/redigo/redis)
1. Initialize the backend `redis.Init(redisOpts)`
1. Initialize a new manager with the desired options,
   `wingman.NewManager(opts)`
1. Start the manager, `manager.Start()`
