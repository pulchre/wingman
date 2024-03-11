# Redis

This is the basic initialization of this backend:
```go
import (
	"log"

	"github.com/pulchre/wingman/backend/redis"
)

func main() {
	backend, err := redis.Init(redis.Options{})
	if err != nil {
		log.Fatal(err)
	}
}
```

This is required both for the application and for the manager.

The options available are:
* A [go-redis](https://pkg.go.dev/github.com/redis/go-redis/v9#Options) options struct.
* `BlockingTimeout` `time.Duration` - Timeout for blocking commands to Redis.

## Internals

Here is an overview of the stages that a job goes through in the Redis backend.
1. `PushJob`: `RPUSH` onto the queue.
1. `PopJob`: `BLPOP` pops the next job off the queue. If no job is on the 
queue, it blocks until the either a job is pushed, the timeout is reached, or
the context is cancelled.
1. `LockJob`: If the job has no `LockKey` or concurrency is less than 1, we
return `wingman.LockNotRequired` (0). A job may have n concurrency. The
locks are a Redis key (wingman:lock:LOCK_KEY:LOCK-ID). We randomly try a lock
until we exhaust all the locks or get one. If we cannot get a lock, we move it
to the held queue (wingman:held:QUEUE).
1. `ProcessJob`: `SET` set the processing key to the job.
(wingman:processing:JOB-ID)
1. `ReleaseJob`: `DEL` the lock key and `LMOVE` a job from the held queue to
the front of the queue.
1. `ClearJob`: `DEL` the processing key
1. `FailJob`: `DEL` & `SET`. first it deletes the processing key, then sets
a failed key to the job (wingman:failed:JOB-ID).
