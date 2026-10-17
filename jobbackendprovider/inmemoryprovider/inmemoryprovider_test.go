package inmemoryprovider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/nbigot/minijob/config"
	"github.com/nbigot/minijob/event"
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/jobbackendprovider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newProvider(t *testing.T, dir string) *InMemoryJobBackendProvider {
	t.Helper()
	conf := &config.Config{}
	conf.Backend.InMemory.MaxJobs = 1000
	conf.Backend.InMemory.EnablePersistentStorage = true
	conf.Backend.InMemory.Directory = dir
	conf.Backend.InMemory.Filename = "jobs.json"
	p, err := NewInMemoryJobBackendProvider(zap.NewNop(), conf)
	require.NoError(t, err)
	ip := p.(*InMemoryJobBackendProvider)
	ip.SetNotifChan(make(chan jobbackendprovider.Event, 1000))
	return ip
}

func newJob(topic string) *job.Job {
	j := &job.Job{Topic: topic, JobProperties: job.JobProperties{}}
	j.Init(uuid.Must(uuid.NewV7()), 1000)
	return j
}

func savedTopics(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "jobs.json"))
	require.NoError(t, err)
	var file struct {
		Jobs map[string]job.Job `json:"jobs"`
	}
	require.NoError(t, json.Unmarshal(data, &file))
	var topics []string
	for _, j := range file.Jobs {
		topics = append(topics, j.Topic)
	}
	return topics
}

func TestChangeDuringSaveIsSavedNextTime(t *testing.T) {
	dir := t.TempDir()
	p := newProvider(t, dir)
	_, err := p.LoadJobs()
	require.NoError(t, err)
	p.NotifyJobEvent(newJob("first"), event.ServiceEventJobCreated)

	// A job is created while the first save is in progress.
	testHookBeforeSave = func() {
		testHookBeforeSave = nil
		p.NotifyJobEvent(newJob("during"), event.ServiceEventJobCreated)
	}
	t.Cleanup(func() { testHookBeforeSave = nil })
	require.NoError(t, p.sync())
	require.NoError(t, p.sync())
	assert.ElementsMatch(t, []string{"first", "during"}, savedTopics(t, dir))
}

func TestFinalSaveAtStop(t *testing.T) {
	dir := t.TempDir()
	p := newProvider(t, dir)
	_, err := p.LoadJobs()
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- p.Run() }()
	p.NotifyJobEvent(newJob("last"), event.ServiceEventJobCreated)
	require.NoError(t, p.Stop())
	assert.Equal(t, []string{"last"}, savedTopics(t, dir))
}

func TestWriteErrorDoesNotStopTheProgram(t *testing.T) {
	dir := t.TempDir()
	p := newProvider(t, dir)
	_, err := p.LoadJobs()
	require.NoError(t, err)
	// The file cannot be written: its path is now a directory.
	require.NoError(t, os.Remove(filepath.Join(dir, "jobs.json")))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "jobs.json"), 0o755))
	p.NotifyJobEvent(newJob("t"), event.ServiceEventJobCreated)
	assert.Error(t, p.sync()) // reported, not fatal
}

func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	p := newProvider(t, dir)
	_, err := p.LoadJobs()
	require.NoError(t, err)
	p.NotifyJobEvent(newJob("kept"), event.ServiceEventJobCreated)
	require.NoError(t, p.sync())

	// The new content cannot be written: the previous file stays complete.
	require.NoError(t, os.Mkdir(filepath.Join(dir, "jobs.json.tmp"), 0o755))
	p.NotifyJobEvent(newJob("lost"), event.ServiceEventJobCreated)
	assert.Error(t, p.sync())
	assert.Equal(t, []string{"kept"}, savedTopics(t, dir))
}

func TestLoadedJobsAreNotShared(t *testing.T) {
	// The service changes the jobs it loaded; the provider saves its own copies.
	dir := t.TempDir()
	j := newJob("t")
	data, err := json.Marshal(map[string]job.JobMap{"jobs": {j.JobUUID: j}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "jobs.json"), data, 0o600))
	p := newProvider(t, dir)
	loaded, err := p.LoadJobs()
	require.NoError(t, err)
	p.mapMutex.RLock()
	defer p.mapMutex.RUnlock()
	assert.NotSame(t, p.jobs[j.JobUUID], loaded[j.JobUUID])
}
