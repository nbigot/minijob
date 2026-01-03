package metrics

import (
	"testing"

	"github.com/nbigot/minijob/event"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrometheusMetrics_Init(t *testing.T) {
	prometheusMetrics := PrometheusMetrics{}
	err := prometheusMetrics.Init()
	require.NoError(t, err)

	assert.NotNil(t, prometheusMetrics.Registry)
	assert.NotNil(t, prometheusMetrics.JobsEventsCounter)
	assert.NotNil(t, prometheusMetrics.JobsStatusGauge)
	assert.NotNil(t, prometheusMetrics.FiberPrometheus)
}

func TestPrometheusMetrics_InitCustomMetrics(t *testing.T) {
	prometheusMetrics := PrometheusMetrics{}
	err := prometheusMetrics.Init()
	require.NoError(t, err)

	topic := "test_topic"
	prometheusMetrics.InitCustomMetrics(topic)

	assert.NotNil(t, prometheusMetrics.JobsEventsCounter.WithLabelValues(event.ServiceEventJobCreated.String(), topic))
	assert.NotNil(t, prometheusMetrics.JobsEventsCounter.WithLabelValues(event.ServiceEventJobEnqueued.String(), topic))
	assert.NotNil(t, prometheusMetrics.JobsEventsCounter.WithLabelValues(event.ServiceEventJobStarted.String(), topic))
	assert.NotNil(t, prometheusMetrics.JobsEventsCounter.WithLabelValues(event.ServiceEventJobSucceeded.String(), topic))
	assert.NotNil(t, prometheusMetrics.JobsEventsCounter.WithLabelValues(event.ServiceEventJobCanceled.String(), topic))
	assert.NotNil(t, prometheusMetrics.JobsEventsCounter.WithLabelValues(event.ServiceEventJobFailed.String(), topic))
	assert.NotNil(t, prometheusMetrics.JobsEventsCounter.WithLabelValues(event.ServiceEventJobDeleted.String(), topic))
	assert.NotNil(t, prometheusMetrics.JobsEventsCounter.WithLabelValues(event.ServiceEventJobTimeout.String(), topic))
	assert.NotNil(t, prometheusMetrics.JobsEventsCounter.WithLabelValues(event.ServiceEventJobTerminated.String(), topic))
}

func TestPrometheusMetrics_SetGauges(t *testing.T) {
	prometheusMetrics := PrometheusMetrics{}
	err := prometheusMetrics.Init()
	require.NoError(t, err)

	topic := "test_topic"
	jm := &JobMetrics{
		ResourcesLockedCount: 0,
		JobsCounterDeleted:   1,
		JobsExisting:         2,
		JobsStatusCreated:    3,
		JobsStatusDelayed:    4,
		JobsStatusPending:    5,
		JobsStatusQueued:     6,
		JobsStatusRunning:    7,
		JobsStatusSucceeded:  8,
		JobsStatusFailed:     9,
		JobsStatusHidden:     10,
		JobsStatusCanceled:   11,
	}
	prometheusMetrics.SetGauges(topic, jm)

	var metric dto.Metric

	// Created
	if err := prometheusMetrics.JobsStatusGauge.WithLabelValues("Created", topic).Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}
	assert.Equal(t, float64(jm.JobsStatusCreated), *metric.Gauge.Value)

	// Delayed
	if err := prometheusMetrics.JobsStatusGauge.WithLabelValues("Delayed", topic).Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}
	assert.Equal(t, float64(jm.JobsStatusDelayed), *metric.Gauge.Value)

	// Pending
	if err := prometheusMetrics.JobsStatusGauge.WithLabelValues("Pending", topic).Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}
	assert.Equal(t, float64(jm.JobsStatusPending), *metric.Gauge.Value)

	// Queued
	if err := prometheusMetrics.JobsStatusGauge.WithLabelValues("Queued", topic).Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}
	assert.Equal(t, float64(jm.JobsStatusQueued), *metric.Gauge.Value)

	// Running
	if err := prometheusMetrics.JobsStatusGauge.WithLabelValues("Running", topic).Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}
	assert.Equal(t, float64(jm.JobsStatusRunning), *metric.Gauge.Value)

	// Succeeded
	if err := prometheusMetrics.JobsStatusGauge.WithLabelValues("Succeeded", topic).Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}
	assert.Equal(t, float64(jm.JobsStatusSucceeded), *metric.Gauge.Value)

	// Failed
	if err := prometheusMetrics.JobsStatusGauge.WithLabelValues("Failed", topic).Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}
	assert.Equal(t, float64(jm.JobsStatusFailed), *metric.Gauge.Value)

	// Hidden
	if err := prometheusMetrics.JobsStatusGauge.WithLabelValues("Hidden", topic).Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}
	assert.Equal(t, float64(jm.JobsStatusHidden), *metric.Gauge.Value)

	// Canceled
	if err := prometheusMetrics.JobsStatusGauge.WithLabelValues("Canceled", topic).Write(&metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}
	assert.Equal(t, float64(jm.JobsStatusCanceled), *metric.Gauge.Value)
}

func TestPrometheusMetrics_ResetAllGauges(t *testing.T) {
	// Setup
	prometheusMetrics := PrometheusMetrics{}
	err := prometheusMetrics.Init()
	require.NoError(t, err)

	topic := "test_topic"

	// Set some gauge values
	jobMetrics := &JobMetrics{
		JobsExisting:      10,
		JobsStatusRunning: 5,
		JobsStatusFailed:  3,
	}
	prometheusMetrics.SetGauges(topic, jobMetrics)

	// Check values before reset
	checkGaugeValue(t, prometheusMetrics.JobsStatusGauge, "Existing", topic, 10)
	checkGaugeValue(t, prometheusMetrics.JobsStatusGauge, "Running", topic, 5)
	checkGaugeValue(t, prometheusMetrics.JobsStatusGauge, "Failed", topic, 3)

	// Reset gauges
	prometheusMetrics.ResetAllGauges()

	// Verify all values are reset to 0 but labels preserved
	checkGaugeValue(t, prometheusMetrics.JobsStatusGauge, "Existing", topic, 0)
	checkGaugeValue(t, prometheusMetrics.JobsStatusGauge, "Running", topic, 0)
	checkGaugeValue(t, prometheusMetrics.JobsStatusGauge, "Failed", topic, 0)
}

func TestPrometheusMetrics_OnEvent(t *testing.T) {
	// Setup
	prometheusMetrics := PrometheusMetrics{}
	err := prometheusMetrics.Init()
	require.NoError(t, err)

	topic := "test_topic"
	jobMetrics := &JobMetrics{
		JobsExisting:      1,
		JobsStatusRunning: 1,
	}

	// Test job created event
	ev := event.ServiceEvent{
		Type:  event.ServiceEventJobCreated,
		Topic: topic,
	}
	prometheusMetrics.OnEvent(ev, jobMetrics)

	// Verify counter was incremented
	checkCounterValue(t, prometheusMetrics.JobsEventsCounter, event.ServiceEventJobCreated.String(), topic, 1)

	// Test job succeeded event with lifetime
	ev = event.ServiceEvent{
		Type:        event.ServiceEventJobSucceeded,
		Topic:       topic,
		JobLifetime: 5000, // 5 seconds in milliseconds
	}
	prometheusMetrics.OnEvent(ev, jobMetrics)

	// Check counters and histograms
	checkCounterValue(t, prometheusMetrics.JobsEventsCounter, event.ServiceEventJobSucceeded.String(), topic, 1)
	checkHistogramCount(t, prometheusMetrics.JobCompletionDurationHistogram, topic, 1)

	// Test DeletedAll event
	ev = event.ServiceEvent{
		Type:  event.ServiceEventJobDeletedAll,
		Topic: topic,
	}
	prometheusMetrics.OnEvent(ev, jobMetrics)

	// Check gauges reset
	checkGaugeValue(t, prometheusMetrics.JobsStatusGauge, "Running", topic, 0)

	// Test Ready event
	ev = event.ServiceEvent{
		Type:  event.ServiceEventReady,
		Topic: topic,
	}
	prometheusMetrics.OnEvent(ev, jobMetrics)

	// Check initialization metrics
	checkHistogramCount(t, prometheusMetrics.JobActiveDurationHistogram, topic, 1)
}

func TestPrometheusMetrics_Shutdown(t *testing.T) {
	// Setup
	prometheusMetrics := PrometheusMetrics{}
	err := prometheusMetrics.Init()
	require.NoError(t, err)

	// Just ensure it doesn't panic - function is currently empty
	prometheusMetrics.Shutdown()
}

func checkGaugeLabelExists(t *testing.T, gauge *prometheus.GaugeVec, topic, status, percentile string) {
	// Use a metric dto to check if metrics with these labels exist
	metricChan := make(chan prometheus.Metric, 100)
	gauge.Collect(metricChan)
	close(metricChan)

	// Check all collected metrics for matching labels
	for metric := range metricChan {
		var m dto.Metric
		err := metric.Write(&m)
		if err != nil {
			continue
		}

		// Need to check if all our labels match
		labelMatches := 0
		for _, labelPair := range m.GetLabel() {
			name := labelPair.GetName()
			value := labelPair.GetValue()

			if (name == "topic" && value == topic) ||
				(name == "status" && value == status) ||
				(name == "percentile" && value == percentile) {
				labelMatches++
			}
		}

		// If all three labels match, we found our metric
		if labelMatches == 3 {
			return
		}
	}

	assert.Fail(t, "Gauge with labels not found")
}

func TestPrometheusMetrics_UpdateJobDurationGauges(t *testing.T) {
	// Setup
	prometheusMetrics := PrometheusMetrics{}
	err := prometheusMetrics.Init()
	require.NoError(t, err)

	topic1 := "test_topic_1"
	topic2 := "test_topic_2"

	// Create percentiles data
	data := UpdateJobDurationPercentilesByTopic{
		topic1: &UpdateJobDurationPercentilesDetail{
			ActiveJobsDurations:    []int64{1000, 2000, 3000, 4000, 5000}, // 1-5 seconds in ms
			CompletedJobsDurations: []int64{500, 1500, 2500},              // 0.5-2.5 seconds in ms
		},
		topic2: &UpdateJobDurationPercentilesDetail{
			ActiveJobsDurations:    []int64{},           // no items
			CompletedJobsDurations: []int64{10, 20, 30}, // 0.01-0.03 seconds in ms
		},
	}

	// Update gauges
	prometheusMetrics.UpdateJobDurationGauges(&data)

	// check gauges values
	percentilesLabels := []string{"p5", "p10", "p25", "p50", "p75", "p90", "p95", "p99", "p100"}
	for _, percentilesLabel := range percentilesLabels {
		checkGaugeLabelExists(t, prometheusMetrics.JobDurationPercentilesGauge, topic1, "active", percentilesLabel)
		checkGaugeLabelExists(t, prometheusMetrics.JobDurationPercentilesGauge, topic1, "completed", percentilesLabel)
		checkGaugeLabelExists(t, prometheusMetrics.JobDurationPercentilesGauge, topic2, "active", percentilesLabel)
		checkGaugeLabelExists(t, prometheusMetrics.JobDurationPercentilesGauge, topic2, "completed", percentilesLabel)
	}
}

func TestPrometheusMetrics_updateJobMetrics(t *testing.T) {
	// Setup
	prometheusMetrics := PrometheusMetrics{}
	err := prometheusMetrics.Init()
	require.NoError(t, err)

	topic := "test_topic"
	jobMetrics := &JobMetrics{
		JobsExisting:        5,
		JobsStatusSucceeded: 3,
	}

	// Call with job succeeded event
	ev := event.ServiceEvent{
		Type:        event.ServiceEventJobSucceeded,
		Topic:       topic,
		JobLifetime: 3000, // 3 seconds in milliseconds
	}

	prometheusMetrics.updateJobMetrics(ev, jobMetrics)

	// Verify counter incremented
	checkCounterValue(t, prometheusMetrics.JobsEventsCounter, event.ServiceEventJobSucceeded.String(), topic, 1)

	// Verify gauge set
	checkGaugeValue(t, prometheusMetrics.JobsStatusGauge, "Succeeded", topic, 3)

	// Verify histogram updated
	checkHistogramCount(t, prometheusMetrics.JobCompletionDurationHistogram, topic, 1)

	// Call with a non-completion event
	ev = event.ServiceEvent{
		Type:  event.ServiceEventJobEnqueued,
		Topic: topic,
	}

	prometheusMetrics.updateJobMetrics(ev, jobMetrics)

	// Verify counter incremented
	checkCounterValue(t, prometheusMetrics.JobsEventsCounter, event.ServiceEventJobEnqueued.String(), topic, 1)

	// Histogram count should still be 1 (only incremented for completion events)
	checkHistogramCount(t, prometheusMetrics.JobCompletionDurationHistogram, topic, 1)
}

// Helper functions for checking metric values

func checkGaugeValue(t *testing.T, gauge *prometheus.GaugeVec, status, topic string, expected float64) {
	metric := &dto.Metric{}
	err := gauge.WithLabelValues(status, topic).Write(metric)
	require.NoError(t, err)
	assert.Equal(t, expected, *metric.Gauge.Value, "Gauge value mismatch")
}

func checkCounterValue(t *testing.T, counter *prometheus.CounterVec, event, topic string, expected float64) {
	metric := &dto.Metric{}
	err := counter.WithLabelValues(event, topic).Write(metric)
	require.NoError(t, err)
	assert.Equal(t, expected, *metric.Counter.Value, "Counter value mismatch")
}

func checkHistogramCount(t *testing.T, histogram *prometheus.HistogramVec, topic string, expected uint64) {
	// Get the histogram with the given label values
	observer := histogram.WithLabelValues(topic)

	// Cast to the concrete Histogram type which has a Write method
	h, ok := observer.(prometheus.Histogram)
	require.True(t, ok, "Failed to cast Observer to Histogram")

	// Write the metric
	metric := &dto.Metric{}
	err := h.Write(metric)
	require.NoError(t, err)

	// Check the sample count
	assert.Equal(t, expected, *metric.Histogram.SampleCount, "Histogram sample count mismatch")
}
