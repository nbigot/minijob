package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseArgs(t *testing.T) {
	o, err := parseArgs([]string{"-url", "http://h:9/", "--concurrency=4", "-scenarios", "ping,get"})
	require.NoError(t, err)
	assert.Equal(t, "http://h:9", o.url)
	assert.Equal(t, 4, o.concurrency)
	assert.Equal(t, []string{"ping", "get"}, o.scenarios)
	assert.Equal(t, "http://h:9", o.label)
	o, err = parseArgs([]string{"-batch-size", "500", "-label", "-x"})
	require.NoError(t, err)
	assert.Equal(t, 100, o.batchSize)
	assert.Equal(t, "-x", o.label)
	for _, args := range [][]string{{"-nope", "1"}, {"-scenarios", "x"}, {"-duration"}, {"-wait", "-1"}, {"-url", "https://h"}, {"extra"}} {
		_, err := parseArgs(args)
		assert.Error(t, err, args)
	}
}

func TestNearestRankPercentiles(t *testing.T) {
	var s samples
	for i := 1; i <= 100; i++ {
		s.push(time.Duration(i) * time.Millisecond)
	}
	l := s.summary()
	assert.Equal(t, latency{Mean: 50.5, P50: 50, P90: 90, P99: 99, Max: 100}, l)
	var empty samples
	assert.Equal(t, latency{}, empty.summary())
}

func TestMedianAndSpread(t *testing.T) {
	assert.Equal(t, "2 (1–3)", cell([]float64{3, 1, 2}, 0))
	assert.Equal(t, "3.0 (2.0–4.0)", cell([]float64{2, 4}, 1))
	assert.Equal(t, "5.00", cell([]float64{5}, 2))
	assert.Equal(t, "-", cell(nil, 2))
}

// fakeServer answers the requests of every scenario: jobs are queued at creation.
type fakeServer struct {
	mu    sync.Mutex
	next  int
	jobs  map[string]*fakeJob
	queue []string
}

type fakeJob struct{ topic, status string }

func newFakeServer() *httptest.Server {
	f := &fakeServer{jobs: map[string]*fakeJob{}}
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("GET /api/v1/system/info", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"systemInfo": map[string]any{"version": "test"}})
	})
	mux.HandleFunc("DELETE /api/v1/jobs/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.jobs, f.queue = map[string]*fakeJob{}, nil
	})
	mux.HandleFunc("GET /api/v1/jobs/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		status, topic := r.URL.Query().Get("status"), r.URL.Query().Get("topic")
		n := 0
		for _, j := range f.jobs {
			if (status == "" || j.status == status) && (topic == "" || j.topic == topic) {
				n++
			}
		}
		writeJSON(w, map[string]any{"total": n})
	})
	mux.HandleFunc("POST /api/v1/job/{$}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Topic string `json:"topic"`
		}
		data, _ := io.ReadAll(r.Body)
		if json.Unmarshal(data, &body) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.next++
		id := "job-" + strconv.Itoa(f.next)
		f.jobs[id] = &fakeJob{topic: body.Topic, status: "queued"}
		f.queue = append(f.queue, id)
		writeJSON(w, map[string]any{"job": map[string]any{"id": id}})
	})
	mux.HandleFunc("GET /api/v1/job/{id}", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("POST /api/v1/job/pull", func(w http.ResponseWriter, r *http.Request) {
		numjobs, _ := strconv.Atoi(r.URL.Query().Get("numjobs"))
		topic := r.URL.Query().Get("topic")
		f.mu.Lock()
		defer f.mu.Unlock()
		var jobs []map[string]any
		rest := f.queue[:0]
		for _, id := range f.queue {
			if j := f.jobs[id]; j != nil && j.topic == topic && len(jobs) < max(numjobs, 1) {
				j.status = "running"
				jobs = append(jobs, map[string]any{"id": id})
			} else if j != nil {
				rest = append(rest, id)
			}
		}
		f.queue = rest
		if len(jobs) == 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"jobs": jobs})
	})
	mux.HandleFunc("POST /api/v1/job/{id}/succeed", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if j := f.jobs[r.PathValue("id")]; j != nil {
			j.status = "succeeded"
		}
	})
	return httptest.NewServer(mux)
}

func TestEveryScenarioAgainstAFakeServer(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "result.json")
	code := run([]string{"-url", srv.URL, "-concurrency", "4", "-duration", "1", "-warmup", "0",
		"-preload", "50", "-batch-jobs", "500", "-batch-size", "10", "-label", "fake", "-json", path})
	require.Equal(t, 0, code)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var r runResult
	require.NoError(t, json.Unmarshal(data, &r))
	assert.Equal(t, "go", r.Implementation)
	assert.Equal(t, "test", r.Server.Version)
	require.Len(t, r.Scenarios, len(scenarioNames))
	for i, s := range r.Scenarios {
		assert.Equal(t, scenarioNames[i], s.Name)
		assert.Positive(t, s.Items, s.Name)
		assert.Zero(t, s.Errors, s.Name)
		if s.Check != nil {
			assert.True(t, s.Check.OK, "%s: %s", s.Name, s.Check.Detail)
		}
	}
	assert.Equal(t, "500 jobs created, 500 pulled, 0 duplicates, 0 missing, 0 unknown", r.Scenarios[2].Check.Detail)
	assert.Equal(t, r.Scenarios[0].Requests, r.Scenarios[0].Statuses["200"])

	text, err := report([]string{path, path})
	require.NoError(t, err)
	assert.Contains(t, text, "### cycle\n")
	assert.Contains(t, text, fmt.Sprintf("| fake | 2 | %s |", cell([]float64{r.Scenarios[0].RequestsPerS}, 0)))
	assert.Contains(t, text, "| fake | 2 | - | - |\n", "no memory figures without -pid")
}

func TestLostJobsFailTheCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST":
			_, _ = w.Write([]byte(`{"job":{"id":"x"}}`))
		case r.URL.Path == "/api/v1/jobs/":
			_, _ = w.Write([]byte(`{"total":0}`)) // nothing is stored
		}
	}))
	defer srv.Close()
	code := run([]string{"-url", srv.URL, "-scenarios", "create", "-concurrency", "1", "-duration", "1", "-warmup", "0"})
	assert.Equal(t, 1, code)
}
