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
1. `PopAndStageJob`: `BLMOVE` from the queue to staging which is a Redis key
(wingman:staging:STAGING-ID) specific to the job.
1. `LockJob`: If the job has no `LockKey` or concurrency is less than 1, we
return `wingman.LockNotRequired` (0). A job may have n concurrency. The
locks are a Redis key (wingman:lock:LOCK_KEY:LOCK-ID). We randomly try a lock
until we exhaust all the locks or get one. If we cannot get a lock, we move it 
to the held queue (wingman:held:QUEUE).
1. `ProcessJob`: `LMOVE` from staging to processing which is also a Redis key
(wingman:processing:JOB-ID) specific to the job.
1. `ReleaseJob`: `DEL` the lock key and `LMOVE` a job from the held queue to
the front of the queue.
1. `ClearJob`: `DEL` the processing key
1. `FailJob`: `LMOVE` the processing key to a failed Redis Key (wingman:failed:JOB-ID).
1. When the job finishes:

We use `BLMOVE` so we can ensure that we don't accidentally lose the in the
event of a crash between popping the job off the queue and moving it to
processing. Moreover, we use a new staging ID because if multiple mangers are
watching a queue, we don't want it to be picked up more than once.

The `BLMOVE` command also blocks. We have a context that, if cancelled, will
unblock the client. A timeout can also be set to occasionally unblock.
