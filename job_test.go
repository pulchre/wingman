package wingman_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/pulchre/wingman"
	"github.com/pulchre/wingman/mock"
)

func TestWrapJob(t *testing.T) {
	j := mock.Job{}
	rawJ, _ := wingman.WrapJob(j)

	if rawJ.ID == uuid.Nil {
		t.Error("Should have uuid")
	}

	if rawJ.TypeName != j.TypeName() {
		t.Error("Should have set the type name on the internal job")
	}
}

func TestInternalJobQueue(t *testing.T) {
	j := mock.NewWrappedJob()

	if j.Queue() != j.Job.Queue() {
		t.Errorf("Expected InternalJob.Queue: %s, to match job.Queue %v", j.Queue(), j.Job.Queue())
	}
}

func TestJobUmarshalJSON(t *testing.T) {
	t.Run("Success", testJobUnmarshalJSONSuccess)
	t.Run("Not JSON", testJobUnmarshalJSONNotJson)
	t.Run("Native Type Fail", testJobUnmarshalJSONNativeTypeFail)
	t.Run("JobType Fail", testJobUnmarshalJSONJobTypeFail)
}

func testJobUnmarshalJSONSuccess(t *testing.T) {
	j := buildJob()
	str := mustMarshalJob(j)

	if err := json.Unmarshal(str, &j); err != nil {
		t.Errorf("Err should be nil: `%s`", err)
	}
}

func testJobUnmarshalJSONNotJson(t *testing.T) {
	var j wingman.InternalJob
	if err := json.Unmarshal([]byte("[]"), &j); err == nil {
		t.Errorf("Expected non JSON to fail to parse")
	}
}

func testJobUnmarshalJSONNativeTypeFail(t *testing.T) {
	j := buildJob()
	str := mustMarshalJob(j)
	str = []byte(strings.Replace(string(str), j.ID.String(), "invalid", 1))

	if err := json.Unmarshal([]byte(str), &j); err == nil || err.Error() != "invalid UUID length: 7" {
		t.Errorf("Expected error parsing ID, `%v`", err)
	}
}

func testJobUnmarshalJSONJobTypeFail(t *testing.T) {
	j := buildJob()
	j.TypeName = "unknown type"
	str := mustMarshalJob(j)

	if err := json.Unmarshal([]byte(str), &j); err == nil || err.Error() != "Job type `unknown type` is not registered" {
		t.Errorf("Expected error unknown job type, `%v`", err)
	}
}

func buildJob() wingman.InternalJob {
	j, err := wingman.WrapJob(mock.NewJob())
	if err != nil {
		panic(err)
	}

	return j
}

func mustMarshalJob(j wingman.InternalJob) []byte {
	b, err := json.Marshal(j)
	if err != nil {
		panic(err)
	}
	return b
}
