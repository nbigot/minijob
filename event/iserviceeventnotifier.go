package event

import "github.com/nbigot/minijob/job"

type IServiceEventObserver interface {
	Init() error
	Shutdown()
	NotifyEvent(ServiceEventType)
	NotifyTopicEvent(ev ServiceEventType, topic string)
	NotifyJobEvent(*job.Job, ServiceEventType)
}

type IServiceEventNotifier interface {
	Init() error
	Shutdown()
	Register(observer IServiceEventObserver)
	NotifyEvent(ServiceEventType)
	NotifyTopicEvent(ev ServiceEventType, topic string)
	NotifyJobEvent(*job.Job, ServiceEventType)
}
