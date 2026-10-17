package redisprovider

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/nbigot/minijob/config"
	"github.com/nbigot/minijob/event"
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/jobbackendprovider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// newTestProvider returns an initialized provider on the miniredis server, which does not
// know the CONFIG command (like a managed Redis that refuses it).
func newTestProvider(t *testing.T, m *miniredis.Miniredis) *RedisJobBackendProvider {
	t.Helper()
	conf := &config.Config{}
	conf.Backend.Redis.MaxJobs = 1000
	conf.Backend.Redis.FinishedJobTTL = 60
	conf.Backend.Redis.URL = "redis://" + m.Addr() + "/0?dial_timeout=200ms&read_timeout=500ms&write_timeout=500ms&max_retries=-1"
	bp, err := NewRedisJobBackendProvider(zap.NewNop(), conf)
	require.NoError(t, err)
	p := bp.(*RedisJobBackendProvider)
	p.SetNotifChan(make(chan jobbackendprovider.Event, 100))
	require.NoError(t, p.Init())
	t.Cleanup(func() { _ = p.Stop() })
	return p
}

func newJob(topic string) *job.Job {
	j := &job.Job{Topic: topic, JobProperties: job.JobProperties{}}
	j.Init(uuid.Must(uuid.NewV7()), time.Now().UnixMilli())
	return j
}

func storeJob(t *testing.T, m *miniredis.Miniredis, j *job.Job) {
	t.Helper()
	data, err := j.ToJSON()
	require.NoError(t, err)
	require.NoError(t, m.Set(JobUUID2RedisKey(j.JobUUID), string(data)))
}

func TestStartsWhenConfigSetIsRefused(t *testing.T) {
	m := miniredis.RunT(t)
	stored := newJob("t")
	storeJob(t, m, stored)
	p := newTestProvider(t, m)
	jobs, err := p.LoadJobs()
	require.NoError(t, err)
	assert.Len(t, jobs, 1)
	assert.Contains(t, jobs, stored.JobUUID)
}

func TestLoadJobsReadsOnlyJobKeys(t *testing.T) {
	m := miniredis.RunT(t)
	for range 1200 {
		storeJob(t, m, newJob("t"))
	}
	require.NoError(t, m.Set("other", "not a job"))
	p := newTestProvider(t, m)
	jobs, err := p.LoadJobs()
	require.NoError(t, err)
	assert.Len(t, jobs, 1200)
}

func TestWriteFailedDuringAnOutageIsRetried(t *testing.T) {
	m := miniredis.RunT(t)
	p := newTestProvider(t, m)
	_, err := p.LoadJobs()
	require.NoError(t, err)
	go func() { _ = p.Run() }()
	require.True(t, p.Healthcheck())

	m.Close() // Redis cannot be reached
	j := newJob("t")
	p.NotifyJobEvent(j, event.ServiceEventJobCreated)
	assert.False(t, p.Healthcheck())

	require.NoError(t, m.Restart())
	key := JobUUID2RedisKey(j.JobUUID)
	assert.Eventually(t, func() bool { return m.Exists(key) }, 10*time.Second, 20*time.Millisecond,
		"the job is saved once Redis is back")
	assert.Eventually(t, p.Healthcheck, 10*time.Second, 20*time.Millisecond)
}

func TestSucceededJobExpires(t *testing.T) {
	m := miniredis.RunT(t)
	p := newTestProvider(t, m)
	_, err := p.LoadJobs()
	require.NoError(t, err)
	j := newJob("t")
	p.NotifyJobEvent(j, event.ServiceEventJobCreated)
	j.AddHistoryEvent(job.JobEventEnqueue, time.Now().UnixMilli())
	j.AddHistoryEvent(job.JobEventStart, time.Now().UnixMilli())
	j.AddHistoryEvent(job.JobEventSuccess, time.Now().UnixMilli())
	p.NotifyJobEvent(j, event.ServiceEventJobSucceeded)
	require.NoError(t, p.Stop()) // the pending writes are made before the stop

	key := JobUUID2RedisKey(j.JobUUID)
	assert.Equal(t, 60*time.Second, m.TTL(key))
	saved, err := m.Get(key)
	require.NoError(t, err)
	assert.Contains(t, saved, `"eventType":"SUCCESS"`)
}

func TestDeleteAllKeepsOtherKeys(t *testing.T) {
	m := miniredis.RunT(t)
	storeJob(t, m, newJob("a"))
	storeJob(t, m, newJob("b"))
	require.NoError(t, m.Set("other", "kept"))
	p := newTestProvider(t, m)
	_, err := p.LoadJobs()
	require.NoError(t, err)
	p.NotifyEvent(event.ServiceEventJobDeletedAll)
	j := newJob("after")
	p.NotifyJobEvent(j, event.ServiceEventJobCreated)
	require.NoError(t, p.Stop())

	assert.Equal(t, []string{JobUUID2RedisKey(j.JobUUID), "other"}, m.Keys())
}

func TestWaitWritesReturnsOnceWritten(t *testing.T) {
	m := miniredis.RunT(t)
	p := newTestProvider(t, m)
	_, err := p.LoadJobs()
	require.NoError(t, err)
	j := newJob("t")
	p.NotifyJobEvent(j, event.ServiceEventJobCreated)
	for i := uint(1); i <= 100; i++ {
		j.VisibilityTimeout = i
		p.NotifyJobEvent(j, event.ServiceEventJobStarted)
	}
	p.WaitWrites()

	saved, err := m.Get(JobUUID2RedisKey(j.JobUUID))
	require.NoError(t, err)
	assert.Contains(t, saved, `"visibilityTimeout":100,`, "the writes are made in order")
}

func TestWaitWritesDuringAnOutageReturnsAfterOneAttempt(t *testing.T) {
	m := miniredis.RunT(t)
	p := newTestProvider(t, m)
	_, err := p.LoadJobs()
	require.NoError(t, err)
	m.Close()
	p.NotifyJobEvent(newJob("t"), event.ServiceEventJobCreated)
	done := make(chan struct{})
	go func() { p.WaitWrites(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("WaitWrites still waiting while Redis cannot be reached")
	}
	assert.False(t, p.writer.healthy())
	require.NoError(t, m.Restart()) // the stop writes the job
}

func TestUpdateKeepsTheExpirationOfACompletedJob(t *testing.T) {
	m := miniredis.RunT(t)
	p := newTestProvider(t, m)
	_, err := p.LoadJobs()
	require.NoError(t, err)
	j := newJob("t")
	j.AddHistoryEvent(job.JobEventEnqueue, time.Now().UnixMilli())
	j.AddHistoryEvent(job.JobEventStart, time.Now().UnixMilli())
	j.AddHistoryEvent(job.JobEventSuccess, time.Now().UnixMilli())
	p.NotifyJobEvent(j, event.ServiceEventJobSucceeded)
	j.VisibilityTimeout = 300
	require.NoError(t, p.OnJobUpdated(j))
	p.WaitWrites()
	assert.Equal(t, 60*time.Second, m.TTL(JobUUID2RedisKey(j.JobUUID)))
}

func TestJobExpiredSinceTheScanIsSkipped(t *testing.T) {
	m := miniredis.RunT(t)
	kept := newJob("t")
	storeJob(t, m, kept)
	p := newTestProvider(t, m)
	jobs, err := p.getJobs([]string{JobUUID2RedisKey(kept.JobUUID), JobUUID2RedisKey(uuid.New())})
	require.NoError(t, err)
	assert.Len(t, jobs, 1)
	assert.Contains(t, jobs, kept.JobUUID)
}

func TestCoalesceKeepsTheLastWriteOfEachKey(t *testing.T) {
	set := func(k, v string) writeOp { return writeOp{key: k, value: v} }
	del := func(k string) writeOp { return writeOp{key: k, del: true} }
	assert.Equal(t,
		[]writeOp{set("a", "2"), del("b")},
		coalesce([]writeOp{set("a", "1"), set("b", "1"), set("a", "2"), del("b")}))
	assert.Equal(t,
		[]writeOp{{delAll: true}, set("b", "1")},
		coalesce([]writeOp{set("a", "1"), {delAll: true}, set("b", "1")}))
}
