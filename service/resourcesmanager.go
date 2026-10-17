package service

import (
	"errors"
	"sync"

	"github.com/nbigot/minijob/job"
	"go.uber.org/zap"
)

type ResourcesManager struct {
	lockedResources job.LockedResources // map of locked resources currently in use by the jobs
	mu              sync.Mutex          // to ensure safe concurrent manipulation of jobs
	logger          *zap.Logger         // logger
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
	m.logger.Info("All resources unlocked")
}

func (m *ResourcesManager) UnlockResource(resourceName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if the resource is locked
	if _, found := m.lockedResources[resourceName]; !found {
		m.logger.Warn("Resource not found", zap.String("resource", resourceName))
		return errors.New("resource not found: " + resourceName)
	}
	// Unlock the resource
	delete(m.lockedResources, resourceName)
	m.logger.Info("Resource unlocked", zap.String("resource", resourceName))
	return nil
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

// LockResources locks the resources of the job: all of them or none. A resource the job
// already holds is kept, and a name listed twice is locked once. It returns how many
// resources were newly locked.
func (m *ResourcesManager) LockResources(j *job.Job) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if all the resources required by the job are free (or already held by it)
	for _, r := range j.LockResources {
		if owner, found := m.lockedResources[r]; found && owner != j.JobUUID {
			return 0, errors.New("resource not available: " + r)
		}
	}

	acquired := 0
	for _, r := range j.LockResources {
		if _, found := m.lockedResources[r]; !found {
			m.lockedResources[r] = j.JobUUID
			acquired++
		}
	}
	return acquired, nil
}

// UnlockResources releases the resources held by the job; a resource locked by another job
// is left alone. It returns the released resources.
func (m *ResourcesManager) UnlockResources(j *job.Job) []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var released []string
	for _, r := range j.LockResources {
		if owner, found := m.lockedResources[r]; found && owner == j.JobUUID {
			delete(m.lockedResources, r)
			released = append(released, r)
		}
	}
	return released
}

func NewResourcesManager(logger *zap.Logger) *ResourcesManager {
	return &ResourcesManager{
		lockedResources: make(job.LockedResources),
		logger:          logger,
	}
}
