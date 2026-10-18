package service

import (
	"container/heap"

	"github.com/nbigot/minijob/job"
)

// jobHeap is a binary heap of jobs in pull order (compareQueueOrder) that can also remove
// any job: it keeps the position of each job.
type jobHeap struct {
	jobs []*job.Job
	pos  map[*job.Job]int
}

func newJobHeap() *jobHeap {
	return &jobHeap{pos: make(map[*job.Job]int)}
}

func (h *jobHeap) Len() int           { return len(h.jobs) }
func (h *jobHeap) Less(a, b int) bool { return compareQueueOrder(h.jobs[a], h.jobs[b]) < 0 }

func (h *jobHeap) Swap(a, b int) {
	h.jobs[a], h.jobs[b] = h.jobs[b], h.jobs[a]
	h.pos[h.jobs[a]] = a
	h.pos[h.jobs[b]] = b
}

func (h *jobHeap) Push(x any) {
	j := x.(*job.Job)
	h.pos[j] = len(h.jobs)
	h.jobs = append(h.jobs, j)
}

func (h *jobHeap) Pop() any {
	last := len(h.jobs) - 1
	j := h.jobs[last]
	h.jobs[last] = nil
	h.jobs = h.jobs[:last]
	delete(h.pos, j)
	return j
}

// queuedJobs indexes the queued jobs in pull order, for all topics and for each topic, so
// that a pull does not go through every job. The service keeps it in sync with the job
// states (addHistoryEventLocked).
type queuedJobs struct {
	all     *jobHeap
	byTopic map[string]*jobHeap
}

func newQueuedJobs() *queuedJobs {
	return &queuedJobs{all: newJobHeap(), byTopic: make(map[string]*jobHeap)}
}

func (q *queuedJobs) add(j *job.Job) {
	if _, found := q.all.pos[j]; found {
		return
	}
	heap.Push(q.all, j)
	h := q.byTopic[j.Topic]
	if h == nil {
		h = newJobHeap()
		q.byTopic[j.Topic] = h
	}
	heap.Push(h, j)
}

func (q *queuedJobs) remove(j *job.Job) {
	i, found := q.all.pos[j]
	if !found {
		return
	}
	heap.Remove(q.all, i)
	if h := q.byTopic[j.Topic]; h != nil {
		if i, found := h.pos[j]; found {
			heap.Remove(h, i)
		}
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
	jobs := make([]*job.Job, 0, min(n, uint(h.Len())))
	for uint(len(jobs)) < n && h.Len() > 0 {
		jobs = append(jobs, heap.Pop(h).(*job.Job))
	}
	for _, j := range jobs {
		heap.Push(h, j)
	}
	return jobs
}
