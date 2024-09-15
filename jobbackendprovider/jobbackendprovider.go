package jobbackendprovider

import (
	"github.com/nbigot/minijob/job"
)

// ServiceEventType is a int type that represents the type of event that can be sent by the service
type JobBackendEventType int

const (
	// EventJobCreated is an event that is sent when a job is created
	EventJobCreated JobBackendEventType = iota
	// EventJobEnqueued is an event that is sent when a job is enqueued
	EventJobEnqueued
	// EventJobDeleted is an event that is sent when a job is deleted
	EventJobDeleted
	// EventJobStarted is an event that is sent when a job is started
	EventJobStarted
	// EventJobSucceeded is an event that is sent when a job is succeeded
	EventJobSucceeded
	// EventJobCanceled is an event that is sent when a job is canceled
	EventJobCanceled
	// EventJobTimeout is an event that is sent when a job is timeout
	EventJobTimeout
	// EventAllJobsDeleted is an event that is sent when all jobs are deleted
	EventAllJobsDeleted
	// EventAllResourcesUnlocked is an event that is sent when all resources are unlocked
	EventAllResourcesUnlocked
	// EventResourceUnlocked is an event that is sent when a resource is unlocked
	EventResourceUnlocked
	// EventFatalError is an event that is sent when a fatal error occurs
	EventFatalError
)

type Event struct {
	Type     JobBackendEventType
	JobUUID  job.JobUUID
	Resource *string
}

type IJobBackendProvider interface {
	Init(notifChan chan Event) error
	Stop() error
	JobExists(jobUUID job.JobUUID) (bool, error)
	LoadJobs() (job.JobMap, error)
	OnJobCreated(j *job.Job) error
	OnJobEnqueued(j *job.Job) error
	OnJobStarted(j *job.Job) error
	OnJobSucceeded(j *job.Job) error
	OnJobTimeout(j *job.Job) error
	OnJobCanceled(j *job.Job) error
	OnJobDeleted(jobUUID job.JobUUID) error
	OnJobsDeleted() error
	OnAllResourcesUnlocked() error
	OnResourceUnlocked(j *job.Job, resource string) error
	Run() error
	Healthcheck() bool
}
