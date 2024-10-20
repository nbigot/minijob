package metrics

import (
	"sync"

	"github.com/gofiber/fiber/v2"
	"github.com/nbigot/minijob/fiberprometheus"
	"github.com/nbigot/minijob/service"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	JobDurationSeconds *prometheus.HistogramVec
	JobsEventsCounter  *prometheus.CounterVec
	JobsStatusGauge    *prometheus.GaugeVec
	ConstLabels        prometheus.Labels
	Registry           *prometheus.Registry
	FiberPrometheus    *fiberprometheus.FiberPrometheus
	notifChan          chan service.ServiceEvent
	wg                 sync.WaitGroup
}

func (m *Metrics) Init(app *fiber.App, notifChan chan service.ServiceEvent) {
	// Create non-global registry.
	m.Registry = prometheus.NewRegistry()
	namespace := ""
	subsystem := ""
	m.ConstLabels = prometheus.Labels{}

	m.JobsEventsCounter = promauto.With(m.Registry).NewCounterVec(
		prometheus.CounterOpts{
			Name:        prometheus.BuildFQName(namespace, subsystem, "jobs_events"),
			Help:        "Count all jobs by event and topic.",
			ConstLabels: m.ConstLabels,
		},
		[]string{"event", "topic"},
	)

	m.JobsStatusGauge = promauto.With(m.Registry).NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName(namespace, subsystem, "jobs_by_status"),
			Help:        "Jobs by status and topic",
			ConstLabels: m.ConstLabels,
		}, []string{"status", "topic"},
	)

	buckets := []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 20, 30, 60}
	labelNames := []string{"topic"}
	m.JobDurationSeconds = promauto.With(m.Registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    prometheus.BuildFQName(namespace, subsystem, "job_duration_seconds"),
			Help:    "A histogram of the job durations in seconds.",
			Buckets: buckets,
		},
		labelNames,
	)

	m.notifChan = notifChan

	m.FiberPrometheus = fiberprometheus.NewWithRegistry(m.Registry, "minijob", "http", "", m.ConstLabels, fiberprometheus.DefaultBuckets)
	m.FiberPrometheus.RegisterAt(app, "/metrics")
	m.FiberPrometheus.SetSkipPaths([]string{"/ping", "/healthcheck", "/api/v1/admin/server/restart"}) // Optional: Remove some paths from metrics
	_ = app.Use(m.FiberPrometheus.Middleware)

	go m.Run()
}

func (m *Metrics) InitCustomMetrics(e service.ServiceEvent) {
	m.JobDurationSeconds.WithLabelValues("").Observe(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobCreated.String(), "").Add(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobEnqueued.String(), "").Add(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobStarted.String(), "").Add(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobSucceeded.String(), "").Add(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobCanceled.String(), "").Add(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobFailed.String(), "").Add(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobDeleted.String(), "").Add(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobTimeout.String(), "").Add(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobTerminated.String(), "").Add(0)
	m.SetGauges(e)
}

func (m *Metrics) Shutdown() {
	m.notifChan <- service.ServiceEvent{Type: service.ServiceEventShutdown}
	m.wg.Wait()
}

func (m *Metrics) SetGauges(e service.ServiceEvent) {
	topicMetric := e.Metrics.JobMetricsByTopicMap[e.Topic]
	m.JobsStatusGauge.WithLabelValues("JobsCounter", e.Topic).Set(float64(topicMetric.JobsCounter))
	m.JobsStatusGauge.WithLabelValues("JobsCounterCreated", e.Topic).Set(float64(topicMetric.JobsCounterCreated))
	m.JobsStatusGauge.WithLabelValues("JobsCounterPending", e.Topic).Set(float64(topicMetric.JobsCounterPending))
	m.JobsStatusGauge.WithLabelValues("JobsCounterQueued", e.Topic).Set(float64(topicMetric.JobsCounterQueued))
	m.JobsStatusGauge.WithLabelValues("JobsCounterRunning", e.Topic).Set(float64(topicMetric.JobsCounterRunning))
	m.JobsStatusGauge.WithLabelValues("JobsCounterSucceeded", e.Topic).Set(float64(topicMetric.JobsCounterSucceeded))
	m.JobsStatusGauge.WithLabelValues("JobsCounterFailed", e.Topic).Set(float64(topicMetric.JobsCounterFailed))
	m.JobsStatusGauge.WithLabelValues("JobsCounterTerminated", e.Topic).Set(float64(topicMetric.JobsCounterTerminated))
	m.JobsStatusGauge.WithLabelValues("JobsCounterTimeout", e.Topic).Set(float64(topicMetric.JobsCounterTimeout))
	m.JobsStatusGauge.WithLabelValues("JobsCounterDeleted", e.Topic).Set(float64(topicMetric.JobsCounterDeleted))
	m.JobsStatusGauge.WithLabelValues("JobsCounterCanceled", e.Topic).Set(float64(topicMetric.JobsCounterCanceled))
	m.JobsStatusGauge.WithLabelValues("JobsCounterFaillure", e.Topic).Set(float64(topicMetric.JobsCounterFaillure))
}

func (m *Metrics) Run() {
	m.wg.Add(1)
	defer m.wg.Done()

	for e := range m.notifChan {
		switch e.Type {
		case service.ServiceEventJobCreated,
			service.ServiceEventJobEnqueued,
			service.ServiceEventJobStarted,
			service.ServiceEventJobSucceeded,
			service.ServiceEventJobCanceled,
			service.ServiceEventJobFailed,
			service.ServiceEventJobDeleted,
			service.ServiceEventJobTimeout,
			service.ServiceEventJobTerminated:
			m.recordEvent(e)
		case service.ServiceEventReady:
			m.InitCustomMetrics(e)
		case service.ServiceEventShutdown:
			return // exit the goroutine
		}
	}
}

func (m *Metrics) recordEvent(e service.ServiceEvent) {
	m.JobsEventsCounter.WithLabelValues(e.Type.String(), e.Topic).Inc()
	m.SetGauges(e)
}

// NewMetrics returns a new Metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{}
}
