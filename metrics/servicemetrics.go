package metrics

import (
	"slices"
	"sync"
	"time"

	"github.com/nbigot/minijob/event"
	"github.com/nbigot/minijob/fiberprometheus"
	"github.com/nbigot/minijob/job"
)

type TopicMetrics struct {
	TopicName       string  `json:"topicName"`       // name of the topic
	TotalJobs       uint    `json:"totalJobs"`       // total number of jobs in the topic
	PercentJobs     float64 `json:"percentJobs"`     // percentage of jobs in the topic (0.0 - 100.0)
	ActiveJobs      uint    `json:"activeJobs"`      // number of active jobs in the topic
	SuccessRate     float64 `json:"successRate"`     // success rate of jobs in the topic (0.0 - 1.0)
	AverageDuration float64 `json:"averageDuration"` // average duration of jobs in the topic (in seconds)
}

// note: a counter represents a continuously increasing value, unlike a gauge, and does not go down
type JobMetrics struct {
	ResourcesLockedCount uint `json:"resourcesLockedCount"` // current number of locked resources
	JobsCounterDeleted   uint `json:"jobsCounterDeleted"`   // total number of deleted jobs (ever)
	JobsExisting         uint `json:"jobsExisting"`         // current number of existing jobs
	JobsStatusCreated    uint `json:"jobsStatusCreated"`    // current number of created jobs
	JobsStatusDelayed    uint `json:"jobsStatusDelayed"`    // current number of delayed jobs
	JobsStatusPending    uint `json:"jobsStatusPending"`    // current number of pending jobs
	JobsStatusQueued     uint `json:"jobsStatusQueued"`     // current number of queued jobs
	JobsStatusRunning    uint `json:"jobsStatusRunning"`    // current number of running jobs
	JobsStatusSucceeded  uint `json:"jobsStatusSucceeded"`  // current number of succeeded jobs
	JobsStatusFailed     uint `json:"jobsStatusFailed"`     // current number of failed jobs
	JobsStatusHidden     uint `json:"jobsStatusHidden"`     // current number of hidden jobs
	JobsStatusCanceled   uint `json:"jobsStatusCanceled"`   // current number of canceled jobs
}

func (j *JobMetrics) Clear() {
	j.ResourcesLockedCount = 0
	j.JobsExisting = 0
	j.JobsStatusCreated = 0
	j.JobsStatusDelayed = 0
	j.JobsStatusPending = 0
	j.JobsStatusQueued = 0
	j.JobsStatusRunning = 0
	j.JobsStatusSucceeded = 0
	j.JobsStatusFailed = 0
	j.JobsStatusHidden = 0
	j.JobsCounterDeleted = 0
	j.JobsStatusCanceled = 0
}

type JobMetricsTopicMap map[string][]JobMetrics

type JobMetricsTopicStatsMap map[string][]TopicMetrics

type ServiceMetrics struct {
	// implements IServiceMetrics & IServiceEventObserver interfaces
	JobMetricsByTopicMap sync.Map          `json:"jobsTopics"` // metrics by topic ([string]*JobMetrics)
	Topics               []string          // list of topics (cache)
	Metrics              PrometheusMetrics // metrics for prometheus
	enabledCollect       bool              // enable metrics collection
	mu                   sync.Mutex        // mutex
}

func (s *ServiceMetrics) Init() error {
	return s.Metrics.Init()
}

func (s *ServiceMetrics) Shutdown() {
	s.Metrics.Shutdown()
}

func (s *ServiceMetrics) GetFiberPrometheus() *fiberprometheus.FiberPrometheus {
	return s.Metrics.FiberPrometheus
}

func (s *ServiceMetrics) OnJobStateChange(previousState job.JobState, newState job.JobState, jobMetrics *JobMetrics) {
	if previousState == newState {
		return
	}

	switch previousState {
	case job.JobCreated:
		jobMetrics.JobsStatusCreated--
	case job.JobDelayed:
		jobMetrics.JobsStatusDelayed--
	case job.JobPending:
		jobMetrics.JobsStatusPending--
	case job.JobQueued:
		jobMetrics.JobsStatusQueued--
	case job.JobRunning:
		jobMetrics.JobsStatusRunning--
	case job.JobSucceeded:
		jobMetrics.JobsStatusSucceeded--
	case job.JobFailed:
		jobMetrics.JobsStatusFailed--
	case job.JobHidden:
		jobMetrics.JobsStatusHidden--
	case job.JobDeleted:
		jobMetrics.JobsCounterDeleted--
	case job.JobCanceled:
		jobMetrics.JobsStatusCanceled--
	}

	switch newState {
	case job.JobCreated:
		jobMetrics.JobsExisting++
		jobMetrics.JobsStatusCreated++
	case job.JobDelayed:
		jobMetrics.JobsStatusDelayed++
	case job.JobPending:
		jobMetrics.JobsStatusPending++
	case job.JobQueued:
		jobMetrics.JobsStatusQueued++
	case job.JobRunning:
		jobMetrics.JobsStatusRunning++
	case job.JobSucceeded:
		jobMetrics.JobsStatusSucceeded++
	case job.JobFailed:
		jobMetrics.JobsStatusFailed++
	case job.JobHidden:
		jobMetrics.JobsStatusHidden++
	case job.JobCanceled:
		jobMetrics.JobsStatusCanceled++
	case job.JobDeleted:
		jobMetrics.JobsExisting--
		jobMetrics.JobsCounterDeleted++
	}
}

func (s *ServiceMetrics) NotifyEvent(ev event.ServiceEventType) {
	switch ev {
	case event.ServiceEventJobDeletedAll:
		s.OnDeleteAllJobs()
	case event.ServiceEventEmptyPollReceived:
		s.Metrics.OnEvent(event.ServiceEvent{Type: ev}, nil)
	}
}

func (s *ServiceMetrics) NotifyTopicEvent(ev event.ServiceEventType, topic string) {
	if !s.enabledCollect {
		return
	}

	if topic != "*" {
		s.EnsureTopicExists(topic)
	}

	switch ev {
	case event.ServiceEventEmptyPollReceived:
		s.Metrics.OnEvent(event.ServiceEvent{Type: ev, Topic: topic}, nil)
	}
}

func (s *ServiceMetrics) NotifyJobEvent(j *job.Job, ev event.ServiceEventType) {
	if !s.enabledCollect {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if ev == event.ServiceEventJobLoaded {
		s.UpdateMetricsFromJobHistory(j)
		return
	}

	previousState := j.GetPreviousState()
	newState := j.GetState()
	jobMetrics := s.GetMetricByTopic(j.Topic)
	s.OnJobStateChange(previousState, newState, jobMetrics)
	s.Metrics.OnEvent(
		event.ServiceEvent{
			Type:        ev,
			JobUUID:     j.JobUUID,
			Topic:       j.Topic,
			JobLifetime: j.GetDurationMsSinceLastAndFirstEvent(),
		},
		jobMetrics,
	)
}

func (s *ServiceMetrics) UpdateMetricsFromJobHistory(j *job.Job) {
	// recompute metrics from job history
	states := j.GetHistoryStates()
	previousState := job.JobNoState
	jobMetrics := s.GetMetricByTopic(j.Topic)
	for _, newState := range states {
		s.OnJobStateChange(previousState, newState, jobMetrics)
		previousState = newState
	}
}

func (s *ServiceMetrics) OnDeleteAllJobs() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.JobMetricsByTopicMap.Range(func(key, value interface{}) bool {
		jobMetrics := value.(*JobMetrics)
		numberOfDeletedJobs := jobMetrics.JobsCounterDeleted + jobMetrics.JobsExisting
		jobMetrics.Clear()
		jobMetrics.JobsCounterDeleted = numberOfDeletedJobs
		return true
	})
}

func (s *ServiceMetrics) GetTopics() []string {
	return s.Topics
}

func (s *ServiceMetrics) GetTopicsStats() []TopicMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()

	stats := []TopicMetrics{}
	for _, topic := range s.Topics {
		jobMetrics := s.GetMetricByTopic(topic)
		stats = append(stats, TopicMetrics{
			TopicName:       topic,
			TotalJobs:       jobMetrics.JobsExisting + jobMetrics.JobsCounterDeleted,
			PercentJobs:     0.0, // TODO: will be computed later
			ActiveJobs:      jobMetrics.JobsStatusRunning + jobMetrics.JobsStatusPending + jobMetrics.JobsStatusQueued,
			SuccessRate:     0.0, // TODO: will be computed later
			AverageDuration: 0.0, // TODO: will be computed later
		})
	}

	return stats
}

func (s *ServiceMetrics) UpdateResourcesLockedCountMetric(topic string, inc int) {
	jobMetrics := s.GetMetricByTopic(topic)
	if inc < 0 {
		jobMetrics.ResourcesLockedCount -= uint(inc)
	} else {
		jobMetrics.ResourcesLockedCount += uint(inc)
	}
}

func (s *ServiceMetrics) EnsureTopicExists(topic string) {
	_, exists := s.JobMetricsByTopicMap.Load(topic)
	if !exists {
		s.AddTopic(topic)
	}
}

func (s *ServiceMetrics) GetMetricByTopic(topic string) *JobMetrics {
	jobMetrics, exists := s.JobMetricsByTopicMap.Load(topic)
	if !exists {
		return s.AddTopic(topic)
	}
	return jobMetrics.(*JobMetrics)
}

func (s *ServiceMetrics) AddTopic(topic string) *JobMetrics {
	jobMetrics := &JobMetrics{}
	s.JobMetricsByTopicMap.Store(topic, jobMetrics)
	s.Topics = append(s.Topics, topic)
	return jobMetrics
}

func (s *ServiceMetrics) UpdateJobStatistics(jm job.JobMap) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// clear previous observations
	s.Metrics.JobActiveDurationHistogram.Reset()

	// add all topics to the metrics (preserve all previous existing topics)
	for _, topic := range s.Topics {
		s.Metrics.JobActiveDurationHistogram.WithLabelValues(topic).Observe(0.0)
	}

	// update active job duration
	now := time.Now().UnixMilli()
	for _, job := range jm {
		if !job.IsCompleted() {
			durationSeconds := float64(now - job.GetCreationTimestamp())
			s.Metrics.JobActiveDurationHistogram.WithLabelValues(job.Topic).Observe(durationSeconds)
		}
	}

	s.UpdateJobDurationPercentiles(jm, now)
}

func (s *ServiceMetrics) UpdateJobDurationPercentiles(jm job.JobMap, now int64) {
	p := UpdateJobDurationPercentilesByTopic{}
	for _, topic := range s.Topics {
		p[topic] = &UpdateJobDurationPercentilesDetail{
			CompletedJobsDurations: make([]int64, 0, 10000),
			ActiveJobsDurations:    make([]int64, 0, 10000),
		}
	}

	for _, job := range jm {
		if job.IsCompleted() {
			p[job.Topic].CompletedJobsDurations = append(p[job.Topic].CompletedJobsDurations, job.GetDurationMsSinceLastAndFirstEvent())
		} else {
			p[job.Topic].ActiveJobsDurations = append(p[job.Topic].ActiveJobsDurations, now-job.GetCreationTimestamp())
		}
	}

	// for each topic, sort durations list ascending
	// (this is required to compute percentiles)
	for _, detail := range p {
		slices.Sort(detail.CompletedJobsDurations)
		slices.Sort(detail.ActiveJobsDurations)
	}

	// s.Metrics.UpdateJobDurationHistogram(&p)
	// s.Metrics.UpdateJobDurationPercentilesHistogram(&p)
	s.Metrics.UpdateJobDurationGauges(&p)
}

func (s *ServiceMetrics) GetJobMetricsByTopicMap() JobMetricsTopicMap {
	s.mu.Lock()
	defer s.mu.Unlock()

	jobMetricsTopicMap := JobMetricsTopicMap{}
	s.JobMetricsByTopicMap.Range(func(key, value interface{}) bool {
		topic := key.(string)
		jobMetrics := value.(*JobMetrics)
		jobMetricsTopicMap[topic] = append(jobMetricsTopicMap[topic], *jobMetrics)
		return true
	})

	return jobMetricsTopicMap
}

func NewSericeMetrics(enabledCollect bool) *ServiceMetrics {
	return &ServiceMetrics{
		JobMetricsByTopicMap: sync.Map{},
		Topics:               make([]string, 0),
		enabledCollect:       enabledCollect,
		Metrics:              PrometheusMetrics{},
	}
}
