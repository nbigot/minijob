package event

import "github.com/nbigot/minijob/job"

type ServiceEventNotifier struct {
	// implement IServiceEventNotifier
	Observers []IServiceEventObserver
}

func (s *ServiceEventNotifier) Init() error {
	for _, observer := range s.Observers {
		if err := observer.Init(); err != nil {
			return err
		}
	}
	return nil
}

func (s *ServiceEventNotifier) Shutdown() {
	for _, observer := range s.Observers {
		observer.Shutdown()
	}
}

func (s *ServiceEventNotifier) Register(observer IServiceEventObserver) {
	s.Observers = append(s.Observers, observer)
}

func (s *ServiceEventNotifier) NotifyEvent(ev ServiceEventType) {
	for _, observer := range s.Observers {
		observer.NotifyEvent(ev)
	}
}

func (s *ServiceEventNotifier) NotifyTopicEvent(ev ServiceEventType, topic string) {
	for _, observer := range s.Observers {
		observer.NotifyTopicEvent(ev, topic)
	}
}

func (s *ServiceEventNotifier) NotifyJobEvent(j *job.Job, ev ServiceEventType) {
	for _, observer := range s.Observers {
		observer.NotifyJobEvent(j, ev)
	}
}

func NewServiceEventNotifier() *ServiceEventNotifier {
	return &ServiceEventNotifier{
		Observers: make([]IServiceEventObserver, 0),
	}
}
