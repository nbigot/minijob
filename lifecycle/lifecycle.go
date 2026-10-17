// Package lifecycle starts and stops a background loop exactly once.
//
// The loop calls Begin when it starts and End when it returns; Stop asks it to stop and
// waits for it. Begin and Stop are ordered by a mutex, so a Stop that arrives before the
// loop has started prevents it from starting (and tells the caller, which can then do the
// final work itself) instead of returning while the loop is about to run.
package lifecycle

import "sync"

type Lifecycle struct {
	mu       sync.Mutex
	started  bool
	stopped  bool
	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
}

func New() *Lifecycle {
	return &Lifecycle{stop: make(chan struct{}), done: make(chan struct{})}
}

// Begin is called by the loop when it starts. It returns false when the loop must not run:
// it already ran, or Stop was called before it started.
func (l *Lifecycle) Begin() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.started || l.stopped {
		return false
	}
	l.started = true
	return true
}

// End is called by the loop when it returns (only after a successful Begin).
func (l *Lifecycle) End() {
	close(l.done)
}

// Stopping is closed when Stop is called: the loop must then return.
func (l *Lifecycle) Stopping() <-chan struct{} {
	return l.stop
}

// Stop asks the loop to stop and waits until it has returned. It reports whether the loop
// had started; it can be called several times.
func (l *Lifecycle) Stop() (started bool) {
	l.mu.Lock()
	l.stopped = true
	started = l.started
	l.mu.Unlock()
	l.stopOnce.Do(func() { close(l.stop) })
	if started {
		<-l.done
	}
	return started
}
