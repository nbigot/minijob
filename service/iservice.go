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
	// ServiceEventJobFailed is an event that is sent when a job is failed
	ServiceEventJobFailed
	// ServiceEventJobDeleted is an event that is sent when a job is deleted
	ServiceEventJobDeleted
	// ServiceEventJobTimeout is an event that is sent when a job is timeout
	ServiceEventJobTimeout
	// ServiceEventJobTerminated is an event that is sent when a job is terminated
	ServiceEventJobTerminated
	// ServiceEventJobDeletedAll is an event that is sent when all jobs are deleted
	ServiceEventJobDeletedAll
	// ServiceEventJobUnlockedAllResources is an event that is sent when all resources are unlocked
	ServiceEventJobUnlockedAllResources
	// ServiceEventJobHealthcheck is an event that is sent when the healthcheck is called
	ServiceEventJobHealthcheck
	// ServiceEventJobMetrics is an event that is sent when the metrics are computed
	ServiceEventJobMetrics
	// ServiceEventReady is an event that is sent when the service is ready
	ServiceEventReady
	// ServiceEventShutdown is an event that is sent when the service is shutdown
	ServiceEventShutdown
)

type JobMetrics struct {
	ResourcesLockedCount  uint `json:"resourcesLockedCount"`  // number of current locked resources
	JobsCounter           uint `json:"jobsCounter"`           // number of current existing jobs
	JobsCounterCreated    uint `json:"jobsCounterCreated"`    // counter of total created jobs
	JobsCounterPending    uint `json:"jobsCounterPending"`    // counter of total pending jobs
	JobsCounterQueued     uint `json:"jobsCounterQueued"`     // counter of total queued jobs
	JobsCounterRunning    uint `json:"jobsCounterRunning"`    // counter of total running jobs
	JobsCounterSucceeded  uint `json:"jobsCounterSucceeded"`  // counter of total succeeded jobs
	JobsCounterFailed     uint `json:"jobsCounterFailed"`     // counter of total failed jobs
	JobsCounterTerminated uint `json:"jobsCounterTerminated"` // counter of total terminated jobs
	JobsCounterTimeout    uint `json:"jobsCounterTimeout"`    // counter of total timeout jobs
	JobsCounterDeleted    uint `json:"jobsCounterDeleted"`    // counter of total deleted jobs
	JobsCounterCanceled   uint `json:"jobsCounterCanceled"`   // counter of total canceled jobs
	JobsCounterFaillure   uint `json:"jobsCounterFaillure"`   // counter of total faillure jobs (this is not related to the status of the job)
}

type ServiceMetrics struct {
	JobMetricsByTopicMap map[string]*JobMetrics `json:"jobsTopics"` // metrics by topic
}

type ServiceEvent struct {
	Type    ServiceEventType
	Metrics *ServiceMetrics
	JobUUID job.JobUUID
	Topic   string
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
	GetMetrics() *ServiceMetrics
	GetJobsTopics() []string
	TryEnqueuePendingJobs()
	CheckJobsVisibility()
	GetServiceEventChan() chan ServiceEvent
	GetLogger() *zap.Logger
}

func (t ServiceEventType) String() string {
	switch t {
	case ServiceEventJobCreated:
		return "Created"
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
	default:
		return "other"
	}
}

func NewSericeMetrics() ServiceMetrics {
	m := make(map[string]*JobMetrics)
	m[""] = &JobMetrics{}
	return ServiceMetrics{
		JobMetricsByTopicMap: m,
	}
}
