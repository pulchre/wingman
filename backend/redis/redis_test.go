package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/google/uuid"
	"github.com/pulchre/wingman"
	"github.com/pulchre/wingman/mock"
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
	case1 := Options{
		MaxIdle: 1,
		Dial:    dialRedis,
	}

	got1, got2 := Init(case1)
	if (got1 != nil && got1.(*Backend).Pool.MaxIdle != 1) || got2 != nil {
		t.Errorf("Init(%v) == %v, `%v`, want %v, `%v`", case1, got1, got2, "redis Pool with MaxIdle 1", nil)
	}

	case2 := "not a redis.Option"

	got1, got2 = Init(case2)
	if got1 != nil || got2 != WrongOptionsTypeError {
		t.Errorf("Init(%v) == %v, `%v`, want %v, `%v`", case2, got1, got2, nil, WrongOptionsTypeError)
	}

}

func TestPushJob(t *testing.T) {
	t.Run("Success", testPushJobSuccess)
}

func testPushJobSuccess(t *testing.T) {
	b := testClient(t)
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

func TestPopAndStageJob(t *testing.T) {
	t.Run("Success", testPopAndStageJobSuccess)
	t.Run("Timeout", testPopAndStageJobTimeout)
	t.Run("Cancel", testPopAndStageJobContextDone)
}

func testPopAndStageJobSuccess(t *testing.T) {
	b := testClient(t)
	defer b.Close()

	initialJob, _ := wingman.WrapJob(mock.NewJob())

	raw, err := json.Marshal(initialJob)
	if err != nil {
		panic(err)
	}

	b.do("RPUSH", initialJob.Job.Queue(), raw)

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

	b := testClient(t)
	defer b.Close()

	b.timeout = 0.01

	job, err := b.PopAndStageJob(context.Background(), mock.DefaultQueue)
	if job != nil {
		t.Errorf("Expected job to be nil: %v", job)
	}

	if err != wingman.ErrCanceled {
		t.Errorf("Expected error nil returned: %v", err)
	}
}

func testPopAndStageJobContextDone(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test")
	}

	b := testClient(t)
	defer b.Close()

	b.timeout = 10

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

	if end.Sub(start) > 10*time.Second {
		t.Error("Timed-out at 10 seconds")
	}
}

func TestProcessJob(t *testing.T) {
	t.Run("Success", testProcessJobSuccess)
	t.Run("NoJob", testProcessJobNoJob)
}

func testProcessJobSuccess(t *testing.T) {
	b := testClient(t)
	defer b.Close()

	initialJob := mock.NewWrappedJob()
	stagingID := uuid.Must(uuid.NewRandom())

	raw, err := json.Marshal(initialJob)
	if err != nil {
		t.Fatal("Failed to marshal job: ", err)
	}

	_, err = b.do("RPUSH", fmtStagingKey(stagingID.String()), raw)
	if err != nil {
		t.Fatal("Failed to add job to redis: ", err)
	}

	err = b.ProcessJob(stagingID.String())
	if err != nil {
		t.Fatal("Expected nil error, got: ", err)
	}

	retrieved, err := redis.ByteSlices(b.do("LRANGE", fmtProcessingKey(initialJob.ID), 0, 1))
	if err != nil {
		t.Fatal("Failed to retrieve job from redis ", err)
	}

	job, err := wingman.InternalJobFromJSON(retrieved[0])
	if err != nil {
		t.Fatal("Failed to unmarshal job ", err)
	}

	if job.ID != initialJob.ID {
		t.Errorf("Expected job on queue to match staged job. Expected: %v, got: %v", initialJob.ID, job.ID)
	}
}

func testProcessJobNoJob(t *testing.T) {
	b := testClient(t)
	defer b.Close()

	stagingID := uuid.Must(uuid.NewRandom())

	err := b.ProcessJob(stagingID.String())
	if err != wingman.ErrorJobNotStaged {
		t.Errorf("Expected: %v, got: %v", wingman.ErrorJobNotStaged, err)
	}
}

func TestClearProcessorSuccess(t *testing.T) {
	b := testClient(t)
	defer b.Close()

	jobID := "job1"

	_, err := b.do("RPUSH", fmtProcessingKey(jobID), "some job")
	if err != nil {
		t.Fatal("Failed to push data to redis with error: ", err)
	}

	err = b.ClearJob(jobID)
	if err != nil {
		t.Error("Expected nil error, got: ", err)
	}

	keys, err := redis.Strings(b.do("KEYS", fmtProcessingKey(jobID)))
	if err != nil {
		t.Fatal("Failed to retrieve key with error: ", err)
	}

	if len(keys) > 0 {
		t.Error("Expected no response, got: ", keys)
	}
}

func TestFailJob(t *testing.T) {
	c := testClient(t)
	defer c.Close()

	job := mock.NewWrappedJob()

	raw, err := json.Marshal(job)
	if err != nil {
		panic(err)
	}

	_, err = c.do("RPUSH", fmtProcessingKey(job.ID), raw)
	if err != nil {
		t.Fatal("Failed to push data to redis with error: ", err)
	}

	err = c.FailJob(job.ID)
	if err != nil {
		t.Error("Expected nil error, got: ", err)
	}

	keys, err := redis.Strings(c.do("KEYS", fmtFailedKey(job.ID)))
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
	b := testClient(t)
	defer b.Close()

	initialJob := mock.NewWrappedJob()
	stagingID := uuid.Must(uuid.NewRandom())

	raw, err := json.Marshal(initialJob)
	if err != nil {
		t.Fatal("Failed to marshal job: ", err)
	}

	_, err = b.do("RPUSH", fmtStagingKey(stagingID.String()), raw)
	if err != nil {
		t.Fatal("Failed to add job to redis: ", err)
	}

	err = b.ReenqueueStagedJob(stagingID.String())
	if err != nil {
		t.Fatal("Expected nil error, got: ", err)
	}

	retrieved, err := redis.ByteSlices(b.do("LRANGE", initialJob.Queue(), 0, 1))
	if err != nil {
		t.Fatal("Failed to retrieve job from redis ", err)
	}

	job, err := wingman.InternalJobFromJSON(retrieved[0])
	if err != nil {
		t.Fatal("Failed to unmarshal job ", err)
	}

	if job.ID != initialJob.ID {
		t.Errorf("Expected job on queue to match staged job. Expected: %v, got: %v", initialJob.ID, job.ID)
	}
}

func testReenqueueStagedJobInvalidJob(t *testing.T) {
	b := testClient(t)
	defer b.Close()

	stagingID := uuid.Must(uuid.NewRandom())

	_, err := b.do("RPUSH", fmtStagingKey(stagingID.String()), "invalid job")
	if err != nil {
		t.Fatal("Failed to add job to redis: ", err)
	}

	err = b.ReenqueueStagedJob(stagingID.String())
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
}

func testReenqueueStagedJobJobNotFound(t *testing.T) {
	b := testClient(t)
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
	b := testClient(t)
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

	_, err = b.do("RPUSH", fmtStagingKey(uuid.Must(uuid.NewRandom()).String()), raw1)
	if err != nil {
		t.Fatal("Failed to add job to redis: ", err)
	}

	_, err = b.do("RPUSH", fmtStagingKey(uuid.Must(uuid.NewRandom()).String()), raw2)
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
	b := testClient(t)
	defer b.Close()

	val := "some value"
	id := uuid.Must(uuid.NewRandom())
	key := fmtStagingKey(id.String())

	_, err := b.do("RPUSH", key, val)
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
	b := testClient(t)
	defer b.Close()

	initialJob := mock.NewWrappedJob()
	id := uuid.Must(uuid.NewRandom())

	raw, err := json.Marshal(initialJob)
	if err != nil {
		t.Fatal("Failed to marshal job: ", err)
	}

	_, err = b.do("RPUSH", fmtStagingKey(id.String()), raw)
	if err != nil {
		t.Error("Unable to add value to redis: ", err)
	}

	err = b.ClearStagedJob(id.String())
	if err != nil {
		t.Error("Expected nil error, got: ", err)
	}

	resp, err := redis.ByteSlices(b.do("LRANGE", fmtStagingKey(id.String()), 0, 0))
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
	b := testClient(t)
	defer b.Close()

	initialJob := mock.NewWrappedJob()

	raw, err := json.Marshal(initialJob)
	if err != nil {
		panic(err)
	}

	b.do("RPUSH", initialJob.Queue(), raw)
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
	b := testClient(t)
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
	b := testClient(t)
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

	b.do("RPUSH", initialJob.Queue(), raw)

	size = b.Size(mock.DefaultQueue)

	if size != 1 {
		t.Fatal("Expected size to be 1. Got: ", size)
	}
}

func testClient(t *testing.T) *Backend {
	client, err := initBackend(Options{
		MaxIdle:         2,
		MaxActive:       2,
		Dial:            dialRedis,
		BlockingTimeout: 10,
	})
	if err != nil {
		t.Fatal("Failed to create testing client: ", err)
	}

	_, err = client.do("FLUSHALL")
	if err != nil {
		t.Fatal("Failed to create testing client: ", err)
	}

	return client
}

func dialRedis() (redis.Conn, error) {
	return redis.Dial("tcp", fmt.Sprintf("%s:%s", redisHost, redisPort))
}
