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

	"github.com/google/uuid"
	"github.com/pulchre/wingman"
	"github.com/pulchre/wingman/mock"
	"github.com/redis/go-redis/v9"
)

var redisHost = ""
var redisPort = "6379"

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
	backend, err := Init(Options{BlockingTimeout: timeout})
	if backend == nil {
		t.Error("Expected backend to be initialized")
	} else if backend.timeout != timeout {
		t.Errorf("Expected backend timeout to be %v, got %v", timeout, backend.timeout)
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

	retrievedJob, err := b.PopAndStageJob(context.Background(), job.Queue())
	if err != nil {
		t.Fatal("PopAndStageJob returned nil job")
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

func TestPopAndStageJob(t *testing.T) {
	t.Run("Success", testPopAndStageJobSuccess)
	t.Run("Timeout", testPopAndStageJobTimeout)
	t.Run("Cancel", testPopAndStageJobContextDone)
}

func testPopAndStageJobSuccess(t *testing.T) {
	b := testClient()
	defer b.Close()

	initialJob, _ := wingman.WrapJob(mock.NewJob())

	raw, err := json.Marshal(initialJob)
	if err != nil {
		panic(err)
	}

	b.RPush(context.Background(), initialJob.Job.Queue(), raw)

	job, err := b.PopAndStageJob(context.Background(), mock.DefaultQueue)
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

	stagedJob, err := b.Peek(fmtStagingKey(job.StagingID))
	if err != nil {
		panic(err)
	}

	if job.ID != stagedJob.ID {
		t.Error("Expected job to be staged")
	}
}

func testPopAndStageJobTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test")
	}

	b := testClient()
	defer b.Close()

	b.timeout = 1 * time.Second

	job, err := b.PopAndStageJob(context.Background(), mock.DefaultQueue)
	if job != nil {
		t.Errorf("Expected job to be nil: %v", job)
	}

	if err != wingman.ErrCanceled {
		t.Errorf("Expected error nil returned: %v", err)
	}
}

func testPopAndStageJobContextDone(t *testing.T) {
	b := testClient()
	defer b.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	start := time.Now()

	job, err := b.PopAndStageJob(ctx, mock.DefaultQueue)
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

	job1 := newStagedJob(b)
	job1.Job.(*mock.Job).LockKeyOverride = "lock"
	job1.Job.(*mock.Job).ConcurrencyOverride = 1

	ok, err := b.LockJob(job1)
	if err != nil {
		t.Fatal("LockJob expected nil error got: ", err)
	}

	if !ok {
		t.Error("Unable to lock job")
	}

	exists, err := b.Exists(context.Background(), fmtLockKey(job1.Job.LockKey(), job1.ID)).Result()
	if err != nil {
		t.Fatal("Unable to check if lock key exists ", err)
	}

	if exists != 1 {
		t.Error("Failed to lock job")
	}

	job2 := newStagedJob(b)
	ok, err = b.LockJob(job2)
	if err != nil {
		t.Fatal("LockJob expected nil error got: ", err)
	}

	if !ok {
		t.Error("Unable to lock job")
	}
}

func testLockJobHold(t *testing.T) {
	b := testClient()
	defer b.Close()

	job1 := newStagedJob(b)
	job1.Job.(*mock.Job).LockKeyOverride = "lock"
	job1.Job.(*mock.Job).ConcurrencyOverride = 1

	ok, err := b.LockJob(job1)
	if err != nil {
		t.Fatal("LockJob expected nil error got: ", err)
	}

	if !ok {
		t.Error("Unable to lock job")
	}

	job2 := newStagedJob(b)
	job2.Job.(*mock.Job).LockKeyOverride = "lock"
	job2.Job.(*mock.Job).ConcurrencyOverride = 1

	ok, err = b.LockJob(job2)
	if err != nil {
		t.Fatal("LockJob expected nil error got: ", err)
	}

	if ok {
		t.Error("Expected job to be held")
	}

	exists, err := b.Exists(context.Background(), fmtLockKey(job2.Job.LockKey(), job2.ID)).Result()
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

	length, err := b.LLen(context.Background(), job2.Queue()).Result()
	if err != nil {
		panic(err)
	}

	if length != 1 {
		t.Error("Expected held job to be moved back to the queue")
	}
}

func TestProcessJob(t *testing.T) {
	t.Run("Success", testProcessJobSuccess)
	t.Run("NoJob", testProcessJobNoJob)
}

func testProcessJobSuccess(t *testing.T) {
	b := testClient()
	defer b.Close()

	initialJob := mock.NewWrappedJob()
	stagingID := uuid.Must(uuid.NewRandom())

	raw, err := json.Marshal(initialJob)
	if err != nil {
		t.Fatal("Failed to marshal job: ", err)
	}

	err = b.RPush(context.Background(), fmtStagingKey(stagingID.String()), raw).Err()
	if err != nil {
		t.Fatal("Failed to add job to redis: ", err)
	}

	err = b.ProcessJob(stagingID.String())
	if err != nil {
		t.Fatal("Expected nil error, got: ", err)
	}

	retrieved, err := b.LRange(context.Background(), fmtProcessingKey(initialJob.ID), 0, 1).Result()
	if err != nil {
		t.Fatal("Failed to retrieve job from redis ", err)
	}

	job, err := wingman.InternalJobFromJSON([]byte(retrieved[0]))
	if err != nil {
		t.Fatal("Failed to unmarshal job ", err)
	}

	if job.ID != initialJob.ID {
		t.Errorf("Expected job on queue to match staged job. Expected: %v, got: %v", initialJob.ID, job.ID)
	}
}

func testProcessJobNoJob(t *testing.T) {
	b := testClient()
	defer b.Close()

	stagingID := uuid.Must(uuid.NewRandom())

	err := b.ProcessJob(stagingID.String())
	if err != wingman.ErrorJobNotStaged {
		t.Errorf("Expected: %v, got: %v", wingman.ErrorJobNotStaged, err)
	}
}

func TestClearProcessorSuccess(t *testing.T) {
	b := testClient()
	defer b.Close()

	jobID := "job1"

	err := b.RPush(context.Background(), fmtProcessingKey(jobID), "some job").Err()
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

	err = b.RPush(context.Background(), fmtProcessingKey(job.ID), raw).Err()
	if err != nil {
		t.Fatal("Failed to push data to redis with error: ", err)
	}

	err = b.FailJob(job.ID)
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

func TestReenqueueStagedJob(t *testing.T) {
	t.Run("Success", testReenqueueStagedJobSuccess)
	t.Run("InvalidJob", testReenqueueStagedJobInvalidJob)
	t.Run("JobNotFound", testReenqueueStagedJobJobNotFound)
}

func testReenqueueStagedJobSuccess(t *testing.T) {
	b := testClient()
	defer b.Close()

	initialJob := mock.NewWrappedJob()
	stagingID := uuid.Must(uuid.NewRandom())

	raw, err := json.Marshal(initialJob)
	if err != nil {
		t.Fatal("Failed to marshal job: ", err)
	}

	err = b.RPush(context.Background(), fmtStagingKey(stagingID.String()), raw).Err()
	if err != nil {
		t.Fatal("Failed to add job to redis: ", err)
	}

	err = b.ReenqueueStagedJob(stagingID.String())
	if err != nil {
		t.Fatal("Expected nil error, got: ", err)
	}

	retrieved, err := b.LRange(context.Background(), initialJob.Queue(), 0, 1).Result()
	if err != nil {
		t.Fatal("Failed to retrieve job from redis ", err)
	}

	job, err := wingman.InternalJobFromJSON([]byte(retrieved[0]))
	if err != nil {
		t.Fatal("Failed to unmarshal job ", err)
	}

	if job.ID != initialJob.ID {
		t.Errorf("Expected job on queue to match staged job. Expected: %v, got: %v", initialJob.ID, job.ID)
	}
}

func testReenqueueStagedJobInvalidJob(t *testing.T) {
	b := testClient()
	defer b.Close()

	stagingID := uuid.Must(uuid.NewRandom())

	err := b.RPush(context.Background(), fmtStagingKey(stagingID.String()), "invalid job").Err()
	if err != nil {
		t.Fatal("Failed to add job to redis: ", err)
	}

	err = b.ReenqueueStagedJob(stagingID.String())
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
}

func testReenqueueStagedJobJobNotFound(t *testing.T) {
	b := testClient()
	defer b.Close()

	stagingID := uuid.Must(uuid.NewRandom())

	err := b.ReenqueueStagedJob(stagingID.String())
	if err != wingman.ErrorJobNotStaged {
		t.Errorf("Expected `%v`, got `%v`", wingman.ErrorJobNotStaged, err)
	}
}

func TestStagedJobs(t *testing.T) {
	t.Run("Success", testStagedJobsSuccess)
	t.Run("InvalidJobPayload", testStagedJobsInvalidJobPayload)
}

func testStagedJobsSuccess(t *testing.T) {
	b := testClient()
	defer b.Close()

	initialJob1 := mock.NewWrappedJob()
	initialJob2 := mock.NewWrappedJob()

	raw1, err := json.Marshal(initialJob1)
	if err != nil {
		t.Fatal("Failed to marshal job: ", err)
	}

	raw2, err := json.Marshal(initialJob2)
	if err != nil {
		t.Fatal("Failed to marshal job: ", err)
	}

	err = b.RPush(context.Background(), fmtStagingKey(uuid.Must(uuid.NewRandom()).String()), raw1).Err()
	if err != nil {
		t.Fatal("Failed to add job to redis: ", err)
	}

	err = b.RPush(context.Background(), fmtStagingKey(uuid.Must(uuid.NewRandom()).String()), raw2).Err()
	if err != nil {
		t.Fatal("Failed to add job to redis: ", err)
	}

	jobs, err := b.StagedJobs()
	if err != nil {
		t.Fatal("Error retrieving staged jobs: ", err)
	}

	unexpectedResults := func() {
		t.Errorf("Unexpected jobs returned. Expected: %v. got: %v", []*wingman.InternalJob{initialJob1, initialJob2}, jobs)
	}

	if len(jobs) == 2 {
		if initialJob1.ID != jobs[0].ID && initialJob1.ID != jobs[1].ID {
			unexpectedResults()
		}

		if initialJob2.ID != jobs[0].ID && initialJob2.ID != jobs[1].ID {
			unexpectedResults()
		}
	} else {
		unexpectedResults()
	}
}

func testStagedJobsInvalidJobPayload(t *testing.T) {
	b := testClient()
	defer b.Close()

	val := "some value"
	id := uuid.Must(uuid.NewRandom())
	key := fmtStagingKey(id.String())

	err := b.RPush(context.Background(), key, val).Err()
	if err != nil {
		t.Fatal("Failed to add job to redis: ", err)
	}

	_, err = b.StagedJobs()
	if err == nil {
		t.Error("Expected error received nil")
	}

	switch err.(type) {
	case wingman.BackendIDError:
		got := err.(wingman.BackendIDError).ID
		if got != key {
			t.Errorf("Expected error to have failed key. Expected: %s. Got: %s", key, got)
		}
	default:
		t.Error("Expected error to be type BackendIDError")
	}
}

func TestClearStagedJobSuccess(t *testing.T) {
	b := testClient()
	defer b.Close()

	initialJob := mock.NewWrappedJob()
	id := uuid.Must(uuid.NewRandom())

	raw, err := json.Marshal(initialJob)
	if err != nil {
		t.Fatal("Failed to marshal job: ", err)
	}

	err = b.RPush(context.Background(), fmtStagingKey(id.String()), raw).Err()
	if err != nil {
		t.Error("Unable to add value to redis: ", err)
	}

	err = b.ClearStagedJob(id.String())
	if err != nil {
		t.Error("Expected nil error, got: ", err)
	}

	resp, err := b.LRange(context.Background(), fmtStagingKey(id.String()), 0, 0).Result()
	if err != nil {
		t.Error("Expected nil error, got: ", err)
	} else if len(resp) > 0 {
		t.Error("Expected nil response, got: ", resp)
	}

	err = b.ClearStagedJob("does_not_exist")
	if err != nil {
		t.Error("Expected nil error, got: ", err)
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

	b.RPush(context.Background(), initialJob.Queue(), raw)
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

	b.RPush(context.Background(), initialJob.Queue(), raw)
	size = b.Size(mock.DefaultQueue)

	if size != 1 {
		t.Fatal("Expected size to be 1. Got: ", size)
	}
}

func testClient() *Backend {
	client, err := Init(Options{
		BlockingTimeout: 2 * time.Second,
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

func newStagedJob(client *Backend) wingman.InternalJob {
	job := mock.NewWrappedJob()
	job.StagingID = uuid.Must(uuid.NewRandom()).String()

	raw, err := json.Marshal(job)
	if err != nil {
		panic(fmt.Sprint("Failed to marshal job ", err))
	}

	err = client.RPush(context.Background(), fmtStagingKey(job.StagingID), raw).Err()
	if err != nil {
		panic(fmt.Sprint("Failed to add job to redis: ", err))
	}

	return *job
}
