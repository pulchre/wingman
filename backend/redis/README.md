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

