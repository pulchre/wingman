package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pulchre/wingman"
	"github.com/pulchre/wingman/mock"
	"github.com/redis/go-redis/v9"
)

var (
	redisHost = ""
	redisPort = "6379"
)

func init() {
	redisHost = os.Getenv("REDIS_HOST")

	if os.Getenv("REDIS_PORT") != "" {
		redisPort = os.Getenv("REDIS_PORT")
	}
}

func TestInit(t *testing.T) {
	t.Run("Success", testInitSuccess)
	t.Run("Fail", testInitFail)
}

func testInitSuccess(t *testing.T) {
	timeout := 10 * time.Second
	config := Config{BlockingTimeout: timeout}
	backend, err := Init(Options{Config: config})
	if backend == nil {
		t.Error("Expected backend to be initialized")
	} else if backend.config != config {
		t.Errorf("Expected backend config to be %v, got %v", config, backend.config)
	}

	if err != nil {
		t.Error("Expected err to be nil, got", err)
	}
}

func testInitFail(t *testing.T) {
	expected := errors.New("dial tcp [::1]:1111: connect: connection refused")

	backend, err := Init(Options{
		Options: redis.Options{
			Addr: "localhost:1111",
		},
	})
	if backend != nil {
		t.Error("Expected backend to be nil")
	}

	if err == nil {
		t.Errorf("Expected err to be %v, got nil", err)
	} else if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("Expected error to be %v, got %v", expected, errors.Unwrap(err))
	}
}

func TestPushJob(t *testing.T) {
	t.Run("Success", testPushJobSuccess)
	t.Run("NilError", testPushJobNil)
}

func testPushJobSuccess(t *testing.T) {
	b := testClient()
	defer b.Close()

	job := mock.NewJob()

	err := b.PushJob(job)
	if err != nil {
		t.Fatal("Expected nil error. Got: ", err)
	}

	retrievedJob, err := b.PopJob(context.Background(), job.Queue())
	if err != nil {
		t.Fatal("PopJob returned nil job")
	}

	if job.Data != retrievedJob.Job.(*mock.Job).Data {
		t.Errorf("Expected job to have been pushed on the queue %v, got: %v", job, retrievedJob.Job)
	}
}

func testPushJobNil(t *testing.T) {
	b := testClient()
	defer b.Close()

	err := b.PushJob(nil)
	if err == nil {
		t.Fatal("Expected error got nil")
	}
}

func TestPopJob(t *testing.T) {
	t.Run("Success", testPopJobSuccess)
	t.Run("Timeout", testPopJobTimeout)
	t.Run("Cancel", testPopJobContextDone)
}

func testPopJobSuccess(t *testing.T) {
	b := testClient()
	defer b.Close()

	initialJob, _ := wingman.WrapJob(mock.NewJob())

	raw, err := json.Marshal(initialJob)
	if err != nil {
		panic(err)
	}

	b.RPush(context.Background(), fmtQueueKey(initialJob.Job.Queue()), raw)

	job, err := b.PopJob(context.Background(), mock.DefaultQueue)
	if err != nil {
		t.Errorf("Expected err to be nil. Got: %v", err)
	}

	if job == nil {
		t.Fatalf("Expected a job to not be nil")
	}

	if job.ID != initialJob.ID ||
		job.Queue() != initialJob.Queue() ||
		job.TypeName != initialJob.TypeName ||
		job.Job == initialJob.Job {
		t.Errorf("Expected input and output to be equal. In: %v. Out: %v", raw, job)
	}
}

func testPopJobTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test")
	}

	b := testClient()
	defer b.Close()

	b.config.BlockingTimeout = 1 * time.Second

	job, err := b.PopJob(context.Background(), mock.DefaultQueue)
	if job != nil {
		t.Errorf("Expected job to be nil: %v", job)
	}

	if err != wingman.ErrCanceled {
		t.Errorf("Expected error nil returned: %v", err)
	}
}

func testPopJobContextDone(t *testing.T) {
	b := testClient()
	defer b.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	start := time.Now()

	job, err := b.PopJob(ctx, mock.DefaultQueue)
	end := time.Now()
	if job != nil {
		t.Errorf("Expected job to be nil: %v", job)
	}

	if err != wingman.ErrCanceled {
		t.Errorf("Expected error nil returned: %v", err)
	}

	if end.Sub(start) > 1*time.Second {
		t.Error("Timed-out at 2 seconds")
	}
}

func TestLockJob(t *testing.T) {
	t.Run("Lock", testLockJobLock)
	t.Run("Hold", testLockJobHold)
}

func testLockJobLock(t *testing.T) {
	b := testClient()
	defer b.Close()

	job1 := mock.NewWrappedJob()
	job1.Job.(*mock.Job).LockKeyOverride = "lock"
	job1.Job.(*mock.Job).ConcurrencyOverride = 1

	lockID, err := b.LockJob(job1)
	if err != nil {
		t.Fatal("LockJob expected nil error got: ", err)
	}

	if lockID <= 0 {
		t.Error("Unable to lock job")
	}

	exists, err := b.Exists(context.Background(), fmtLockKey(job1.Job.LockKey(), int(lockID))).Result()
	if err != nil {
		t.Fatal("Unable to check if lock key exists ", err)
	}

	if exists != 1 {
		t.Error("Failed to lock job")
	}

	job2 := mock.NewWrappedJob()
	lockID, err = b.LockJob(job2)
	if err != nil {
		t.Fatal("LockJob expected nil error got: ", err)
	}

	if lockID == wingman.JobHeld {
		t.Error("Job with concurrency of 0 should not be held")
	} else if lockID > 0 {
		t.Error("Job with concurrency of 0 should not be locked")
	}
}

func testLockJobHold(t *testing.T) {
	b := testClient()
	defer b.Close()

	job1 := mock.NewWrappedJob()
	job1.Job.(*mock.Job).LockKeyOverride = "lock"
	job1.Job.(*mock.Job).ConcurrencyOverride = 1

	lockID, err := b.LockJob(job1)
	if err != nil {
		t.Fatal("LockJob expected nil error got: ", err)
	}
	job1.LockID = lockID

	if int(lockID) <= 0 {
		t.Error("Unable to lock job")
	}

	job2 := mock.NewWrappedJob()
	job2.Job.(*mock.Job).LockKeyOverride = "lock"
	job2.Job.(*mock.Job).ConcurrencyOverride = 1

	lockID, err = b.LockJob(job2)
	if err != nil {
		t.Fatal("LockJob expected nil error got: ", err)
	}

	if lockID != wingman.JobHeld {
		t.Error("Expected job to be held")
	}

	exists, err := b.Exists(context.Background(), fmtLockKey(job2.Job.LockKey(), int(lockID))).Result()
	if err != nil {
		t.Fatal("Unable to check if lock key exists ", err)
	}

	if exists == 1 {
		t.Error("Should not have locked job")
	}

	err = b.ReleaseJob(job1)
	if err != nil {
		t.Fatal("ReleaseJob expected nil error got: ", err)
	}

	length, err := b.LLen(context.Background(), fmtQueueKey(job2.Queue())).Result()
	if err != nil {
		panic(err)
	}

	if length != 1 {
		t.Error("Expected held job to be moved back to the queue ")
	}
}

func TestReleaseJob(t *testing.T) {
	t.Run("NotLocked", testReleaseJobNotLocked)
	t.Run("NoLockKey", testReleaseJobNoLockKey)
	t.Run("ConcurrencyLessThanOne", testReleaseJobConcurrencyLessThanOne)
	t.Run("LockNotRequired", testReleaseJobLockNotRequired)
}

func testReleaseJobNotLocked(t *testing.T) {
	b := testClient()
	job := mock.NewWrappedJob()
	job.Job.(*mock.Job).LockKeyOverride = "lock_key"
	job.Job.(*mock.Job).ConcurrencyOverride = 1
	job.LockID = wingman.LockID(1)
	err := b.ReleaseJob(job)
	if err != nil {
		t.Fatal("Expected nil error, got: ", err)
	}
}

func testReleaseJobNoLockKey(t *testing.T) {
	b := testClient()
	job := mock.NewWrappedJob()
	job.Job.(*mock.Job).LockKeyOverride = ""
	job.Job.(*mock.Job).ConcurrencyOverride = 1
	job.LockID = wingman.LockID(1)
	err := b.ReleaseJob(job)
	if err != nil {
		t.Fatal("Expected nil error, got: ", err)
	}
}

func testReleaseJobConcurrencyLessThanOne(t *testing.T) {
	b := testClient()
	job := mock.NewWrappedJob()
	job.Job.(*mock.Job).LockKeyOverride = "lock_key"
	job.Job.(*mock.Job).ConcurrencyOverride = 0
	job.LockID = wingman.LockID(1)
	err := b.ReleaseJob(job)
	if err != nil {
		t.Fatal("Expected nil error, got: ", err)
	}
}

func testReleaseJobLockNotRequired(t *testing.T) {
	b := testClient()
	job := mock.NewWrappedJob()
	job.Job.(*mock.Job).LockKeyOverride = "lock_key"
	job.Job.(*mock.Job).ConcurrencyOverride = 1
	job.LockID = wingman.LockNotRequired
	err := b.ReleaseJob(job)
	if err != nil {
		t.Fatal("Expected nil error, got: ", err)
	}
}

func TestProcessJob(t *testing.T) {
	t.Run("Success", testProcessJobSuccess)
}

func testProcessJobSuccess(t *testing.T) {
	b := testClient()
	defer b.Close()

	initialJob := mock.NewWrappedJob()

	err := b.ProcessJob(initialJob)
	if err != nil {
		t.Fatal("Expected nil error, got: ", err)
	}

	retrieved, err := b.Get(context.Background(), fmtProcessingKey(initialJob.ID)).Result()
	if err != nil {
		t.Fatal("Failed to retrieve job from redis ", err)
	}

	job, err := wingman.InternalJobFromJSON([]byte(retrieved))
	if err != nil {
		t.Fatal("Failed to unmarshal job ", err)
	}

	if job.ID != initialJob.ID {
		t.Errorf("Expected job at processing key to match the one sent. Expected: %v, got: %v", initialJob.ID, job.ID)
	}
}

func TestClearProcessorSuccess(t *testing.T) {
	b := testClient()
	defer b.Close()

	jobID := "job1"

	err := b.Set(context.Background(), fmtProcessingKey(jobID), "some job", 0).Err()
	if err != nil {
		t.Fatal("Failed to push data to redis with error: ", err)
	}

	err = b.ClearJob(jobID)
	if err != nil {
		t.Error("Expected nil error, got: ", err)
	}

	keys, err := b.Keys(context.Background(), fmtProcessingKey(jobID)).Result()
	if err != nil {
		t.Fatal("Failed to retrieve key with error: ", err)
	}

	if len(keys) > 0 {
		t.Error("Expected no response, got: ", keys)
	}
}

func TestFailJob(t *testing.T) {
	b := testClient()
	defer b.Close()

	job := mock.NewWrappedJob()

	raw, err := json.Marshal(job)
	if err != nil {
		panic(err)
	}

	err = b.Set(context.Background(), fmtProcessingKey(job.ID), raw, 0).Err()
	if err != nil {
		t.Fatal("Failed to push data to redis with error: ", err)
	}

	err = b.FailJob(job)
	if err != nil {
		t.Error("Expected nil error, got: ", err)
	}

	keys, err := b.Keys(context.Background(), fmtFailedKey(job.ID)).Result()
	if err != nil {
		t.Fatal("Failed to retrieve key with error: ", err)
	}

	if len(keys) != 1 {
		t.Error("Expected 1 job to be moved, got: ", len(keys))
	}
}

func TestPeek(t *testing.T) {
	t.Run("Success", testPeekSuccess)
	t.Run("SuccessWithoutJob", testPeekSuccessWithoutJob)
}

func testPeekSuccess(t *testing.T) {
	b := testClient()
	defer b.Close()

	initialJob := mock.NewWrappedJob()

	raw, err := json.Marshal(initialJob)
	if err != nil {
		panic(err)
	}

	b.RPush(context.Background(), fmtQueueKey(initialJob.Queue()), raw)
	size := b.Size(mock.DefaultQueue)

	job, err := b.Peek(mock.DefaultQueue)
	if err != nil {
		t.Errorf("Expected err to be nil. Got: %v", err)
	}

	if job == nil {
		t.Fatalf("Expected job to not be nil")
	}

	if job.ID != initialJob.ID ||
		job.Queue() != initialJob.Queue() ||
		job.TypeName != initialJob.TypeName ||
		job.Job == initialJob.Job {
		t.Errorf("Expected input and output to be equal. In: %v. Out: %v", raw, job)
	}

	if b.Size(mock.DefaultQueue) != size {
		t.Error("Expected job to not be removed from the queue")
	}
}

func testPeekSuccessWithoutJob(t *testing.T) {
	b := testClient()
	defer b.Close()

	job, err := b.Peek(mock.DefaultQueue)
	if err != nil {
		t.Errorf("Expected err to be nil. Got: %v", err)
	}

	if job != nil {
		t.Error("Expected job to be nil: ", job)
	}

	size := b.Size(mock.DefaultQueue)
	if size != 0 {
		t.Error("Expected queue to be empty ", size)
	}
}

func TestSize(t *testing.T) {
	b := testClient()
	defer b.Close()

	size := b.Size(mock.DefaultQueue)

	if size != 0 {
		t.Fatal("Expected size to be 0. Got: ", size)
	}

	initialJob := mock.NewWrappedJob()

	raw, err := json.Marshal(initialJob)
	if err != nil {
		panic(err)
	}

	b.RPush(context.Background(), fmtQueueKey(initialJob.Queue()), raw)
	size = b.Size(mock.DefaultQueue)

	if size != 1 {
		t.Fatal("Expected size to be 1. Got: ", size)
	}
}

func testClient() *Backend {
	client, err := Init(Options{
		Config: Config{
			BlockingTimeout: 2 * time.Second,
		},
	})
	if err != nil {
		panic(fmt.Sprint("Failed to create testing client: ", err))
	}

	err = client.Client.FlushAll(context.Background()).Err()
	if err != nil {
		panic(fmt.Sprint("Failed to create testing client: ", err))
	}

	return client
}
