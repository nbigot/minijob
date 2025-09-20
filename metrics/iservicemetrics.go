package metrics

import (
	"github.com/nbigot/minijob/event"
	"github.com/nbigot/minijob/fiberprometheus"
	"github.com/nbigot/minijob/job"
)

type IServiceMetrics interface {
	Init() error
	Shutdown()
	GetFiberPrometheus() *fiberprometheus.FiberPrometheus
	GetTopics() []string
	GetTopicsStats() []TopicMetrics
	GetResourcesMetrics() []ResourceMetrics
	GetMetricByTopic(topic string) *JobMetrics
	NotifyEvent(event.ServiceEventType)
	NotifyTopicEvent(ev event.ServiceEventType, topic string)
	NotifyJobEvent(*job.Job, event.ServiceEventType)
	UpdateResourcesLockedCountMetric(topic string, inc int)
	UpdateJobStatistics(m job.JobMap)
}
