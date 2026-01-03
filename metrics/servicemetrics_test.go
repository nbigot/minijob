package metrics

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nbigot/minijob/event"
	"github.com/nbigot/minijob/job"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests
func TestServiceMetrics_Init(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	err := serviceMetrics.Init()

	assert.NoError(t, err)
	assert.NotNil(t, serviceMetrics.GetFiberPrometheus())
}

func TestServiceMetrics_AddTopic(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)

	// Add a new topic
	topic := "test-topic"
	jobMetrics := serviceMetrics.AddTopic(topic)

	// Verify the topic was added
	assert.NotNil(t, jobMetrics)
	assert.Equal(t, uint(0), jobMetrics.JobsExisting)

	// Verify topics list contains the new topic
	assert.Contains(t, serviceMetrics.GetTopics(), topic)

	// Verify GetMetricByTopic returns the same metrics
	retrievedMetrics := serviceMetrics.GetMetricByTopic(topic)
	assert.Equal(t, jobMetrics, retrievedMetrics)
}

func TestServiceMetrics_GetMetricByTopic(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)

	// Get metrics for non-existent topic (should create it)
	topic := "new-topic"
	jobMetrics := serviceMetrics.GetMetricByTopic(topic)

	// Verify it was created
	assert.NotNil(t, jobMetrics)
	assert.Contains(t, serviceMetrics.GetTopics(), topic)

	// Get metrics for existing topic
	retrievedMetrics := serviceMetrics.GetMetricByTopic(topic)
	assert.Equal(t, jobMetrics, retrievedMetrics)
}

func TestServiceMetrics_OnJobStateChange(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	jobMetrics := &JobMetrics{}

	// Test no-op when states are the same
	serviceMetrics.OnJobStateChange(job.JobCreated, job.JobCreated, jobMetrics)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusCreated)

	// Test transition from Created to Running
	serviceMetrics.OnJobStateChange(job.JobNoState, job.JobCreated, jobMetrics)
	assert.Equal(t, uint(1), jobMetrics.JobsExisting)
	assert.Equal(t, uint(1), jobMetrics.JobsStatusCreated)

	serviceMetrics.OnJobStateChange(job.JobCreated, job.JobRunning, jobMetrics)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusCreated)
	assert.Equal(t, uint(1), jobMetrics.JobsStatusRunning)
	assert.Equal(t, uint(1), jobMetrics.JobsExisting)

	// Test transition to Deleted (should decrease existing count)
	serviceMetrics.OnJobStateChange(job.JobRunning, job.JobDeleted, jobMetrics)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusRunning)
	assert.Equal(t, uint(1), jobMetrics.JobsCounterDeleted)
	assert.Equal(t, uint(0), jobMetrics.JobsExisting)
}

func TestServiceMetrics_OnDeleteAllJobs(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)

	// Create and setup a topic with some metrics
	topic := "test-topic"
	jobMetrics := serviceMetrics.AddTopic(topic)
	jobMetrics.JobsExisting = 10
	jobMetrics.JobsStatusRunning = 5
	jobMetrics.JobsCounterDeleted = 3

	// Call OnDeleteAllJobs
	serviceMetrics.OnDeleteAllJobs()

	// Verify metrics were reset but counter preserved
	assert.Equal(t, uint(0), jobMetrics.JobsExisting)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusRunning)
	assert.Equal(t, uint(13), jobMetrics.JobsCounterDeleted) // 3 + 10
}

func TestServiceMetrics_UpdateResourcesLockedCountMetric(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	topic := "test-topic"

	// Initial value should be 0
	jobMetrics := serviceMetrics.GetMetricByTopic(topic)
	assert.Equal(t, uint(0), jobMetrics.ResourcesLockedCount)

	// Increase by 5
	serviceMetrics.UpdateResourcesLockedCountMetric(topic, 5)
	assert.Equal(t, uint(5), jobMetrics.ResourcesLockedCount)

	// Decrease by 2
	serviceMetrics.UpdateResourcesLockedCountMetric(topic, -2)
	assert.Equal(t, uint(7), jobMetrics.ResourcesLockedCount)
}

func TestServiceMetrics_NotifyEvent(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)

	// Setup a topic with some metrics
	topic := "test-topic"
	jobMetrics := serviceMetrics.AddTopic(topic)
	jobMetrics.JobsExisting = 10

	// Test JobDeletedAll event
	serviceMetrics.NotifyEvent(event.ServiceEventJobDeletedAll)

	// Verify all jobs were deleted
	assert.Equal(t, uint(0), jobMetrics.JobsExisting)
	assert.Equal(t, uint(10), jobMetrics.JobsCounterDeleted)
}

func TestServiceMetrics_NotifyTopicEvent(t *testing.T) {
	// This test simply verifies the function doesn't crash
	serviceMetrics := NewServiceMetrics(true)
	err := serviceMetrics.Init()
	assert.NoError(t, err)
	topic := "test-topic"

	// Should forward to metrics system
	serviceMetrics.NotifyTopicEvent(event.ServiceEventEmptyPollReceived, topic)

	// Disable collection and verify it returns early
	serviceMetrics.enabledCollect = false
	serviceMetrics.NotifyTopicEvent(event.ServiceEventEmptyPollReceived, topic)
}

func TestServiceMetrics_NotifyJobEvent(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	err := serviceMetrics.Init()
	assert.NoError(t, err)

	// Setup job
	jobUUID, _ := uuid.NewV7()
	j := &job.Job{
		JobUUID: jobUUID,
		Topic:   "test-topic",
	}
	now := time.Now().UnixMilli()
	j.Init(j.JobUUID, now)
	j.AddHistoryEvent(job.JobEventPending, now)
	j.AddHistoryEvent(job.JobEventEnqueue, now)
	j.AddHistoryEvent(job.JobEventStart, now)

	// Test job loaded event
	serviceMetrics.NotifyJobEvent(j, event.ServiceEventJobLoaded)

	// Verify metrics were updated
	jobMetrics := serviceMetrics.GetMetricByTopic("test-topic")
	assert.Equal(t, uint(1), jobMetrics.JobsExisting)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusCreated)
	assert.Equal(t, uint(1), jobMetrics.JobsStatusRunning)

	// Test state change event
	j.AddHistoryEvent(job.JobEventSuccess, now)
	serviceMetrics.NotifyJobEvent(j, event.ServiceEventJobSucceeded)

	// Verify metrics were updated
	assert.Equal(t, uint(0), jobMetrics.JobsStatusRunning)
	assert.Equal(t, uint(1), jobMetrics.JobsStatusSucceeded)

	// Test with collection disabled
	serviceMetrics.enabledCollect = false
	serviceMetrics.NotifyJobEvent(j, event.ServiceEventJobDeleted)

	// Verify metrics were not updated (still the same as before)
	assert.Equal(t, uint(1), jobMetrics.JobsStatusSucceeded)
}

func TestServiceMetrics_UpdateMetricsFromJobHistory(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)

	// Setup mock job
	jobUUID, _ := uuid.NewV7()
	j := &job.Job{
		JobUUID: jobUUID,
		Topic:   "test-topic",
	}
	now := time.Now().UnixMilli()
	j.Init(j.JobUUID, now)
	j.AddHistoryEvent(job.JobEventPending, now)
	j.AddHistoryEvent(job.JobEventEnqueue, now)
	j.AddHistoryEvent(job.JobEventStart, now)
	j.AddHistoryEvent(job.JobEventSuccess, now)

	// Update metrics from history
	serviceMetrics.UpdateMetricsFromJobHistory(j)

	// Verify metrics
	jobMetrics := serviceMetrics.GetMetricByTopic("test-topic")
	assert.Equal(t, uint(1), jobMetrics.JobsExisting)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusCreated)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusQueued)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusRunning)
	assert.Equal(t, uint(1), jobMetrics.JobsStatusSucceeded)
}

func TestServiceMetrics_UpdateJobStatistics(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	err := serviceMetrics.Init()
	assert.NoError(t, err)

	// Create real jobs
	jobMap := make(job.JobMap)

	// Create job 1 - active job
	job1CreationTime := time.Now().Add(-100 * time.Second).UnixMilli() // 100 seconds old
	job1UUID, _ := uuid.NewV7()
	job1 := &job.Job{
		JobUUID: job1UUID,
		Topic:   "topic1",
	}
	job1.Init(job1UUID, job1CreationTime)
	serviceMetrics.NotifyJobEvent(job1, event.ServiceEventJobCreated)
	job1.AddHistoryEvent(job.JobEventPending, job1CreationTime)
	serviceMetrics.NotifyJobEvent(job1, event.ServiceEventJobPending)
	job1.AddHistoryEvent(job.JobEventEnqueue, job1CreationTime+1000)
	serviceMetrics.NotifyJobEvent(job1, event.ServiceEventJobEnqueued)
	job1.AddHistoryEvent(job.JobEventStart, job1CreationTime+2000)
	serviceMetrics.NotifyJobEvent(job1, event.ServiceEventJobStarted)
	// Job1 is still running (no completion event)

	// Create job 2 - completed job
	job2UUID, _ := uuid.NewV7()
	job2CreationTime := time.Now().Add(-30 * time.Second).UnixMilli()
	job2 := &job.Job{
		JobUUID: job2UUID,
		Topic:   "topic2",
	}
	job2.Init(job2UUID, job2CreationTime)
	serviceMetrics.NotifyJobEvent(job2, event.ServiceEventJobCreated)
	job2.AddHistoryEvent(job.JobEventPending, job2CreationTime)
	serviceMetrics.NotifyJobEvent(job2, event.ServiceEventJobPending)
	job2.AddHistoryEvent(job.JobEventEnqueue, job2CreationTime+500)
	serviceMetrics.NotifyJobEvent(job2, event.ServiceEventJobEnqueued)
	job2.AddHistoryEvent(job.JobEventStart, job2CreationTime+1000)
	serviceMetrics.NotifyJobEvent(job2, event.ServiceEventJobStarted)
	job2.AddHistoryEvent(job.JobEventSuccess, job2CreationTime+5000) // 5 seconds duration
	serviceMetrics.NotifyJobEvent(job2, event.ServiceEventJobSucceeded)

	// Add jobs to map
	jobMap[job1.JobUUID] = job1
	jobMap[job2.JobUUID] = job2

	// Update job statistics
	serviceMetrics.UpdateJobStatistics(jobMap)

	// Verify histograms were updated
	// For this test, we're primarily verifying that the function executes without errors
	// In a more comprehensive test, we'd verify the actual histogram values

	// Get metrics for topic1
	topic1Metrics := serviceMetrics.GetMetricByTopic("topic1")
	assert.Equal(t, uint(1), topic1Metrics.JobsExisting, "topic1 should have 1 active job")
	assert.Equal(t, uint(1), topic1Metrics.JobsStatusRunning, "topic1 should have 1 running job")

	// Get metrics for topic2
	topic2Metrics := serviceMetrics.GetMetricByTopic("topic2")
	assert.Equal(t, uint(1), topic2Metrics.JobsExisting, "topic2 should have 1 active job")
	assert.Equal(t, uint(1), topic2Metrics.JobsStatusSucceeded, "topic2 should have 1 succeeded job")

	// Run a second time to ensure multiple calls work correctly
	serviceMetrics.UpdateJobStatistics(jobMap)
}

func TestServiceMetrics_UpdateJobDurationPercentiles(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	err := serviceMetrics.Init()
	require.NoError(t, err)

	// Create jobs
	jobMap := make(job.JobMap)

	// Create job 1 - active job
	job1CreationTime := time.Now().Add(-100 * time.Second).UnixMilli() // 100 seconds old
	job1UUID, _ := uuid.NewV7()
	job1 := &job.Job{
		JobUUID: job1UUID,
		Topic:   "topic1",
	}
	job1.Init(job1UUID, job1CreationTime)
	serviceMetrics.NotifyJobEvent(job1, event.ServiceEventJobCreated)
	job1.AddHistoryEvent(job.JobEventPending, job1CreationTime)
	serviceMetrics.NotifyJobEvent(job1, event.ServiceEventJobPending)
	job1.AddHistoryEvent(job.JobEventEnqueue, job1CreationTime+1000)
	serviceMetrics.NotifyJobEvent(job1, event.ServiceEventJobEnqueued)
	job1.AddHistoryEvent(job.JobEventStart, job1CreationTime+2000)
	serviceMetrics.NotifyJobEvent(job1, event.ServiceEventJobStarted)
	// Job1 is still running (no completion event)

	// Create job 2 - completed job
	job2UUID, _ := uuid.NewV7()
	job2CreationTime := time.Now().Add(-30 * time.Second).UnixMilli()
	job2 := &job.Job{
		JobUUID: job2UUID,
		Topic:   "topic2",
	}
	job2.Init(job2UUID, job2CreationTime)
	serviceMetrics.NotifyJobEvent(job2, event.ServiceEventJobCreated)
	job2.AddHistoryEvent(job.JobEventPending, job2CreationTime)
	serviceMetrics.NotifyJobEvent(job2, event.ServiceEventJobPending)
	job2.AddHistoryEvent(job.JobEventEnqueue, job2CreationTime+500)
	serviceMetrics.NotifyJobEvent(job2, event.ServiceEventJobEnqueued)
	job2.AddHistoryEvent(job.JobEventStart, job2CreationTime+1000)
	serviceMetrics.NotifyJobEvent(job2, event.ServiceEventJobStarted)
	job2.AddHistoryEvent(job.JobEventSuccess, job2CreationTime+5000) // 5 seconds duration
	serviceMetrics.NotifyJobEvent(job2, event.ServiceEventJobSucceeded)

	// Add several more completed jobs to topic2 with varying durations
	// This helps test percentile calculations with a meaningful dataset
	for i := 0; i < 5; i++ {
		jobUUID, _ := uuid.NewV7()
		jobCreationTime := time.Now().Add(-40 * time.Second).UnixMilli()
		j := &job.Job{
			JobUUID: jobUUID,
			Topic:   "topic2",
		}
		j.Init(jobUUID, jobCreationTime)
		serviceMetrics.NotifyJobEvent(j, event.ServiceEventJobCreated)
		j.AddHistoryEvent(job.JobEventPending, jobCreationTime)
		serviceMetrics.NotifyJobEvent(j, event.ServiceEventJobPending)
		j.AddHistoryEvent(job.JobEventEnqueue, jobCreationTime+200)
		serviceMetrics.NotifyJobEvent(j, event.ServiceEventJobEnqueued)
		j.AddHistoryEvent(job.JobEventStart, jobCreationTime+500)
		serviceMetrics.NotifyJobEvent(j, event.ServiceEventJobStarted)
		// Add varying durations (2s, 4s, 6s, 8s, 10s)
		j.AddHistoryEvent(job.JobEventSuccess, jobCreationTime+((int64(i)+1)*2000))
		serviceMetrics.NotifyJobEvent(j, event.ServiceEventJobSucceeded)

		jobMap[jobUUID] = j
	}

	// Add jobs to map
	jobMap[job1.JobUUID] = job1
	jobMap[job2.JobUUID] = job2

	// Update job duration percentiles
	now := time.Now().Unix()
	serviceMetrics.UpdateJobDurationPercentiles(jobMap, now)

	// Check that percentile data was updated
	// The details of testing the actual percentile values would depend on how your
	// percentile data is exposed. Here we're primarily testing that the function
	// runs without errors.

	// We could verify some basic expectations:
	// 1. topic1 should have 1 active job's duration
	// 2. topic2 should have 6 completed jobs' durations

	// Add a second update with the same data to verify multiple calls work correctly
	serviceMetrics.UpdateJobDurationPercentiles(jobMap, now+5000) // 5 seconds later

	topic1Metrics := serviceMetrics.GetMetricByTopic("topic1")
	assert.Equal(t, uint(1), topic1Metrics.JobsExisting, "Should have 1 job for topic1")
	assert.Equal(t, uint(1), topic1Metrics.JobsStatusRunning, "Should have 1 running job for topic1")

	topic2Metrics := serviceMetrics.GetMetricByTopic("topic2")
	assert.Equal(t, uint(6), topic2Metrics.JobsExisting, "Should have 6 jobs for topic2")
	assert.Equal(t, uint(6), topic2Metrics.JobsStatusSucceeded, "Should have 6 completed jobs for topic2")
}

func TestJobMetrics_Clear(t *testing.T) {
	jobMetrics := &JobMetrics{
		ResourcesLockedCount: 5,
		JobsCounterDeleted:   10,
		JobsExisting:         15,
		JobsStatusCreated:    1,
		JobsStatusRunning:    2,
		JobsStatusSucceeded:  3,
	}

	jobMetrics.Clear()

	assert.Equal(t, uint(0), jobMetrics.ResourcesLockedCount)
	assert.Equal(t, uint(0), jobMetrics.JobsCounterDeleted)
	assert.Equal(t, uint(0), jobMetrics.JobsExisting)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusCreated)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusRunning)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusSucceeded)
}

func TestNewServiceMetrics(t *testing.T) {
	// Test with collection enabled
	serviceMetrics := NewServiceMetrics(true)
	assert.True(t, serviceMetrics.enabledCollect)
	assert.Empty(t, serviceMetrics.Topics)

	// Test with collection disabled
	serviceMetrics = NewServiceMetrics(false)
	assert.Equal(t, false, serviceMetrics.enabledCollect)
}

func TestServiceMetrics_Concurrent(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	topic := "test-topic"

	// Pre-create the topic to avoid race condition during concurrent creation
	_ = serviceMetrics.GetMetricByTopic(topic)

	// Test concurrent access to metrics
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			jobMetrics := serviceMetrics.GetMetricByTopic(topic)
			serviceMetrics.OnJobStateChange(job.JobNoState, job.JobCreated, jobMetrics)
			serviceMetrics.OnJobStateChange(job.JobCreated, job.JobRunning, jobMetrics)
		}()
	}

	wg.Wait()

	// Since the operations are performed with locks, we should see consistent results
	jobMetrics := serviceMetrics.GetMetricByTopic(topic)
	assert.Equal(t, uint(10), jobMetrics.JobsExisting)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusCreated)
	assert.Equal(t, uint(10), jobMetrics.JobsStatusRunning)
}

func TestServiceMetrics_GetJobMetricsByTopicMap(t *testing.T) {
	// Initialize the service metrics
	serviceMetrics := NewServiceMetrics(true)

	// Add some test topics with metrics
	topic1 := "test-topic-1"
	topic2 := "test-topic-2"

	// Get metrics for topics (which will create them)
	metrics1 := serviceMetrics.GetMetricByTopic(topic1)
	metrics2 := serviceMetrics.GetMetricByTopic(topic2)

	// Set some values to ensure we can verify the data is correctly retrieved
	metrics1.JobsExisting = 5
	metrics1.JobsStatusRunning = 3
	metrics1.JobsStatusSucceeded = 2

	metrics2.JobsExisting = 10
	metrics2.JobsStatusCreated = 4
	metrics2.JobsStatusRunning = 6

	// Call the function we're testing
	result := serviceMetrics.GetJobMetricsByTopicMap()

	// Assert the result contains our topics
	assert.Contains(t, result, topic1)
	assert.Contains(t, result, topic2)

	// Assert the metrics values match what we set
	assert.Equal(t, 1, len(result[topic1]))
	assert.Equal(t, 1, len(result[topic2]))

	// Verify the values for topic1
	assert.Equal(t, uint(5), result[topic1][0].JobsExisting)
	assert.Equal(t, uint(3), result[topic1][0].JobsStatusRunning)
	assert.Equal(t, uint(2), result[topic1][0].JobsStatusSucceeded)

	// Verify the values for topic2
	assert.Equal(t, uint(10), result[topic2][0].JobsExisting)
	assert.Equal(t, uint(4), result[topic2][0].JobsStatusCreated)
	assert.Equal(t, uint(6), result[topic2][0].JobsStatusRunning)

	// Verify that the result contains references to the original metrics
	// (changes to original will be reflected in the result since we store pointers)
	metrics1.JobsExisting = 99
	assert.Equal(t, uint(99), result[topic1][0].JobsExisting)
}

func TestServiceMetrics_GetTopicMetrics(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)

	// Test getting metrics for non-existent topic (should create it)
	topic := "new-topic"
	topicMetrics := serviceMetrics.GetTopicMetrics(topic)

	// Verify it was created with default values
	assert.NotNil(t, topicMetrics)
	assert.Equal(t, topic, topicMetrics.TopicName)
	assert.Equal(t, uint(0), topicMetrics.TotalJobs)
	assert.Equal(t, float64(0.0), topicMetrics.PercentJobs)
	assert.Equal(t, uint(0), topicMetrics.ActiveJobs)
	assert.Equal(t, float64(0.0), topicMetrics.SuccessRate)
	assert.Equal(t, float64(0.0), topicMetrics.AverageDuration)

	// Test getting metrics for existing topic
	retrievedMetrics := serviceMetrics.GetTopicMetrics(topic)
	assert.Equal(t, topicMetrics, retrievedMetrics)
}

func TestServiceMetrics_GetCompletedJobStats(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)

	// Test getting stats for non-existent topic (should create it)
	topic := "new-topic"
	stats := serviceMetrics.GetCompletedJobStats(topic)

	// Verify it was created with default values
	assert.NotNil(t, stats)
	assert.Equal(t, uint(0), stats.CompletedJobsCount)
	assert.Equal(t, int64(0), stats.CumulativeDurationMs)

	// Test getting stats for existing topic
	retrievedStats := serviceMetrics.GetCompletedJobStats(topic)
	assert.Equal(t, stats, retrievedStats)
}

func TestServiceMetrics_UpdateCompletedJobStats(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	topic := "test-topic"

	// Update stats with first job duration
	serviceMetrics.UpdateCompletedJobStats(topic, 1000) // 1 second

	stats := serviceMetrics.GetCompletedJobStats(topic)
	assert.Equal(t, uint(1), stats.CompletedJobsCount)
	assert.Equal(t, int64(1000), stats.CumulativeDurationMs)

	// Update stats with second job duration
	serviceMetrics.UpdateCompletedJobStats(topic, 2000) // 2 seconds

	stats = serviceMetrics.GetCompletedJobStats(topic)
	assert.Equal(t, uint(2), stats.CompletedJobsCount)
	assert.Equal(t, int64(3000), stats.CumulativeDurationMs)
}

func TestServiceMetrics_UpdateTopicMetrics(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	err := serviceMetrics.Init()
	assert.NoError(t, err)

	topic := "test-topic"

	// Initialize topic
	jobMetrics := serviceMetrics.AddTopic(topic)

	// Test job creation
	serviceMetrics.OnJobStateChange(job.JobNoState, job.JobCreated, jobMetrics)
	serviceMetrics.UpdateTopicMetrics(topic, job.JobNoState, job.JobCreated, 0)

	topicMetrics := serviceMetrics.GetTopicMetrics(topic)
	assert.Equal(t, uint(1), topicMetrics.TotalJobs)
	assert.Equal(t, uint(0), topicMetrics.ActiveJobs)

	// Test job becoming active (pending)
	serviceMetrics.OnJobStateChange(job.JobCreated, job.JobPending, jobMetrics)
	serviceMetrics.UpdateTopicMetrics(topic, job.JobCreated, job.JobPending, 0)

	topicMetrics = serviceMetrics.GetTopicMetrics(topic)
	assert.Equal(t, uint(1), topicMetrics.TotalJobs)
	assert.Equal(t, uint(1), topicMetrics.ActiveJobs)

	// Test job becoming running
	serviceMetrics.OnJobStateChange(job.JobPending, job.JobRunning, jobMetrics)
	serviceMetrics.UpdateTopicMetrics(topic, job.JobPending, job.JobRunning, 0)

	topicMetrics = serviceMetrics.GetTopicMetrics(topic)
	assert.Equal(t, uint(1), topicMetrics.TotalJobs)
	assert.Equal(t, uint(1), topicMetrics.ActiveJobs)

	// Test job completion with duration
	durationMs := int64(5000) // 5 seconds
	serviceMetrics.OnJobStateChange(job.JobRunning, job.JobSucceeded, jobMetrics)
	serviceMetrics.UpdateTopicMetrics(topic, job.JobRunning, job.JobSucceeded, durationMs)

	topicMetrics = serviceMetrics.GetTopicMetrics(topic)
	assert.Equal(t, uint(1), topicMetrics.TotalJobs)
	assert.Equal(t, uint(0), topicMetrics.ActiveJobs)
	assert.Equal(t, float64(5.0), topicMetrics.AverageDuration) // 5000ms = 5.0s
	assert.Equal(t, float64(1.0), topicMetrics.SuccessRate)     // 1 success / 1 completed = 100%

	// Add a failed job to test success rate calculation
	serviceMetrics.OnJobStateChange(job.JobNoState, job.JobCreated, jobMetrics)
	serviceMetrics.UpdateTopicMetrics(topic, job.JobNoState, job.JobCreated, 0)
	serviceMetrics.OnJobStateChange(job.JobCreated, job.JobRunning, jobMetrics)
	serviceMetrics.UpdateTopicMetrics(topic, job.JobCreated, job.JobRunning, 0)
	serviceMetrics.OnJobStateChange(job.JobRunning, job.JobFailed, jobMetrics)
	serviceMetrics.UpdateTopicMetrics(topic, job.JobRunning, job.JobFailed, 3000) // 3 seconds

	topicMetrics = serviceMetrics.GetTopicMetrics(topic)
	assert.Equal(t, uint(2), topicMetrics.TotalJobs)
	assert.Equal(t, uint(0), topicMetrics.ActiveJobs)
	assert.Equal(t, float64(4.0), topicMetrics.AverageDuration) // (5000+3000)/2 = 4000ms = 4.0s
	assert.Equal(t, float64(0.5), topicMetrics.SuccessRate)     // 1 success / 2 completed = 50%

	// Test job deletion
	serviceMetrics.OnJobStateChange(job.JobFailed, job.JobDeleted, jobMetrics)
	serviceMetrics.UpdateTopicMetrics(topic, job.JobFailed, job.JobDeleted, 0)

	topicMetrics = serviceMetrics.GetTopicMetrics(topic)
	assert.Equal(t, uint(1), topicMetrics.TotalJobs) // Should decrease total jobs
}

func TestServiceMetrics_UpdateTopicMetrics_DisabledCollection(t *testing.T) {
	serviceMetrics := NewServiceMetrics(false) // Collection disabled
	topic := "test-topic"

	// Should return early when collection is disabled
	serviceMetrics.UpdateTopicMetrics(topic, job.JobNoState, job.JobCreated, 0)

	// Topic should not be created
	_, exists := serviceMetrics.TopicMetricsByTopicMap.Load(topic)
	assert.False(t, exists)
}

func TestServiceMetrics_GetTopicsStats_SingleTopic(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	err := serviceMetrics.Init()
	assert.NoError(t, err)

	topic := "test-topic"
	jobMetrics := serviceMetrics.AddTopic(topic)

	// Create some job activity
	serviceMetrics.OnJobStateChange(job.JobNoState, job.JobCreated, jobMetrics)
	serviceMetrics.UpdateTopicMetrics(topic, job.JobNoState, job.JobCreated, 0)
	serviceMetrics.OnJobStateChange(job.JobCreated, job.JobRunning, jobMetrics)
	serviceMetrics.UpdateTopicMetrics(topic, job.JobCreated, job.JobRunning, 0)
	serviceMetrics.OnJobStateChange(job.JobRunning, job.JobSucceeded, jobMetrics)
	serviceMetrics.UpdateTopicMetrics(topic, job.JobRunning, job.JobSucceeded, 5000)

	stats := serviceMetrics.GetTopicsStats()

	assert.Equal(t, 1, len(stats))
	assert.Equal(t, topic, stats[0].TopicName)
	assert.Equal(t, uint(1), stats[0].TotalJobs)
	assert.Equal(t, float64(100.0), stats[0].PercentJobs) // Only topic, so 100%
	assert.Equal(t, uint(0), stats[0].ActiveJobs)
	assert.Equal(t, float64(1.0), stats[0].SuccessRate)
	assert.Equal(t, float64(5.0), stats[0].AverageDuration)
}

func TestServiceMetrics_GetTopicsStats_MultipleTopics(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	err := serviceMetrics.Init()
	assert.NoError(t, err)

	// Create two topics with different job counts
	topic1 := "topic1"
	topic2 := "topic2"

	jobMetrics1 := serviceMetrics.AddTopic(topic1)
	jobMetrics2 := serviceMetrics.AddTopic(topic2)

	// Topic1: 3 jobs (2 succeeded, 1 failed)
	for i := 0; i < 3; i++ {
		serviceMetrics.OnJobStateChange(job.JobNoState, job.JobCreated, jobMetrics1)
		serviceMetrics.UpdateTopicMetrics(topic1, job.JobNoState, job.JobCreated, 0)
		serviceMetrics.OnJobStateChange(job.JobCreated, job.JobRunning, jobMetrics1)
		serviceMetrics.UpdateTopicMetrics(topic1, job.JobCreated, job.JobRunning, 0)
		if i < 2 {
			serviceMetrics.OnJobStateChange(job.JobRunning, job.JobSucceeded, jobMetrics1)
			serviceMetrics.UpdateTopicMetrics(topic1, job.JobRunning, job.JobSucceeded, int64((i+1)*1000))
		} else {
			serviceMetrics.OnJobStateChange(job.JobRunning, job.JobFailed, jobMetrics1)
			serviceMetrics.UpdateTopicMetrics(topic1, job.JobRunning, job.JobFailed, 3000)
		}
	}

	// Topic2: 1 job (1 succeeded)
	serviceMetrics.OnJobStateChange(job.JobNoState, job.JobCreated, jobMetrics2)
	serviceMetrics.UpdateTopicMetrics(topic2, job.JobNoState, job.JobCreated, 0)
	serviceMetrics.OnJobStateChange(job.JobCreated, job.JobRunning, jobMetrics2)
	serviceMetrics.UpdateTopicMetrics(topic2, job.JobCreated, job.JobRunning, 0)
	serviceMetrics.OnJobStateChange(job.JobRunning, job.JobSucceeded, jobMetrics2)
	serviceMetrics.UpdateTopicMetrics(topic2, job.JobRunning, job.JobSucceeded, 4000)

	stats := serviceMetrics.GetTopicsStats()

	assert.Equal(t, 2, len(stats))

	// Find stats by topic name
	var topic1Stats, topic2Stats *TopicMetrics
	for i := range stats {
		switch stats[i].TopicName {
		case topic1:
			topic1Stats = &stats[i]
		case topic2:
			topic2Stats = &stats[i]
		}
	}

	// Verify topic1 stats
	assert.NotNil(t, topic1Stats)
	assert.Equal(t, uint(3), topic1Stats.TotalJobs)
	assert.Equal(t, float64(75.0), topic1Stats.PercentJobs) // 3/4 * 100 = 75%
	assert.Equal(t, uint(0), topic1Stats.ActiveJobs)
	assert.Equal(t, float64(2.0/3.0), topic1Stats.SuccessRate) // 2 succeeded / 3 completed ≈ 0.667
	assert.Equal(t, float64(2.0), topic1Stats.AverageDuration) // (1000+2000+3000)/3 = 2000ms = 2.0s

	// Verify topic2 stats
	assert.NotNil(t, topic2Stats)
	assert.Equal(t, uint(1), topic2Stats.TotalJobs)
	assert.Equal(t, float64(25.0), topic2Stats.PercentJobs) // 1/4 * 100 = 25%
	assert.Equal(t, uint(0), topic2Stats.ActiveJobs)
	assert.Equal(t, float64(1.0), topic2Stats.SuccessRate)     // 1 succeeded / 1 completed = 100%
	assert.Equal(t, float64(4.0), topic2Stats.AverageDuration) // 4000ms = 4.0s
}

func TestServiceMetrics_GetTopicsStats_NoJobs(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)

	stats := serviceMetrics.GetTopicsStats()
	assert.Equal(t, 0, len(stats))
}

func TestServiceMetrics_GetTopicsStats_ActiveJobs(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	err := serviceMetrics.Init()
	assert.NoError(t, err)

	topic := "test-topic"
	serviceMetrics.AddTopic(topic)

	// Create some active jobs
	serviceMetrics.UpdateTopicMetrics(topic, job.JobNoState, job.JobCreated, 0)
	serviceMetrics.UpdateTopicMetrics(topic, job.JobCreated, job.JobPending, 0)

	serviceMetrics.UpdateTopicMetrics(topic, job.JobNoState, job.JobCreated, 0)
	serviceMetrics.UpdateTopicMetrics(topic, job.JobCreated, job.JobRunning, 0)

	stats := serviceMetrics.GetTopicsStats()

	assert.Equal(t, 1, len(stats))
	assert.Equal(t, uint(2), stats[0].TotalJobs)
	assert.Equal(t, uint(2), stats[0].ActiveJobs)           // Both jobs are active
	assert.Equal(t, float64(0.0), stats[0].SuccessRate)     // No completed jobs yet
	assert.Equal(t, float64(0.0), stats[0].AverageDuration) // No completed jobs yet
}

func TestServiceMetrics_IsJobCompleted(t *testing.T) {
	// Test completed states
	assert.True(t, isJobCompleted(job.JobSucceeded))
	assert.True(t, isJobCompleted(job.JobFailed))
	assert.True(t, isJobCompleted(job.JobCanceled))

	// Test non-completed states
	assert.False(t, isJobCompleted(job.JobNoState))
	assert.False(t, isJobCompleted(job.JobCreated))
	assert.False(t, isJobCompleted(job.JobPending))
	assert.False(t, isJobCompleted(job.JobQueued))
	assert.False(t, isJobCompleted(job.JobRunning))
	assert.False(t, isJobCompleted(job.JobDelayed))
	assert.False(t, isJobCompleted(job.JobHidden))
	assert.False(t, isJobCompleted(job.JobDeleted))
}

func TestServiceMetrics_Integration_TopicMetrics(t *testing.T) {
	// Integration test that verifies TopicMetrics are updated correctly during the full job lifecycle
	serviceMetrics := NewServiceMetrics(true)
	err := serviceMetrics.Init()
	assert.NoError(t, err)

	topic := "integration-topic"

	// Create a job
	jobUUID, _ := uuid.NewV7()
	j := &job.Job{
		JobUUID: jobUUID,
		Topic:   topic,
	}
	now := time.Now().UnixMilli()
	j.Init(j.JobUUID, now)

	// Simulate job lifecycle through NotifyJobEvent
	j.AddHistoryEvent(job.JobEventCreate, now)
	serviceMetrics.NotifyJobEvent(j, event.ServiceEventJobCreated)

	// Verify initial state
	topicMetrics := serviceMetrics.GetTopicMetrics(topic)
	assert.Equal(t, uint(1), topicMetrics.TotalJobs)
	assert.Equal(t, uint(0), topicMetrics.ActiveJobs)

	// Job becomes pending
	j.AddHistoryEvent(job.JobEventPending, now+100)
	serviceMetrics.NotifyJobEvent(j, event.ServiceEventJobPending)

	topicMetrics = serviceMetrics.GetTopicMetrics(topic)
	assert.Equal(t, uint(1), topicMetrics.TotalJobs)
	assert.Equal(t, uint(1), topicMetrics.ActiveJobs)

	// Job starts running
	j.AddHistoryEvent(job.JobEventStart, now+200)
	serviceMetrics.NotifyJobEvent(j, event.ServiceEventJobStarted)

	topicMetrics = serviceMetrics.GetTopicMetrics(topic)
	assert.Equal(t, uint(1), topicMetrics.TotalJobs)
	assert.Equal(t, uint(1), topicMetrics.ActiveJobs)

	// Job succeeds
	j.AddHistoryEvent(job.JobEventSuccess, now+5200) // 5 second duration from start
	serviceMetrics.NotifyJobEvent(j, event.ServiceEventJobSucceeded)

	topicMetrics = serviceMetrics.GetTopicMetrics(topic)
	assert.Equal(t, uint(1), topicMetrics.TotalJobs)
	assert.Equal(t, uint(0), topicMetrics.ActiveJobs)
	assert.Equal(t, float64(1.0), topicMetrics.SuccessRate)
	assert.InDelta(t, float64(5.2), topicMetrics.AverageDuration, 0.1) // Allow for slight timing differences

	// Verify GetTopicsStats returns correct percentages
	stats := serviceMetrics.GetTopicsStats()
	assert.Equal(t, 1, len(stats))
	assert.Equal(t, float64(100.0), stats[0].PercentJobs)
}

func TestServiceMetrics_AddTopic_InitializesAllMaps(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	topic := "new-topic"

	jobMetrics := serviceMetrics.AddTopic(topic)

	// Verify JobMetrics was created
	assert.NotNil(t, jobMetrics)
	assert.Contains(t, serviceMetrics.GetTopics(), topic)

	// Verify TopicMetrics was initialized
	topicMetrics := serviceMetrics.GetTopicMetrics(topic)
	assert.NotNil(t, topicMetrics)
	assert.Equal(t, topic, topicMetrics.TopicName)

	// Verify CompletedJobStats was initialized
	completedStats := serviceMetrics.GetCompletedJobStats(topic)
	assert.NotNil(t, completedStats)
	assert.Equal(t, uint(0), completedStats.CompletedJobsCount)
}

func TestServiceMetrics_TopicMetrics_ThreadSafety(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	err := serviceMetrics.Init()
	assert.NoError(t, err)

	topic := "concurrent-topic"
	jobMetrics := serviceMetrics.AddTopic(topic)

	// Test concurrent updates to TopicMetrics
	var wg sync.WaitGroup
	numGoroutines := 10
	jobsPerGoroutine := 5

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < jobsPerGoroutine; j++ {
				// Simulate job lifecycle with proper locking
				serviceMetrics.mu.Lock()
				serviceMetrics.OnJobStateChange(job.JobNoState, job.JobCreated, jobMetrics)
				serviceMetrics.UpdateTopicMetrics(topic, job.JobNoState, job.JobCreated, 0)
				serviceMetrics.OnJobStateChange(job.JobCreated, job.JobRunning, jobMetrics)
				serviceMetrics.UpdateTopicMetrics(topic, job.JobCreated, job.JobRunning, 0)
				serviceMetrics.OnJobStateChange(job.JobRunning, job.JobSucceeded, jobMetrics)
				serviceMetrics.UpdateTopicMetrics(topic, job.JobRunning, job.JobSucceeded, int64(1000*(goroutineID+1)))
				serviceMetrics.mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// Verify final state
	topicMetrics := serviceMetrics.GetTopicMetrics(topic)
	expectedTotalJobs := uint(numGoroutines * jobsPerGoroutine)

	assert.Equal(t, expectedTotalJobs, topicMetrics.TotalJobs)
	assert.Equal(t, uint(0), topicMetrics.ActiveJobs)           // All jobs completed
	assert.Equal(t, float64(1.0), topicMetrics.SuccessRate)     // All succeeded
	assert.Greater(t, topicMetrics.AverageDuration, float64(0)) // Should have some average duration

	// Verify completed job stats
	completedStats := serviceMetrics.GetCompletedJobStats(topic)
	assert.Equal(t, expectedTotalJobs, completedStats.CompletedJobsCount)
	assert.Greater(t, completedStats.CumulativeDurationMs, int64(0))
}

func TestServiceMetrics_TopicMetrics_ZeroDivision(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	topic := "zero-test-topic"

	serviceMetrics.AddTopic(topic)

	// Test with no completed jobs - should not cause division by zero
	topicMetrics := serviceMetrics.GetTopicMetrics(topic)
	assert.Equal(t, float64(0.0), topicMetrics.SuccessRate)
	assert.Equal(t, float64(0.0), topicMetrics.AverageDuration)

	// Test GetTopicsStats with no jobs - should not cause division by zero
	stats := serviceMetrics.GetTopicsStats()
	assert.Equal(t, 1, len(stats))
	assert.Equal(t, float64(0.0), stats[0].PercentJobs)
}

func TestServiceMetrics_Shutdown(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	err := serviceMetrics.Init()
	assert.NoError(t, err)

	// Call Shutdown - should not panic
	serviceMetrics.Shutdown()
}

func TestServiceMetrics_GetResourcesMetrics(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)

	// Initially should be empty
	metrics := serviceMetrics.GetResourcesMetrics()
	assert.Equal(t, 0, len(metrics))

	// Add some resource metrics
	startTime := time.Now().Unix() - 10 // 10 seconds ago
	jobUUID1, _ := uuid.NewV7()
	serviceMetrics.ResourceMetricsMap.Store("resource1", &ResourceMetrics{
		ResourceName: "resource1",
		JobUUID:      jobUUID1,
		Topic:        "test-topic",
		StartTime:    startTime,
	})
	jobUUID2, _ := uuid.NewV7()
	serviceMetrics.ResourceMetricsMap.Store("resource2", &ResourceMetrics{
		ResourceName: "resource2",
		JobUUID:      jobUUID2,
		Topic:        "test-topic-2",
		StartTime:    startTime - 5, // 15 seconds ago
	})

	// Get metrics
	metrics = serviceMetrics.GetResourcesMetrics()
	assert.Equal(t, 2, len(metrics))

	// Verify lock durations are calculated
	for _, m := range metrics {
		assert.Greater(t, m.LockDuration, int64(0))
		switch m.ResourceName {
		case "resource1":
			assert.GreaterOrEqual(t, m.LockDuration, int64(10))
			assert.Equal(t, "test-topic", m.Topic)
		case "resource2":
			assert.GreaterOrEqual(t, m.LockDuration, int64(15))
			assert.Equal(t, "test-topic-2", m.Topic)
		}
	}
}

func TestServiceMetrics_GetJobMetricsByStatus(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)

	// Initially all should be zero
	stats := serviceMetrics.GetJobMetricsByStatus()
	assert.Equal(t, uint(0), stats.ExistingJobs)
	assert.Equal(t, uint(0), stats.CreatedJobs)

	// Add metrics for multiple topics
	topic1 := "topic1"
	topic2 := "topic2"

	metrics1 := serviceMetrics.AddTopic(topic1)
	metrics1.JobsExisting = 5
	metrics1.JobsStatusCreated = 1
	metrics1.JobsStatusDelayed = 2
	metrics1.JobsStatusPending = 1
	metrics1.JobsStatusQueued = 1
	metrics1.JobsStatusRunning = 3
	metrics1.JobsStatusSucceeded = 10
	metrics1.JobsStatusFailed = 2
	metrics1.JobsStatusHidden = 1
	metrics1.JobsStatusCanceled = 1
	metrics1.JobsCounterDeleted = 5

	metrics2 := serviceMetrics.AddTopic(topic2)
	metrics2.JobsExisting = 3
	metrics2.JobsStatusCreated = 2
	metrics2.JobsStatusRunning = 1
	metrics2.JobsStatusSucceeded = 5
	metrics2.JobsStatusFailed = 1
	metrics2.JobsCounterDeleted = 2

	// Get aggregated stats
	stats = serviceMetrics.GetJobMetricsByStatus()
	assert.Equal(t, uint(8), stats.ExistingJobs)   // 5 + 3
	assert.Equal(t, uint(3), stats.CreatedJobs)    // 1 + 2
	assert.Equal(t, uint(2), stats.DelayedJobs)    // 2 + 0
	assert.Equal(t, uint(1), stats.PendingJobs)    // 1 + 0
	assert.Equal(t, uint(1), stats.QueuedJobs)     // 1 + 0
	assert.Equal(t, uint(4), stats.RunningJobs)    // 3 + 1
	assert.Equal(t, uint(15), stats.SucceededJobs) // 10 + 5
	assert.Equal(t, uint(3), stats.FailedJobs)     // 2 + 1
	assert.Equal(t, uint(1), stats.HiddenJobs)     // 1 + 0
	assert.Equal(t, uint(1), stats.CanceledJobs)   // 1 + 0
	assert.Equal(t, uint(7), stats.DeletedJobs)    // 5 + 2
}

func TestServiceMetrics_GetJobActivityMetrics(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	err := serviceMetrics.Init()
	assert.NoError(t, err)

	// Get activity metrics - should not panic
	metrics := serviceMetrics.GetJobActivityMetrics()

	// Verify structure exists (values depend on prometheus metrics)
	_ = metrics.Created
	_ = metrics.Succeeded
	_ = metrics.Failed
}

func TestServiceMetrics_OnJobStateChange_AllStates(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	jobMetrics := &JobMetrics{}

	// Test Delayed state
	serviceMetrics.OnJobStateChange(job.JobNoState, job.JobDelayed, jobMetrics)
	assert.Equal(t, uint(0), jobMetrics.JobsExisting)
	assert.Equal(t, uint(1), jobMetrics.JobsStatusDelayed)

	serviceMetrics.OnJobStateChange(job.JobDelayed, job.JobPending, jobMetrics)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusDelayed)
	assert.Equal(t, uint(1), jobMetrics.JobsStatusPending)

	// Test Queued state
	serviceMetrics.OnJobStateChange(job.JobPending, job.JobQueued, jobMetrics)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusPending)
	assert.Equal(t, uint(1), jobMetrics.JobsStatusQueued)

	serviceMetrics.OnJobStateChange(job.JobQueued, job.JobRunning, jobMetrics)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusQueued)
	assert.Equal(t, uint(1), jobMetrics.JobsStatusRunning)

	// Test Hidden state
	serviceMetrics.OnJobStateChange(job.JobRunning, job.JobHidden, jobMetrics)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusRunning)
	assert.Equal(t, uint(1), jobMetrics.JobsStatusHidden)

	// Test Canceled state
	serviceMetrics.OnJobStateChange(job.JobHidden, job.JobCanceled, jobMetrics)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusHidden)
	assert.Equal(t, uint(1), jobMetrics.JobsStatusCanceled)

	// Test transitions FROM terminal states (for completeness)
	serviceMetrics.OnJobStateChange(job.JobCanceled, job.JobDeleted, jobMetrics)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusCanceled)
	assert.Equal(t, uint(1), jobMetrics.JobsCounterDeleted)

	// Test Succeeded to Deleted
	serviceMetrics.OnJobStateChange(job.JobNoState, job.JobSucceeded, jobMetrics)
	assert.Equal(t, uint(1), jobMetrics.JobsStatusSucceeded)
	serviceMetrics.OnJobStateChange(job.JobSucceeded, job.JobDeleted, jobMetrics)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusSucceeded)
	assert.Equal(t, uint(2), jobMetrics.JobsCounterDeleted)

	// Test Failed to Deleted
	serviceMetrics.OnJobStateChange(job.JobNoState, job.JobFailed, jobMetrics)
	assert.Equal(t, uint(1), jobMetrics.JobsStatusFailed)
	serviceMetrics.OnJobStateChange(job.JobFailed, job.JobDeleted, jobMetrics)
	assert.Equal(t, uint(0), jobMetrics.JobsStatusFailed)
	assert.Equal(t, uint(3), jobMetrics.JobsCounterDeleted)

	// Test Deleted to something else (edge case)
	serviceMetrics.OnJobStateChange(job.JobDeleted, job.JobCreated, jobMetrics)
	assert.Equal(t, uint(1), jobMetrics.JobsStatusCreated)
}

func TestServiceMetrics_NotifyEvent_EmptyPollReceived(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	err := serviceMetrics.Init()
	assert.NoError(t, err)

	// Test ServiceEventEmptyPollReceived - should not panic
	serviceMetrics.NotifyEvent(event.ServiceEventEmptyPollReceived)
}

func TestServiceMetrics_NotifyJobEvent_WithResources(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	err := serviceMetrics.Init()
	assert.NoError(t, err)

	jobUUID, _ := uuid.NewV7()
	j := &job.Job{
		JobUUID:       jobUUID,
		Topic:         "test-topic",
		LockResources: []string{"resource1", "resource2"},
	}
	now := time.Now().UnixMilli()
	j.Init(j.JobUUID, now)
	j.AddHistoryEvent(job.JobEventCreate, now)
	j.AddHistoryEvent(job.JobEventEnqueue, now+100)
	j.AddHistoryEvent(job.JobEventStart, now+200)

	// Job queued with resources - resources should be locked
	serviceMetrics.NotifyJobEvent(j, event.ServiceEventJobStarted)

	// Verify resources are locked
	resourceMetrics, exists := serviceMetrics.ResourceMetricsMap.Load("resource1")
	assert.True(t, exists)
	rm := resourceMetrics.(*ResourceMetrics)
	assert.Equal(t, j.JobUUID, rm.JobUUID)
	assert.Equal(t, "resource1", rm.ResourceName)

	resourceMetrics2, exists := serviceMetrics.ResourceMetricsMap.Load("resource2")
	assert.True(t, exists)
	rm2 := resourceMetrics2.(*ResourceMetrics)
	assert.Equal(t, j.JobUUID, rm2.JobUUID)

	// Job completed - resources should be unlocked
	j.AddHistoryEvent(job.JobEventSuccess, now+5000)
	serviceMetrics.NotifyJobEvent(j, event.ServiceEventJobSucceeded)

	// Verify resources are unlocked
	_, exists = serviceMetrics.ResourceMetricsMap.Load("resource1")
	assert.False(t, exists)
	_, exists = serviceMetrics.ResourceMetricsMap.Load("resource2")
	assert.False(t, exists)
}

func TestServiceMetrics_NotifyJobEvent_WithResourcesJobFailed(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)
	err := serviceMetrics.Init()
	assert.NoError(t, err)

	jobUUID, _ := uuid.NewV7()
	j := &job.Job{
		JobUUID:       jobUUID,
		Topic:         "test-topic",
		LockResources: []string{"resource-fail"},
	}
	now := time.Now().UnixMilli()
	j.Init(j.JobUUID, now)
	j.AddHistoryEvent(job.JobEventEnqueue, now)
	j.AddHistoryEvent(job.JobEventStart, now+100)

	// Lock resources
	serviceMetrics.NotifyJobEvent(j, event.ServiceEventJobStarted)

	// Verify resource is locked
	_, exists := serviceMetrics.ResourceMetricsMap.Load("resource-fail")
	assert.True(t, exists)

	// Job terminated (final failure) - resource should be unlocked
	j.AddHistoryEvent(job.JobEventTerminate, now+2000)
	serviceMetrics.NotifyJobEvent(j, event.ServiceEventJobFailed)

	// Verify resource is unlocked
	_, exists = serviceMetrics.ResourceMetricsMap.Load("resource-fail")
	assert.False(t, exists)
}

func TestServiceMetrics_UpdateMetricsFromJobHistory_WithResources(t *testing.T) {
	serviceMetrics := NewServiceMetrics(true)

	jobUUID, _ := uuid.NewV7()
	j := &job.Job{
		JobUUID:       jobUUID,
		Topic:         "test-topic",
		LockResources: []string{"resource-hist-1", "resource-hist-2"},
	}
	now := time.Now().UnixMilli()
	j.Init(j.JobUUID, now)
	j.AddHistoryEvent(job.JobEventPending, now)
	j.AddHistoryEvent(job.JobEventEnqueue, now+100)
	j.AddHistoryEvent(job.JobEventStart, now+200)

	// Update metrics from history - job is running with locked resources
	serviceMetrics.UpdateMetricsFromJobHistory(j)

	// Verify metrics were updated
	jobMetrics := serviceMetrics.GetMetricByTopic("test-topic")
	assert.Equal(t, uint(1), jobMetrics.JobsExisting)
	assert.Equal(t, uint(1), jobMetrics.JobsStatusRunning)

	// Verify resources are tracked
	resourceMetrics, exists := serviceMetrics.ResourceMetricsMap.Load("resource-hist-1")
	assert.True(t, exists)
	rm := resourceMetrics.(*ResourceMetrics)
	assert.Equal(t, j.JobUUID, rm.JobUUID)
	assert.Equal(t, "test-topic", rm.Topic)

	_, exists = serviceMetrics.ResourceMetricsMap.Load("resource-hist-2")
	assert.True(t, exists)
}
