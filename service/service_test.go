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

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/nbigot/minijob/config"
	"github.com/nbigot/minijob/constants"
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/jobbackendprovider/registry"
	"github.com/nbigot/minijob/metrics"
	"github.com/nbigot/minijob/web/apierror"
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

// idleService is initialized but its periodic loop does not run: the tests call the
// periodic tasks themselves.
func idleService(t *testing.T) *Service {
	t.Helper()
	svc, err := CreateAndInitService(testConfig(t), "test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Stop() })
	return svc
}

func createJob(t *testing.T, svc *Service, body string) job.JobUUID {
	t.Helper()
	j, err := svc.CreateJob([]byte(body))
	require.NoError(t, err)
	return j.JobUUID
}

func stateOf(t *testing.T, svc *Service, id job.JobUUID) job.JobState {
	t.Helper()
	j, _ := svc.GetJob(id)
	require.NotNil(t, j)
	return j.GetState()
}

func pullOne(t *testing.T, svc *Service, id job.JobUUID) {
	t.Helper()
	_, err := svc.PullJobs(&RequestPullJobs{JobUUID: &id, NumJobs: 1, VisibilityTimeout: 60})
	require.NoError(t, err)
}

func TestDeletingAJobKeepsTheLocksOfOthers(t *testing.T) {
	svc := idleService(t)
	a := createJob(t, svc, `{"properties":{},"lockResources":["r"]}`)
	b := createJob(t, svc, `{"properties":{},"lockResources":["r"]}`)
	require.Equal(t, job.JobQueued, stateOf(t, svc, a))
	require.Equal(t, job.JobPending, stateOf(t, svc, b))

	// Deleting the pending job does not release the lock of the queued one.
	require.NoError(t, svc.DeleteJob(b))
	assert.Equal(t, a, svc.GetLockedResources()["r"])

	// Deleting a completed job does not release the lock taken by the next job.
	c := createJob(t, svc, `{"properties":{},"lockResources":["r"]}`)
	pullOne(t, svc, a)
	require.NoError(t, svc.SetJobAsSuccessful(a))
	require.Equal(t, job.JobQueued, stateOf(t, svc, c))
	require.NoError(t, svc.DeleteJob(a))
	assert.Equal(t, c, svc.GetLockedResources()["r"])
}

func TestDelayedRetryLocksItsResourcesAgain(t *testing.T) {
	svc := idleService(t)
	a := createJob(t, svc, `{"properties":{},"lockResources":["r"]}`)
	pullOne(t, svc, a)
	_, err := svc.FailJob(a) // retried after the backoff delay (milliseconds)
	require.NoError(t, err)
	require.Equal(t, job.JobHidden, stateOf(t, svc, a))

	b := createJob(t, svc, `{"properties":{},"lockResources":["r"]}`)
	pullOne(t, svc, b)
	time.Sleep(20 * time.Millisecond) // the retry of a is due
	svc.AttemptJobRetries()
	assert.Equal(t, job.JobHidden, stateOf(t, svc, a), "a must wait: b holds the resource")
	assert.Equal(t, b, svc.GetLockedResources()["r"])

	require.NoError(t, svc.SetJobAsSuccessful(b))
	svc.AttemptJobRetries()
	assert.Equal(t, job.JobQueued, stateOf(t, svc, a))
	assert.Equal(t, a, svc.GetLockedResources()["r"])
}

func TestImmediateRetryWaitsForATakenResource(t *testing.T) {
	// Without retry delay, a failed job is queued again at once, which locks its resources
	// again. When one of them had been released (admin API) and taken by another job, the
	// failure answered 412 although it was recorded, and the job stayed "running" in no
	// queue, never retried. It now waits in the retry queue, as with a retry delay.
	svc, err := CreateAndInitService(testConfig(t, "            base: 1  #", "            base: 0  #"), "test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Stop() })

	free := createJob(t, svc, `{"properties":{},"lockResources":["s"]}`)
	pullOne(t, svc, free)
	_, err = svc.FailJob(free)
	require.NoError(t, err)
	assert.Equal(t, job.JobQueued, stateOf(t, svc, free), "queued again at once")

	a := createJob(t, svc, `{"properties":{},"lockResources":["r"]}`)
	pullOne(t, svc, a)
	require.NoError(t, svc.UnlockResource("r"))
	b := createJob(t, svc, `{"properties":{},"lockResources":["r"]}`)
	require.Equal(t, job.JobQueued, stateOf(t, svc, b))
	reachedMaxRetry, err := svc.FailJob(a)
	require.NoError(t, err)
	assert.False(t, reachedMaxRetry)
	assert.Equal(t, job.JobHidden, stateOf(t, svc, a))
	assert.Equal(t, b, svc.GetLockedResources()["r"])

	pullOne(t, svc, b)
	require.NoError(t, svc.SetJobAsSuccessful(b))
	svc.AttemptJobRetries()
	assert.Equal(t, job.JobQueued, stateOf(t, svc, a))
	assert.Equal(t, a, svc.GetLockedResources()["r"])
}

func TestDuplicateResourceNames(t *testing.T) {
	svc := idleService(t)
	a := createJob(t, svc, `{"properties":{},"lockResources":["r","r","s"]}`)
	assert.Equal(t, job.JobQueued, stateOf(t, svc, a))
	pullOne(t, svc, a)
	require.NoError(t, svc.SetJobAsSuccessful(a))
	assert.Empty(t, svc.GetLockedResources())
}

func TestRestoredLocksAreCountedOnce(t *testing.T) {
	// After a restart, the count of locked resources of a topic was the sum of the lists of
	// resources of its queued and running jobs: a resource claimed by two jobs (a running job
	// whose lock was released by the admin API, and the queued job that took it) or listed
	// twice was counted twice, so the count never came back to 0. The owner of a resource
	// claimed by two jobs changed from one start to the next (map order): it is the last job
	// in creation order.
	dir := t.TempDir()
	ids := sequentialIDs(3)
	now := time.Now().UnixMilli()
	stored := map[string]*job.Job{}
	add := func(id job.JobUUID, resources job.ResourceList, events ...string) {
		j := &job.Job{Topic: "t", JobProperties: job.JobProperties{}, LockResources: resources, VisibilityTimeout: 3600}
		j.Init(id, now)
		for _, e := range events {
			j.AddHistoryEvent(e, now)
		}
		stored[id.String()] = j
	}
	running := []string{job.JobEventPending, job.JobEventEnqueue, job.JobEventStart}
	add(ids[0], job.ResourceList{"r", "s"}, running...)
	add(ids[1], job.ResourceList{"r"}, job.JobEventPending, job.JobEventEnqueue)
	add(ids[2], job.ResourceList{"u", "u"}, running...)
	data, err := json.Marshal(map[string]interface{}{"jobs": stored})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "jobs.json"), data, 0o600))

	start := func() *Service {
		svc, err := CreateAndInitService(persistentConfig(t, dir), "test")
		require.NoError(t, err)
		return svc
	}
	lockedCount := func(svc *Service) uint {
		return svc.GetMetrics().GetMetricByTopic("t").ResourcesLockedCount
	}
	for i := 0; i < 10; i++ {
		svc := start()
		assert.Equal(t, job.LockedResources{"r": ids[1], "s": ids[0], "u": ids[2]}, svc.GetLockedResources())
		assert.Equal(t, uint(3), lockedCount(svc))
		require.NoError(t, svc.Stop())
	}

	svc := start()
	t.Cleanup(func() { _ = svc.Stop() })
	require.NoError(t, svc.SetJobAsSuccessful(ids[0]))
	assert.Equal(t, uint(2), lockedCount(svc), "s is released, r is held by another job")
	require.NoError(t, svc.SetJobAsSuccessful(ids[2]))
	pullOne(t, svc, ids[1])
	require.NoError(t, svc.SetJobAsSuccessful(ids[1]))
	assert.Empty(t, svc.GetLockedResources())
	assert.Equal(t, uint(0), lockedCount(svc))
}

func TestCreatedJobIsScheduledAfterLoad(t *testing.T) {
	// A job saved between its creation and its first scheduling (only a CREATE event).
	dir := t.TempDir()
	ids := sequentialIDs(2)
	now := time.Now().UnixMilli()
	ready := &job.Job{Topic: "t", JobProperties: job.JobProperties{}}
	ready.Init(ids[0], now)
	later := &job.Job{Topic: "t", JobProperties: job.JobProperties{}, StartAfter: now + 3_600_000}
	later.Init(ids[1], now)
	data, err := json.Marshal(map[string]interface{}{"jobs": map[string]*job.Job{
		ids[0].String(): ready,
		ids[1].String(): later,
	}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "jobs.json"), data, 0o600))

	svc, err := CreateAndInitService(persistentConfig(t, dir), "test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Stop() })
	assert.Equal(t, job.JobQueued, stateOf(t, svc, ids[0]))
	assert.Equal(t, job.JobDelayed, stateOf(t, svc, ids[1]))
}

func TestJobStatisticsPanicDoesNotStopTheService(t *testing.T) {
	// The job statistics run in the loop of the service, where nothing recovered a panic: a
	// topic that was not a valid Prometheus label stopped the server. The statistics only
	// feed the metrics: such a failure is logged.
	svc := idleService(t)
	createJob(t, svc, `{"topic":"a","properties":{}}`)
	svc.metrics.(*metrics.ServiceMetrics).AddTopic("\xff")
	assert.NotPanics(t, svc.UpdateJobStatistics)
	assert.NotPanics(t, svc.UpdateJobStatistics, "the locks are released")
}

func TestTopicsListedWithMetricsDisabled(t *testing.T) {
	dir := t.TempDir()
	conf := func() *config.Config {
		return testConfig(t, "    metrics:\n        enable: true\n", "    metrics:\n        enable: false\n",
			"        enablePersistentStorage: false\n",
			"        enablePersistentStorage: true\n        directory: \""+filepath.ToSlash(dir)+"\"\n        filename: \"jobs.json\"\n")
	}
	svc, err := CreateAndInitService(conf(), "test")
	require.NoError(t, err)
	createJob(t, svc, `{"topic":"a","properties":{}}`)
	createJob(t, svc, `{"topic":"b","properties":{},"lockResources":["r"]}`)
	assert.Equal(t, []string{"a", "b"}, svc.GetTopics())
	require.NoError(t, svc.Stop())

	svc, err = CreateAndInitService(conf(), "test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Stop() })
	assert.Equal(t, []string{"a", "b"}, svc.GetTopics(), "after a restart")
}

func TestBackendMaxJobsIsEnforced(t *testing.T) {
	svc, err := CreateAndInitService(testConfig(t, "        maxJobs: 10000000\n", "        maxJobs: 3\n"), "test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Stop() })
	var first job.JobUUID
	for i := 0; i < 3; i++ {
		id := createJob(t, svc, `{"properties":{}}`)
		if i == 0 {
			first = id
		}
	}
	_, err = svc.CreateJob([]byte(`{"properties":{}}`))
	var apiErr *apierror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, 503, apiErr.HttpCode)
	assert.Equal(t, constants.ErrorCantCreateJob, apiErr.Code)
	assert.Equal(t, "cannot create job", apiErr.Message)
	_, err = svc.CloneJob(first)
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, 503, apiErr.HttpCode)
	assert.Equal(t, uint(3), svc.GetJobsCount(), "no job is added beyond the limit")
}

func TestMaxAllowedJobsIsEnforcedOnClone(t *testing.T) {
	// Cloning a job created jobs beyond jobs.maxAllowedJobs.
	svc, err := CreateAndInitService(testConfig(t, "    maxAllowedJobs: 0 ", "    maxAllowedJobs: 2 "), "test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Stop() })
	first := createJob(t, svc, `{"properties":{}}`)
	_, err = svc.CloneJob(first)
	require.NoError(t, err)

	for _, create := range []func() error{
		func() error { _, err := svc.CreateJob([]byte(`{"properties":{}}`)); return err },
		func() error { _, err := svc.CloneJob(first); return err },
	} {
		var apiErr *apierror.APIError
		require.ErrorAs(t, create(), &apiErr)
		assert.Equal(t, 503, apiErr.HttpCode)
		assert.Equal(t, constants.ErrorCantCreateJob, apiErr.Code)
		assert.Equal(t, "cannot create job", apiErr.Message)
	}
	assert.Equal(t, uint(2), svc.GetJobsCount())

	// an unknown job is still reported as such
	var apiErr *apierror.APIError
	_, err = svc.CloneJob(job.JobUUID{})
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, constants.ErrorCantCloneJob, apiErr.Code)
}

func TestVisibilityTimeoutChangeIsSaved(t *testing.T) {
	dir := t.TempDir()
	svc, err := CreateAndInitService(persistentConfig(t, dir), "test")
	require.NoError(t, err)
	go func() { _ = svc.Run() }()
	id := createJob(t, svc, `{"properties":{}}`)
	pullOne(t, svc, id)
	require.NoError(t, svc.ChangeVisibilityTimeoutJob(id, 300))
	require.NoError(t, svc.Stop()) // final save

	data, err := os.ReadFile(filepath.Join(dir, "jobs.json"))
	require.NoError(t, err)
	var file struct {
		Jobs map[string]job.Job `json:"jobs"`
	}
	require.NoError(t, json.Unmarshal(data, &file))
	assert.Equal(t, uint(300), file.Jobs[id.String()].VisibilityTimeout)
}

func TestRedisBackendKeepsTheJobs(t *testing.T) {
	m := miniredis.RunT(t)
	redisBackend := "    type: \"redis\"\n    redis:\n        url: \"redis://" + m.Addr() + "/0\"\n" +
		"        maxJobs: 1000\n        finishedJobTTL: 60\n"
	conf := testConfig(t, "    type: \"inMemory\"\n", redisBackend)
	svc, err := CreateAndInitService(conf, "test")
	require.NoError(t, err)
	go func() { _ = svc.Run() }()
	done := createJob(t, svc, `{"properties":{"n":12345678901234567890}}`)
	pullOne(t, svc, done)
	require.NoError(t, svc.SetJobAsSuccessful(done))
	queued := createJob(t, svc, `{"properties":{}}`)
	// the answers are given once the changes are in Redis
	assert.Equal(t, 60*time.Second, m.TTL("job:"+done.String()))
	assert.True(t, m.Exists("job:"+queued.String()))
	require.NoError(t, svc.Stop())

	restarted, err := CreateAndInitService(conf, "test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = restarted.Stop() })
	assert.Equal(t, job.JobSucceeded, stateOf(t, restarted, done))
	assert.Equal(t, job.JobQueued, stateOf(t, restarted, queued))
	j, _ := restarted.GetJob(done)
	assert.Equal(t, uint64(12345678901234567890), j.JobProperties["n"])
}
