package job

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestJob_UnmarshalJSON(t *testing.T) {
	// Test case 1: perfect example of Valid JSON input
	jsonData1 := []byte(`{
		"topic": "example",
		"properties": {
			"key1": "value1",
			"key2": 42,
			"key3": [1, 2, 3],
			"key4": {"a": 1, "b": [], "c": {"x": "value1", "y": 42}}
		},
		"lockResources": ["resource1", "resource2", "resource3:abc:def"],
		"userAgent": "user agent test",
		"requester": "unit-test",
		"sessionId": "0190e951-c29a-70aa-ba59-18be4abe97a1",
		"traceId": "0190e951-c29a-70aa-ba59-18be4abe97a0"
	}`)

	job1 := &Job{}
	err := json.Unmarshal(jsonData1, job1)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Test case 2: Valid JSON input
	jsonData2 := []byte(`{
		"id": "0190e951-c29a-70aa-ba59-18be4abe97a2",
		"topic": "example",
		"properties": {
			"key1": "value1",
			"key2": "value2"
		},
		"history": [
			{
				"eventType": "start",
				"timestamp": 1629876543
			},
			{
				"eventType": "end",
				"timestamp": 1629876545
			}
		],
		"lockResources": ["resource1", "resource2", "resource3:abc:def"],
		"userAgent": "user agent test",
		"requester": "unit-test",
		"sessionId": "0190e951-c29a-70aa-ba59-18be4abe97a1",
		"traceId": "0190e951-c29a-70aa-ba59-18be4abe97a0",
		"debugMode": true
	}`)

	job2 := &Job{}
	err = json.Unmarshal(jsonData2, job2)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Test case 3: valid JSON input
	invalidJsonData2 := []byte(`{
		"properties": {
			"key1": "value1",
			"key2": "value2"
		}
	}`)

	job3 := &Job{}
	err = json.Unmarshal(invalidJsonData2, job3)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Test case 4: Valid JSON input
	jsonData4 := []byte(`{
		"properties": {}
	}`)

	job4 := &Job{}
	err = json.Unmarshal(jsonData4, job4)
	if err != nil {
		t.Error("Expected nil, but got error")
	}
}

func TestJob_MarshalJSON(t *testing.T) {
	j := &Job{
		JobUUID:       JobUUID(uuid.MustParse("0190e951-c29a-70aa-ba59-18be4abe9700")),
		Topic:         "topic1",
		JobProperties: JobProperties{"key1": "value1", "key2": 42, "key3": true, "key4": []string{"a", "b", "c"}},
		History:       []JobHistoryEvent{},
		LockResources: []string{"resource1", "resource2", "resource3:abc:def"},
		UserAgent:     "user agent test",
		Requester:     "unit-test",
		SessionId:     "0190e951-c29a-70aa-ba59-18be4abe97a1",
		TraceId:       "0190e951-c29a-70aa-ba59-18be4abe97a0",
		DebugMode:     false,
		state:         JobPending,
	}

	expectedJSON := `{"id":"0190e951-c29a-70aa-ba59-18be4abe9700","topic":"topic1","priority":0,"properties":{"key1":"value1","key2":42,"key3":true,"key4":["a","b","c"]},"history":[],"lockResources":["resource1","resource2","resource3:abc:def"],"userAgent":"user agent test","requester":"unit-test","name":"","sessionId":"0190e951-c29a-70aa-ba59-18be4abe97a1","traceId":"0190e951-c29a-70aa-ba59-18be4abe97a0","debugMode":false,"visibilityTimeout":0,"startAfter":0}`

	resultJSON, err := json.Marshal(j)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	resultJSONstr := string(resultJSON)

	if resultJSONstr != expectedJSON {
		t.Errorf("Expected JSON: %s, got: %s", expectedJSON, resultJSONstr)
	}
}

func TestJob_GetPreviousState(t *testing.T) {
	tests := []struct {
		name string
		j    *Job
		want JobState
	}{
		{
			"Test case 1: no history (invalid)",
			&Job{state: JobPending},
			JobNoState,
		},
		{
			"Test case 2: freshly created job",
			&Job{
				state: JobPending,
				History: []JobHistoryEvent{
					{EventType: JobEventCreate, Timestamp: 1735992487270},
				},
			},
			JobNoState,
		},
		{
			"Test case 3: pending job",
			&Job{
				state: JobPending,
				History: []JobHistoryEvent{
					{EventType: JobEventCreate, Timestamp: 1735992487270},
					{EventType: JobEventEnqueue, Timestamp: 1735992487272},
				},
			},
			JobPending,
		},
		{
			"Test case 4: started job",
			&Job{
				state: JobPending,
				History: []JobHistoryEvent{
					{EventType: JobEventCreate, Timestamp: 1735992487270},
					{EventType: JobEventEnqueue, Timestamp: 1735992487272},
					{EventType: JobEventStart, Timestamp: 1735992487274},
				},
			},
			JobQueued,
		},
		{
			"Test case 4: succeeded job",
			&Job{
				state: JobPending,
				History: []JobHistoryEvent{
					{EventType: JobEventCreate, Timestamp: 1735992487270},
					{EventType: JobEventEnqueue, Timestamp: 1735992487272},
					{EventType: JobEventStart, Timestamp: 1735992487274},
					{EventType: JobEventSuccess, Timestamp: 1735992487276},
				},
			},
			JobRunning,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.j.GetPreviousState(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Job.GetPreviousState() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJob_ComputeStateFromHistory(t *testing.T) {
	tests := []struct {
		name string
		j    *Job
		want JobState
	}{
		{
			"Test case 1: no history (invalid)",
			&Job{state: JobPending},
			JobNoState,
		},
		{
			"Test case 2: freshly created job",
			&Job{
				state: JobPending,
				History: []JobHistoryEvent{
					{EventType: JobEventCreate, Timestamp: 1735992487270},
				},
			},
			JobPending,
		},
		{
			"Test case 3: pending job",
			&Job{
				state: JobPending,
				History: []JobHistoryEvent{
					{EventType: JobEventCreate, Timestamp: 1735992487270},
					{EventType: JobEventEnqueue, Timestamp: 1735992487272},
				},
			},
			JobQueued,
		},
		{
			"Test case 4: started job",
			&Job{
				state: JobPending,
				History: []JobHistoryEvent{
					{EventType: JobEventCreate, Timestamp: 1735992487270},
					{EventType: JobEventEnqueue, Timestamp: 1735992487272},
					{EventType: JobEventStart, Timestamp: 1735992487274},
				},
			},
			JobRunning,
		},
		{
			"Test case 4: succeeded job",
			&Job{
				state: JobPending,
				History: []JobHistoryEvent{
					{EventType: JobEventCreate, Timestamp: 1735992487270},
					{EventType: JobEventEnqueue, Timestamp: 1735992487272},
					{EventType: JobEventStart, Timestamp: 1735992487274},
					{EventType: JobEventSuccess, Timestamp: 1735992487276},
				},
			},
			JobSucceeded,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.j.ComputeStateFromHistory(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Job.ComputeStateFromHistory() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJob_Init(t *testing.T) {
	j := Job{}
	jobUUID := uuid.MustParse("0190e951-c29a-70aa-ba59-18be4abe9700")
	j.Init(JobUUID(jobUUID), 1735992487270)
	assert.Equal(t, jobUUID, j.JobUUID)
	assert.Equal(t, 1, len(j.History))
	assert.Equal(t, JobEventCreate, j.History[0].EventType)
	assert.Equal(t, JobPending, j.state)
	assert.Equal(t, j.ComputeStateFromHistory(), j.state)
}
