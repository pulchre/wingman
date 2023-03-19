package wingman

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
)

// ContextKey defines the type for our context value keys. We need this per the
// documentation for Context that states that the keys should not be a standard
// type.
type ContextKey string

// ContextJobIDKey is the key under which the processor ID is stored.
const ContextJobIDKey = ContextKey("jobID")

var ErrorJobNotStaged = errors.New("Could not find staged job")
var ErrorJobNotFound = errors.New("Could not find any job")

// Job is the interface definition for a given job. Handler should return the
// name of the handler that this job will be processed by. Payload should be
// the data stored in the queue. SetPayload is so the manager can rebuild the
// job after it is pull off the queue.
type Job interface {
	TypeName() string
	Handle(context.Context) error
	Queue() string
	LockKey() string
	Concurrency() int
}

// JobTypeBuilder is a function that this libraries consumer must set to
// facilitate deserializing the job into it's proper concrete type. The type
// name string will be passed and the proper struct is expected to be returned.
var registeredJobTypes map[string]Job

// RegisterJobType should be called, on program initialization, with the
// default value for every used type so that jobs can be properly deserialized
// when retrieved from the backend.
func RegisterJobType(job Job) {
	if registeredJobTypes == nil {
		registeredJobTypes = make(map[string]Job)
	}

	registeredJobTypes[job.TypeName()] = job
}

// InternalJob is used by a backend and holds the id and type name for
// deserialization. This should only be used by backend code.
type InternalJob struct {
	ID       string
	TypeName string
	Job      Job

	StartTime time.Time `json:"-"`
	EndTime   time.Time `json:"-"`

	// We use StagingID instead of the job ID itself because we want to
	// keep the job in the backend we are finished with it so that we don't
	// lose any data. So when we pop off a job from the queue, we won't
	// know what the job ID is. Thus we use StagingID.
	StagingID string `json:"-"`
}

// InternalJobFromJSON is a helper intended for use by backends to deserialize
// retrieved jobs
func InternalJobFromJSON(b []byte) (*InternalJob, error) {
	job := &InternalJob{}

	err := json.Unmarshal(b, job)
	if err != nil {
		return nil, err
	}

	return job, nil
}

func WrapJob(j Job) (*InternalJob, error) {
	id, err := uuid.NewRandom()

	return &InternalJob{
		ID:       id.String(),
		TypeName: j.TypeName(),
		Job:      j,
	}, err
}

func (j InternalJob) Queue() string { return j.Job.Queue() }

func (j *InternalJob) UnmarshalJSON(b []byte) error {
	var objMap map[string]*json.RawMessage
	err := json.Unmarshal(b, &objMap)
	if err != nil {
		return err
	}

	var rawJob *json.RawMessage
	for k, v := range objMap {
		var err error
		switch k {
		case "ID":
			err = json.Unmarshal(*v, &j.ID)
		case "TypeName":
			err = json.Unmarshal(*v, &j.TypeName)
		case "Job":
			rawJob = v
		}

		if err != nil {
			return err
		}
	}

	j.Job, err = DeserializeJob(j.TypeName, *rawJob)

	return err
}

func DeserializeJob(typeName string, val []byte) (Job, error) {
	var job Job

	jobType := registeredJobTypes[typeName]
	if jobType == nil {
		return nil, fmt.Errorf("Job type `%s` is not registered", typeName)
	}

	reflectType := reflect.TypeOf(jobType)
	if reflectType.Kind() == reflect.Pointer {
		job = reflect.New(reflectType.Elem()).Interface().(Job)
	} else {
		job = reflect.New(reflectType).Interface().(Job)
	}

	err := json.Unmarshal(val, &job)
	if err != nil {
		return nil, err
	}

	return job, nil
}
