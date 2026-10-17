package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nbigot/minijob/config"
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/jobbackendprovider/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var registryOnce sync.Once

// testConfig loads the load test template with the given replacements.
func testConfig(t *testing.T, replacements ...string) *config.Config {
	t.Helper()
	registryOnce.Do(func() { require.NoError(t, registry.Initialize()) })
	template, err := os.ReadFile(filepath.Join("..", "config-templates", "standard", "config", "config.loadtest.yaml"))
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "config.yaml")
	text := strings.NewReplacer(replacements...).Replace(string(template))
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	conf, err := config.LoadConfig(path)
	require.NoError(t, err)
	return conf
}

// persistentConfig stores the jobs in dir/jobs.json.
func persistentConfig(t *testing.T, dir string) *config.Config {
	return testConfig(t, "        enablePersistentStorage: false\n",
		"        enablePersistentStorage: true\n        directory: \""+filepath.ToSlash(dir)+"\"\n        filename: \"jobs.json\"\n")
}

// sequentialIDs returns n version 7 ids in increasing order.
func sequentialIDs(n int) []job.JobUUID {
	ids := make([]job.JobUUID, n)
	for i := range ids {
		ids[i] = uuid.Must(uuid.NewV7())
	}
	slices.SortFunc(ids, func(a, b job.JobUUID) int { return bytes.Compare(a[:], b[:]) })
	return ids
}

func uuidStrings(jobs []*job.Job) []string {
	s := make([]string, len(jobs))
	for i, j := range jobs {
		s[i] = j.JobUUID.String()
	}
	return s
}

func TestSortJobsIsDeterministic(t *testing.T) {
	// Jobs with the same priority, topic and timestamps are ordered by id.
	ids := sequentialIDs(100)
	expected := make([]string, len(ids))
	jobs := make([]*job.Job, len(ids))
	for i, id := range ids {
		expected[i] = id.String()
		j := &job.Job{Topic: "t"}
		j.Init(id, 1000)
		jobs[i] = j
	}
	svc := &Service{}
	for _, sort := range []string{"", "created_desc", "priority_desc", "topic_asc,updated_desc"} {
		rand.Shuffle(len(jobs), func(a, b int) { jobs[a], jobs[b] = jobs[b], jobs[a] })
		svc.sortJobs(jobs, sort)
		assert.Equal(t, expected, uuidStrings(jobs), "sort %q", sort)
	}
}

func TestGetAllJobsInIdOrder(t *testing.T) {
	ids := sequentialIDs(100)
	svc := &Service{jobs: make(job.JobMap)}
	expected := make([]string, len(ids))
	for i, id := range ids {
		expected[i] = id.String()
		j := &job.Job{}
		j.Init(id, int64(i))
		svc.jobs[id] = j
	}
	assert.Equal(t, expected, uuidStrings(svc.GetAllJobs()))
}

func TestTopicsAfterLoadInCreationOrder(t *testing.T) {
	// The topics of loaded jobs are registered in the order of the jobs (creation order),
	// not in the random order of a map.
	dir := t.TempDir()
	ids := sequentialIDs(30)
	stored := map[string]*job.Job{}
	var topics []string
	for i, id := range ids {
		topic := fmt.Sprintf("topic-%02d", i)
		topics = append(topics, topic)
		j := &job.Job{Topic: topic, JobProperties: job.JobProperties{}}
		j.Init(id, int64(1000+i))
		j.AddHistoryEvent(job.JobEventPending, int64(1000+i))
		stored[id.String()] = j
	}
	data, err := json.Marshal(map[string]interface{}{"jobs": stored})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "jobs.json"), data, 0o600))

	svc, err := CreateAndInitService(persistentConfig(t, dir), "test")
	require.NoError(t, err)
	assert.Equal(t, topics, svc.GetTopics())
}

// runningService starts the service on the load test template (in memory, no file).
func runningService(t *testing.T) *Service {
	t.Helper()
	svc, err := CreateAndInitService(testConfig(t), "test")
	require.NoError(t, err)
	go func() { _ = svc.Run() }()
	t.Cleanup(func() { _ = svc.Stop() })
	return svc
}

func TestConcurrentCompletionsOfOneJob(t *testing.T) {
	// A running job completed by several workers at the same time is completed once.
	svc := runningService(t)
	const jobs = 100
	for i := 0; i < jobs; i++ {
		_, err := svc.CreateJob([]byte(`{"topic":"once","properties":{}}`))
		require.NoError(t, err)
	}
	require.Eventually(t, func() bool {
		res, _ := svc.GetJobs(&GetJobsRequest{Page: 1, Limit: 1, Status: "queued", Topic: "once"})
		return res.Total == jobs
	}, 5*time.Second, 10*time.Millisecond)
	res, err := svc.PullJobs(&RequestPullJobs{Topic: "once", NumJobs: jobs, VisibilityTimeout: 60})
	require.NoError(t, err)
	require.Len(t, res.Jobs, jobs)

	var successes atomic.Int64
	var wg sync.WaitGroup
	for _, j := range res.Jobs {
		id := j.JobUUID
		for k := 0; k < 4; k++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var err error
				if k%2 == 0 {
					err = svc.SetJobAsSuccessful(id)
				} else {
					_, err = svc.FailJob(id)
				}
				if err == nil {
					successes.Add(1)
				}
			}()
		}
	}
	wg.Wait()
	assert.Equal(t, int64(jobs), successes.Load(), "each running job is completed exactly once")
}

func TestMixedLoad(t *testing.T) {
	// Creations, pulls, completions, reads and deletions at the same time. Run with -race:
	// the scheduler state and the jobs are only changed under the service lock.
	svc := runningService(t)
	stop := time.Now().Add(1500 * time.Millisecond)
	var wg sync.WaitGroup
	worker := func(f func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; time.Now().Before(stop); i++ {
				f(i)
			}
		}()
	}
	for p := 0; p < 4; p++ {
		worker(func(i int) {
			body := fmt.Sprintf(`{"topic":"mixed","priority":%d,"properties":{"i":%d},"lockResources":["r%d"]}`, i%3, i, i%7)
			_, _ = svc.CreateJob([]byte(body))
		})
	}
	for w := 0; w < 4; w++ {
		worker(func(i int) {
			res, err := svc.PullJobs(&RequestPullJobs{Topic: "mixed", NumJobs: 3, VisibilityTimeout: 1})
			if err != nil {
				return
			}
			for k, j := range res.Jobs {
				switch (i + k) % 4 {
				case 0:
					_ = svc.SetJobAsSuccessful(j.JobUUID)
				case 1:
					_, _ = svc.FailJob(j.JobUUID)
				case 2:
					_ = svc.CancelJob(j.JobUUID)
				default:
					_ = svc.DeleteJob(j.JobUUID)
				}
			}
		})
	}
	worker(func(int) {
		_, _ = svc.GetJobs(&GetJobsRequest{Page: 1, Limit: 50, Sort: "priority_desc"})
		for _, j := range svc.GetAllJobs() {
			_, _ = json.Marshal(j)
		}
		_ = svc.GetLockedResources()
		_ = svc.GetTopics()
	})
	worker(func(int) {
		svc.CheckJobsVisibilityTimeout()
		svc.AttemptJobRetries()
		svc.TryEnqueuePendingJobs()
		time.Sleep(10 * time.Millisecond)
	})
	wg.Wait()

	all := svc.GetAllJobs()
	assert.Equal(t, uint(len(all)), svc.GetJobsCount())
	// Every locked resource belongs to a queued or running job.
	byID := map[job.JobUUID]*job.Job{}
	for _, j := range all {
		byID[j.JobUUID] = j
	}
	for resource, owner := range svc.GetLockedResources() {
		j := byID[owner]
		if assert.NotNil(t, j, "resource %s is locked by an unknown job", resource) {
			assert.Contains(t, []job.JobState{job.JobQueued, job.JobRunning}, j.GetState(), "resource %s", resource)
		}
	}
}

func TestLongPollDoesNotBlockOtherPulls(t *testing.T) {
	svc := runningService(t)
	go func() {
		_, _ = svc.PullJobs(&RequestPullJobs{Topic: "a", NumJobs: 1, VisibilityTimeout: 60, WaitTimeSeconds: 3})
	}()
	time.Sleep(100 * time.Millisecond) // the long poll is waiting
	_, err := svc.CreateJob([]byte(`{"topic":"b","properties":{}}`))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		res, _ := svc.GetJobs(&GetJobsRequest{Page: 1, Limit: 1, Status: "queued", Topic: "b"})
		return res.Total == 1
	}, 2*time.Second, 10*time.Millisecond)

	start := time.Now()
	res, err := svc.PullJobs(&RequestPullJobs{Topic: "b", NumJobs: 1, VisibilityTimeout: 60})
	require.NoError(t, err)
	assert.Len(t, res.Jobs, 1)
	assert.Less(t, time.Since(start), 500*time.Millisecond, "another pull waited for the long poll")
}

func TestLongPollReturnsAsSoonAsAJobIsQueued(t *testing.T) {
	svc := runningService(t)
	type result struct {
		res *ResponsePullJobs
		err error
		at  time.Time
	}
	done := make(chan result, 1)
	go func() {
		res, err := svc.PullJobs(&RequestPullJobs{Topic: "c", NumJobs: 1, VisibilityTimeout: 60, WaitTimeSeconds: 10})
		done <- result{res, err, time.Now()}
	}()
	time.Sleep(200 * time.Millisecond)
	created := time.Now()
	_, err := svc.CreateJob([]byte(`{"topic":"c","properties":{}}`))
	require.NoError(t, err)
	select {
	case r := <-done:
		require.NoError(t, r.err)
		assert.Len(t, r.res.Jobs, 1)
		assert.Less(t, r.at.Sub(created), 300*time.Millisecond, "the long poll noticed the job late")
	case <-time.After(5 * time.Second):
		t.Fatal("the long poll did not return")
	}
}

func TestLongPollTimesOut(t *testing.T) {
	svc := runningService(t)
	start := time.Now()
	_, err := svc.PullJobs(&RequestPullJobs{Topic: "none", NumJobs: 1, VisibilityTimeout: 60, WaitTimeSeconds: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no jobs found in the queue")
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, time.Second)
	assert.Less(t, elapsed, 2*time.Second)
}
