package mockprovider

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nbigot/minijob/config"
	"github.com/nbigot/minijob/event"
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/jobbackendprovider"
	"go.uber.org/zap"
)

type MockJobBackendProvider struct {
	// implements IJobBackendProvider & IServiceEventObserver interfaces
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
	restoreFlag    bool                          // restoreFlag is a flag to indicate if the provider is in restore mode
}

func (p *MockJobBackendProvider) SetNotifChan(notifChan chan jobbackendprovider.Event) {
	p.notifChan = notifChan
}

func (p *MockJobBackendProvider) Init() error {
	return nil
}

func (p *MockJobBackendProvider) Shutdown() {
	p.Stop()
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

func (p *MockJobBackendProvider) SetRestoreFlag(enabled bool) {
	p.restoreFlag = enabled
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

func (p *MockJobBackendProvider) OnJobDelayed(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobDelayed, JobUUID: j.JobUUID})
	return nil
}

func (p *MockJobBackendProvider) OnJobPending(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobPending, JobUUID: j.JobUUID})
	return nil
}

func (p *MockJobBackendProvider) OnJobEnqueued(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobEnqueued, JobUUID: j.JobUUID})
	return nil
}

func (p *MockJobBackendProvider) OnJobCanceled(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobCanceled, JobUUID: j.JobUUID})
	return nil
}

func (p *MockJobBackendProvider) OnJobFailed(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobFailed, JobUUID: j.JobUUID})
	return nil
}

func (p *MockJobBackendProvider) OnJobHidden(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobHidden, JobUUID: j.JobUUID})
	return nil
}

func (p *MockJobBackendProvider) OnJobTerminated(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobTerminated, JobUUID: j.JobUUID})
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

func (p *MockJobBackendProvider) NotifyEvent(ev event.ServiceEventType) {
	if p.restoreFlag {
		return
	}

	switch ev {
	case event.ServiceEventJobDeletedAll:
		p.OnJobsDeleted()
	}
}

func (p *MockJobBackendProvider) NotifyTopicEvent(ev event.ServiceEventType, topic string) {
}

func (p *MockJobBackendProvider) NotifyJobEvent(j *job.Job, ev event.ServiceEventType) {
	if p.restoreFlag {
		return
	}

	switch ev {
	case event.ServiceEventJobCreated:
		p.OnJobCreated(j)
	case event.ServiceEventJobDelayed:
		p.OnJobDelayed(j)
	case event.ServiceEventJobPending:
		p.OnJobPending(j)
	case event.ServiceEventJobEnqueued:
		p.OnJobEnqueued(j)
	case event.ServiceEventJobDeleted:
		p.OnJobDeleted(j.JobUUID)
	case event.ServiceEventJobStarted:
		p.OnJobStarted(j)
	case event.ServiceEventJobSucceeded:
		p.OnJobSucceeded(j)
	case event.ServiceEventJobCanceled:
		p.OnJobCanceled(j)
	case event.ServiceEventJobFailed:
		p.OnJobFailed(j)
	case event.ServiceEventJobHidden:
		p.OnJobHidden(j)
	case event.ServiceEventJobTerminated:
		p.OnJobTerminated(j)
	}
}

func NewMockJobBackendProvider(logger *zap.Logger, conf *config.Config) (jobbackendprovider.IJobBackendProvider, error) {
	return &MockJobBackendProvider{
		logger:         logger,
		logVerbosity:   4,
		maxJobs:        100,
		writeFrequency: 10,
		hasChanged:     false,
		stopChan:       make(chan bool, 1),
		restoreFlag:    false,
	}, nil
}
