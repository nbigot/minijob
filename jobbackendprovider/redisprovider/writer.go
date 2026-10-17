package redisprovider

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	maxBatch     = 1024                   // operations sent in one pipeline
	minBackoff   = 100 * time.Millisecond // first delay before retrying failed writes
	maxBackoff   = 5 * time.Second        // longest delay between two retries
	flushTimeout = 5 * time.Second        // how long the stop waits for the pending writes
)

// writeOp is one change to save: a job to set or delete, or every job to delete.
type writeOp struct {
	key    string        // key of the job
	value  string        // job JSON
	ttl    time.Duration // expiration of the key (0: none)
	del    bool          // deletes the key
	delAll bool          // deletes every job key
}

// writer saves the changes to Redis in the order they were made, in pipelines, outside of
// the service lock. A write that fails is retried in the background (the job is kept in
// memory), and the provider is reported unhealthy until it succeeds.
type writer struct {
	logger  *zap.Logger
	client  *redis.Client
	mu      sync.Mutex
	cond    *sync.Cond // signaled when operations are queued or attempted, or on stop
	queue   []writeOp
	pushed  uint64 // operations queued since the start
	done    uint64 // operations attempted at least once
	running bool
	closed  bool          // no more operations are accepted (stop)
	exited  chan struct{} // closed when the loop ends
	abandon chan struct{} // closed when the stop does not wait any longer
	ok      atomic.Bool   // false while failed writes are waiting to be retried
}

func newWriter(logger *zap.Logger, client *redis.Client) *writer {
	w := &writer{
		logger:  logger,
		client:  client,
		exited:  make(chan struct{}),
		abandon: make(chan struct{}),
	}
	w.cond = sync.NewCond(&w.mu)
	w.ok.Store(true)
	return w
}

// start runs the loop (once).
func (w *writer) start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running || w.closed {
		return
	}
	w.running = true
	go w.loop()
}

// push queues an operation; it is dropped after the stop.
func (w *writer) push(op writeOp) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.queue = append(w.queue, op)
	w.pushed++
	w.cond.Broadcast()
}

// wait returns once the operations queued before the call were attempted once.
func (w *writer) wait() {
	w.mu.Lock()
	defer w.mu.Unlock()
	target := w.pushed
	for w.running && w.done < target && !isClosed(w.exited) {
		w.cond.Wait()
	}
}

// healthy is false while failed writes are waiting to be retried.
func (w *writer) healthy() bool {
	return w.ok.Load()
}

// stop refuses new operations and waits (at most flushTimeout) for the queued ones.
func (w *writer) stop() {
	w.mu.Lock()
	w.closed = true
	running := w.running
	w.cond.Broadcast()
	w.mu.Unlock()
	if !running {
		return
	}
	select {
	case <-w.exited:
	case <-time.After(flushTimeout):
		w.logger.Error(
			"Pending Redis writes not flushed at shutdown",
			zap.String("topic", "backendProvider"),
			zap.String("method", "Stop"),
		)
		close(w.abandon)
	}
}

func (w *writer) loop() {
	defer func() {
		w.mu.Lock()
		close(w.exited)
		w.cond.Broadcast()
		w.mu.Unlock()
	}()
	var retry []writeOp
	backoff := minBackoff
	for {
		w.mu.Lock()
		for len(w.queue) == 0 && len(retry) == 0 && !w.closed {
			w.cond.Wait()
		}
		n := min(len(w.queue), maxBatch)
		batch := w.queue[:n:n]
		w.queue = w.queue[n:]
		attempted := w.done + uint64(n)
		w.mu.Unlock()
		if n == 0 && len(retry) == 0 {
			return // stopped, nothing left to write
		}

		ops := append(retry, batch...)
		err := w.execute(ops)
		// the requests waiting for these writes are released after the first attempt
		w.mu.Lock()
		w.done = attempted
		w.cond.Broadcast()
		w.mu.Unlock()

		if err == nil {
			retry = nil
			backoff = minBackoff
			w.ok.Store(true)
			continue
		}
		w.logger.Error(
			"Error saving job to Redis",
			zap.String("topic", "backendProvider"),
			zap.String("method", "SaveJob"),
			zap.Error(err),
		)
		w.ok.Store(false)
		retry = coalesce(ops)
		select {
		case <-time.After(backoff):
		case <-w.abandon:
			return
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

// execute sends the operations in order (SET and DEL in pipelines).
func (w *writer) execute(ops []writeOp) error {
	ctx := context.Background()
	pipe := w.client.Pipeline()
	flush := func() error {
		if pipe.Len() == 0 {
			return nil
		}
		_, err := pipe.Exec(ctx)
		return err
	}
	for _, op := range ops {
		switch {
		case op.delAll:
			if err := flush(); err != nil {
				return err
			}
			if err := deleteAllJobs(ctx, w.client); err != nil {
				return err
			}
		case op.del:
			pipe.Del(ctx, op.key)
		default:
			pipe.Set(ctx, op.key, op.value, op.ttl)
		}
	}
	return flush()
}

// deleteAllJobs deletes every job key (the other keys of the database are kept).
func deleteAllJobs(ctx context.Context, client *redis.Client) error {
	var cursor uint64
	for {
		keys, next, err := client.Scan(ctx, cursor, jobKeyPattern, scanCount).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		if next == 0 {
			return nil
		}
		cursor = next
	}
}

// coalesce keeps the last operation of each key, in order (a deletion of every job
// replaces the operations before it). The writes are retried this way after a failure.
func coalesce(ops []writeOp) []writeOp {
	var out []writeOp
	for _, op := range ops {
		if op.delAll {
			out = out[:0]
		} else {
			kept := out[:0]
			for _, o := range out {
				if o.delAll || o.key != op.key {
					kept = append(kept, o)
				}
			}
			out = kept
		}
		out = append(out, op)
	}
	return out
}

func isClosed(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}
