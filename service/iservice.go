package service

import (
	"github.com/nbigot/minijob/job"
	"go.uber.org/zap"
)

// ServiceEventType is a int type that represents the type of event that can be sent by the service
type ServiceEventType int

const (
	// ServiceEventJobCreated is an event that is sent when a job is created
	ServiceEventJobCreated ServiceEventType = iota
	// ServiceEventJobEnqueued is an event that is sent when a job is enqueued
	ServiceEventJobEnqueued
	// ServiceEventJobStarted is an event that is sent when a job is started
	ServiceEventJobStarted
	// ServiceEventJobSucceeded is an event that is sent when a job is succeeded
	ServiceEventJobSucceeded
	// ServiceEventJobCanceled is an event that is sent when a job is canceled
	ServiceEventJobCanceled
	// ServiceEventJobDeleted is an event that is sent when a job is deleted
	ServiceEventJobDeleted
	// ServiceEventJobTimeout is an event that is sent when a job is timeout
	ServiceEventJobTimeout
	// ServiceEventJobDeletedAll is an event that is sent when all jobs are deleted
	ServiceEventJobDeletedAll
	// ServiceEventJobUnlockedAllResources is an event that is sent when all resources are unlocked
	ServiceEventJobUnlockedAllResources
	// ServiceEventJobHealthcheck is an event that is sent when the healthcheck is called
	ServiceEventJobHealthcheck
	// ServiceEventJobMetrics is an event that is sent when the metrics are computed
	ServiceEventJobMetrics
	// ServiceEventJobWatchdog is an event that is sent when the watchdog is called
	ServiceEventJobWatchdog
	// ServiceEventShutdown is an event that is sent when the service is shutdown
	ServiceEventShutdown
)

type ServiceMetrics struct {
	ResourcesLockedCount uint // number of current locked resources
	JobsCount            uint // number of current existing jobs
	JobsCounterCreated   uint // counter of total created jobs
	JobsCounterPending   uint // counter of total pending jobs
	JobsCounterQueued    uint // counter of total queued jobs
	JobsCounterRunning   uint // counter of total running jobs
	JobsCounterSucceeded uint // counter of total succeeded jobs
	JobsCounterFailed    uint // counter of total failed jobs
	JobsCounterTimeout   uint // counter of total timeout jobs
	JobsCounterDeleted   uint // counter of total deleted jobs
	JobsCounterCanceled  uint // counter of total canceled jobs
	JobsCounterFaillure  uint // counter of total faillure jobs (this is not related to the status of the job)
}

type ServiceEvent struct {
	Type    ServiceEventType
	Metrics ServiceMetrics
	JobUUID job.JobUUID
}

type RequestPullJobs struct {
	JobUUID           *job.JobUUID // if nil pull any job, else pull the specific job
	Topic             string       // topic to pull the job from (if empty pull from any topic)
	NumJobs           uint         // number of jobs to pull (if 0 pull only one job)
	VisibilityTimeout uint         // parameter to hide the job for a specific duration
	WaitTimeSeconds   uint         // parameter enables long-poll (if 0 return immediately if no job available)
}

type ResponsePullJobs struct {
	Jobs []*job.Job `json:"jobs"`
}

type IService interface {
	Init() error
	Stop() error
	Finalize() error
	GetJobsCount() uint
	GetJobsUUIDs() job.JobUUIDList
	LoadJobs() error
	GetJob(job.JobUUID) (*job.Job, error)
	GetAllJobs() ([]*job.Job, error)
	CreateJob(payload []byte) (*job.Job, error)
	PullJobs(*RequestPullJobs) (*ResponsePullJobs, error)
	StartJob(job.JobUUID, *RequestPullJobs) error
	CloneJob(job.JobUUID) (*job.Job, error)
	CancelJob(job.JobUUID) error
	SetJobAsSuccessful(job.JobUUID) error
	FailJob(job.JobUUID) error
	DeleteJob(job.JobUUID) error
	DeleteAllJobs() error
	GenerateNewJobUuid() (job.JobUUID, error)
	GetLockedResources() (job.LockedResources, error)
	UnlockAllResources() error
	ChangeVisibilityTimeoutJob(job.JobUUID, uint) error
	Healthcheck() bool
	ComputeMetrics() error
	TryEnqueuePendingJobs()
	Watchdog()
	GetServiceEventChan() chan ServiceEvent
	GetLogger() *zap.Logger
}
