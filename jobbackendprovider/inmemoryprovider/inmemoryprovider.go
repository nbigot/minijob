package inmemoryprovider

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

type InMemoryJobBackendProvider struct {
	// implements IJobBackendProvider interface
	logger                  *zap.Logger                   // logger is the logger
	logVerbosity            int                           // logVerbosity is the log verbosity level
	mu                      sync.Mutex                    // mu is a mutex to protect the jobs hashmap
	hasChanged              bool                          // hasChanged is a flag to indicate if at least on job has changed
	jobs                    job.JobMap                    // hashmap of jobs
	maxJobs                 uint                          // maxJobs is the maximum number of jobs that can be stored in the backend
	writeFrequency          int                           // writeFrequency is the frequency in seconds to write the jobs to the storage
	enablePersistantStorage bool                          // enablePersistantStorage is a flag to enable persistant storage
	storage                 *DBFileStorage                // storage is the storage to save the jobs
	running                 atomic.Bool                   // Add this to track if the provider is running
	wg                      sync.WaitGroup                // wg is a wait group to wait for the Run function to finish
	stopChan                chan bool                     // stopChan is a channel to stop the Run function
	notifChan               chan jobbackendprovider.Event // notifChan is a channel to send notifications to the service
}

func (p *InMemoryJobBackendProvider) Init(notifChan chan jobbackendprovider.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.storage.Init(); err != nil {
		return err
	}
	p.notifChan = notifChan
	return nil
}

func (p *InMemoryJobBackendProvider) Stop() error {
	if !p.running.Load() {
		return errors.New("provider is not running")
	}

	// send stop notification
	p.stopChan <- true
	// wait for the Run function to finish
	p.wg.Wait()
	return nil
}

func (p *InMemoryJobBackendProvider) JobExists(jobUUID job.JobUUID) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, ok := p.jobs[jobUUID]
	if ok {
		return true, nil
	}
	return false, nil
}

func (p *InMemoryJobBackendProvider) LoadJobs() (job.JobMap, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.enablePersistantStorage {
		var err error
		p.jobs, err = p.storage.Load()
		if err != nil {
			return nil, err
		}
		// make a copy of the jobs
		// this is to prevent the caller from modifying the jobs hashmap
		jobsCopy := make(job.JobMap)
		for k, v := range p.jobs {
			jobsCopy[k] = v
		}
		p.hasChanged = false
		return jobsCopy, nil
	}

	p.hasChanged = false
	p.jobs = make(job.JobMap)
	// make a copy of the jobs (even if it's empty)
	// this is to prevent the caller from modifying the jobs hashmap
	jobsCopy := make(job.JobMap)
	return jobsCopy, nil
}

func (p *InMemoryJobBackendProvider) SaveToFile() error {
	if !p.enablePersistantStorage {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.storage.Save(p.jobs)
}

func (p *InMemoryJobBackendProvider) OnJobCreated(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if uint(len(p.jobs)) == p.maxJobs {
		return fmt.Errorf("cannot create job, max limit reached: %d", p.maxJobs)
	}

	p.jobs[j.JobUUID] = j
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobCreated, JobUUID: j.JobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobEnqueued(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.jobs[j.JobUUID] = j
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobEnqueued, JobUUID: j.JobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobCanceled(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobCanceled, JobUUID: j.JobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobDeleted(jobUUID job.JobUUID) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.jobs, jobUUID)
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobDeleted, JobUUID: jobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobStarted(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobStarted, JobUUID: j.JobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobSucceeded(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobSucceeded, JobUUID: j.JobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobTimeout(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobTimeout, JobUUID: j.JobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobsDeleted() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.jobs = make(job.JobMap)
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventAllJobsDeleted})
	return nil
}

func (p *InMemoryJobBackendProvider) OnAllResourcesUnlocked() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventAllResourcesUnlocked})
	return nil
}

func (p *InMemoryJobBackendProvider) OnResourceUnlocked(j *job.Job, resource string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventResourceUnlocked, JobUUID: j.JobUUID, Resource: &resource})
	return nil
}

func (p *InMemoryJobBackendProvider) NotifyChange(event jobbackendprovider.Event) {
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

func (p *InMemoryJobBackendProvider) Run() error {
	if !p.running.CompareAndSwap(false, true) {
		// send an error if the provider is already running into p.notifChan for the service to handle it
		p.notifChan <- jobbackendprovider.Event{Type: jobbackendprovider.EventFatalError}
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

func (p *InMemoryJobBackendProvider) sync() error {
	if p.hasChanged {
		if err := p.SaveToFile(); err != nil {
			return err
		}
		p.hasChanged = false
	}

	return nil
}

func (p *InMemoryJobBackendProvider) Healthcheck() bool {
	// assume the in memory backend is always healthy
	return true
}

func NewInMemoryJobBackendProvider(logger *zap.Logger, conf *config.Config) (jobbackendprovider.IJobBackendProvider, error) {
	if conf.Backend.InMemory.MaxJobs == 0 {
		return nil, fmt.Errorf("invalid value for configuration backend.inMemory.maxJobs: %d", conf.Backend.InMemory.MaxJobs)
	}

	writeFrequency := conf.Backend.InMemory.WriteFrequency
	if writeFrequency == 0 {
		writeFrequency = 300 // default value in seconds is 5 minutes
	}

	return &InMemoryJobBackendProvider{
		logger:                  logger,
		logVerbosity:            conf.Backend.LogVerbosity,
		maxJobs:                 conf.Backend.InMemory.MaxJobs,
		writeFrequency:          writeFrequency,
		enablePersistantStorage: conf.Backend.InMemory.EnablePersistantStorage,
		storage:                 NewDBFileStorage(logger, conf.Backend.InMemory.Directory, conf.Backend.InMemory.Filename),
		hasChanged:              false,
		stopChan:                make(chan bool, 1),
	}, nil
}
