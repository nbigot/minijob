package metrics

import (
	"slices"
	"sync"
	"time"

	"github.com/nbigot/minijob/event"
	"github.com/nbigot/minijob/fiberprometheus"
	"github.com/nbigot/minijob/job"
)

type ResourceMetrics struct {
	ResourceName string      `json:"resourceName"` // name of the resource
	JobUUID      job.JobUUID `json:"jobUuid"`      // UUID of the job that locked the resource
	Topic        string      `json:"topic"`        // topic of the job that locked the resource
	StartTime    int64       `json:"startTime"`    // begin time when the resource was locked in seconds
	LockDuration int64       `json:"lockDuration"` // lock duration in seconds
}

type TopicMetrics struct {
	TopicName       string  `json:"topicName"`       // name of the topic
	TotalJobs       uint    `json:"totalJobs"`       // total number of jobs in the topic
	PercentJobs     float64 `json:"percentJobs"`     // percentage of jobs in the topic (0.0 - 100.0)
	ActiveJobs      uint    `json:"activeJobs"`      // number of active jobs in the topic
	SuccessRate     float64 `json:"successRate"`     // success rate of jobs in the topic (0.0 - 1.0)
	AverageDuration float64 `json:"averageDuration"` // average duration of jobs in the topic (in seconds)
}

// Internal struct to track cumulative data for completed jobs
type TopicCompletedJobStats struct {
	CompletedJobsCount   uint  `json:"completedJobsCount"`   // total number of completed jobs (succeeded + failed + canceled)
	CumulativeDurationMs int64 `json:"cumulativeDurationMs"` // cumulative duration in milliseconds for completed jobs
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

type JobStatsMetrics struct {
	ExistingJobs  uint `json:"existingJobs"`  // current number of existing jobs
	CreatedJobs   uint `json:"createdJobs"`   // total number of jobs
	DelayedJobs   uint `json:"delayedJobs"`   // number of delayed jobs
	PendingJobs   uint `json:"pendingJobs"`   // number of pending jobs
	QueuedJobs    uint `json:"queuedJobs"`    // number of queued jobs
	RunningJobs   uint `json:"runningJobs"`   // number of running jobs
	SucceededJobs uint `json:"succeededJobs"` // number of succeeded jobs
	FailedJobs    uint `json:"failedJobs"`    // number of failed jobs
	HiddenJobs    uint `json:"hiddenJobs"`    // number of hidden jobs
	CanceledJobs  uint `json:"canceledJobs"`  // number of canceled jobs
	DeletedJobs   uint `json:"deletedJobs"`   // number of deleted jobs
}

type JobMetricsTopicMap map[string][]JobMetrics

type JobMetricsTopicStatsMap map[string][]TopicMetrics

type JobMetricsResourceStatsMap map[string][]ResourceMetrics

type ServiceMetrics struct {
	// implements IServiceMetrics & IServiceEventObserver interfaces
	JobMetricsByTopicMap      sync.Map          `json:"jobsTopics"`         // metrics by topic ([string]*JobMetrics)
	TopicMetricsByTopicMap    sync.Map          `json:"topicsMetrics"`      // pre-calculated topic metrics by topic ([string]*TopicMetrics)
	CompletedJobsStatsByTopic sync.Map          `json:"completedJobsStats"` // completed jobs stats by topic ([string]*TopicCompletedJobStats)
	ResourceMetricsMap        sync.Map          `json:"resourcesMetrics"`   // metrics by resource ([string]*ResourceMetrics)
	Topics                    []string          // list of topics (cache)
	Metrics                   PrometheusMetrics // metrics for prometheus
	enabledCollect            bool              // enable metrics collection
	mu                        sync.Mutex        // mutex
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

// GetTopicMetrics returns the TopicMetrics for a given topic, creating it if it doesn't exist
func (s *ServiceMetrics) GetTopicMetrics(topic string) *TopicMetrics {
	topicMetrics, exists := s.TopicMetricsByTopicMap.Load(topic)
	if !exists {
		return s.AddTopicMetrics(topic)
	}
	return topicMetrics.(*TopicMetrics)
}

// AddTopicMetrics creates and stores new TopicMetrics for a topic
func (s *ServiceMetrics) AddTopicMetrics(topic string) *TopicMetrics {
	topicMetrics := &TopicMetrics{
		TopicName:       topic,
		TotalJobs:       0,
		PercentJobs:     0.0,
		ActiveJobs:      0,
		SuccessRate:     0.0,
		AverageDuration: 0.0,
	}
	s.TopicMetricsByTopicMap.Store(topic, topicMetrics)
	return topicMetrics
}

// GetCompletedJobStats returns the completed job stats for a given topic, creating it if it doesn't exist
func (s *ServiceMetrics) GetCompletedJobStats(topic string) *TopicCompletedJobStats {
	stats, exists := s.CompletedJobsStatsByTopic.Load(topic)
	if !exists {
		return s.AddCompletedJobStats(topic)
	}
	return stats.(*TopicCompletedJobStats)
}

// AddCompletedJobStats creates and stores new TopicCompletedJobStats for a topic
func (s *ServiceMetrics) AddCompletedJobStats(topic string) *TopicCompletedJobStats {
	stats := &TopicCompletedJobStats{
		CompletedJobsCount:   0,
		CumulativeDurationMs: 0,
	}
	s.CompletedJobsStatsByTopic.Store(topic, stats)
	return stats
}

// UpdateCompletedJobStats updates the cumulative stats when a job completes
func (s *ServiceMetrics) UpdateCompletedJobStats(topic string, durationMs int64) {
	stats := s.GetCompletedJobStats(topic)
	stats.CompletedJobsCount++
	stats.CumulativeDurationMs += durationMs
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

	// Update TopicMetrics in real-time
	jobDurationMs := j.GetDurationMsSinceLastAndFirstEvent()
	s.UpdateTopicMetrics(j.Topic, previousState, newState, jobDurationMs)

	// Update ResourceMetrics for resources locked by the job
	if j.LockResources != nil {
		switch newState {
		case job.JobQueued, job.JobRunning:
			for _, resource := range j.LockResources {
				// Only one job can lock a resource at a time, so we can safely update
				// the metrics
				startTime := j.GetQueuedOrRunningTimestamp()
				resourceMetrics := &ResourceMetrics{
					ResourceName: resource,
					JobUUID:      j.JobUUID,
					Topic:        j.Topic,
					StartTime:    startTime,
					LockDuration: 0, // will be updated later (used by GetResourcesMetrics only)
				}
				s.ResourceMetricsMap.Store(resource, resourceMetrics)
			}
		case job.JobSucceeded, job.JobFailed, job.JobCanceled, job.JobDeleted:
			// If the job is completed and had locked resources, remove the resource metrics
			for _, resource := range j.LockResources {
				// By security check if the resource exists in the map and is locked by this job
				if resourceMetrics, ok := s.ResourceMetricsMap.Load(resource); ok {
					rm := resourceMetrics.(*ResourceMetrics)
					if rm.JobUUID == j.JobUUID {
						// Remove the resource metrics
						s.ResourceMetricsMap.Delete(resource)
					}
				}
			}
		}
	}

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

	// Update metrics (ResourceMetricsMap) for resources locked by the job
	// Fisrt check if the jobs state is complient with resource locking state
	jobState := j.GetState()
	if jobState == job.JobQueued || jobState == job.JobRunning {
		// Then iterate over the locked resources and update their metrics
		for _, resource := range j.LockResources {
			// Only one job can lock a resource at a time, so we can safely update the metrics
			startTime := j.GetQueuedOrRunningTimestamp()
			resourceMetrics := &ResourceMetrics{
				ResourceName: resource,
				JobUUID:      j.JobUUID,
				Topic:        j.Topic,
				StartTime:    startTime,
				LockDuration: 0, // will be updated later (used by GetResourcesMetrics only)
			}
			s.ResourceMetricsMap.Store(resource, resourceMetrics)
		}
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

	s.ResourceMetricsMap = sync.Map{} // clear resource metrics
}

func (s *ServiceMetrics) GetResourcesMetrics() []ResourceMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	stats := []ResourceMetrics{}
	// Compute metrics for resources that are currently locked
	s.ResourceMetricsMap.Range(func(key, value interface{}) bool {
		resourceName := key.(string)
		resourceMetrics := value.(*ResourceMetrics)
		stats = append(stats, ResourceMetrics{
			ResourceName: resourceName,
			JobUUID:      resourceMetrics.JobUUID,
			Topic:        resourceMetrics.Topic,
			StartTime:    resourceMetrics.StartTime,
			LockDuration: now - resourceMetrics.StartTime, // in seconds
		})
		return true
	})

	return stats
}

func (s *ServiceMetrics) GetTopics() []string {
	return s.Topics
}

func (s *ServiceMetrics) GetTopicsStats() []TopicMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()

	stats := []TopicMetrics{}
	totalJobsAllTopics := uint(0)

	// First pass: collect pre-calculated metrics and calculate total jobs
	for _, topic := range s.Topics {
		topicMetrics := s.GetTopicMetrics(topic)
		stats = append(stats, *topicMetrics)
		totalJobsAllTopics += topicMetrics.TotalJobs
	}

	// Second pass: calculate percentages
	for i := range stats {
		if totalJobsAllTopics > 0 {
			stats[i].PercentJobs = (float64(stats[i].TotalJobs) / float64(totalJobsAllTopics)) * 100.0
		} else {
			stats[i].PercentJobs = 0.0
		}
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

	// Also initialize TopicMetrics and CompletedJobStats for the new topic
	s.AddTopicMetrics(topic)
	s.AddCompletedJobStats(topic)

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

func (s *ServiceMetrics) GetJobMetricsByStatus() JobStatsMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()

	var stats JobStatsMetrics

	s.JobMetricsByTopicMap.Range(func(key, value interface{}) bool {
		jobMetrics := value.(*JobMetrics)
		stats.ExistingJobs += jobMetrics.JobsExisting
		stats.CreatedJobs += jobMetrics.JobsStatusCreated
		stats.DelayedJobs += jobMetrics.JobsStatusDelayed
		stats.PendingJobs += jobMetrics.JobsStatusPending
		stats.QueuedJobs += jobMetrics.JobsStatusQueued
		stats.RunningJobs += jobMetrics.JobsStatusRunning
		stats.SucceededJobs += jobMetrics.JobsStatusSucceeded
		stats.FailedJobs += jobMetrics.JobsStatusFailed
		stats.HiddenJobs += jobMetrics.JobsStatusHidden
		stats.CanceledJobs += jobMetrics.JobsStatusCanceled
		stats.DeletedJobs += jobMetrics.JobsCounterDeleted
		return true
	})

	return stats
}

// UpdateTopicMetrics updates the pre-calculated TopicMetrics for a given topic and job
func (s *ServiceMetrics) UpdateTopicMetrics(topic string, previousState job.JobState, newState job.JobState, jobDurationMs int64) {
	if !s.enabledCollect {
		return
	}

	topicMetrics := s.GetTopicMetrics(topic)

	// Update active jobs count
	switch previousState {
	case job.JobPending, job.JobQueued, job.JobRunning:
		topicMetrics.ActiveJobs--
	}

	switch newState {
	case job.JobPending, job.JobQueued, job.JobRunning:
		topicMetrics.ActiveJobs++
	case job.JobCreated:
		topicMetrics.TotalJobs++
	case job.JobDeleted:
		topicMetrics.TotalJobs--
	}

	// Update completed job stats if job just completed
	if isJobCompleted(newState) && !isJobCompleted(previousState) && jobDurationMs > 0 {
		s.UpdateCompletedJobStats(topic, jobDurationMs)

		// Update average duration
		completedStats := s.GetCompletedJobStats(topic)
		if completedStats.CompletedJobsCount > 0 {
			avgDurationMs := completedStats.CumulativeDurationMs / int64(completedStats.CompletedJobsCount)
			topicMetrics.AverageDuration = float64(avgDurationMs) / 1000.0 // convert to seconds
		}
	}

	// Update success rate
	jobMetrics := s.GetMetricByTopic(topic)
	completedJobs := jobMetrics.JobsStatusSucceeded + jobMetrics.JobsStatusFailed + jobMetrics.JobsStatusCanceled
	if completedJobs > 0 {
		topicMetrics.SuccessRate = float64(jobMetrics.JobsStatusSucceeded) / float64(completedJobs)
	} else {
		topicMetrics.SuccessRate = 0.0
	}
}

// isJobCompleted checks if a job state represents a completed job
func isJobCompleted(state job.JobState) bool {
	switch state {
	case job.JobSucceeded, job.JobFailed, job.JobCanceled:
		return true
	default:
		return false
	}
}

func NewServiceMetrics(enabledCollect bool) *ServiceMetrics {
	return &ServiceMetrics{
		JobMetricsByTopicMap:      sync.Map{},
		TopicMetricsByTopicMap:    sync.Map{},
		CompletedJobsStatsByTopic: sync.Map{},
		ResourceMetricsMap:        sync.Map{},
		Topics:                    make([]string, 0),
		enabledCollect:            enabledCollect,
		Metrics:                   PrometheusMetrics{},
	}
}
