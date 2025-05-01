package event

import "github.com/nbigot/minijob/job"

// ServiceEventType is a int type that represents the type of event that can be sent by the service
type ServiceEventType int

const (
	// ServiceEventJobCreated is an event that is sent when a job is created
	ServiceEventJobCreated ServiceEventType = iota
	// ServiceEventJobDelayed is an event that is sent when a job is delayed
	ServiceEventJobDelayed
	// ServiceEventJobPending is an event that is sent when a job is pending
	ServiceEventJobPending
	// ServiceEventJobEnqueued is an event that is sent when a job is enqueued
	ServiceEventJobEnqueued
	// ServiceEventJobStarted is an event that is sent when a job is started
	ServiceEventJobStarted
	// ServiceEventJobSucceeded is an event that is sent when a job is succeeded
	ServiceEventJobSucceeded
	// ServiceEventJobCanceled is an event that is sent when a job is canceled
	ServiceEventJobCanceled
	// ServiceEventJobFailed is an event that is sent when a job is failed
	ServiceEventJobFailed
	// ServiceEventJobHidden is an event that is sent when a job is hidden
	ServiceEventJobHidden
	// ServiceEventJobDeleted is an event that is sent when a job is deleted
	ServiceEventJobDeleted
	// ServiceEventJobTimeout is an event that is sent when a job is timeout
	ServiceEventJobTimeout
	// ServiceEventJobTerminated is an event that is sent when a job is terminated
	ServiceEventJobTerminated
	// ServiceEventJobLoaded is an event that is sent when a job is loaded/restored from the database/disk
	ServiceEventJobLoaded
	// ServiceEventJobDeletedAll is an event that is sent when all jobs are deleted
	ServiceEventJobDeletedAll
	// ServiceEventJobUnlockedAllResources is an event that is sent when all resources are unlocked
	ServiceEventJobUnlockedAllResources
	// ServiceEventJobHealthcheck is an event that is sent when the healthcheck is called
	ServiceEventJobHealthcheck
	// ServiceEventJobMetrics is an event that is sent when the metrics are computed
	ServiceEventJobMetrics
	// ServiceEventEmptyPollReceived is an event that is sent when there is no available job to process to reply to a pull job request
	ServiceEventEmptyPollReceived
	// ServiceEventReady is an event that is sent when the service is ready
	ServiceEventReady
	// ServiceEventShutdown is an event that is sent when the service is shutdown
	ServiceEventShutdown
)

type ServiceEvent struct {
	Type        ServiceEventType
	JobUUID     job.JobUUID
	Topic       string
	JobLifetime int64 // in milliseconds
}

func (t ServiceEventType) String() string {
	switch t {
	case ServiceEventJobCreated:
		return "Created"
	case ServiceEventJobDelayed:
		return "Delayed"
	case ServiceEventJobPending:
		return "Pending"
	case ServiceEventJobEnqueued:
		return "Enqueued"
	case ServiceEventJobStarted:
		return "Started"
	case ServiceEventJobSucceeded:
		return "Succeeded"
	case ServiceEventJobCanceled:
		return "Canceled"
	case ServiceEventJobFailed:
		return "Failed"
	case ServiceEventJobDeleted:
		return "Deleted"
	case ServiceEventJobTimeout:
		return "Timeout"
	case ServiceEventJobTerminated:
		return "Terminated"
	case ServiceEventJobLoaded:
		return "Loaded"
	case ServiceEventJobDeletedAll:
		return "DeletedAll"
	case ServiceEventJobUnlockedAllResources:
		return "UnlockedAllResources"
	case ServiceEventJobHealthcheck:
		return "Healthcheck"
	case ServiceEventJobMetrics:
		return "Metrics"
	case ServiceEventReady:
		return "Ready"
	case ServiceEventShutdown:
		return "Shutdown"
	case ServiceEventEmptyPollReceived:
		return "EmptyPollReceived"
	default:
		return "other"
	}
}
