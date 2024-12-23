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
	JobsCounterGauge   *prometheus.GaugeVec
	ConstLabels        prometheus.Labels
	Registry           *prometheus.Registry
	FiberPrometheus    *fiberprometheus.FiberPrometheus
	serviceMetrics     service.IServiceMetrics
	notifChan          chan service.ServiceEvent
	wg                 sync.WaitGroup
}

func (m *Metrics) Init(app *fiber.App, notifChan chan service.ServiceEvent, serviceMetrics service.IServiceMetrics) {
	// Create non-global registry
	m.Registry = prometheus.NewRegistry()
	namespace := ""
	subsystem := ""
	m.ConstLabels = prometheus.Labels{}

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

	m.JobsCounterGauge = promauto.With(m.Registry).NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName(namespace, subsystem, "job_counter"),
			Help:        "Jobs counter by topic",
			ConstLabels: m.ConstLabels,
		}, []string{"topic"},
	)

	buckets := []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 20, 30, 60}
	labelNames := []string{"topic"}
	m.JobDurationSeconds = promauto.With(m.Registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    prometheus.BuildFQName(namespace, subsystem, "job_duration"),
			Help:    "A histogram of the job durations in seconds.",
			Buckets: buckets,
		},
		labelNames,
	)

	m.serviceMetrics = serviceMetrics
	m.notifChan = notifChan

	m.FiberPrometheus = fiberprometheus.NewWithRegistry(m.Registry, "minijob", "http", "", m.ConstLabels, fiberprometheus.DefaultBuckets)
	m.FiberPrometheus.RegisterAt(app, "/metrics")
	m.FiberPrometheus.SetSkipPaths([]string{"/ping", "/healthcheck", "/api/v1/admin/server/restart"}) // Optional: Remove some paths from metrics
	_ = app.Use(m.FiberPrometheus.Middleware)

	go m.Run()
}

func (m *Metrics) InitCustomMetrics(e service.ServiceEvent) {
	topic := ""
	m.JobDurationSeconds.WithLabelValues(topic).Observe(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobCreated.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobEnqueued.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobStarted.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobSucceeded.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobCanceled.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobFailed.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobDeleted.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobTimeout.String(), topic).Add(0)
	m.JobsEventsCounter.WithLabelValues(service.ServiceEventJobTerminated.String(), topic).Add(0)
	m.SetGauges(e)
}

func (m *Metrics) Shutdown() {
	m.notifChan <- service.ServiceEvent{Type: service.ServiceEventShutdown}
	m.wg.Wait()
}

func (m *Metrics) SetGauges(e service.ServiceEvent) {
	topicMetric := m.serviceMetrics.GetMetricByTopic(e.Topic)
	m.JobsCounterGauge.WithLabelValues(e.Topic).Set(float64(topicMetric.JobsCounter))
	m.JobsStatusGauge.WithLabelValues("Created", e.Topic).Set(float64(topicMetric.JobsCounterCreated))
	m.JobsStatusGauge.WithLabelValues("Pending", e.Topic).Set(float64(topicMetric.JobsCounterPending))
	m.JobsStatusGauge.WithLabelValues("Queued", e.Topic).Set(float64(topicMetric.JobsCounterQueued))
	m.JobsStatusGauge.WithLabelValues("Running", e.Topic).Set(float64(topicMetric.JobsCounterRunning))
	m.JobsStatusGauge.WithLabelValues("Succeeded", e.Topic).Set(float64(topicMetric.JobsCounterSucceeded))
	m.JobsStatusGauge.WithLabelValues("Failed", e.Topic).Set(float64(topicMetric.JobsCounterFailed))
	m.JobsStatusGauge.WithLabelValues("Terminated", e.Topic).Set(float64(topicMetric.JobsCounterTerminated))
	m.JobsStatusGauge.WithLabelValues("Timeout", e.Topic).Set(float64(topicMetric.JobsCounterTimeout))
	m.JobsStatusGauge.WithLabelValues("Deleted", e.Topic).Set(float64(topicMetric.JobsCounterDeleted))
	m.JobsStatusGauge.WithLabelValues("Canceled", e.Topic).Set(float64(topicMetric.JobsCounterCanceled))
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
