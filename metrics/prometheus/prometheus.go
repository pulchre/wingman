package prometheus

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/pulchre/wingman"
)

var wg sync.WaitGroup

const (
	successfulJobsTotal = "successful_jobs_total"
	failedJobTotal      = "failed_jobs_total"
)

var collectors = []prometheus.Collector{
	successfulJobs,
	failedJobs,
}

var (
	successfulJobs = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: successfulJobsTotal,
			Help: "Number of jobs successfully processed.",
		},
	)

	failedJobs = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: failedJobTotal,
			Help: "Number of jobs which failed during processing.",
		},
	)
)

type Options struct {
	Bind            string
	Port            int
	UpdateFrequency time.Duration
}

func DefaultOptions() Options {
	return Options{
		Port:            8080,
		UpdateFrequency: 2 * time.Second,
	}
}

func (o Options) SetBind(bind string) Options {
	o.Bind = bind
	return o
}

func (o Options) SetPort(port int) Options {
	o.Port = port
	return o
}

func (o Options) SetUpdateFrequency(d time.Duration) Options {
	o.UpdateFrequency = d
	return o
}

func (o Options) ListeningString() string {
	return fmt.Sprintf("%s:%d", o.Bind, o.Port)
}

func init() {

	for _, c := range collectors {
		prometheus.MustRegister(c)
	}
}

func watch(ctx context.Context, backend wingman.Backend, opts Options) {
	defer wg.Done()
	ticker := time.NewTicker(time.Duration(opts.UpdateFrequency))
	defer ticker.Stop()

	lastCounter := map[string]uint64{
		successfulJobsTotal: 0,
		failedJobTotal:      0,
	}

	for {
		sucessfulNew := backend.SuccessfulJobs()
		successfulJobs.Add(float64(sucessfulNew - lastCounter[successfulJobsTotal]))
		lastCounter[successfulJobsTotal] = sucessfulNew

		failedNew := backend.FailedJobs()
		failedJobs.Add(float64(failedNew - lastCounter[failedJobTotal]))
		lastCounter[failedJobTotal] = failedNew

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

// Serve begins watching the exported metrics at the interval specified by the
// UpdateFrequency option. This should be imported by a standalone program.
//
// Here is a simple example:
//
//	package main
//
//	import (
//		"log"
//
//		r "github.com/gomodule/redigo/redis"
//		"github.com/pulchre/wingman/backend/redis"
//		"github.com/pulchre/wingman/metrics/prometheus"
//	)
//
//	func main() {
//		backend, err := redis.Init(redis.Options{
//			Dial:            func() (r.Conn, error) { return r.Dial("tcp", "localhost:6379") },
//			BlockingTimeout: 10,
//		})
//		if err != nil {
//			log.Fatal(err)
//		}
//
//		prometheus.Serve(backend, prometheus.DefaultOptions())
//	}
func Serve(backend wingman.Backend, opts Options) {
	wg.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	go watch(ctx, backend, opts)
	defer wg.Wait()
	defer cancel()

	http.Handle("/metrics", promhttp.Handler())

	wingman.Log.Fatal().Err(http.ListenAndServe(opts.ListeningString(), nil))
}
