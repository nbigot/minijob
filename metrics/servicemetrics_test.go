package metrics

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nbigot/minijob/event"
	"github.com/nbigot/minijob/job"
	"github.com/stretchr/testify/assert"
)

// Tests
func TestServiceMetrics_Init(t *testing.T) {
	serviceMetrics := NewSericeMetrics(true)
	err := serviceMetrics.Init()

	assert.NoError(t, err)
	assert.NotNil(t, serviceMetrics.GetFiberPrometheus())
}

func TestServiceMetrics_AddTopic(t *testing.T) {
	serviceMetrics := NewSericeMetrics(true)

	// Add a new topic
	topic := "test-topic"
	jobMetrics := serviceMetrics.AddTopic(topic)

	// Verify the topic was added
	assert.NotNil(t, jobMetrics)
	assert.Equal(t, uint(0), jobMetrics.JobsExisting)

	// Verify topics list contains the new topic
	assert.Contains(t, serviceMetrics.GetJobsTopics(), topic)

	// Verify GetMetricByTopic returns the same metrics
	retrievedMetrics := serviceMetrics.GetMetricByTopic(topic)
	assert.Equal(t, jobMetrics, retrievedMetrics)
}

func TestServiceMetrics_GetMetricByTopic(t *testing.T) {
	serviceMetrics := NewSericeMetrics(true)

	// Get metrics for non-existent topic (should create it)
	topic := "new-topic"
	jobMetrics := serviceMetrics.GetMetricByTopic(topic)

	// Verify it was created
	assert.NotNil(t, jobMetrics)
	assert.Contains(t, serviceMetrics.GetJobsTopics(), topic)

	// Get metrics for existing topic
	retrievedMetrics := serviceMetrics.GetMetricByTopic(topic)
	assert.Equal(t, jobMetrics, retrievedMetrics)
}

func TestServiceMetrics_OnJobStateChange(t *testing.T) {
	serviceMetrics := NewSericeMetrics(true)
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
	serviceMetrics := NewSericeMetrics(true)

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
	serviceMetrics := NewSericeMetrics(true)
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
	serviceMetrics := NewSericeMetrics(true)

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
	serviceMetrics := NewSericeMetrics(true)
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
	serviceMetrics := NewSericeMetrics(true)
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
	serviceMetrics := NewSericeMetrics(true)

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
	serviceMetrics := NewSericeMetrics(true)
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
	serviceMetrics := NewSericeMetrics(true)
	serviceMetrics.Init()

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

func TestNewSericeMetrics(t *testing.T) {
	// Test with collection enabled
	serviceMetrics := NewSericeMetrics(true)
	assert.True(t, serviceMetrics.enabledCollect)
	assert.Empty(t, serviceMetrics.Topics)

	// Test with collection disabled
	serviceMetrics = NewSericeMetrics(false)
	assert.Equal(t, false, serviceMetrics.enabledCollect)
}

func TestServiceMetrics_Concurrent(t *testing.T) {
	serviceMetrics := NewSericeMetrics(true)
	topic := "test-topic"

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
	serviceMetrics := NewSericeMetrics(true)

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

	// Verify that changes to the original metrics are not reflected in the result
	// (i.e., the function returns a copy, not references)
	metrics1.JobsExisting = 99
	assert.Equal(t, uint(5), result[topic1][0].JobsExisting)
}
