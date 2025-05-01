package eventlogger

import (
	"os"
	"time"

	"github.com/nbigot/minijob/event"
	"github.com/nbigot/minijob/job"
)

type EventLoggerObserver struct {
	// implements IServiceEventObserver interface
	FilePath string   // path to the log file
	file     *os.File // file descriptor
}

func (e *EventLoggerObserver) Init() error {
	return e.OpenOrCreateFile()
}

func (e *EventLoggerObserver) Shutdown() {
	if e.file != nil {
		e.file.Close()
		e.file = nil
	}
}

func (e *EventLoggerObserver) NotifyEvent(ev event.ServiceEventType) {
	// filter events
	switch ev {
	case event.ServiceEventEmptyPollReceived, event.ServiceEventJobHealthcheck, event.ServiceEventJobMetrics:
		return
	}

	// write the event to the log file
	_, err := e.file.WriteString(e.ServiceEventToJson(ev) + "\n")
	if err != nil {
		panic(err)
	}
}

func (e *EventLoggerObserver) NotifyTopicEvent(ev event.ServiceEventType, topic string) {
	// filter events
	switch ev {
	case event.ServiceEventEmptyPollReceived, event.ServiceEventJobHealthcheck, event.ServiceEventJobMetrics:
		return
	}

	// write the event to the log file
	_, err := e.file.WriteString(e.ServiceEventToJson(ev) + "\n")
	if err != nil {
		panic(err)
	}
}

func (e *EventLoggerObserver) NotifyJobEvent(j *job.Job, ev event.ServiceEventType) {
	// write the event to the log file
	_, err := e.file.WriteString(e.JobEventToJson(j, ev) + "\n")
	if err != nil {
		panic(err)
	}
}

func (e *EventLoggerObserver) ServiceEventToJson(ev event.ServiceEventType) string {
	// convert the event to a JSON string
	// format: {"timestamp": "2000-01-01T12:34:00.123", "type": "Created"}
	now := time.Now().Format("2006-01-02T15:04:05.000")
	return `{"timestamp": "` + now + `", "type": "` + ev.String() + `"}`
}

func (e *EventLoggerObserver) JobEventToJson(j *job.Job, ev event.ServiceEventType) string {
	// convert the event to a JSON string
	// format: {"timestamp": "2000-01-01T12:34:00.123", "type": "ServiceEventJobCreated", "jobUUID": "1234", "topic": "topic"}
	now := time.Now().Format("2006-01-02T15:04:05.000")
	return `{"timestamp": "` + now + `", "type": "` + ev.String() + `", "jobUUID": "` + j.JobUUID.String() + `", "topic": "` + j.Topic + `"}`
}

func (e *EventLoggerObserver) OpenOrCreateFile() error {
	// create the log file if it does not exist, or open it and keep the file descriptor in e.file
	if _, err := os.Stat(e.FilePath); os.IsNotExist(err) {
		file, err := os.Create(e.FilePath)
		if err != nil {
			return err
		}
		e.file = file
	} else {
		file, err := os.OpenFile(e.FilePath, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
		if err != nil {
			return err
		}
		e.file = file
	}
	return nil
}

func NewEventLoggerObserver(filePath string) *EventLoggerObserver {
	return &EventLoggerObserver{
		FilePath: filePath,
	}
}
