package service

import (
	"bytes"
	"cmp"
	"container/heap"

	"github.com/nbigot/minijob/job"
)

// queueEntry is a queued job in the index: its sort keys, read once, and its position in
// the heap of all topics (pos[0]) and in the heap of its topic (pos[1]).
type queueEntry struct {
	job      *job.Job
	priority int8
	created  int64
	pos      [2]int
}

// compareQueueEntries is compareQueueOrder on the keys of the entries.
func compareQueueEntries(a, b *queueEntry) int {
	if a.priority != b.priority {
		return cmp.Compare(b.priority, a.priority)
	}
	if a.created != b.created {
		return cmp.Compare(a.created, b.created)
	}
	return bytes.Compare(a.job.JobUUID[:], b.job.JobUUID[:])
}

// jobHeap is a binary heap of queue entries in pull order; each entry keeps its position
// (pos[which]), so that any entry can be removed.
type jobHeap struct {
	entries []*queueEntry
	which   int
}

func (h *jobHeap) Len() int { return len(h.entries) }
func (h *jobHeap) Less(a, b int) bool {
	return compareQueueEntries(h.entries[a], h.entries[b]) < 0
}

func (h *jobHeap) Swap(a, b int) {
	h.entries[a], h.entries[b] = h.entries[b], h.entries[a]
	h.entries[a].pos[h.which] = a
	h.entries[b].pos[h.which] = b
}

func (h *jobHeap) Push(x any) {
	e := x.(*queueEntry)
	e.pos[h.which] = len(h.entries)
	h.entries = append(h.entries, e)
}

func (h *jobHeap) Pop() any {
	last := len(h.entries) - 1
	e := h.entries[last]
	h.entries[last] = nil
	h.entries = h.entries[:last]
	return e
}

// queuedJobs indexes the queued jobs in pull order, for all topics and for each topic, so
// that a pull does not go through every job. The service keeps it in sync with the job
// states (addHistoryEventLocked).
type queuedJobs struct {
	entries map[*job.Job]*queueEntry
	all     *jobHeap
	byTopic map[string]*jobHeap
}

func newQueuedJobs() *queuedJobs {
	return &queuedJobs{
		entries: make(map[*job.Job]*queueEntry),
		all:     &jobHeap{which: 0},
		byTopic: make(map[string]*jobHeap),
	}
}

func (q *queuedJobs) add(j *job.Job) {
	if _, found := q.entries[j]; found {
		return
	}
	e := &queueEntry{job: j, priority: j.Priority, created: j.GetCreationTimestamp()}
	q.entries[j] = e
	heap.Push(q.all, e)
	h := q.byTopic[j.Topic]
	if h == nil {
		h = &jobHeap{which: 1}
		q.byTopic[j.Topic] = h
	}
	heap.Push(h, e)
}

func (q *queuedJobs) remove(j *job.Job) {
	e, found := q.entries[j]
	if !found {
		return
	}
	delete(q.entries, j)
	heap.Remove(q.all, e.pos[0])
	if h := q.byTopic[j.Topic]; h != nil {
		heap.Remove(h, e.pos[1])
		if h.Len() == 0 {
			delete(q.byTopic, j.Topic)
		}
	}
}

// first returns up to n queued jobs of a topic ("*": any topic), in pull order, and leaves
// them queued.
func (q *queuedJobs) first(topic string, n uint) []*job.Job {
	h := q.all
	if topic != "*" {
		if h = q.byTopic[topic]; h == nil {
			return nil
		}
	}
	entries := make([]*queueEntry, 0, min(n, uint(h.Len())))
	for uint(len(entries)) < n && h.Len() > 0 {
		entries = append(entries, heap.Pop(h).(*queueEntry))
	}
	jobs := make([]*job.Job, len(entries))
	for i, e := range entries {
		heap.Push(h, e)
		jobs[i] = e.job
	}
	return jobs
}
