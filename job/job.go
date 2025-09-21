package job

import (
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/nbigot/minijob/constants"
	"github.com/nbigot/minijob/web/apierror"
)

type JobUUID = uuid.UUID                    // JobUUID is the unique identifier of a job
type JobProperties = map[string]interface{} // JobProperties is the properties of a job
type JobUUIDList []JobUUID                  // JobUUIDList is a list of JobUUID
type ResourceList []string                  // ResourceList is a list of resources
type LockedResources = map[string]JobUUID   // LockedResources is a map of resources and their associated job

// JobEvent describes every event that can happen to a job (in job history)
const (
	JobEventCreate    = "CREATE"    // event type for the creation of a job (first event)
	JobEventDelay     = "DELAY"     // event type for the delay of a job (not yet visible in the queue)
	JobEventPending   = "PENDING"   // event type for the pending state of a job (not yet ready to start)
	JobEventEnqueue   = "ENQUEUE"   // event type for the queuing of a job (ready to start) (only occurs after a create event)
	JobEventStart     = "START"     // event type for the start of a job (only occurs after a queue event)
	JobEventSuccess   = "SUCCESS"   // event type for the success of a job (final state) (only occurs after a start or retry event)
	JobEventCancel    = "CANCEL"    // event type for the cancellation of a job (final state) (only occurs after a start or retry event)
	JobEventFail      = "FAIL"      // failure (might be retried, depending on the retry policy) (only occurs after a start or retry event)
	JobEventRetry     = "RETRY"     // retry of a failed job (only occurs after a fail event)
	JobEventHide      = "HIDE"      // event type for the hiding of a job (not visible in the queue)
	JobEventTerminate = "TERMINATE" // definitive failure, when no more retries are possible (final state) (only occurs after a fail event)
	JobEventDelete    = "DELETE"    // event type for the deletion of a job
)

// JobState describes the state of a job, it depends on the job history
type JobState int8

const (
	JobNoState   JobState = iota // NoState is a special state when no state is found
	JobCreated                   // JobCreated is the state of a job when it is created
	JobDelayed                   // JobDelayed is the state of a job when it is delayed (not yet visible in the queue)
	JobPending                   // JobPending is the initial state of a job
	JobQueued                    // JobQueued is the state of a job when it is ready to start
	JobRunning                   // JobRunning is the state of a job when it is running
	JobSucceeded                 // JobSucceeded is the state of a job when it is completed successfully
	JobFailed                    // JobFailed is the state of a job when it definitively failed (no more possible retries)
	JobCanceled                  // JobCanceled is the state of a job when it is canceled
	JobDeleted                   // JobDeleted is the state of a job when it is deleted
	JobHidden                    // JobHidden is the state of a job when it is hidden (not visible in the queue)
)

type JobHistoryEvent struct {
	EventType string `json:"eventType"` // The event type (required)
	Timestamp int64  `json:"timestamp"` // The event timestamp (unixmilliseconds) (required)
}

type JobHistory []JobHistoryEvent

type Job struct {
	JobUUID           JobUUID       `json:"id"`                // The unique identifier of a job (required)
	Topic             string        `json:"topic"`             // The topic name for which the job has been created (optional)
	Priority          int8          `json:"priority"`          // The priority of the job (optional)
	JobProperties     JobProperties `json:"properties"`        // The job properties (required)
	History           JobHistory    `json:"history"`           // The list of events of the job
	LockResources     ResourceList  `json:"lockResources"`     // The list of resources to lock (optional)
	UserAgent         string        `json:"userAgent"`         // The user agent (or program name) that make the request (optional)
	Requester         string        `json:"requester"`         // The identifier of the job requester (optional)
	Name              string        `json:"name"`              // The name of the job (optional)
	SessionId         string        `json:"sessionId"`         // The session identifier of the requester (optional)
	TraceId           string        `json:"traceId"`           // The trace identifier of the job (optional)
	DebugMode         bool          `json:"debugMode"`         // The debug flag of the job (optional)
	VisibilityTimeout uint          `json:"visibilityTimeout"` // Duration (in seconds) to keep the job hidden from the queue after it is fetched
	StartAfter        int64         `json:"startAfter"`        // Timestamp (in milliseconds) to start the job after
	CallbackURL       string        `json:"callbackUrl"`       // URL to call when job completes (success or failure) (optional)
	state             JobState      // The current state of the job
	previousState     JobState      // The previous state of the job
}

type JobMap = map[JobUUID]*Job

// JobRequest is the request to create a job, it is used to create a job from a http request
type JobRequest struct {
	Topic         string        `json:"topic" example:"default"`                                  // Topic is the topic name for which the job has been created (optional)
	Priority      int8          `json:"priority" example:"0"`                                     // Priority is the priority of the job (optional)
	JobProperties JobProperties `json:"properties" validate:"required"`                           // Properties is the job properties (required)
	LockResources ResourceList  `json:"lockResources"`                                            // LockResources is the list of resources to lock (optional)
	UserAgent     string        `json:"userAgent" example:"myJobProducerName"`                    // UserAgent is the user agent (or program name) that make the request (optional)
	Requester     string        `json:"requester"`                                                // Requester is the identifier of the job requester (optional)
	Name          string        `json:"name" example:"myJobName01"`                               // Name is the name of the job (optional)
	SessionId     string        `json:"sessionId" example:"6e70464d-85cf-431d-a9c9-a1f9c0ac82dc"` // SessionId is the session identifier of the requester (optional)
	TraceId       string        `json:"traceId" example:"e7b46b16-c971-464e-8a47-c791be0ce2ed"`   // TraceId is the trace identifier of the job (optional)
	DebugMode     bool          `json:"debugMode" example:"false"`                                // DebugMode is the debug flag of the job (optional)
	StartAfter    int64         `json:"startAfter"`                                               // Timestamp (in milliseconds) to start the job after
	Delay         int64         `json:"delay" example:"0"`                                        // Delay (in seconds) to wait before starting the job
	CallbackURL   string        `json:"callbackUrl" example:"https://api.example.com/webhook"`    // URL to call when job completes (success or failure) (optional)
}

func (j Job) ToJSON() (string, error) {
	jobJSON, err := json.Marshal(j)
	if err != nil {
		return "", err
	}
	return string(jobJSON), nil
}

// Get job creation timestamp in milliseconds
func (j *Job) GetCreationTimestamp() int64 {
	// the creation date time is the first event in the history
	if len(j.History) == 0 {
		panic("no creation timestamp found")
	}

	return j.History[0].Timestamp
}

// Get job last update date
func (j *Job) GetLastUpdateDate() int64 {
	// the last update date is the last event in the history
	if len(j.History) > 0 {
		return j.History[len(j.History)-1].Timestamp
	}

	return 0
}

func (j *Job) GetState() JobState {
	return j.state
}

func (j *Job) GetStateString() string {
	switch j.state {
	case JobCreated:
		return "created"
	case JobDelayed:
		return "delayed"
	case JobPending:
		return "pending"
	case JobQueued:
		return "queued"
	case JobRunning:
		return "running"
	case JobSucceeded:
		return "succeeded"
	case JobFailed:
		return "failed"
	case JobCanceled:
		return "canceled"
	case JobDeleted:
		return "deleted"
	case JobHidden:
		return "hidden"
	default:
		return "unknown"
	}
}

func (j *Job) GetPreviousState() JobState {
	return j.previousState
}

func (j *Job) SetStateFromHistory() {
	j.state = JobNoState
	statesHistory := j.GetHistoryStates()
	if len(statesHistory) > 0 {
		j.state = statesHistory[len(statesHistory)-1]
	}
	j.previousState = JobNoState
	if len(statesHistory) > 1 {
		j.previousState = statesHistory[len(statesHistory)-2]
	}
}

func (j *Job) GetStateFromEvent(eventType string) JobState {
	switch eventType {
	case JobEventCreate:
		return JobCreated
	case JobEventDelay:
		return JobDelayed
	case JobEventPending:
		return JobPending
	case JobEventEnqueue:
		return JobQueued
	case JobEventStart, JobEventFail, JobEventRetry:
		return JobRunning
	case JobEventSuccess:
		return JobSucceeded
	case JobEventHide:
		return JobHidden
	case JobEventCancel:
		return JobCanceled
	case JobEventTerminate:
		return JobFailed
	default:
		return JobNoState
	}
}

func (j *Job) IsCompleted() bool {
	// Check if the job is completed
	switch j.state {
	case JobSucceeded, JobCanceled, JobFailed:
		return true
	default:
		return false
	}
}

func (j *Job) GetCountFaillures() uint {
	// Get the number of faillures of a job
	count := uint(0)
	for _, event := range j.History {
		if event.EventType == JobEventFail {
			count++
		}
	}
	return count
}

func (j *Job) Clone() *Job {
	// Clone a job (except the JobUUID and History)
	return &Job{
		Topic:             j.Topic,
		Priority:          j.Priority,
		JobProperties:     j.JobProperties,
		History:           make(JobHistory, 0),
		LockResources:     j.LockResources,
		UserAgent:         j.UserAgent,
		Requester:         j.Requester,
		Name:              j.Name,
		SessionId:         j.SessionId,
		TraceId:           j.TraceId,
		DebugMode:         j.DebugMode,
		VisibilityTimeout: j.VisibilityTimeout,
		StartAfter:        j.StartAfter,
		CallbackURL:       j.CallbackURL,
		state:             j.state,
		previousState:     j.previousState,
	}
}

func (j *Job) Init(id JobUUID, creationDate int64) {
	j.JobUUID = id
	j.state = JobNoState
	j.previousState = JobNoState
	j.History = make(JobHistory, 0)
	j.AddHistoryEvent(JobEventCreate, creationDate)
}

func (j *Job) AddHistoryEvent(eventType string, timestamp int64) {
	j.History = append(j.History, JobHistoryEvent{
		EventType: eventType,
		Timestamp: timestamp,
	})
	j.previousState = j.state
	j.state = j.GetStateFromEvent(eventType)
}

func (j *Job) GetHistoryStates() []JobState {
	// Get the list of states from the history
	states := make([]JobState, len(j.History))
	for i, event := range j.History {
		states[i] = j.GetStateFromEvent(event.EventType)
	}
	return states
}

func (j *Job) GetCountLockResources() uint {
	switch j.state {
	case JobQueued, JobRunning:
		return uint(len(j.LockResources))
	default:
		return 0
	}
}

func (j *Job) GetQueuedOrRunningTimestamp() int64 {
	// Get the unix epoch in seconds since the job is in a running or queued state
	switch j.state {
	case JobQueued, JobRunning:
		// parse all the history events (in reverse order: from the most recent to the least recent) to find the first event of type JobEventEnqueue or JobEventStart
		for i := len(j.History) - 1; i >= 0; i-- {
			if j.History[i].EventType == JobEventEnqueue || j.History[i].EventType == JobEventStart {
				return j.History[i].Timestamp / 1000 // convert milliseconds to seconds
			}
		}
		// if no such event is found, return 0
		return 0
	default:
		return 0
	}
}

func (j *Job) GetDurationMsSinceLastAndFirstEvent() int64 {
	// Get the time difference between the last event and the second event in milliseconds
	if len(j.History) < 2 {
		return 0
	}
	return j.History[len(j.History)-1].Timestamp - j.History[0].Timestamp
}

func NewJob(payload []byte) (*Job, error) {
	// Create a new job from a backend (ex: redis) payload
	j := &Job{}
	err := json.Unmarshal(payload, j)
	if err != nil {
		return nil, &apierror.APIError{
			Message:  "cannot create job",
			Code:     constants.ErrorCantCreateJob,
			HttpCode: fiber.StatusInternalServerError,
			Err:      err,
		}
	}
	j.SetStateFromHistory()
	return j, nil
}

func NewJobFromRequest(payload []byte) (*Job, error) {
	// Create a new job from a http request
	// A job created from a request does not have a JobUUID nor History
	req := &JobRequest{}
	err := json.Unmarshal(payload, req)
	if err != nil {
		return nil, &apierror.APIError{
			Message:  "cannot create job from request",
			Code:     constants.ErrorCantCreateJob,
			HttpCode: fiber.StatusBadRequest,
			Err:      err,
		}
	}

	// Compute the startAfter timestamp
	startAfter := req.StartAfter
	if req.Delay > 0 {
		if startAfter == 0 {
			startAfter = time.Now().UnixMilli()
		}
		startAfter = startAfter + req.Delay*1000
	}

	// Convert JobRequest to Job
	j := &Job{
		Topic:         req.Topic,
		Priority:      req.Priority,
		JobProperties: req.JobProperties,
		LockResources: req.LockResources,
		UserAgent:     req.UserAgent,
		Requester:     req.Requester,
		Name:          req.Name,
		SessionId:     req.SessionId,
		TraceId:       req.TraceId,
		DebugMode:     req.DebugMode,
		StartAfter:    startAfter,
		CallbackURL:   req.CallbackURL,
		state:         JobNoState,
		previousState: JobNoState,
	}

	// note that the JobUUID and History will be set later
	return j, nil
}
