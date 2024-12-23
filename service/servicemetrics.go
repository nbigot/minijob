package service

import (
	"github.com/nbigot/minijob/job"
)

type IServiceMetrics interface {
	Init() error
	GetJobsTopics() []string
	GetMetricByTopic(topic string) *JobMetrics
	Update(*job.Job)
	UpdateFromHistory(*job.Job)
	UpdateResourcesLockedCountMetric(topic string, inc int)
	OnDeleteAllJobs()
}

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
}

type ServiceMetrics struct {
	// implements IServiceMetrics interface
	JobMetricsByTopicMap map[string]*JobMetrics `json:"jobsTopics"` // metrics by topic
}

func (s *ServiceMetrics) Init() error {
	return nil
}

func (s *ServiceMetrics) OnJobStateChange(previousState job.JobState, newState job.JobState, topic string) {
	if previousState == newState {
		return
	}

	jobMetrics := s.GetMetricByTopic(topic)

	switch previousState {
	case job.JobPending:
		jobMetrics.JobsCounterPending--
	case job.JobQueued:
		jobMetrics.JobsCounterQueued--
	case job.JobRunning:
		jobMetrics.JobsCounterRunning--
	case job.JobSucceeded:
		jobMetrics.JobsCounterSucceeded--
	case job.JobFailed:
		jobMetrics.JobsCounterFailed--
	case job.JobCanceled:
		jobMetrics.JobsCounterCanceled--
	case job.JobDeleted:
		jobMetrics.JobsCounterDeleted--
	}

	switch newState {
	case job.JobPending:
		jobMetrics.JobsCounterPending++
		if previousState == job.JobNoState {
			jobMetrics.JobsCounter++
			jobMetrics.JobsCounterCreated++ //TODO: not sure
		}
	case job.JobQueued:
		jobMetrics.JobsCounterQueued++
	case job.JobRunning:
		jobMetrics.JobsCounterRunning++
	case job.JobSucceeded:
		jobMetrics.JobsCounterSucceeded++
	case job.JobFailed:
		jobMetrics.JobsCounterFailed++
	case job.JobCanceled:
		jobMetrics.JobsCounterCanceled++
	case job.JobDeleted:
		jobMetrics.JobsCounter--
		jobMetrics.JobsCounterDeleted++
	}
}

func (s *ServiceMetrics) Update(j *job.Job) {
	previousState := j.GetPreviousState()
	newState := j.GetState()
	s.OnJobStateChange(previousState, newState, j.Topic)
}

func (s *ServiceMetrics) UpdateFromHistory(j *job.Job) {
	// recompute metrics from job history
	states := j.GetHistoryStates()
	previousState := job.JobNoState
	for _, newState := range states {
		s.OnJobStateChange(previousState, newState, j.Topic)
		previousState = newState
	}
}

func (s *ServiceMetrics) OnDeleteAllJobs() {
	for _, jobMetrics := range s.JobMetricsByTopicMap {
		jobMetrics.JobsCounterDeleted += jobMetrics.JobsCounter
		jobMetrics.JobsCounter = 0
		jobMetrics.JobsCounterPending = 0
		jobMetrics.JobsCounterQueued = 0
		jobMetrics.JobsCounterRunning = 0
		jobMetrics.ResourcesLockedCount = 0
	}
}

func (s *ServiceMetrics) GetJobsTopics() []string {
	topics := make([]string, 0, len(s.JobMetricsByTopicMap))
	for topic := range s.JobMetricsByTopicMap {
		topics = append(topics, topic)
	}

	return topics
}

func (s *ServiceMetrics) UpdateResourcesLockedCountMetric(topic string, inc int) {
	var jobMetrics *JobMetrics
	var found bool
	if jobMetrics, found = s.JobMetricsByTopicMap[topic]; !found {
		jobMetrics = &JobMetrics{}
		s.JobMetricsByTopicMap[topic] = jobMetrics
	}
	if inc < 0 {
		jobMetrics.ResourcesLockedCount -= uint(inc)
	} else {
		jobMetrics.ResourcesLockedCount += uint(inc)
	}
}

func (s *ServiceMetrics) GetMetricByTopic(topic string) *JobMetrics {
	jobMetrics, exists := s.JobMetricsByTopicMap[topic]
	if !exists {
		jobMetrics = &JobMetrics{}
		s.JobMetricsByTopicMap[topic] = jobMetrics
	}
	return jobMetrics
}

func NewSericeMetrics() *ServiceMetrics {
	m := make(map[string]*JobMetrics)
	m[""] = &JobMetrics{}
	return &ServiceMetrics{
		JobMetricsByTopicMap: m,
	}
}
