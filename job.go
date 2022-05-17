package wingman

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/google/uuid"
)

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
	ID        uuid.UUID
	TypeName  string
	Job       Job
	StagingID uuid.UUID `json:"-"`

	Ctx    context.Context    `json:"-"`
	Cancel context.CancelFunc `json:"-"`
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

func WrapJob(j Job) (InternalJob, error) {
	id, err := uuid.NewRandom()

	return InternalJob{
		ID:       id,
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
	jobType := registeredJobTypes[typeName]
	if jobType == nil {
		return nil, fmt.Errorf("Job type `%s` is not registered", typeName)
	}

	reflectType := reflect.TypeOf(jobType)
	job := reflect.New(reflectType).Interface().(Job)

	err := json.Unmarshal(val, &job)
	if err != nil {
		return nil, err
	}

	return job, nil
}
