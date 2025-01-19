package service

import (
	"errors"
	"fmt"
	"sync"

	"github.com/nbigot/minijob/job"
	"go.uber.org/zap"
)

type ResourcesManager struct {
	lockedResources job.LockedResources // map of locked resources currently in use by the jobs
	//mapMutex        sync.RWMutex        // mutex to protect hashmap of jobs
	mu     sync.Mutex  // to ensure safe concurrent manipulation of jobs
	logger *zap.Logger // logger
}

func (m *ResourcesManager) GetLockedResources() job.LockedResources {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create a copy of the lockedResources map
	copyLockedResources := make(job.LockedResources)
	for key, value := range m.lockedResources {
		copyLockedResources[key] = value
	}
	return copyLockedResources
}

func (m *ResourcesManager) UnlockAllResources() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.lockedResources = make(job.LockedResources)
}

func (m *ResourcesManager) LockResourcesForJobs(jobs job.JobMap) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Lock the resources required by the jobs
	m.lockedResources = job.LockedResources{}
	for _, j := range jobs {
		switch j.GetState() {
		case job.JobQueued, job.JobRunning:
			for _, r := range j.LockResources {
				m.lockedResources[r] = j.JobUUID
			}
		}
	}
}

func (m *ResourcesManager) LockResources(j *job.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if all the resources required by the job are free
	if success, non_available_resource := m.areRequiredResourcesFree(j); !success {
		// at least one of the resources is not available
		return errors.New("resource not available: " + non_available_resource)
	}

	return m.lockResources(j)
}

func (m *ResourcesManager) UnlockResources(j *job.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Unlock the resources
	for _, r := range j.LockResources {
		delete(m.lockedResources, r)
	}

	return nil
}

func (m *ResourcesManager) areRequiredResourcesFree(j *job.Job) (bool, string) {
	// Check if the resources required by the job are free
	for _, r := range j.LockResources { // BUG: fatal error: concurrent map read and map write
		if _, found := m.lockedResources[r]; found {
			return false, r
		}
	}

	return true, ""
}

func (m *ResourcesManager) lockResources(j *job.Job) error {
	// Lock the resources
	for _, r := range j.LockResources {
		// safety test
		if _, found := m.lockedResources[r]; found {
			reason := fmt.Sprintf("Resource '%s' already locked by job id %s", r, j.JobUUID.String())
			m.logger.Error(reason)
			return errors.New(reason)
		}

		// lock the resource
		m.lockedResources[r] = j.JobUUID // BUG: fatal error: concurrent map read and map write
	}

	return nil
}

func NewResourcesManager(logger *zap.Logger) *ResourcesManager {
	return &ResourcesManager{
		lockedResources: make(job.LockedResources),
		logger:          logger,
	}
}
