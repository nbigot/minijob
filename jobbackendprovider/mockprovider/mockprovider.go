package mockprovider

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nbigot/minijob/config"
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/jobbackendprovider"
	"go.uber.org/zap"
)

type MockJobBackendProvider struct {
	// implements IJobBackendProvider interface
	logger         *zap.Logger                   // logger is the logger
	logVerbosity   int                           // logVerbosity is the log verbosity level
	mu             sync.Mutex                    // mu is a mutex to protect the jobs hashmap
	hasChanged     bool                          // hasChanged is a flag to indicate if at least on job has changed
	jobs           job.JobMap                    // hashmap of jobs
	maxJobs        uint                          // maxJobs is the maximum number of jobs that can be stored in the backend
	writeFrequency int                           // writeFrequency is the frequency in seconds to write the jobs to the storage
	running        atomic.Bool                   // Add this to track if the provider is running
	wg             sync.WaitGroup                // wg is a wait group to wait for the Run function to finish
	stopChan       chan bool                     // stopChan is a channel to stop the Run function
	notifChan      chan jobbackendprovider.Event // notifChan is a channel to send notifications to the service
}

func (p *MockJobBackendProvider) Init(notifChan chan jobbackendprovider.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.notifChan = notifChan
	return nil
}

func (p *MockJobBackendProvider) Stop() error {
	if !p.running.Load() {
		return errors.New("provider is not running")
	}

	// send stop notification
	p.stopChan <- true
	// wait for the Run function to finish
	p.wg.Wait()
	return nil
}

func (p *MockJobBackendProvider) JobExists(jobUUID job.JobUUID) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, ok := p.jobs[jobUUID]
	if ok {
		return true, nil
	}
	return false, nil
}

func (p *MockJobBackendProvider) LoadJobs() (job.JobMap, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.hasChanged = false
	p.jobs = make(job.JobMap)
	// make a copy of the jobs (even if it's empty)
	// this is to prevent the caller from modifying the jobs hashmap
	jobsCopy := make(job.JobMap)
	return jobsCopy, nil
}

func (p *MockJobBackendProvider) OnJobCreated(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if uint(len(p.jobs)) == p.maxJobs {
		return fmt.Errorf("cannot create job, max limit reached: %d", p.maxJobs)
	}

	p.jobs[j.JobUUID] = j
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobCreated, JobUUID: j.JobUUID})
	return nil
}

func (p *MockJobBackendProvider) OnJobEnqueued(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobEnqueued, JobUUID: j.JobUUID})
	return nil
}

func (p *MockJobBackendProvider) OnJobDeleted(jobUUID job.JobUUID) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.jobs, jobUUID)
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobDeleted, JobUUID: jobUUID})
	return nil
}

func (p *MockJobBackendProvider) OnJobStarted(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobStarted, JobUUID: j.JobUUID})
	return nil
}

func (p *MockJobBackendProvider) OnJobSucceeded(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobSucceeded, JobUUID: j.JobUUID})
	return nil
}

func (p *MockJobBackendProvider) OnJobTimeout(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobTimeout, JobUUID: j.JobUUID})
	return nil
}

func (p *MockJobBackendProvider) OnJobsDeleted() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.jobs = make(job.JobMap)
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventAllJobsDeleted})
	return nil
}

func (p *MockJobBackendProvider) OnAllResourcesUnlocked() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventAllResourcesUnlocked})
	return nil
}

func (p *MockJobBackendProvider) OnResourceUnlocked(j *job.Job, resource string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventResourceUnlocked, JobUUID: j.JobUUID, Resource: &resource})
	return nil
}

func (p *MockJobBackendProvider) NotifyChange(event jobbackendprovider.Event) {
	if p.logVerbosity >= 2 {
		p.logger.Debug(
			"notify event",
			zap.String("topic", "backendProvider"),
			zap.String("method", "NotifyChange"),
			zap.Any("event", event),
		)
	}

	p.hasChanged = true
	p.notifChan <- event
}

func (p *MockJobBackendProvider) Run() error {
	if !p.running.CompareAndSwap(false, true) {
		return errors.New("provider is already running")
	}
	defer p.running.Store(false)

	// this function must be called in a goroutine
	p.wg.Add(1)

	// create a ticker to save the jobs to the storage
	ticker := time.NewTicker(time.Duration(p.writeFrequency) * time.Second)

	for {
		select {
		case <-ticker.C:
			// save jobs to storage every writeFrequency seconds
			_ = p.sync()
		case <-p.stopChan:
			// save jobs to storage before exiting
			ticker.Stop()
			close(p.notifChan)
			err := p.sync()
			p.wg.Done()
			return err
		}
	}
}

func (p *MockJobBackendProvider) sync() error {
	if p.hasChanged {
		p.hasChanged = false
	}

	return nil
}

func (p *MockJobBackendProvider) Healthcheck() bool {
	// assume the in memory backend is always healthy
	return true
}

func NewMockJobBackendProvider(logger *zap.Logger, conf *config.Config) (jobbackendprovider.IJobBackendProvider, error) {
	return &MockJobBackendProvider{
		logger:         logger,
		logVerbosity:   4,
		maxJobs:        100,
		writeFrequency: 10,
		hasChanged:     false,
		stopChan:       make(chan bool, 1),
	}, nil
}
