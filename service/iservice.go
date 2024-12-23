package service

import (
	"github.com/nbigot/minijob/job"
	"go.uber.org/zap"
)

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
	GetMetrics() IServiceMetrics
	GetJobsTopics() []string
	TryEnqueuePendingJobs()
	CheckJobsVisibility()
	GetServiceEventChan() chan ServiceEvent
	GetLogger() *zap.Logger
}
