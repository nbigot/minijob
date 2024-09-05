package job

import (
	"encoding/json"
	"errors"
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
	JobEventCreate  = "CREATE"  // event type for the creation of a job (first event)
	JobEventEnqueue = "ENQUEUE" // event type for the queuing of a job (ready to start) (only occurs after a create event)
	JobEventStart   = "START"   // event type for the start of a job (only occurs after a queue event)
	JobEventSuccess = "SUCCESS" // event type for the success of a job (final state) (only occurs after a start or retry event)
	JobEventCancel  = "CANCEL"  // event type for the cancellation of a job (final state) (only occurs after a start or retry event)
	// events to handle errors
	JobEventFail      = "FAIL"      // failure (might be retried, depending on the retry policy) (only occurs after a start or retry event)
	JobEventRetry     = "RETRY"     // retry of a failed job (only occurs after a fail event)
	JobEventTerminate = "TERMINATE" // definitive failure, when no more retries are possible (final state) (only occurs after a fail event)
)

// JobState describes the state of a job, it depends on the job history
type JobState int

const (
	JobPending   JobState = iota // JobPending is the initial state of a job
	JobQueued                    // JobQueued is the state of a job when it is ready to start
	JobRunning                   // JobRunning is the state of a job when it is running
	JobSucceeded                 // JobSucceeded is the state of a job when it is completed successfully
	JobFailed                    // JobFailed is the state of a job when it definitively failed (no more possible retries)
	JobCanceled                  // JobCanceled is the state of a job when it is canceled
)

type JobHistoryEvent struct {
	EventType string `json:"eventType"` // EventType is the event type (required)
	Timestamp int64  `json:"timestamp"` // Timestamp is the event timestamp (unixmilliseconds) (required)
}

type JobHistory []JobHistoryEvent

type Job struct {
	JobUUID           JobUUID       `json:"id"`
	Topic             string        `json:"topic"`             // Topic is the topic name for which the job has been created (optional)
	Priority          int           `json:"priority"`          // Priority is the priority of the job (optional)
	JobProperties     JobProperties `json:"properties"`        // Properties is the job properties (required)
	History           JobHistory    `json:"history"`           // History is the list of events of the job
	LockResources     ResourceList  `json:"lockResources"`     // LockResources is the list of resources to lock (optional)
	UserAgent         string        `json:"userAgent"`         // UserAgent is the user agent (or program name) that make the request (optional)
	Requester         string        `json:"requester"`         // Requester is the identifier of the job requester (optional)
	Name              string        `json:"name"`              // Name is the name of the job (optional)
	SessionId         string        `json:"sessionId"`         // SessionId is the session identifier of the requester (optional)
	TraceId           string        `json:"traceId"`           // TraceId is the trace identifier of the job (optional)
	DebugMode         bool          `json:"debugMode"`         // DebugMode is the debug flag of the job (optional)
	VisibilityTimeout uint          `json:"visibilityTimeout"` // Duration (in seconds) to keep the job hidden from the queue after it is fetched
	StartAfter        int64         `json:"startAfter"`        // Timestamp (in milliseconds) to start the job after
}

type JobMap = map[JobUUID]*Job

// // JobMeta is the metadata of a job
// type JobMeta struct {
// 	Topic         string       `json:"topic"`
// 	CreationDate  int64        `json:"creationDate"`
// 	LockResources ResourceList `json:"lockResources"`
// 	UserAgent     string       `json:"userAgent"`
// 	Requester     string       `json:"requester"`
// 	SessionId     string       `json:"sessionId"`
// 	TraceId       string       `json:"traceId"`
// 	DebugMode     bool         `json:"debugMode"`
// }

// type JobPayload struct {
// 	Meta       JobMeta           `json:"meta" validate:"required"`
// 	Properties map[string]string `json:"properties" validate:"required,lte=128,dive,keys,gt=0,lte=64,endkeys,max=1024,required"`
// }

// JobRequest is the request to create a job, it is used to create a job from a http request
type JobRequest struct {
	Topic         string        `json:"topic"`                          // Topic is the topic name for which the job has been created (optional)
	Priority      int           `json:"priority"`                       // Priority is the priority of the job (optional)
	JobProperties JobProperties `json:"properties" validate:"required"` // Properties is the job properties (required)
	LockResources ResourceList  `json:"lockResources"`                  // LockResources is the list of resources to lock (optional)
	UserAgent     string        `json:"userAgent"`                      // UserAgent is the user agent (or program name) that make the request (optional)
	Requester     string        `json:"requester"`                      // Requester is the identifier of the job requester (optional)
	Name          string        `json:"name"`                           // Name is the name of the job (optional)
	SessionId     string        `json:"sessionId"`                      // SessionId is the session identifier of the requester (optional)
	TraceId       string        `json:"traceId"`                        // TraceId is the trace identifier of the job (optional)
	DebugMode     bool          `json:"debugMode"`                      // DebugMode is the debug flag of the job (optional)
	StartAfter    int64         `json:"startAfter"`                     // Timestamp (in milliseconds) to start the job after
	Delay         int64         `json:"delay"`                          // Delay (in seconds) to wait before starting the job
	// TODO
	/*
		partitionId: string;  // The partition identifier for the job to be assigned
		priority: number;
		deadline: number;
		cron: string;
		maxAttempts: number;
		maxDuration: number;

		// Retention policy
		retentionPolicy: {

			// The duration to keep the job in the backend
			duration: number;

			// The maximum number of jobs to keep in the backend
			maxJobs: number;

			// The maximum number of jobs to keep per topic
			maxJobsPerTopic: number;

			// The maximum number of jobs to keep per requester
			maxJobsPerRequester: number;

			// The maximum number of jobs to keep per session
			maxJobsPerSession: number;

			// The maximum number of jobs to keep per trace
			maxJobsPerTrace: number;

			// The maximum number of jobs to keep per user agent
			maxJobsPerUserAgent: number;

			// The maximum number of jobs to keep per lock resource
			maxJobsPerLockResource: number;
		}

		// Retry policy
		retryPolicy: {

			// The number of attempts to make before giving up
			maxAttempts: number;

			// The delay between each attempt
			delay: number;

			// The backoff factor to apply between each attempt
			backoff: number;

			// The maximum delay between each attempt
			maxDelay: number;

			// The maximum duration of the retry policy
			maxDuration: number;

			// The jitter factor to apply to the delay
			jitter: number;

			// The retry strategy
			strategy: 'fixed' | 'linear' | 'exponential';
	*/
}

func (j Job) ToJSON() (string, error) {
	jobJSON, err := json.Marshal(j)
	if err != nil {
		return "", err
	}
	return string(jobJSON), nil
}

// Get job creation date
func (j *Job) GetCreationDate() int64 {
	// the creation date is the first event in the history
	if len(j.History) == 0 {
		// no creation date found
		panic("no creation date found")
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

// Get job state
func (j *Job) GetState() (JobState, error) {
	// the state is the last event in the history
	if len(j.History) > 0 {
		switch j.History[len(j.History)-1].EventType {
		case JobEventCreate:
			return JobPending, nil
		case JobEventEnqueue:
			return JobQueued, nil
		case JobEventStart, JobEventFail, JobEventRetry:
			return JobRunning, nil
		case JobEventSuccess:
			return JobSucceeded, nil
		case JobEventCancel:
			return JobCanceled, nil
		case JobEventTerminate:
			return JobFailed, nil
		default:
			return 0, errors.New("unknown state")
		}
	}

	return 0, errors.New("no state found")
}

func (j *Job) IsCompleted() bool {
	// Check if the job is completed
	state, err := j.GetState()
	if err != nil {
		return false
	}
	switch state {
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
	}
}

func (j *Job) Init(id JobUUID, creationDate int64) {
	j.JobUUID = id
	j.History = make(JobHistory, 0)
	j.AddHistoryEvent(JobEventCreate, creationDate)
}

func (j *Job) AddHistoryEvent(eventType string, timestamp int64) {
	j.History = append(j.History, JobHistoryEvent{
		EventType: eventType,
		Timestamp: timestamp,
	})
}

func NewJob(payload []byte) (*Job, error) {
	// Create a new job from a backend (redis) payload
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
	return j, nil
}

func NewJobFromRequest(payload []byte) (*Job, error) {
	// Create a new job from a http request
	// A job created from a request does not have a JobUUID and History
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
	}

	return j, nil
}
