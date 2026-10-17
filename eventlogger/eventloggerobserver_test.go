package eventlogger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nbigot/minijob/event"
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestJobEventLineIsValidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.log")
	e := NewEventLoggerObserver(path)
	require.NoError(t, e.Init())
	topic := "a\"b\\c\n<&> é"
	j := &job.Job{Topic: topic}
	j.Init(uuid.New(), 1)
	e.NotifyJobEvent(j, event.ServiceEventJobCreated)
	e.NotifyJobEvent(&job.Job{Topic: "plain <topic> & co"}, event.ServiceEventJobCreated)
	e.Shutdown()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	require.Len(t, lines, 2)
	var decoded struct {
		Topic string `json:"topic"`
	}
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &decoded), lines[0])
	assert.Equal(t, topic, decoded.Topic)
	// Characters that do not break the line are written as they are.
	assert.Contains(t, lines[1], `"topic": "plain <topic> & co"}`)
}

func TestWriteErrorDoesNotPanic(t *testing.T) {
	logger := zap.NewNop()
	log.Logger = logger
	e := NewEventLoggerObserver(filepath.Join(t.TempDir(), "events.log"))
	require.NoError(t, e.Init())
	// Closed underneath: every write fails.
	require.NoError(t, e.file.Close())
	j := &job.Job{Topic: "t"}
	assert.NotPanics(t, func() {
		e.NotifyJobEvent(j, event.ServiceEventJobCreated)
		e.NotifyEvent(event.ServiceEventReady)
	})
	// After Shutdown (the service may still notify events while it stops).
	e.Shutdown()
	assert.NotPanics(t, func() {
		e.NotifyJobEvent(j, event.ServiceEventJobCreated)
		e.NotifyTopicEvent(event.ServiceEventReady, "t")
	})
}
