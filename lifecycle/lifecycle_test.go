package lifecycle

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func loop(l *Lifecycle, ran *bool) {
	if !l.Begin() {
		return
	}
	defer l.End()
	*ran = true
	<-l.Stopping()
	time.Sleep(10 * time.Millisecond) // final work
}

func TestStopWaitsForTheLoop(t *testing.T) {
	l := New()
	var ran bool
	go loop(l, &ran)
	assert.Eventually(t, func() bool {
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.started
	}, time.Second, time.Millisecond)
	assert.True(t, l.Stop())
	assert.True(t, ran)
	assert.True(t, l.Stop(), "a second Stop returns at once")
}

func TestStopBeforeStart(t *testing.T) {
	l := New()
	assert.False(t, l.Stop(), "the loop never started")
	var ran bool
	loop(l, &ran)
	assert.False(t, ran, "the loop does not start after Stop")
}

func TestBeginOnlyOnce(t *testing.T) {
	l := New()
	assert.True(t, l.Begin())
	assert.False(t, l.Begin())
	l.End()
	assert.True(t, l.Stop())
}

func TestConcurrentStartAndStop(t *testing.T) {
	for i := 0; i < 200; i++ {
		l := New()
		var ran bool
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			loop(l, &ran)
		}()
		started := l.Stop()
		wg.Wait()
		assert.Equal(t, started, ran)
	}
}
