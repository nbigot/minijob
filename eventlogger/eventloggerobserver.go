package eventlogger

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nbigot/minijob/event"
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/log"
	"go.uber.org/zap"
)

type EventLoggerObserver struct {
	// implements IServiceEventObserver interface
	FilePath string     // path to the log file
	mu       sync.Mutex // protects file (events come from several goroutines)
	file     *os.File   // file descriptor (nil once shut down)
}

func (e *EventLoggerObserver) Init() error {
	return e.OpenOrCreateFile()
}

func (e *EventLoggerObserver) Shutdown() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.file != nil {
		_ = e.file.Close() // Ignore error during shutdown
		e.file = nil
	}
}

// writeLine appends one line. A write error is logged: it must not stop the service.
func (e *EventLoggerObserver) writeLine(line string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.file == nil {
		return
	}
	if _, err := e.file.WriteString(line + "\n"); err != nil && log.Logger != nil {
		log.Logger.Error(
			"Cannot write to the events log file",
			zap.String("topic", "eventsLogger"),
			zap.Error(err),
		)
	}
}

func (e *EventLoggerObserver) NotifyEvent(ev event.ServiceEventType) {
	// filter events
	switch ev {
	case event.ServiceEventEmptyPollReceived, event.ServiceEventJobHealthcheck, event.ServiceEventJobMetrics:
		return
	}

	// write the event to the log file
	e.writeLine(e.ServiceEventToJson(ev))
}

func (e *EventLoggerObserver) NotifyTopicEvent(ev event.ServiceEventType, topic string) {
	// filter events
	switch ev {
	case event.ServiceEventEmptyPollReceived, event.ServiceEventJobHealthcheck, event.ServiceEventJobMetrics:
		return
	}

	// write the event to the log file
	e.writeLine(e.ServiceEventToJson(ev))
}

func (e *EventLoggerObserver) NotifyJobEvent(j *job.Job, ev event.ServiceEventType) {
	// write the event to the log file
	e.writeLine(e.JobEventToJson(j, ev))
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
	return `{"timestamp": "` + now + `", "type": "` + ev.String() + `", "jobUUID": "` + j.JobUUID.String() + `", "topic": "` + escapeJSONString(j.Topic) + `"}`
}

// escapeJSONString escapes what would break a JSON string: quote, backslash and control
// characters. Other characters are written as they are.
func escapeJSONString(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r == '"' || r == '\\' || r < 0x20 }) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (e *EventLoggerObserver) OpenOrCreateFile() error {
	// create the log file if it does not exist, or open it and keep the file descriptor in e.file
	e.mu.Lock()
	defer e.mu.Unlock()
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
