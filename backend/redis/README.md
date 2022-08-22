# Redis

This is the basic initialization of this backend:
```go
backend, err := redis.Init(redis.Options{
	Dial:            func() (r.Conn, error) { return r.Dial("tcp", "localhost:6379") },
})
if err != nil {
	return err
}
```

This is required both for the application and for the manager.

The options available are:
* `Dial` `func() { redis.Conn, error }` Dial is an application supplied function
  for creating and configuring a connection. The connection returned from Dial
  must not be in a special state (subscribed to pubsub channel, transaction
  started, ...).
* `BlockingTimeout` `float32` - Timeout for blocking commands to Redis.
* `MaxIdle` `int` - Maximum number of idle connections in the pool.
* `MaxActive` `int` - Maximum number of connections allocated by the pool at a
  given time. When zero, there is no limit on the number of connections in the
  pool.
* `IdleTimeout` `time.Duration` - Close connections after remaining idle for
  this duration. If the value is zero, then idle connections are not closed.
  Applications should set the timeout to a value less than the server's
  timeout.
* `MaxConnLifetime` `time.Duration` - Close connections older than this
  duration. If the value is zero, then the pool does not close connections
  based on age.

## Internals

Here is an overview of the stages that a job goes through in the Redis backend.
1. `RPUSH` onto the queue.
1. `BLMOVE` from the queue to staging which is a Redis key
(wingman:staging:STAGING-ID) specific to the job.
1. `LMOVE` from staging to processing which is also a Redis key
(wingman:processing:JOB-ID) specific to the job.
1. When the job finishes:
	* Successfully: `DEL` the processing key.
	* Failed: `LMOVE` the processing key to a failed Redis Key
	(wingman:failed:JOB-ID).

We use `BLMOVE` so we can ensure that we don't accidentally lose the in the
event of a crash between popping the job off the queue and moving it to
processing. Moreover, we use a new staging ID because if multiple mangers are
watching a queue, we don't want it to be picked up more than once.

The `BLMOVE` command is also block. We have a context that, if cancelled, will
unblock the client. A timeout can also be set to occasionally unblock.
