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
	JobsTotal          *prometheus.CounterVec
	JobsInProgress     *prometheus.GaugeVec
	ConstLabels        prometheus.Labels
	Registry           *prometheus.Registry
	FiberPrometheus    *fiberprometheus.FiberPrometheus
	notifChan          chan service.ServiceEvent // notification channel
	wg                 sync.WaitGroup
}

func (m *Metrics) Init(app *fiber.App, notifChan chan service.ServiceEvent) {
	// Create non-global registry.
	m.Registry = prometheus.NewRegistry()

	namespace := "job"
	subsystem := ""

	m.ConstLabels = prometheus.Labels{}

	m.JobsTotal = promauto.With(m.Registry).NewCounterVec(
		prometheus.CounterOpts{
			Name:        prometheus.BuildFQName(namespace, subsystem, "jobs_total"),
			Help:        "Count all jobs by status code, method and path.",
			ConstLabels: m.ConstLabels,
		},
		[]string{"status_code", "method", "path"},
	)

	m.JobsInProgress = promauto.With(m.Registry).NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName(namespace, subsystem, "jobs_in_progress"),
			Help:        "All the jobs in progress",
			ConstLabels: m.ConstLabels,
		}, []string{"method"},
	)

	buckets := []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 20, 30, 60}
	labelNames := []string{"default", "topic", "others"} // TODO
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

func (m *Metrics) Shutdown() {
	m.notifChan <- service.ServiceEvent{Type: service.ServiceEventShutdown}
	m.wg.Wait()
}

func (m *Metrics) Run() {
	m.wg.Add(1)
	for {
		select {
		case e := <-m.notifChan:
			switch e.Type {
			// case "job": // TODO
			// 	// m.JobsTotal.WithLabelValues(e.Value, "GET", "/").Inc()
			// case "duration": // TODO
			// 	// m.JobDurationSeconds.WithLabelValues("default").Observe(e.Value)
			// case "inprogress": // TODO
			// 	// m.JobsInProgress.WithLabelValues("GET").Inc()
			// case "completed": // TODO
			// 	// m.JobsInProgress.WithLabelValues("GET").Dec()
			// case "error": // TODO
			// 	// m.JobsTotal.WithLabelValues("500", "GET", "/").Inc()
			case service.ServiceEventShutdown:
				m.wg.Done()
				return // exit the goroutine
			default:
			}
		}
	}
}

// NewMetrics returns a new Metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{}
}
