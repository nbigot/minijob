package metrics

import (
	"github.com/nbigot/minijob/event"
	"github.com/nbigot/minijob/fiberprometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	dto "github.com/prometheus/client_model/go"
)

// Prometheus bucket definitions
var (
	// Buckets for job completion duration (in seconds)
	BucketsJobCompletionDuration = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 20, 30, 60, 300, 600, 1800, 3600}

	// Buckets for active job duration (in seconds)
	BucketsActiveJobDuration = []float64{1, 2, 5, 10, 20, 30, 60, 120, 300, 600, 1800, 3600, 7200, 14400, 28800, 57600, 86400}

	// Buckets for job duration percentiles
	BucketsJobDurationPercentiles = []float64{.05, .1, .25, .5, .75, .9, .95, .99, 1.0}
)

type PrometheusMetrics struct {
	ActiveJobsPercentiles          *PercentilesMetrics // reusable percentile calculator to avoid memory allocations during metrics computation
	CompletedJobsPercentiles       *PercentilesMetrics // reusable percentile calculator to avoid memory allocations during metrics computation
	ConstLabels                    prometheus.Labels
	JobCompletionDurationHistogram *prometheus.HistogramVec // duration of job completion in seconds by topic
	JobActiveDurationHistogram     *prometheus.HistogramVec // duration of active jobs in seconds by topic
	JobDurationPercentilesGauge    *prometheus.GaugeVec     // percentiles of job duration in seconds by topic and by status (active, completed)
	JobsEventsCounter              *prometheus.CounterVec   // number of job events by type and by topic
	JobsStatusGauge                *prometheus.GaugeVec     // number of jobs by status and by topic
	AppEventsCounter               *prometheus.CounterVec   // number of events related to the application (not jobs)
	Registry                       *prometheus.Registry
	FiberPrometheus                *fiberprometheus.FiberPrometheus
}

type UpdateJobDurationPercentilesByTopic map[string]*UpdateJobDurationPercentilesDetail

type UpdateJobDurationPercentilesDetail struct {
	ActiveJobsDurations    []int64 // durations in milliseconds (sorted asc)
	CompletedJobsDurations []int64 // durations in milliseconds (sorted asc)
}

func (m *PrometheusMetrics) Init() error {
	// Create non-global registry
	m.Registry = prometheus.NewRegistry()
	namespace := ""
	subsystem := ""
	m.ConstLabels = prometheus.Labels{}

	m.ActiveJobsPercentiles = NewPercentilesMetrics(BucketsJobDurationPercentiles)
	m.CompletedJobsPercentiles = NewPercentilesMetrics(BucketsJobDurationPercentiles)

	m.AppEventsCounter = promauto.With(m.Registry).NewCounterVec(
		prometheus.CounterOpts{
			Name:        prometheus.BuildFQName(namespace, subsystem, "app_events"),
			Help:        "Count all events by topic.",
			ConstLabels: m.ConstLabels,
		},
		[]string{"event", "topic"},
	)

	m.JobsEventsCounter = promauto.With(m.Registry).NewCounterVec(
		prometheus.CounterOpts{
			Name:        prometheus.BuildFQName(namespace, subsystem, "job_events"),
			Help:        "Count all jobs by event and topic.",
			ConstLabels: m.ConstLabels,
		},
		[]string{"event", "topic"},
	)

	m.JobsStatusGauge = promauto.With(m.Registry).NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName(namespace, subsystem, "job_status"),
			Help:        "Jobs by status and topic",
			ConstLabels: m.ConstLabels,
		}, []string{"status", "topic"},
	)

	m.JobCompletionDurationHistogram = promauto.With(m.Registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    prometheus.BuildFQName(namespace, subsystem, "job_completion_duration"),
			Help:    "Histogram tracking job execution time from start to completion in seconds.",
			Buckets: BucketsJobCompletionDuration,
		},
		[]string{"topic"},
	)

	m.JobActiveDurationHistogram = promauto.With(m.Registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    prometheus.BuildFQName(namespace, subsystem, "job_active_duration_seconds"),
			Help:    "Distribution of active job durations in seconds by topic",
			Buckets: BucketsActiveJobDuration,
		},
		[]string{"topic"},
	)

	m.JobDurationPercentilesGauge = promauto.With(m.Registry).NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName(namespace, subsystem, "job_duration_percentiles"),
			Help:        "Percentiles of job duration in seconds by topic and by status (active, completed)",
			ConstLabels: m.ConstLabels,
			// Buckets: BucketsJobDurationPercentiles,
		},
		[]string{"topic", "status", "percentile"},
	)

	m.FiberPrometheus = fiberprometheus.NewWithRegistry(m.Registry, "minijob", "http", "", m.ConstLabels, fiberprometheus.DefaultBuckets)
	return nil
}

func (m *PrometheusMetrics) InitCustomMetrics(topic string) {
	// Initialize all metrics with value 0 even when no data is present.
	// This ensures metrics appear in Grafana immediately with proper labels,
	// prevents "No Data" errors, enables template variable discovery, and
	// maintains time-range consistency for visualization and alerting.
	m.JobCompletionDurationHistogram.WithLabelValues(topic).Observe(0)
	m.JobActiveDurationHistogram.WithLabelValues(topic).Observe(0)
	m.JobsEventsCounter.WithLabelValues(event.ServiceEventJobCreated.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(event.ServiceEventJobDelayed.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(event.ServiceEventJobEnqueued.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(event.ServiceEventJobStarted.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(event.ServiceEventJobSucceeded.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(event.ServiceEventJobCanceled.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(event.ServiceEventJobFailed.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(event.ServiceEventJobHidden.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(event.ServiceEventJobDeleted.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(event.ServiceEventJobTimeout.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(event.ServiceEventJobTerminated.String(), topic).Add(0)
}

func (m *PrometheusMetrics) SetGauges(topic string, jm *JobMetrics) {
	m.JobsStatusGauge.WithLabelValues("Existing", topic).Set(float64(jm.JobsExisting))
	m.JobsStatusGauge.WithLabelValues("Created", topic).Set(float64(jm.JobsStatusCreated))
	m.JobsStatusGauge.WithLabelValues("Delayed", topic).Set(float64(jm.JobsStatusDelayed))
	m.JobsStatusGauge.WithLabelValues("Pending", topic).Set(float64(jm.JobsStatusPending))
	m.JobsStatusGauge.WithLabelValues("Queued", topic).Set(float64(jm.JobsStatusQueued))
	m.JobsStatusGauge.WithLabelValues("Running", topic).Set(float64(jm.JobsStatusRunning))
	m.JobsStatusGauge.WithLabelValues("Succeeded", topic).Set(float64(jm.JobsStatusSucceeded))
	m.JobsStatusGauge.WithLabelValues("Failed", topic).Set(float64(jm.JobsStatusFailed))
	m.JobsStatusGauge.WithLabelValues("Hidden", topic).Set(float64(jm.JobsStatusHidden))
	m.JobsStatusGauge.WithLabelValues("Canceled", topic).Set(float64(jm.JobsStatusCanceled))
}

func (m *PrometheusMetrics) ResetAllGauges() {
	// Reset all gauges but keep the existing labels
	// Preserve metric labels during gauge reset by extracting existing label combinations,
	// resetting the gauge, then recreating all metrics with zero values. This works around
	// Prometheus's limitation where Reset() removes all labels, ensuring Grafana dashboards
	// maintain consistent label dimensions and visualization continuity.

	// Get the metric families to find our existing labels
	metricChan := make(chan prometheus.Metric)
	go func() {
		m.JobsStatusGauge.Collect(metricChan)
		close(metricChan)
	}()

	// Store all status/topic label combinations
	type labelPair struct {
		status string
		topic  string
	}
	labels := make([]labelPair, 0)

	// Extract all existing label pairs
	for metric := range metricChan {
		pb := &dto.Metric{}
		err := metric.Write(pb)
		if err != nil {
			continue
		}

		var status, topic string
		for _, labelPair := range pb.Label {
			if labelPair.GetName() == "status" {
				status = labelPair.GetValue()
			} else if labelPair.GetName() == "topic" {
				topic = labelPair.GetValue()
			}
		}

		if status != "" && topic != "" {
			labels = append(labels, labelPair{status: status, topic: topic})
		}
	}

	// Now reset the gauge (this removes all metrics)
	m.JobsStatusGauge.Reset()

	// Re-create all metrics with value 0
	for _, pair := range labels {
		m.JobsStatusGauge.WithLabelValues(pair.status, pair.topic).Set(0)
	}
}

func (m *PrometheusMetrics) OnEvent(ev event.ServiceEvent, jm *JobMetrics) {
	switch ev.Type {
	case event.ServiceEventJobCreated,
		event.ServiceEventJobDelayed,
		event.ServiceEventJobPending,
		event.ServiceEventJobEnqueued,
		event.ServiceEventJobStarted,
		event.ServiceEventJobSucceeded,
		event.ServiceEventJobCanceled,
		event.ServiceEventJobFailed,
		event.ServiceEventJobHidden,
		event.ServiceEventJobDeleted,
		event.ServiceEventJobTimeout,
		event.ServiceEventJobTerminated:
		m.updateJobMetrics(ev, jm)
	case event.ServiceEventJobDeletedAll:
		m.ResetAllGauges()
	case event.ServiceEventReady:
		m.InitCustomMetrics(ev.Topic)
		m.SetGauges(ev.Topic, &JobMetrics{})
	case event.ServiceEventEmptyPollReceived:
		m.AppEventsCounter.WithLabelValues(ev.Type.String(), ev.Topic).Inc()
	}
}

func (m *PrometheusMetrics) Shutdown() {
}

// GetJobEventCounters extracts cumulative event counts from JobsEventsCounter
// aggregating across all topics to provide total counts for each event type
func (m *PrometheusMetrics) GetJobEventCounters() JobActivityMetrics {
	metrics := JobActivityMetrics{}

	// Collect all metrics from the counter
	metricChan := make(chan prometheus.Metric)
	go func() {
		m.JobsEventsCounter.Collect(metricChan)
		close(metricChan)
	}()

	// Extract values and aggregate by event type
	for metric := range metricChan {
		pb := &dto.Metric{}
		err := metric.Write(pb)
		if err != nil {
			continue
		}

		// Get event label value
		var eventType string
		for _, labelPair := range pb.Label {
			if labelPair.GetName() == "event" {
				eventType = labelPair.GetValue()
				break
			}
		}

		// Get counter value and aggregate by event type
		if pb.Counter != nil {
			value := uint64(pb.Counter.GetValue())
			switch eventType {
			case "Created":
				metrics.Created += value
			case "Delayed":
				metrics.Delayed += value
			case "Pending":
				metrics.Pending += value
			case "Enqueued":
				metrics.Enqueued += value
			case "Started":
				metrics.Started += value
			case "Succeeded":
				metrics.Succeeded += value
			case "Failed":
				metrics.Failed += value
			case "Canceled":
				metrics.Canceled += value
			case "Hidden":
				metrics.Hidden += value
			case "Deleted":
				metrics.Deleted += value
			case "Timeout":
				metrics.Timeout += value
			case "Terminated":
				metrics.Terminated += value
			}
		}
	}

	return metrics
}

func (m *PrometheusMetrics) UpdateJobDurationGauges(p *UpdateJobDurationPercentilesByTopic) {
	m.JobDurationPercentilesGauge.Reset()
	cptPercentiles := len(BucketsJobDurationPercentiles)

	for topic, detail := range *p {
		// Handle active jobs percentiles
		if len(detail.ActiveJobsDurations) > 0 {
			m.ActiveJobsPercentiles.Compute(&detail.ActiveJobsDurations)
			for i := range cptPercentiles {
				durationSec := float64(m.ActiveJobsPercentiles.Values[i]) / 1000.0
				m.JobDurationPercentilesGauge.WithLabelValues(
					topic,
					"active",
					m.ActiveJobsPercentiles.PercentileLabels[i],
				).Set(durationSec)
			}
		} else {
			// If no active jobs, initialize with zeros for all percentiles
			for i := range cptPercentiles {
				m.JobDurationPercentilesGauge.WithLabelValues(
					topic,
					"active",
					m.ActiveJobsPercentiles.PercentileLabels[i],
				).Set(0)
			}
		}

		// Handle completed jobs percentiles
		if len(detail.CompletedJobsDurations) > 0 {
			m.CompletedJobsPercentiles.Compute(&detail.CompletedJobsDurations)
			for i := range cptPercentiles {
				durationSec := float64(m.CompletedJobsPercentiles.Values[i]) / 1000.0
				m.JobDurationPercentilesGauge.WithLabelValues(
					topic,
					"completed",
					m.CompletedJobsPercentiles.PercentileLabels[i],
				).Set(durationSec)
			}
		} else {
			// If no completed jobs, initialize with zeros for all percentiles
			for i := range cptPercentiles {
				m.JobDurationPercentilesGauge.WithLabelValues(
					topic,
					"completed",
					m.CompletedJobsPercentiles.PercentileLabels[i],
				).Set(0)
			}
		}
	}
}

func (m *PrometheusMetrics) updateJobMetrics(ev event.ServiceEvent, jm *JobMetrics) {
	m.JobsEventsCounter.WithLabelValues(ev.Type.String(), ev.Topic).Inc()
	m.SetGauges(ev.Topic, jm)
	switch ev.Type {
	case event.ServiceEventJobSucceeded,
		event.ServiceEventJobDeleted,
		event.ServiceEventJobTerminated:
		durationSeconds := float64(ev.JobLifetime) / 1000.0
		m.JobCompletionDurationHistogram.WithLabelValues(ev.Topic).Observe(durationSeconds)
	}
}
