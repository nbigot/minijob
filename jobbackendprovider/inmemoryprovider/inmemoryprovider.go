package inmemoryprovider

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

type InMemoryJobBackendProvider struct {
	// implements IJobBackendProvider & IServiceEventObserver interfaces
	logger                  *zap.Logger                   // logger is the logger
	logVerbosity            int                           // logVerbosity is the log verbosity level
	mu                      sync.Mutex                    // mu is a mutex to protect the jobs hashmap
	mapMutex                sync.RWMutex                  // mutex to protect hashmap of jobs
	hasChanged              atomic.Bool                   // hasChanged is a flag to indicate if at least on job has changed
	jobs                    job.JobMap                    // hashmap of jobs
	maxJobs                 uint                          // maxJobs is the maximum number of jobs that can be stored in the backend
	writeFrequency          int                           // writeFrequency is the frequency in seconds to write the jobs to the storage
	enablePersistentStorage bool                          // enablePersistentStorage is a flag to enable persistent storage
	storage                 *DBFileStorage                // storage is the storage to save the jobs
	running                 atomic.Bool                   // Add this to track if the provider is running
	wg                      sync.WaitGroup                // wg is a wait group to wait for the Run function to finish
	stopChan                chan bool                     // stopChan is a channel to stop the Run function
	notifChan               chan jobbackendprovider.Event // notifChan is a channel to send notifications to the service
	restoreFlag             bool                          // restoreFlag is a flag to indicate if the provider is in restore mode
}

func (p *InMemoryJobBackendProvider) SetNotifChan(notifChan chan jobbackendprovider.Event) {
	p.notifChan = notifChan
}

func (p *InMemoryJobBackendProvider) Init() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.storage.Init(); err != nil {
		return err
	}
	return nil
}

func (p *InMemoryJobBackendProvider) Shutdown() {
	_ = p.Stop() // Ignore error during shutdown
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

func (p *InMemoryJobBackendProvider) SetRestoreFlag(enabled bool) {
	p.restoreFlag = enabled
}

func (p *InMemoryJobBackendProvider) JobExists(jobUUID job.JobUUID) (bool, error) {
	p.mapMutex.RLock()
	_, ok := p.jobs[jobUUID]
	p.mapMutex.RUnlock()
	if ok {
		return true, nil
	}
	return false, nil
}

func (p *InMemoryJobBackendProvider) LoadJobs() (job.JobMap, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.enablePersistentStorage {
		var err error
		p.mapMutex.Lock()
		p.jobs, err = p.storage.Load()
		p.mapMutex.Unlock()
		if err != nil {
			return nil, err
		}
		// make a copy of the jobs
		// this is to prevent the caller from modifying the jobs hashmap
		jobsCopy := make(job.JobMap)
		p.mapMutex.RLock()
		for k, v := range p.jobs {
			jobsCopy[k] = v
		}
		p.mapMutex.RUnlock()
		p.hasChanged.Store(false)
		return jobsCopy, nil
	}

	p.hasChanged.Store(false)
	p.mapMutex.Lock()
	p.jobs = make(job.JobMap)
	p.mapMutex.Unlock()
	// make a copy of the jobs (even if it's empty)
	// this is to prevent the caller from modifying the jobs hashmap
	jobsCopy := make(job.JobMap)
	return jobsCopy, nil
}

func (p *InMemoryJobBackendProvider) SaveToFile() error {
	if !p.enablePersistentStorage {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.mapMutex.RLock()
	defer p.mapMutex.RUnlock()
	return p.storage.Save(p.jobs)
}

func (p *InMemoryJobBackendProvider) OnJobCreated(j *job.Job) error {
	p.mapMutex.Lock()
	if uint(len(p.jobs)) == p.maxJobs {
		p.mapMutex.Unlock()
		return fmt.Errorf("cannot create job, max limit reached: %d", p.maxJobs)
	}

	p.jobs[j.JobUUID] = j
	p.mapMutex.Unlock()
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobCreated, JobUUID: j.JobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobDelayed(j *job.Job) error {
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobDelayed, JobUUID: j.JobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobPending(j *job.Job) error {
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobPending, JobUUID: j.JobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobEnqueued(j *job.Job) error {
	p.mapMutex.Lock()
	p.jobs[j.JobUUID] = j
	p.mapMutex.Unlock()
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobEnqueued, JobUUID: j.JobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobCanceled(j *job.Job) error {
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobCanceled, JobUUID: j.JobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobFailed(j *job.Job) error {
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobFailed, JobUUID: j.JobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobHidden(j *job.Job) error {
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobHidden, JobUUID: j.JobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobTerminated(j *job.Job) error {
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobTerminated, JobUUID: j.JobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobDeleted(jobUUID job.JobUUID) error {
	p.mapMutex.Lock()
	delete(p.jobs, jobUUID)
	p.mapMutex.Unlock()
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobDeleted, JobUUID: jobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobStarted(j *job.Job) error {
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobStarted, JobUUID: j.JobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobSucceeded(j *job.Job) error {
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobSucceeded, JobUUID: j.JobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobTimeout(j *job.Job) error {
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventJobTimeout, JobUUID: j.JobUUID})
	return nil
}

func (p *InMemoryJobBackendProvider) OnJobsDeleted() error {
	p.mapMutex.Lock()
	p.jobs = make(job.JobMap)
	p.mapMutex.Unlock()
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventAllJobsDeleted})
	return nil
}

func (p *InMemoryJobBackendProvider) OnAllResourcesUnlocked() error {
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventAllResourcesUnlocked})
	return nil
}

func (p *InMemoryJobBackendProvider) OnResourceUnlocked(j *job.Job, resource string) error {
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

	p.hasChanged.Store(true)
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
	// Check if there are changes to be synchronized
	if !p.hasChanged.Load() {
		return nil
	}

	// If persistent storage is enabled, save changes to file
	if p.enablePersistentStorage {
		if err := p.SaveToFile(); err != nil {
			return err
		}
	}

	// Reset the change flag after synchronization
	p.hasChanged.Store(false)
	return nil
}

func (p *InMemoryJobBackendProvider) Healthcheck() bool {
	// assume the in memory backend is always healthy
	return true
}

func (p *InMemoryJobBackendProvider) NotifyEvent(ev event.ServiceEventType) {
	if p.restoreFlag {
		return
	}

	switch ev {
	case event.ServiceEventJobDeletedAll:
		if err := p.OnJobsDeleted(); err != nil {
			p.logger.Error(
				"Failed to handle OnJobsDeleted event",
				zap.String("topic", "backendProvider"),
				zap.String("method", "NotifyEvent"),
				zap.Error(err),
			)
		}
	}
}

func (p *InMemoryJobBackendProvider) NotifyTopicEvent(ev event.ServiceEventType, topic string) {
}

func (p *InMemoryJobBackendProvider) NotifyJobEvent(j *job.Job, ev event.ServiceEventType) {
	if p.restoreFlag {
		return
	}

	switch ev {
	case event.ServiceEventJobCreated:
		if err := p.OnJobCreated(j); err != nil {
			p.logger.Error(
				"Failed to handle OnJobCreated event",
				zap.String("topic", "backendProvider"),
				zap.String("method", "NotifyJobEvent"),
				zap.String("jobUUID", j.JobUUID.String()),
				zap.Error(err),
			)
		}
	case event.ServiceEventJobDelayed:
		if err := p.OnJobDelayed(j); err != nil {
			p.logger.Error(
				"Failed to handle OnJobDelayed event",
				zap.String("topic", "backendProvider"),
				zap.String("method", "NotifyJobEvent"),
				zap.String("jobUUID", j.JobUUID.String()),
				zap.Error(err),
			)
		}
	case event.ServiceEventJobPending:
		if err := p.OnJobPending(j); err != nil {
			p.logger.Error(
				"Failed to handle OnJobPending event",
				zap.String("topic", "backendProvider"),
				zap.String("method", "NotifyJobEvent"),
				zap.String("jobUUID", j.JobUUID.String()),
				zap.Error(err),
			)
		}
	case event.ServiceEventJobEnqueued:
		if err := p.OnJobEnqueued(j); err != nil {
			p.logger.Error(
				"Failed to handle OnJobEnqueued event",
				zap.String("topic", "backendProvider"),
				zap.String("method", "NotifyJobEvent"),
				zap.String("jobUUID", j.JobUUID.String()),
				zap.Error(err),
			)
		}
	case event.ServiceEventJobDeleted:
		if err := p.OnJobDeleted(j.JobUUID); err != nil {
			p.logger.Error(
				"Failed to handle OnJobDeleted event",
				zap.String("topic", "backendProvider"),
				zap.String("method", "NotifyJobEvent"),
				zap.String("jobUUID", j.JobUUID.String()),
				zap.Error(err),
			)
		}
	case event.ServiceEventJobStarted:
		if err := p.OnJobStarted(j); err != nil {
			p.logger.Error(
				"Failed to handle OnJobStarted event",
				zap.String("topic", "backendProvider"),
				zap.String("method", "NotifyJobEvent"),
				zap.String("jobUUID", j.JobUUID.String()),
				zap.Error(err),
			)
		}
	case event.ServiceEventJobSucceeded:
		if err := p.OnJobSucceeded(j); err != nil {
			p.logger.Error(
				"Failed to handle OnJobSucceeded event",
				zap.String("topic", "backendProvider"),
				zap.String("method", "NotifyJobEvent"),
				zap.String("jobUUID", j.JobUUID.String()),
				zap.Error(err),
			)
		}
	case event.ServiceEventJobCanceled:
		if err := p.OnJobCanceled(j); err != nil {
			p.logger.Error(
				"Failed to handle OnJobCanceled event",
				zap.String("topic", "backendProvider"),
				zap.String("method", "NotifyJobEvent"),
				zap.String("jobUUID", j.JobUUID.String()),
				zap.Error(err),
			)
		}
	case event.ServiceEventJobFailed:
		if err := p.OnJobFailed(j); err != nil {
			p.logger.Error(
				"Failed to handle OnJobFailed event",
				zap.String("topic", "backendProvider"),
				zap.String("method", "NotifyJobEvent"),
				zap.String("jobUUID", j.JobUUID.String()),
				zap.Error(err),
			)
		}
	case event.ServiceEventJobHidden:
		if err := p.OnJobHidden(j); err != nil {
			p.logger.Error(
				"Failed to handle OnJobHidden event",
				zap.String("topic", "backendProvider"),
				zap.String("method", "NotifyJobEvent"),
				zap.String("jobUUID", j.JobUUID.String()),
				zap.Error(err),
			)
		}
	case event.ServiceEventJobTerminated:
		if err := p.OnJobTerminated(j); err != nil {
			p.logger.Error(
				"Failed to handle OnJobTerminated event",
				zap.String("topic", "backendProvider"),
				zap.String("method", "NotifyJobEvent"),
				zap.String("jobUUID", j.JobUUID.String()),
				zap.Error(err),
			)
		}
	}
}

func (p *InMemoryJobBackendProvider) GetDiskUsage() int64 {
	if p.enablePersistentStorage {
		return p.storage.GetDiskUsage()
	} else {
		return 0
	}
}

func (p *InMemoryJobBackendProvider) GetType() string {
	return "InMemory"
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
		enablePersistentStorage: conf.Backend.InMemory.EnablePersistentStorage,
		storage:                 NewDBFileStorage(logger, conf.Backend.InMemory.Directory, conf.Backend.InMemory.Filename),
		hasChanged:              atomic.Bool{},
		stopChan:                make(chan bool, 1),
		restoreFlag:             false,
	}, nil
}
