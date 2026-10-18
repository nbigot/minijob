package service

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/nbigot/minijob/job"
	"github.com/stretchr/testify/require"
)

// checkQueuedJobs fails when the index of the queued jobs differs from the job states.
func checkQueuedJobs(t *testing.T, svc *Service) {
	t.Helper()
	svc.mu.RLock()
	defer svc.mu.RUnlock()
	want := map[string][]*job.Job{}
	all := []*job.Job{}
	for _, j := range svc.jobs {
		if j.GetState() == job.JobQueued {
			want[j.Topic] = append(want[j.Topic], j)
			all = append(all, j)
		}
	}
	sorted := func(jobs []*job.Job) []*job.Job {
		slices.SortFunc(jobs, compareQueueOrder)
		return jobs
	}
	require.Equal(t, sorted(all), svc.queuedJobs.first("*", uint(len(svc.jobs)+1)))
	require.Len(t, svc.queuedJobs.byTopic, len(want))
	for topic, jobs := range want {
		require.Equal(t, sorted(jobs), svc.queuedJobs.first(topic, uint(len(jobs)+1)), topic)
	}
}

func TestQueuedJobsFollowTheJobStates(t *testing.T) {
	svc := idleService(t)
	r := rand.New(rand.NewPCG(1, 2))
	var ids []job.JobUUID
	pick := func() job.JobUUID { return ids[r.IntN(len(ids))] }
	topics := []string{"a", "b", ""}
	for step := range 3000 {
		switch op := r.IntN(10); {
		case op < 3 || len(ids) == 0:
			j, err := svc.CreateJob(fmt.Appendf(nil, `{"topic":"%s","priority":%d,"properties":{}}`,
				topics[r.IntN(len(topics))], r.IntN(4)))
			require.NoError(t, err)
			ids = append(ids, j.JobUUID)
		case op < 5:
			topic := append(topics, "*")[r.IntN(len(topics)+1)]
			_, _ = svc.PullJobs(&RequestPullJobs{Topic: topic, NumJobs: uint(1 + r.IntN(3)), VisibilityTimeout: 60})
		case op == 5:
			_ = svc.SetJobAsSuccessful(pick())
		case op == 6:
			_, _ = svc.FailJob(pick()) // retried later (hidden), or failed for good
		case op == 7:
			_ = svc.CancelJob(pick()) // queued again
		case op == 8:
			if r.IntN(4) == 0 {
				_ = svc.DeleteJob(pick())
			} else {
				_, _ = svc.CloneJob(pick())
			}
		default:
			time.Sleep(3 * time.Millisecond) // retry delay of the template: 2 ms
			svc.AttemptJobRetries()
		}
		if step%50 == 0 {
			checkQueuedJobs(t, svc)
		}
	}
	checkQueuedJobs(t, svc)
	require.NoError(t, svc.DeleteAllJobs())
	checkQueuedJobs(t, svc)
}

func TestQueuedJobsAfterLoad(t *testing.T) {
	dir := t.TempDir()
	svc, err := CreateAndInitService(persistentConfig(t, dir), "test")
	require.NoError(t, err)
	for i := range 20 {
		createJob(t, svc, fmt.Sprintf(`{"topic":"t%d","priority":%d,"properties":{}}`, i%3, i%4))
	}
	_, err = svc.PullJobs(&RequestPullJobs{Topic: "*", NumJobs: 5, VisibilityTimeout: 60})
	require.NoError(t, err)
	require.NoError(t, svc.Stop())

	svc, err = CreateAndInitService(persistentConfig(t, dir), "test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Stop() })
	checkQueuedJobs(t, svc)
	require.Equal(t, 15, svc.queuedJobs.all.Len())
}

// BenchmarkPullCandidates chooses the next 100 jobs to pull among 100,000 queued jobs.
func BenchmarkPullCandidates(b *testing.B) {
	svc, err := CreateAndInitService(testConfig(&testing.T{}), "test")
	require.NoError(b, err)
	defer func() { _ = svc.Stop() }()
	for i := range 100_000 {
		_, err := svc.CreateJob(fmt.Appendf(nil, `{"topic":"t%d","priority":%d,"properties":{}}`, i%2, i%10))
		require.NoError(b, err)
	}
	for _, topic := range []string{"*", "t1"} {
		b.Run("topic="+topic, func(b *testing.B) {
			req := &RequestPullJobs{Topic: topic, NumJobs: 100}
			svc.mu.Lock()
			defer svc.mu.Unlock()
			for b.Loop() {
				jobs, err := svc.findBestJobCandidatesLocked(req)
				if err != nil || len(jobs) != 100 {
					b.Fatal(len(jobs), err)
				}
			}
		})
	}
}
