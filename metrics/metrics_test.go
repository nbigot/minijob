package metrics

import (
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/nbigot/minijob/service"
	"github.com/stretchr/testify/assert"
)

func TestMetrics_Init(t *testing.T) {
	app := fiber.New()
	notifChan := make(chan service.ServiceEvent)
	metrics := NewMetrics()
	serviceMetrics := service.NewSericeMetrics()

	metrics.Init(app, notifChan, serviceMetrics)

	assert.NotNil(t, metrics.Registry)
	assert.NotNil(t, metrics.JobsEventsCounter)
	assert.NotNil(t, metrics.JobsStatusGauge)
	assert.NotNil(t, metrics.JobsCounterGauge)
	assert.NotNil(t, metrics.JobDurationSeconds)
	assert.NotNil(t, metrics.FiberPrometheus)
	assert.Equal(t, notifChan, metrics.notifChan)
}

func TestMetrics_InitCustomMetrics(t *testing.T) {
	metrics := NewMetrics()
	e := service.ServiceEvent{
		Type:  service.ServiceEventReady,
		Topic: "test_topic",
	}

	app := fiber.New()
	notifChan := make(chan service.ServiceEvent)
	serviceMetrics := service.NewSericeMetrics()
	metrics.Init(app, notifChan, serviceMetrics)
	metrics.InitCustomMetrics(e)

	assert.NotNil(t, metrics.JobDurationSeconds.WithLabelValues("test_topic"))
	assert.NotNil(t, metrics.JobsEventsCounter.WithLabelValues(service.ServiceEventJobCreated.String(), "test_topic"))
	assert.NotNil(t, metrics.JobsEventsCounter.WithLabelValues(service.ServiceEventJobEnqueued.String(), "test_topic"))
	assert.NotNil(t, metrics.JobsEventsCounter.WithLabelValues(service.ServiceEventJobStarted.String(), "test_topic"))
	assert.NotNil(t, metrics.JobsEventsCounter.WithLabelValues(service.ServiceEventJobSucceeded.String(), "test_topic"))
	assert.NotNil(t, metrics.JobsEventsCounter.WithLabelValues(service.ServiceEventJobCanceled.String(), "test_topic"))
	assert.NotNil(t, metrics.JobsEventsCounter.WithLabelValues(service.ServiceEventJobFailed.String(), "test_topic"))
	assert.NotNil(t, metrics.JobsEventsCounter.WithLabelValues(service.ServiceEventJobDeleted.String(), "test_topic"))
	assert.NotNil(t, metrics.JobsEventsCounter.WithLabelValues(service.ServiceEventJobTimeout.String(), "test_topic"))
	assert.NotNil(t, metrics.JobsEventsCounter.WithLabelValues(service.ServiceEventJobTerminated.String(), "test_topic"))
}

func TestMetrics_Shutdown(t *testing.T) {
	metrics := NewMetrics()
	notifChan := make(chan service.ServiceEvent)
	metrics.notifChan = notifChan

	go func() {
		metrics.Shutdown()
	}()

	event := <-notifChan
	assert.Equal(t, service.ServiceEventShutdown, event.Type)
}
