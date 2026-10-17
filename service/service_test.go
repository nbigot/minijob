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
	"testing"

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
