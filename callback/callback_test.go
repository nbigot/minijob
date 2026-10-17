package callback

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nbigot/minijob/event"
	"github.com/nbigot/minijob/job"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type receiver struct {
	mu       sync.Mutex
	payloads []CallbackPayload
}

func (r *receiver) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		var p CallbackPayload
		_ = json.Unmarshal(body, &p)
		r.mu.Lock()
		r.payloads = append(r.payloads, p)
		r.mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (r *receiver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.payloads)
}

func newJob(url string, events ...string) *job.Job {
	j := &job.Job{Topic: "t", CallbackURL: url}
	j.Init(uuid.New(), 1000)
	for i, e := range events {
		j.AddHistoryEvent(e, int64(1001+i))
	}
	return j
}

func TestCallbackOnlyOnCompletingTransition(t *testing.T) {
	var r receiver
	srv := r.server(t)
	cs := NewCallbackService(zap.NewNop())

	// A completed job loaded at startup, or deleted, was already notified.
	done := newJob(srv.URL, job.JobEventPending, job.JobEventEnqueue, job.JobEventStart, job.JobEventSuccess)
	cs.NotifyJobEvent(done, event.ServiceEventJobLoaded)
	cs.NotifyJobEvent(done, event.ServiceEventJobDeleted)
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 0, r.count(), "no callback for a loaded or deleted job")

	// The transition into a final state sends exactly one callback.
	cs.NotifyJobEvent(done, event.ServiceEventJobSucceeded)
	require.Eventually(t, func() bool { return r.count() == 1 }, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, "succeeded", r.payloads[0].Status)
}

func TestCallbackPayloadIsTakenAtTheTransition(t *testing.T) {
	var r receiver
	srv := r.server(t)
	cs := NewCallbackService(zap.NewNop())

	// A canceled job is put back in the queue right after the cancel event.
	j := newJob(srv.URL, job.JobEventPending, job.JobEventEnqueue, job.JobEventStart, job.JobEventCancel)
	cs.NotifyJobEvent(j, event.ServiceEventJobCanceled)
	j.AddHistoryEvent(job.JobEventEnqueue, 2000)
	cs.NotifyJobEvent(j, event.ServiceEventJobEnqueued)

	require.Eventually(t, func() bool { return r.count() == 1 }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 1, r.count())
	assert.Equal(t, "canceled", r.payloads[0].Status)
	assert.Len(t, r.payloads[0].Job.History, 5, "the payload does not include later events")
}

func TestCallbackConcurrencyIsBounded(t *testing.T) {
	var inFlight, maxInFlight atomic.Int64
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		n := inFlight.Add(1)
		for {
			m := maxInFlight.Load()
			if n <= m || maxInFlight.CompareAndSwap(m, n) {
				break
			}
		}
		<-release
		inFlight.Add(-1)
	}))
	defer srv.Close()
	defer close(release) // runs first: unblocks the handlers so that Close returns
	cs := NewCallbackService(zap.NewNop())
	for i := 0; i < maxConcurrentCallbacks+50; i++ {
		cs.NotifyJobEvent(newJob(srv.URL, job.JobEventStart, job.JobEventSuccess), event.ServiceEventJobSucceeded)
	}
	require.Eventually(t, func() bool { return inFlight.Load() == maxConcurrentCallbacks }, 10*time.Second, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, int64(maxConcurrentCallbacks), maxInFlight.Load())
}
