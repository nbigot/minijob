package service

import (
	"bytes"
	"cmp"
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/nbigot/minijob/callback"
	"github.com/nbigot/minijob/config"
	"github.com/nbigot/minijob/constants"
	"github.com/nbigot/minijob/event"
	"github.com/nbigot/minijob/eventlogger"
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/jobbackendprovider"
	"github.com/nbigot/minijob/jobbackendprovider/registry"
	"github.com/nbigot/minijob/lifecycle"
	"github.com/nbigot/minijob/log"
	"github.com/nbigot/minijob/metrics"
	"github.com/nbigot/minijob/pq"

	"github.com/nbigot/minijob/web/apierror"

	"go.uber.org/zap"
)

// Service is the job scheduler.
//
// Locking: mu protects the whole scheduler state (jobs, pending jobs, retry and delayed
// queues, resource locks) and every job it holds; job state changes and the notification of
// the observers (backend provider, metrics, event log, callbacks) happen with mu held, so
// they are seen in the same order by everyone. Methods whose name ends with "Locked" expect
// mu to be held. Jobs returned to callers are snapshots (Job.Snapshot), which later changes
// do not affect.
type Service struct {
	// implements IService interface
	version                    string                                 // service version
	jobs                       job.JobMap                             // hashmap of jobs
	pendingJobs                job.JobMap                             // hashmap of pending jobs
	pqRetryJobs                pq.PriorityQueue[*job.Job]             // priority queue of retry jobs
	pqDelayedJobs              pq.PriorityQueue[*job.Job]             // priority queue of delayed jobs
	mu                         sync.RWMutex                           // protects the scheduler state (see above)
	queued                     chan struct{}                          // closed (and replaced) when a job is queued: wakes the long polls
	checkJobsRetentionRunning  atomic.Value                           // atomic value to track if the check jobs retention is running
	checkJobsVisibilityRunning atomic.Value                           // atomic value to track if the check jobs visibility is running
	checkJobsDelayRunning      atomic.Value                           // atomic value to track if the check delayed jobs is running
	notifChanBP                chan jobbackendprovider.Event          // notification channel for job backend provider
	metrics                    metrics.IServiceMetrics                // service to manage metrics
	eventLogger                *eventlogger.EventLoggerObserver       // event logger
	eventNotifier              event.IServiceEventNotifier            // service to manage events
	lc                         *lifecycle.Lifecycle                   // runs the service loop once, stops it once
	stopping                   chan struct{}                          // closed when the server starts stopping (ends the long polls)
	stoppingOnce               sync.Once                              // closes stopping once
	stopOnce                   sync.Once                              // runs Stop once
	startTime                  time.Time                              // service start time
	bp                         jobbackendprovider.IJobBackendProvider // backend provider
	conf                       *config.Config                         // configuration
	logger                     *zap.Logger                            // logger
	resourcesManager           *ResourcesManager                      // resources manager
	callbackService            *callback.CallbackService              // callback service for job completion notifications
}

func (svc *Service) Init() error {
	var err error

	svc.checkJobsRetentionRunning.Store(false)
	svc.checkJobsVisibilityRunning.Store(false)
	svc.checkJobsDelayRunning.Store(false)

	svc.bp.SetNotifChan(svc.notifChanBP)
	svc.eventNotifier.Register(svc.bp)
	if svc.conf.WebServer.Metrics.Enable {
		svc.eventNotifier.Register(svc.metrics)
	}
	if svc.conf.EventsLogger.Enable && svc.conf.EventsLogger.FilePath != "" {
		svc.eventNotifier.Register(svc.eventLogger)
	}
	svc.eventNotifier.Register(svc.callbackService)
	if err := svc.eventNotifier.Init(); err != nil {
		log.Logger.Error("Error initializing event notifier",
			zap.String("topic", "service"),
			zap.String("method", "Init"),
			zap.Error(err),
		)
		return err
	}

	err = svc.LoadJobs()
	if err != nil {
		log.Logger.Error("Error while loading jobs",
			zap.String("topic", "service"),
			zap.String("method", "Init"),
			zap.Error(err),
		)
		return err
	}

	svc.startTime = time.Now()
	return nil
}

func (svc *Service) GetVersion() string {
	return svc.version
}

func (svc *Service) GetEnvironment() string {
	return svc.conf.Environment
}

func (svc *Service) GetHostname() string {
	return svc.conf.WebServer.HTTP.Address
}

func (svc *Service) GetJobsCount() uint {
	svc.mu.RLock()
	defer svc.mu.RUnlock()
	return uint(len(svc.jobs))
}

func (svc *Service) UpdateResourcesLockedCountMetric(topic string, inc int) {
	svc.metrics.UpdateResourcesLockedCountMetric(topic, inc)
}

func (svc *Service) LoadJobs() error {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	var err error
	svc.jobs, err = svc.bp.LoadJobs()
	if err != nil {
		return err
	}

	svc.resourcesManager.LockResourcesForJobs(svc.jobs)

	// prevent the backend provider from processing notifications
	// while restoring the jobs from the backend provider
	svc.bp.SetRestoreFlag(true)
	svc.pendingJobs = make(job.JobMap)
	now := time.Now().UnixMilli()
	// in id order, so that topics and metrics are registered in creation order
	for _, id := range sortedJobIDs(svc.jobs) {
		j := svc.jobs[id]
		switch j.GetState() {
		case job.JobPending:
			svc.pendingJobs[j.JobUUID] = j
		case job.JobHidden:
			// can retry the job now
			svc.pqRetryJobs.Enqueue(j, -now)
		case job.JobDelayed:
			svc.pqDelayedJobs.Enqueue(j, -j.StartAfter)
		}
		svc.eventNotifier.NotifyJobEvent(j, event.ServiceEventJobLoaded)
		svc.UpdateResourcesLockedCountMetric(j.Topic, int(j.GetCountLockResources()))
	}
	// allow the backend provider to process notifications
	svc.bp.SetRestoreFlag(false)

	return nil
}

func (svc *Service) CreateJob(payload []byte) (*job.Job, error) {
	// decode the request before taking the lock
	j, decodeErr := job.NewJobFromRequest(payload)

	svc.mu.Lock()
	defer svc.mu.Unlock()

	// check if a job can be created
	if svc.conf.Jobs.MaxAllowedJobs > 0 && uint(len(svc.jobs)) >= svc.conf.Jobs.MaxAllowedJobs {
		err := errors.New("cannot create job, limit reached")
		svc.logger.Error(
			"Cannot create job",
			zap.String("topic", "service"),
			zap.String("method", "CreateJob"),
			zap.Error(err),
		)
		return nil, &apierror.APIError{
			Message:  "cannot create job",
			Code:     constants.ErrorCantCreateJob,
			HttpCode: fiber.StatusServiceUnavailable,
			Err:      err,
		}
	}

	if decodeErr != nil {
		svc.logger.Error(
			"Cannot create job",
			zap.String("topic", "service"),
			zap.String("method", "CreateJob"),
			zap.Error(decodeErr),
		)
		return nil, decodeErr
	}

	return svc.finalizeJobCreationLocked(j)
}

// finalizeJobCreationLocked adds a new job and schedules it. It returns the job as it was
// created (pending or delayed), before it is queued.
func (svc *Service) finalizeJobCreationLocked(j *job.Job) (*job.Job, error) {
	now := time.Now().UnixMilli()
	newJobUuid, err := svc.generateNewJobUuidLocked()
	if err != nil {
		svc.logger.Error(
			"Cannot create job",
			zap.String("topic", "service"),
			zap.String("method", "FinalizeJobCreation"),
			zap.Error(err),
		)
		return nil, err
	}
	j.Init(newJobUuid, now)

	svc.jobs[j.JobUUID] = j

	if svc.conf.Jobs.LogVerbosity > 1 {
		svc.logger.Info(
			"Job created",
			zap.String("topic", "service"),
			zap.String("method", "FinalizeJobCreation"),
			zap.String("JobUUID", j.JobUUID.String()),
		)
	}

	// log the job value
	if svc.conf.Jobs.LogVerbosity > 2 {
		svc.logger.Info(
			"New job",
			zap.Any("Job", j),
		)
	}

	// send a notification that a new job has been created
	svc.eventNotifier.NotifyJobEvent(j, event.ServiceEventJobCreated)

	now = time.Now().UnixMilli()
	if j.StartAfter != 0 && j.StartAfter > now {
		svc.setJobStateToDelayedLocked(j)
	} else {
		svc.setJobStateToPendingLocked(j)
	}

	created := j.Snapshot()
	svc.tryEnqueuePendingJobsLocked()
	return created, nil
}

func (svc *Service) setJobStateToDelayedLocked(j *job.Job) {
	// delay the job by adding a history event
	j.AddHistoryEvent(job.JobEventDelay, time.Now().UnixMilli())
	// the job is delayed
	svc.pqDelayedJobs.Enqueue(j, -j.StartAfter)
	// send a notification that the job has been delayed
	svc.eventNotifier.NotifyJobEvent(j, event.ServiceEventJobDelayed)
}

func (svc *Service) setJobStateToPendingLocked(j *job.Job) {
	// add a history event
	j.AddHistoryEvent(job.JobEventPending, time.Now().UnixMilli())
	// the job is pending (the caller tries to enqueue the pending jobs)
	svc.pendingJobs[j.JobUUID] = j
	// send a notification that the job is pending
	svc.eventNotifier.NotifyJobEvent(j, event.ServiceEventJobPending)
}

func (svc *Service) TryEnqueuePendingJobs() {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	svc.tryEnqueuePendingJobsLocked()
}

func (svc *Service) tryEnqueuePendingJobsLocked() {
	// This function check if a pending job can be enqueued
	// if a job is pending and the resources are available the job is enqueued

	// optimization: if there are no pending jobs, return
	cptPendingJobs := len(svc.pendingJobs)
	if cptPendingJobs == 0 {
		return
	}

	// create a list of pending jobs from variable svc.pendingJobs for sorting
	lockedResources := svc.resourcesManager.GetLockedResources()
	pendingJobs := make([]*job.Job, 0, len(svc.pendingJobs))
	for _, j := range svc.pendingJobs {
		if !requiresLockedResources(lockedResources, j) {
			pendingJobs = append(pendingJobs, j)
		}
	}

	// sort the pending jobs by (priority, creation timestamp, id)
	slices.SortFunc(pendingJobs, compareQueueOrder)

	// try to enqueue the pending jobs (may fail if resources are not available)
	for _, j := range pendingJobs {
		_ = svc.enqueueJobLocked(j)
	}
}

func (svc *Service) AttemptJobRetries() {
	// this function check if some jobs can be retried
	svc.mu.Lock()
	defer svc.mu.Unlock()

	// loop over the priority queue of retry jobs
	// note: the priority queue is ordered by the retry time,
	// so the first job in the retry queue is not ready to be retried yet
	// then the others are not ready too
	minPriority := -time.Now().UnixMilli()
	type blockedJob struct {
		j        *job.Job
		priority int64
	}
	var blocked []blockedJob
	for {
		// get the first job from the priority queue with priority >= minPriority
		// here the priority is the retry time (negative value)
		j, priority, ok := svc.pqRetryJobs.DequeueWithPriority(minPriority)
		if !ok {
			break
		}
		if err := svc.retryJobLocked(j); err != nil {
			// the job cannot be retried yet (its resources are held by another job):
			// it goes back in the queue, the other jobs are still retried
			blocked = append(blocked, blockedJob{j, priority})
		}
	}
	for _, b := range blocked {
		svc.pqRetryJobs.Enqueue(b.j, b.priority)
	}
}

func (svc *Service) retryJobLocked(j *job.Job) error {
	// Retry the job immediately. Its resources were released if it waited in the retry
	// queue: lock them again (the job waits there while another job holds them).
	if apiError := svc.lockResourcesLocked(j); apiError != nil {
		return apiError
	}
	now := time.Now().UnixMilli()

	// Enqueue the job by adding a history event
	j.AddHistoryEvent(job.JobEventEnqueue, now)

	if svc.conf.Jobs.LogVerbosity > 1 {
		svc.logger.Info(
			"Job enqueued",
			zap.String("topic", "service"),
			zap.String("method", "RetryJob"),
			zap.String("JobUUID", j.JobUUID.String()),
		)
	}

	// notify event
	svc.eventNotifier.NotifyJobEvent(j, event.ServiceEventJobEnqueued)
	svc.notifyQueuedLocked()

	return nil
}

func (svc *Service) lockResourcesLocked(j *job.Job) *apierror.APIError {
	// Lock the resources required by the job
	acquired, err := svc.resourcesManager.LockResources(j)
	if err != nil {
		// the job is not ready to be enqueued
		return &apierror.APIError{
			Message:  "cannot enqueue job",
			Code:     constants.ErrorCantEnqueueJob,
			HttpCode: fiber.StatusPreconditionFailed,
			JobUUID:  j.JobUUID,
			Err:      err,
		}
	}

	// Update the resources locked count metric
	if acquired > 0 {
		svc.UpdateResourcesLockedCountMetric(j.Topic, acquired)
	}

	return nil
}

func (svc *Service) enqueueJobLocked(j *job.Job) error {
	// Attempt to enqueue the job.
	// If it fails, it means that the job is not ready to be enqueued due to resource unavailability.

	// get the current time
	if j.StartAfter != 0 && j.StartAfter > time.Now().UnixMilli() {
		// the job is not ready to be enqueued
		return &apierror.APIError{
			Message:  "cannot enqueue job",
			Code:     constants.ErrorCantEnqueueJob,
			HttpCode: fiber.StatusPreconditionFailed,
			JobUUID:  j.JobUUID,
			Err:      errors.New("job is not ready yet to be enqueued"),
		}
	}

	// Lock the resources
	if apiError := svc.lockResourcesLocked(j); apiError != nil {
		return apiError
	}

	// Enqueue the job by adding a history event
	j.AddHistoryEvent(job.JobEventEnqueue, time.Now().UnixMilli())

	// Remove the job from the pending jobs
	delete(svc.pendingJobs, j.JobUUID)

	if svc.conf.Jobs.LogVerbosity > 1 {
		svc.logger.Info(
			"Job enqueued",
			zap.String("topic", "service"),
			zap.String("method", "EnqueueJob"),
			zap.String("JobUUID", j.JobUUID.String()),
		)
	}

	// notify event
	svc.eventNotifier.NotifyJobEvent(j, event.ServiceEventJobEnqueued)
	svc.notifyQueuedLocked()

	return nil
}

// notifyQueuedLocked wakes the long polls waiting for a queued job.
func (svc *Service) notifyQueuedLocked() {
	close(svc.queued)
	svc.queued = make(chan struct{})
}

func (svc *Service) GenerateNewJobUuid() (job.JobUUID, error) {
	svc.mu.RLock()
	defer svc.mu.RUnlock()
	return svc.generateNewJobUuidLocked()
}

func (svc *Service) generateNewJobUuidLocked() (job.JobUUID, error) {
	// ensure new job uuid is unique
	for {
		uuidCandidate, err := uuid.NewV7()
		if err != nil {
			return job.JobUUID(uuidCandidate), &apierror.APIError{
				Message:  "cannot create job",
				Code:     constants.ErrorCantCreateJob,
				HttpCode: fiber.StatusInternalServerError,
				Err:      err,
			}
		}
		if _, exists := svc.jobs[uuidCandidate]; !exists {
			return job.JobUUID(uuidCandidate), nil
		}
	}
}

func (svc *Service) DeleteJob(jobUUID job.JobUUID) error {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	return svc.deleteJobLocked(jobUUID)
}

func (svc *Service) deleteJobLocked(jobUUID job.JobUUID) error {
	j := svc.jobs[jobUUID]
	if j == nil {
		apiErr := apierror.APIError{
			Message:  "job not found",
			Code:     constants.ErrorCantDeleteJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
		}
		svc.logger.Error(
			"Cannot delete job",
			zap.String("topic", "service"),
			zap.String("method", "DeleteJob"),
			zap.String("JobUUID", jobUUID.String()),
		)
		return &apiErr
	}

	svc.unlockJobResourcesLocked(j)

	// delete the job (add a history event)
	j.AddHistoryEvent(job.JobEventDelete, time.Now().UnixMilli())

	// Remove the job from the retry queue (if it is in the retry queue)
	_ = svc.pqRetryJobs.Remove(j) // Ignore error - job might not be in the queue

	// Remove the job from the delayed queue (if it is in the delayed queue)
	_ = svc.pqDelayedJobs.Remove(j) // Ignore error - job might not be in the queue

	// delete uuid from hashmap
	delete(svc.jobs, jobUUID)

	// Remove the job from the pending jobs (if it is in the pending jobs)
	delete(svc.pendingJobs, jobUUID)

	if svc.conf.Jobs.LogVerbosity > 1 {
		svc.logger.Info(
			"Job deleted",
			zap.String("topic", "service"),
			zap.String("method", "DeleteJob"),
			zap.String("JobUUID", jobUUID.String()),
		)
	}

	// notify event
	svc.eventNotifier.NotifyJobEvent(j, event.ServiceEventJobDeleted)

	// the resources of the job may let pending jobs start
	svc.tryEnqueuePendingJobsLocked()
	return nil
}

func (svc *Service) DeleteAllJobs() error {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	svc.pqRetryJobs.Clear()
	svc.pqDelayedJobs.Clear()
	svc.jobs = make(job.JobMap)
	svc.pendingJobs = make(job.JobMap)
	_ = svc.unlockAllResourcesLocked() // Ignore error during deletion
	svc.eventNotifier.NotifyEvent(event.ServiceEventJobDeletedAll)
	return nil
}

func (svc *Service) DeleteQueuedJobs() error {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	// collect the job UUIDs to delete (in id order)
	jobUUIDsToDelete := make([]job.JobUUID, 0)
	for _, jobUUID := range sortedJobIDs(svc.jobs) {
		jobState := svc.jobs[jobUUID].GetState()
		if jobState == job.JobCreated || jobState == job.JobDelayed || jobState == job.JobPending || jobState == job.JobQueued {
			jobUUIDsToDelete = append(jobUUIDsToDelete, jobUUID)
		}
	}

	for _, jobUUID := range jobUUIDsToDelete {
		if err := svc.deleteJobLocked(jobUUID); err != nil {
			svc.logger.Error(
				"Cannot delete queued job",
				zap.String("topic", "service"),
				zap.String("method", "DeleteQueuedJobs"),
				zap.String("JobUUID", jobUUID.String()),
				zap.Error(err),
			)
			return err
		}
	}

	return nil
}

func (svc *Service) unlockJobResourcesLocked(j *job.Job) {
	// only the resources this job holds (another job may hold the same names)
	released := svc.resourcesManager.UnlockResources(j)
	if len(released) == 0 {
		return
	}

	// Update the resources locked count metric
	svc.UpdateResourcesLockedCountMetric(j.Topic, -len(released))

	for _, resource := range released {
		// notify the backend provider
		if err := svc.bp.OnResourceUnlocked(j, resource); err != nil {
			svc.logger.Error(
				"Cannot unlock resources",
				zap.String("topic", "service"),
				zap.String("method", "UnlockJobResources"),
				zap.String("JobUUID", j.JobUUID.String()),
				zap.Error(err),
			)
		}
	}
}

func (svc *Service) PullJobs(req *RequestPullJobs) (*ResponsePullJobs, error) {
	// pull one or multiple jobs from the queue and start it/them
	if req.JobUUID != nil {
		return svc.PullSpecificJob(req)
	}

	// pull any job from the queue; with a wait time (long poll), wait until a job is
	// queued, the wait time is over or the service stops
	deadline := time.Now().Add(time.Duration(req.WaitTimeSeconds) * time.Second)
	for {
		last := !time.Now().Before(deadline)
		res, found, wake, err := svc.pullJobsOnce(req, last)
		if found || last {
			return res, err
		}
		timer := time.NewTimer(time.Until(deadline))
		select {
		case <-wake:
		case <-timer.C:
		case <-svc.stopping:
			// the service stops: answer now
			timer.Stop()
			res, _, _, err := svc.pullJobsOnce(req, true)
			return res, err
		}
		timer.Stop()
	}
}

// pullJobsOnce starts the best job candidates, if any. The candidates are chosen and
// started under the same lock, so a job cannot be given to two workers. found is false
// when no job is ready: wake is then closed when a job is queued. The empty poll is
// notified when last is true.
func (svc *Service) pullJobsOnce(req *RequestPullJobs, last bool) (res *ResponsePullJobs, found bool, wake <-chan struct{}, err error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	// note: when no candidate is found, err is not nil
	candidates, err := svc.findBestJobCandidatesLocked(req)
	if len(candidates) == 0 {
		if last {
			// no job candidate found in the time limit
			svc.eventNotifier.NotifyTopicEvent(event.ServiceEventEmptyPollReceived, req.Topic)
		}
		return nil, false, svc.queued, err
	}

	res = &ResponsePullJobs{
		Jobs: make([]*job.Job, 0),
	}

	// try to start the jobs from the candidates
	for _, j := range candidates {
		if err := svc.startJobLocked(j.JobUUID, req); err != nil {
			// log the warning
			svc.logger.Warn(
				"Cannot start job",
				zap.String("topic", "service"),
				zap.String("method", "PullJobs"),
				zap.String("JobUUID", j.JobUUID.String()),
				zap.Error(err),
			)

			// in this case, we skip the job (best effort)
			continue
		}
		res.Jobs = append(res.Jobs, j.Snapshot())
	}

	if len(res.Jobs) == 0 {
		svc.eventNotifier.NotifyTopicEvent(event.ServiceEventEmptyPollReceived, req.Topic)

		return nil, true, nil, &apierror.APIError{
			Message:  "no job found that is ready or matches the criteria",
			Code:     constants.ErrorCantPullAnyJob,
			HttpCode: fiber.StatusNotFound,
			JobUUID:  job.JobUUID{},
		}
	}

	return res, true, nil, nil
}

func (svc *Service) findBestJobCandidatesLocked(req *RequestPullJobs) ([]*job.Job, error) {
	// find the best job candidates to start
	if len(svc.jobs) == 0 {
		// job not found in the queue
		return nil, &apierror.APIError{
			Message:  "no jobs found in the queue",
			Code:     constants.ErrorCantPullAnyJob,
			HttpCode: fiber.StatusNotFound,
			JobUUID:  job.JobUUID{},
		}
	}

	bestCandidates := make([]*job.Job, 0)

	// first step: loop over the jobs and filter the jobs
	for _, j := range svc.jobs {
		// filter the jobs by topic
		// note: the topic "*" means all topics
		if req.Topic != "*" && j.Topic != req.Topic {
			continue
		}

		// filter the jobs by state
		if j.GetState() != job.JobQueued {
			continue
		}

		bestCandidates = append(bestCandidates, j)
	}

	if len(bestCandidates) == 0 {
		return nil, &apierror.APIError{
			Message:  "no job found that is ready or matches the criteria",
			Code:     constants.ErrorCantPullAnyJob,
			HttpCode: fiber.StatusNotFound,
			JobUUID:  job.JobUUID{},
		}
	}

	// second step: sort the jobs by (priority, creation timestamp, id)
	slices.SortFunc(bestCandidates, compareQueueOrder)

	// third step: limit the number of jobs
	if uint(len(bestCandidates)) > req.NumJobs {
		bestCandidates = bestCandidates[:req.NumJobs]
	}

	return bestCandidates, nil
}

func (svc *Service) PullSpecificJob(req *RequestPullJobs) (*ResponsePullJobs, error) {
	// ignore the parameters topic and wait time
	// check if the specific job (req.JobUUID) is in the queue
	if req.JobUUID == nil {
		return nil, &apierror.APIError{
			Message:  "job uuid is missing",
			Code:     constants.ErrorCantPullSpecificJob,
			HttpCode: fiber.StatusBadRequest,
		}
	}

	jobUUID := *req.JobUUID

	svc.mu.Lock()
	defer svc.mu.Unlock()

	if j, found := svc.jobs[jobUUID]; found {
		if err := svc.startJobLocked(jobUUID, req); err != nil {
			return nil, err
		}

		return &ResponsePullJobs{
			Jobs: []*job.Job{j.Snapshot()},
		}, nil
	}

	// job not found in the queue
	return nil, &apierror.APIError{
		Message:  "job not found in the queue",
		Code:     constants.ErrorCantPullSpecificJob,
		HttpCode: fiber.StatusBadRequest,
		JobUUID:  jobUUID,
	}
}

func (svc *Service) StartJob(jobUUID job.JobUUID, req *RequestPullJobs) error {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	return svc.startJobLocked(jobUUID, req)
}

func (svc *Service) startJobLocked(jobUUID job.JobUUID, req *RequestPullJobs) error {
	// get the job
	j := svc.jobs[jobUUID]
	if j == nil {
		apiErr := apierror.APIError{
			Message:  "job not found",
			Code:     constants.ErrorCantStartJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
		}
		svc.logger.Error(
			"Cannot start job",
			zap.String("topic", "service"),
			zap.String("method", "StartJob"),
			zap.String("JobUUID", jobUUID.String()),
		)

		return &apiErr
	}

	// check if the job is able to be started (from its state)
	if j.GetState() != job.JobQueued {
		// This occurs when the job is already started,
		// for instance, when multiple workers attempt to start a job.
		apiErr := apierror.APIError{
			Message:  "job already started",
			Code:     constants.ErrorCantStartJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
		}
		svc.logger.Error(
			"Cannot start job",
			zap.String("topic", "service"),
			zap.String("method", "StartJob"),
			zap.String("JobUUID", jobUUID.String()),
		)

		return &apiErr
	}

	// Remove the job from the pending jobs
	delete(svc.pendingJobs, j.JobUUID)

	// start the job (add a history event)
	j.AddHistoryEvent(job.JobEventStart, time.Now().UnixMilli())

	j.VisibilityTimeout = req.VisibilityTimeout

	if svc.conf.Jobs.LogVerbosity > 1 {
		svc.logger.Info(
			"Job started",
			zap.String("topic", "service"),
			zap.String("method", "StartJob"),
			zap.String("JobUUID", jobUUID.String()),
		)
	}

	// notify event
	svc.eventNotifier.NotifyJobEvent(j, event.ServiceEventJobStarted)

	return nil
}

func (svc *Service) CloneJob(jobUUID job.JobUUID) (*job.Job, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	// get the job
	j := svc.jobs[jobUUID]
	if j == nil {
		apiErr := apierror.APIError{
			Message:  "job not found",
			Code:     constants.ErrorCantCloneJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
		}
		svc.logger.Error(
			"Cannot clone job",
			zap.String("topic", "service"),
			zap.String("method", "CloneJob"),
			zap.String("JobUUID", jobUUID.String()),
		)

		return nil, &apiErr
	}

	clone := j.Clone()
	return svc.finalizeJobCreationLocked(clone)
}

func (svc *Service) SetJobAsSuccessful(jobUUID job.JobUUID) error {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	// get the job
	j := svc.jobs[jobUUID]
	if j == nil {
		apiErr := apierror.APIError{
			Message:  "job not found",
			Code:     constants.ErrorCantSetJobAsSuccessful,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
		}
		svc.logger.Error(
			"Cannot set job as successful",
			zap.String("topic", "service"),
			zap.String("method", "SetJobAsSuccessful"),
			zap.String("JobUUID", jobUUID.String()),
		)

		return &apiErr
	}

	// check if the job is running
	if j.GetState() != job.JobRunning {
		apiErr := apierror.APIError{
			Message:  "job is not running",
			Code:     constants.ErrorCantSetJobAsSuccessful,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
		}
		svc.logger.Error(
			"Cannot set job as successful",
			zap.String("topic", "service"),
			zap.String("method", "SetJobAsSuccessful"),
			zap.String("JobUUID", jobUUID.String()),
		)

		return &apiErr
	}

	// unlock the resources
	svc.unlockJobResourcesLocked(j)

	// finish the job (add a history event)
	j.AddHistoryEvent(job.JobEventSuccess, time.Now().UnixMilli())

	if svc.conf.Jobs.LogVerbosity > 1 {
		svc.logger.Info(
			"Job succeeded",
			zap.String("topic", "service"),
			zap.String("method", "SetJobAsSuccessful"),
			zap.String("JobUUID", jobUUID.String()),
		)
	}

	// notify event
	svc.eventNotifier.NotifyJobEvent(j, event.ServiceEventJobSucceeded)

	svc.tryEnqueuePendingJobsLocked()
	return nil
}

func (svc *Service) CancelJob(jobUUID job.JobUUID) error {
	// cancel a job that is running and put it back in the queue
	svc.mu.Lock()
	defer svc.mu.Unlock()

	// get the job
	j := svc.jobs[jobUUID]
	if j == nil {
		apiErr := apierror.APIError{
			Message:  "job not found",
			Code:     constants.ErrorCantCancelJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
		}
		svc.logger.Error(
			"Cannot cancel job",
			zap.String("topic", "service"),
			zap.String("method", "CancelJob"),
			zap.String("JobUUID", jobUUID.String()),
		)

		return &apiErr
	}

	// check if the job is running
	if j.GetState() != job.JobRunning {
		apiErr := apierror.APIError{
			Message:  "job is not running",
			Code:     constants.ErrorCantCancelJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
		}
		svc.logger.Error(
			"Cannot cancel job",
			zap.String("topic", "service"),
			zap.String("method", "CancelJob"),
			zap.String("JobUUID", jobUUID.String()),
		)

		return &apiErr
	}

	// cancel the job (add a history event)
	j.AddHistoryEvent(job.JobEventCancel, time.Now().UnixMilli())

	if svc.conf.Jobs.LogVerbosity > 1 {
		svc.logger.Info(
			"Job canceled",
			zap.String("topic", "service"),
			zap.String("method", "CancelJob"),
			zap.String("JobUUID", jobUUID.String()),
		)
	}

	// notify event
	svc.eventNotifier.NotifyJobEvent(j, event.ServiceEventJobCanceled)

	// Enqueue the job by adding a history event
	j.AddHistoryEvent(job.JobEventEnqueue, time.Now().UnixMilli())

	if svc.conf.Jobs.LogVerbosity > 1 {
		svc.logger.Info(
			"Job enqueued",
			zap.String("topic", "service"),
			zap.String("method", "CancelJob"),
			zap.String("JobUUID", j.JobUUID.String()),
		)
	}

	// notify event
	svc.eventNotifier.NotifyJobEvent(j, event.ServiceEventJobEnqueued)
	svc.notifyQueuedLocked()

	svc.tryEnqueuePendingJobsLocked()
	return nil
}

func (svc *Service) FailJob(jobUUID job.JobUUID) (reachedMaxRetry bool, err error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	return svc.failJobLocked(jobUUID)
}

func (svc *Service) failJobLocked(jobUUID job.JobUUID) (reachedMaxRetry bool, err error) {
	// Mark a running job as failed

	// get the job
	j := svc.jobs[jobUUID]
	if j == nil {
		apiErr := apierror.APIError{
			Message:  "job not found",
			Code:     constants.ErrorCantFailJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
		}
		svc.logger.Error(
			"Cannot set job as failed",
			zap.String("topic", "service"),
			zap.String("method", "FailJob"),
			zap.String("JobUUID", jobUUID.String()),
		)

		return false, &apiErr
	}

	// check if the job is running
	if j.GetState() != job.JobRunning {
		apiErr := apierror.APIError{
			Message:  "job is not running",
			Code:     constants.ErrorCantFailJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
		}
		svc.logger.Error(
			"Cannot set job as failed",
			zap.String("topic", "service"),
			zap.String("method", "FailJob"),
			zap.String("JobUUID", jobUUID.String()),
		)

		return false, &apiErr
	}

	if svc.conf.Jobs.LogVerbosity > 1 {
		svc.logger.Info(
			"Job failed",
			zap.String("topic", "service"),
			zap.String("method", "FailJob"),
			zap.String("JobUUID", jobUUID.String()),
		)
	}

	// fail the job (add a history event)
	j.AddHistoryEvent(job.JobEventFail, time.Now().UnixMilli())

	// notify event
	svc.eventNotifier.NotifyJobEvent(j, event.ServiceEventJobFailed)

	// if the job has failed too many times, set it as terminated
	if j.GetCountFaillures() >= svc.conf.Jobs.RetryPolicy.MaxRetry {
		return svc.onJobFailedTooManyTimesLocked(j)
	}

	// The job has not yet failed too many times
	delay := svc.conf.Jobs.RetryPolicy.BackoffStrategy.ComputeDelay(j.GetCountFaillures())

	// Check if the minimum duration between each retry is 0
	if delay == 0 {
		// Retry the job immediately
		return false, svc.retryJobLocked(j)
	}

	// compute the next retry time (current time + duration between retry)
	now := time.Now().UnixMilli()
	nextRetryTime := now + delay.Milliseconds()
	return false, svc.retryJobLaterLocked(j, nextRetryTime)
}

func (svc *Service) onJobFailedTooManyTimesLocked(j *job.Job) (reachedMaxRetry bool, err error) {
	// the job has failed too many times, set it as terminated
	now := time.Now().UnixMilli()
	j.AddHistoryEvent(job.JobEventTerminate, now)

	// unlock the resources
	svc.unlockJobResourcesLocked(j)

	if svc.conf.Jobs.LogVerbosity > 1 {
		svc.logger.Info(
			"Job terminated",
			zap.String("topic", "service"),
			zap.String("method", "FailJob"),
			zap.String("reason", "Max retry limit reached"),
			zap.String("JobUUID", j.JobUUID.String()),
		)
	}

	// notify event
	svc.eventNotifier.NotifyJobEvent(j, event.ServiceEventJobTerminated)

	svc.tryEnqueuePendingJobsLocked()
	return true, nil
}

func (svc *Service) retryJobLaterLocked(j *job.Job, nextRetryTime int64) error {
	if svc.conf.Jobs.LogVerbosity > 1 {
		svc.logger.Info(
			"Job added to the retry queue",
			zap.String("topic", "service"),
			zap.String("method", "FailJob"),
			zap.String("JobUUID", j.JobUUID.String()),
			zap.String("NextRetryTime", time.UnixMilli(nextRetryTime).String()),
		)
	}

	// set job state to JobHidden
	now := time.Now().UnixMilli()
	j.AddHistoryEvent(job.JobEventHide, now)

	// unlock the resources
	svc.unlockJobResourcesLocked(j)

	// notify event
	svc.eventNotifier.NotifyJobEvent(j, event.ServiceEventJobHidden)

	// Add the job to a waiting list (priority queue) for retry
	// Tip: the priority queue is sorted by the next retry time,
	// the job with the smallest next retry time is at the top,
	// that's why we use a negative value for the next retry time
	svc.pqRetryJobs.Enqueue(j, -nextRetryTime)

	// the released resources may let pending jobs start
	svc.tryEnqueuePendingJobsLocked()
	return nil
}

func (svc *Service) ChangeVisibilityTimeoutJob(jobUUID job.JobUUID, visibilityTimeout uint) error {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	// get the job
	j := svc.jobs[jobUUID]
	if j == nil {
		apiErr := apierror.APIError{
			Message:  "job not found",
			Code:     constants.ErrorCantCloneJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
		}
		svc.logger.Error(
			"Cannot change job visibility timeout",
			zap.String("topic", "service"),
			zap.String("method", "ChangeVisibilityTimeoutJob"),
			zap.String("JobUUID", jobUUID.String()),
		)

		return &apiErr
	}

	j.VisibilityTimeout = visibilityTimeout

	return nil
}

// GetJob returns a snapshot of the job, or nil when it does not exist.
func (svc *Service) GetJob(uuid job.JobUUID) (*job.Job, error) {
	svc.mu.RLock()
	defer svc.mu.RUnlock()

	if s, found := svc.jobs[uuid]; found {
		return s.Snapshot(), nil
	}

	return nil, nil
}

func (svc *Service) GetJobsUUIDs() job.JobUUIDList {
	svc.mu.RLock()
	defer svc.mu.RUnlock()

	uuids := make([]job.JobUUID, 0, len(svc.jobs))
	for k := range svc.jobs {
		uuids = append(uuids, k)
	}
	return uuids
}

func (svc *Service) GetLogger() *zap.Logger {
	return svc.logger
}

func (svc *Service) Finalize() error {
	return svc.Stop()
}

// BeginShutdown is called when the server starts stopping: the long polls return at once
// (with their usual "no job" answer) instead of keeping the HTTP server open.
func (svc *Service) BeginShutdown() {
	svc.stoppingOnce.Do(func() { close(svc.stopping) })
}

// Stop stops the service loop and the backend (which saves the jobs), then the observers.
// It can be called several times and from several goroutines.
func (svc *Service) Stop() error {
	svc.stopOnce.Do(func() {
		svc.BeginShutdown()
		if !svc.lc.Stop() {
			// the service loop never ran: stop the backend here
			_ = svc.bp.Stop()
		}
		// notify the observers that the service has stopped
		svc.eventNotifier.Shutdown()
	})
	return nil
}

// GetAllJobs returns snapshots of all the jobs, in id order (creation order).
func (svc *Service) GetAllJobs() []*job.Job {
	svc.mu.RLock()
	defer svc.mu.RUnlock()

	jobs := make([]*job.Job, 0, len(svc.jobs))
	for _, id := range sortedJobIDs(svc.jobs) {
		jobs = append(jobs, svc.jobs[id].Snapshot())
	}
	return jobs
}

func (svc *Service) GetLockedResources() job.LockedResources {
	// get the locked resources currently in use by the jobs
	svc.mu.RLock()
	defer svc.mu.RUnlock()
	return svc.resourcesManager.GetLockedResources()
}

func (svc *Service) UnlockAllResources() error {
	// this is used to unlock all resources in case of a blocking situation
	// it is not used in normal operation (it is a safety net)
	svc.mu.Lock()
	defer svc.mu.Unlock()
	err := svc.unlockAllResourcesLocked()
	svc.tryEnqueuePendingJobsLocked()
	return err
}

func (svc *Service) unlockAllResourcesLocked() error {
	svc.resourcesManager.UnlockAllResources()
	svc.metrics.OnUnlockAllResources()
	return svc.bp.OnAllResourcesUnlocked()
}

func (svc *Service) UnlockResource(resourceName string) error {
	// this is used to unlock a specific resource
	// it is not used in normal operation (it is a safety net)
	svc.mu.Lock()
	defer svc.mu.Unlock()

	// topic of the job holding the resource (for the metrics)
	topic := ""
	if owner, locked := svc.resourcesManager.GetLockedResources()[resourceName]; locked {
		if j := svc.jobs[owner]; j != nil {
			topic = j.Topic
		}
	}
	if err := svc.resourcesManager.UnlockResource(resourceName); err != nil {
		return &apierror.APIError{
			Message:  err.Error(),
			Code:     constants.ErrorResourceNotFound,
			HttpCode: fiber.StatusNotFound,
			Err:      err,
		}
	}
	svc.metrics.OnResourceUnlocked(resourceName, topic)
	svc.tryEnqueuePendingJobsLocked()
	return nil
}

func (svc *Service) Healthcheck() bool {
	return svc.bp.Healthcheck()
}

func (svc *Service) GetMetrics() metrics.IServiceMetrics {
	return svc.metrics
}

func (svc *Service) GetTopics() []string {
	return svc.metrics.GetTopics()
}

func (svc *Service) CheckJobsRetention() {
	// Check if the retention policy is enabled
	if !svc.conf.Jobs.RetentionPolicy.Enable {
		return
	}

	// Return immediately if this function is already running
	// it will retry on the next call (called periodically)
	if svc.checkJobsRetentionRunning.Load().(bool) {
		return
	}

	// Set the flag to true
	svc.checkJobsRetentionRunning.Store(true)
	defer svc.checkJobsRetentionRunning.Store(false)

	svc.mu.Lock()
	defer svc.mu.Unlock()

	svc.deleteOldCompletedJobsLocked()
}

func (svc *Service) UpdateJobStatistics() {
	svc.mu.RLock()
	defer svc.mu.RUnlock()

	// Update the job statistics
	svc.metrics.UpdateJobStatistics(svc.jobs)
}

func (svc *Service) deleteOldCompletedJobsLocked() {
	// check if the retention policy is enabled
	if !svc.conf.Jobs.RetentionPolicy.Enable {
		return
	}

	// filter the completed jobs
	completedJobs := make([]*job.Job, 0, len(svc.jobs))
	for _, job := range svc.jobs {
		if job.IsCompleted() {
			completedJobs = append(completedJobs, job)
		}
	}

	// Sort jobs by the oldest event in their history
	sort.Slice(completedJobs, func(i, j int) bool {
		return completedJobs[i].GetLastUpdateDate() < completedJobs[j].GetLastUpdateDate()
	})

	// check if the number of jobs is too high
	cptCompletedJobsToDelete := 0
	if svc.conf.Jobs.RetentionPolicy.MaxJobs >= 0 {
		cptCompletedJobsToDelete = max(0, len(completedJobs)-svc.conf.Jobs.RetentionPolicy.MaxJobs)
	}

	// delete the oldest completed jobs
	now := time.Now().UnixMilli()

	maxJobAge := int64(^uint64(0) >> 1)
	if svc.conf.Jobs.RetentionPolicy.MaxJobAge >= 0 {
		maxJobAge = int64(max(0, svc.conf.Jobs.RetentionPolicy.MaxJobAge)) * 1000
	}
	cptDeletedJobs := 0
	for _, j := range completedJobs {
		if cptDeletedJobs < cptCompletedJobsToDelete || now-j.GetLastUpdateDate() >= maxJobAge {
			if err := svc.deleteJobLocked(j.JobUUID); err != nil {
				return
			}
			cptDeletedJobs++
		}
	}
}

func (svc *Service) CheckJobsVisibilityTimeout() {
	// This function is called periodically to check the visibility of the jobs

	// Return immediately if this function is already running
	// it will retry on the next call (called periodically)
	if svc.checkJobsVisibilityRunning.Load().(bool) {
		return
	}

	// Set the flag to true
	svc.checkJobsVisibilityRunning.Store(true)
	defer svc.checkJobsVisibilityRunning.Store(false)

	svc.mu.Lock()
	defer svc.mu.Unlock()

	// Create a list of jobs that have been running for too long
	now := time.Now().UnixMilli()
	runningForTooLongJobs := make([]*job.Job, 0)
	for _, id := range sortedJobIDs(svc.jobs) {
		j := svc.jobs[id]
		// Check if the job is running
		if j.GetState() == job.JobRunning {
			// Check if the job is running for too long
			if now-j.GetLastUpdateDate() >= int64(j.VisibilityTimeout*1000) {
				runningForTooLongJobs = append(runningForTooLongJobs, j)
			}
		}
	}

	for _, j := range runningForTooLongJobs {
		svc.logger.Info(
			"Job is running for too long",
			zap.String("method", "CheckJobsVisibilityTimeout"),
			zap.String("JobUUID", string(j.JobUUID.String())),
		)
		_, _ = svc.failJobLocked(j.JobUUID)
	}

	if len(runningForTooLongJobs) > 0 {
		svc.tryEnqueuePendingJobsLocked()
	}
}

func (svc *Service) CheckDelayedJobs() {
	// This function is called periodically to check the delayed jobs

	// Return immediately if this function is already running
	// it will retry on the next call (called periodically)
	if svc.checkJobsDelayRunning.Load().(bool) {
		return
	}

	// Set the flag to true
	svc.checkJobsDelayRunning.Store(true)
	defer svc.checkJobsDelayRunning.Store(false)

	svc.mu.Lock()
	defer svc.mu.Unlock()

	// Check for delayed jobs

	// loop over the delayed job queue
	// note: the priority queue is ordered by the retry time,
	// so the first job in the delayed queue is not ready to be started yet
	// then the others are not ready too
	minPriority := -time.Now().UnixMilli()
	found := false
	for {
		// get the first job from the priority queue with priority >= minPriority
		// here the priority is the retry time (negative value)
		j, _, ok := svc.pqDelayedJobs.DequeueWithPriority(minPriority)
		if !ok {
			break
		}
		// the job is ready to change it's state into JobPending
		svc.setJobStateToPendingLocked(j)
		found = true
	}
	if found {
		svc.tryEnqueuePendingJobsLocked()
	}
}

func (svc *Service) GetUptime() int64 {
	return int64(time.Since(svc.startTime).Seconds())
}

func (svc *Service) GetBackendProvider() jobbackendprovider.IJobBackendProvider {
	return svc.bp
}

func (svc *Service) Run() error {
	if !svc.lc.Begin() {
		return errors.New("service is already running or stopped")
	}
	defer svc.lc.End()

	defer func() {
		log.Logger.Info(
			"Service stopped",
			zap.String("topic", "service"),
			zap.String("method", "Run"),
		)
	}()

	log.Logger.Info(
		"Service started",
		zap.String("topic", "service"),
		zap.String("method", "Run"),
	)

	// create a ticker for the pending jobs
	pendingJobsTicker := time.NewTicker(time.Duration(1) * time.Second)

	// create a ticker for the job visibility
	jobVisibilityTicket := time.NewTicker(time.Duration(1) * time.Second)

	// create a ticker for delayed jobs
	jobDelayedTicker := time.NewTicker(time.Duration(1) * time.Second)

	// create a ticker for the job retention policy
	jobRetentionTicker := time.NewTicker(time.Duration(max(svc.conf.Jobs.RetentionPolicy.Interval, 60)) * time.Second)

	// create a ticker for job retry
	jobRetryTicker := time.NewTicker(time.Duration(1) * time.Second)

	// create a ticker for job statistics
	jobStatsTicker := time.NewTicker(time.Duration(10) * time.Second)

	// Run the job backend provider in a separate goroutine
	go func() {
		_ = svc.bp.Run()
	}()

	// Notify that the service is ready
	svc.eventNotifier.NotifyEvent(event.ServiceEventReady)

	// The periodic tasks run here, one at a time: they take the service lock and are
	// short. Once this function returns, no task is left running.
	for {
		select {
		case <-pendingJobsTicker.C:
			svc.TryEnqueuePendingJobs()
		case <-jobRetryTicker.C:
			svc.AttemptJobRetries()
		case <-jobVisibilityTicket.C:
			svc.CheckJobsVisibilityTimeout()
		case <-jobDelayedTicker.C:
			svc.CheckDelayedJobs()
		case <-jobRetentionTicker.C:
			svc.CheckJobsRetention()
		case <-jobStatsTicker.C:
			svc.UpdateJobStatistics()
		case <-svc.lc.Stopping():
			// Channel was closed, time to stop
			// received a signal to stop the service
			// stop the tickers
			pendingJobsTicker.Stop()
			jobRetryTicker.Stop()
			jobVisibilityTicket.Stop()
			jobDelayedTicker.Stop()
			jobRetentionTicker.Stop()
			jobStatsTicker.Stop()
			// stop and wait for the job backend provider to end (synchronous)
			_ = svc.bp.Stop()
			// stop the main goroutine (the service is stopped)
			return nil
		case event := <-svc.notifChanBP:
			switch event.Type {
			case jobbackendprovider.EventFatalError:
				svc.logger.Error(
					"Fatal error received from job backend provider",
					zap.String("topic", "job"),
					zap.String("method", "Run"),
					zap.Any("eventType", event),
				)
				// stop the tickers
				pendingJobsTicker.Stop()
				jobRetryTicker.Stop()
				jobVisibilityTicket.Stop()
				jobDelayedTicker.Stop()
				jobRetentionTicker.Stop()
				jobStatsTicker.Stop()
				// stop and wait for the job backend provider to end (synchronous)
				_ = svc.bp.Stop()
				// stop the main goroutine (the service is stopped)
				return errors.New("job backend provider received a fatal error")
			default:
				if svc.conf.Jobs.LogVerbosity > 2 {
					svc.logger.Info(
						"Event received",
						zap.String("topic", "job"),
						zap.String("method", "Run"),
						zap.Any("eventType", event),
					)
				}
			}
		}
	}
}

func requiresLockedResources(lockedResources job.LockedResources, job *job.Job) bool {
	// Helper function to check if a job requires resources locked by another job
	for _, r := range job.LockResources {
		if owner, found := lockedResources[r]; found && owner != job.JobUUID {
			return true
		}
	}

	return false
}

// compareQueueOrder orders jobs as the queue serves them: highest priority first, then the
// oldest, then by id (jobs created in the same millisecond keep a fixed order).
func compareQueueOrder(a, b *job.Job) int {
	if a.Priority != b.Priority {
		return int(b.Priority) - int(a.Priority)
	}
	if ca, cb := a.GetCreationTimestamp(), b.GetCreationTimestamp(); ca != cb {
		return cmp.Compare(ca, cb)
	}
	return bytes.Compare(a.JobUUID[:], b.JobUUID[:])
}

// sortedJobIDs returns the ids of the jobs in increasing order (creation order for
// version 7 ids).
func sortedJobIDs(jobs job.JobMap) []job.JobUUID {
	ids := make([]job.JobUUID, 0, len(jobs))
	for id := range jobs {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b job.JobUUID) int { return bytes.Compare(a[:], b[:]) })
	return ids
}

func jobEqual(a, b *job.Job) bool {
	// Equality function for comparing jobs by pointer address
	if a == nil || b == nil {
		return false
	}
	return a.JobUUID == b.JobUUID
}

func NewService(logger *zap.Logger, conf *config.Config, version string) (*Service, error) {
	bp, err := registry.NewJobBackendProvider(conf)
	if err != nil {
		return nil, err
	}
	return &Service{
		version:          version,
		logger:           logger,
		conf:             conf,
		bp:               bp,
		jobs:             make(job.JobMap),
		pendingJobs:      make(job.JobMap),
		pqRetryJobs:      *pq.NewPriorityQueue(jobEqual),
		pqDelayedJobs:    *pq.NewPriorityQueue(jobEqual),
		notifChanBP:      make(chan jobbackendprovider.Event, 1000),
		queued:           make(chan struct{}),
		lc:               lifecycle.New(),
		stopping:         make(chan struct{}),
		metrics:          metrics.NewServiceMetrics(conf.WebServer.Metrics.Enable),
		callbackService:  callback.NewCallbackService(logger),
		eventLogger:      eventlogger.NewEventLoggerObserver(conf.EventsLogger.FilePath),
		eventNotifier:    event.NewServiceEventNotifier(),
		resourcesManager: NewResourcesManager(logger),
	}, nil
}

func CreateAndInitService(conf *config.Config, version string) (*Service, error) {
	var err error

	svc, err := NewService(log.Logger, conf, version)
	if err != nil {
		log.Logger.Error("Error while instantiate job service",
			zap.String("topic", "service"),
			zap.String("method", "CreateAndStartService"),
			zap.Error(err),
		)
		return nil, err
	}

	err = svc.Init()
	if err != nil {
		log.Logger.Error("Error while initialize stream service",
			zap.String("topic", "service"),
			zap.String("method", "CreateAndStartService"),
			zap.Error(err),
		)
		return nil, err
	}

	log.Logger.Info(
		"Job server started",
		zap.String("topic", "service"),
		zap.String("method", "CreateAndStartService"),
	)

	return svc, nil
}

type GetJobsRequest struct {
	Page   int    // page number (1-based)
	Limit  int    // number of jobs per page
	Status string // filter by job status
	Topic  string // filter by topic
	Sort   string // sort order
	Search string // search by job ID
}

type GetJobsResponse struct {
	Jobs       []*job.Job `json:"jobs"`
	Total      int        `json:"total"`
	Page       int        `json:"page"`
	Limit      int        `json:"limit"`
	TotalPages int        `json:"total_pages"`
}

func (svc *Service) GetJobs(req *GetJobsRequest) (*GetJobsResponse, error) {
	svc.mu.RLock()
	defer svc.mu.RUnlock()

	// Convert jobs map to slice for processing
	allJobs := make([]*job.Job, 0, len(svc.jobs))
	for _, j := range svc.jobs {
		allJobs = append(allJobs, j)
	}

	// Apply filters
	filteredJobs := svc.filterJobs(allJobs, req)

	// Apply sorting
	svc.sortJobs(filteredJobs, req.Sort)

	// Calculate pagination
	total := len(filteredJobs)
	totalPages := (total + req.Limit - 1) / req.Limit
	if req.Limit <= 0 {
		totalPages = 1
	}

	// Apply pagination
	start := (req.Page - 1) * req.Limit
	end := start + req.Limit
	if start < 0 {
		start = 0
	}
	if end > total {
		end = total
	}

	// snapshots of the jobs of the page (the caller reads them after the lock is released)
	paginatedJobs := []*job.Job{}
	if start < total {
		for _, j := range filteredJobs[start:end] {
			paginatedJobs = append(paginatedJobs, j.Snapshot())
		}
	}

	return &GetJobsResponse{
		Jobs:       paginatedJobs,
		Total:      total,
		Page:       req.Page,
		Limit:      req.Limit,
		TotalPages: totalPages,
	}, nil
}

func (svc *Service) filterJobs(jobs []*job.Job, req *GetJobsRequest) []*job.Job {
	filtered := make([]*job.Job, 0, len(jobs))

	for _, j := range jobs {
		// Filter by status
		if req.Status != "" {
			targetState := stringToJobState(req.Status)
			if targetState == job.JobNoState || j.GetState() != targetState {
				continue
			}
		}

		// Filter by topic
		if req.Topic != "" && j.Topic != req.Topic {
			continue
		}

		// Filter by search (job ID)
		if req.Search != "" {
			jobID := j.JobUUID.String()
			if !contains(jobID, req.Search) {
				continue
			}
		}

		filtered = append(filtered, j)
	}

	return filtered
}

// stringToJobState converts a string status to JobState
func stringToJobState(status string) job.JobState {
	switch strings.ToLower(status) {
	case "created":
		return job.JobCreated
	case "delayed":
		return job.JobDelayed
	case "pending":
		return job.JobPending
	case "queued":
		return job.JobQueued
	case "running":
		return job.JobRunning
	case "succeeded":
		return job.JobSucceeded
	case "failed":
		return job.JobFailed
	case "canceled":
		return job.JobCanceled
	case "deleted":
		return job.JobDeleted
	case "hidden":
		return job.JobHidden
	default:
		return job.JobNoState
	}
}
func (svc *Service) sortJobs(jobs []*job.Job, sortParam string) {
	if sortParam == "" {
		sortParam = "created_desc" // Default sort
	}

	// Parse sort criteria
	sortCriteria := strings.Split(sortParam, ",")
	for i := range sortCriteria {
		sortCriteria[i] = strings.TrimSpace(sortCriteria[i])
	}

	sort.Slice(jobs, func(i, j int) bool {
		for _, criterion := range sortCriteria {
			var result int

			switch criterion {
			case "created_asc":
				if jobs[i].GetCreationTimestamp() < jobs[j].GetCreationTimestamp() {
					return true
				} else if jobs[i].GetCreationTimestamp() > jobs[j].GetCreationTimestamp() {
					return false
				}
				// Equal, continue to next criterion

			case "created_desc":
				if jobs[i].GetCreationTimestamp() > jobs[j].GetCreationTimestamp() {
					return true
				} else if jobs[i].GetCreationTimestamp() < jobs[j].GetCreationTimestamp() {
					return false
				}
				// Equal, continue to next criterion

			case "priority_asc":
				if jobs[i].Priority < jobs[j].Priority {
					return true
				} else if jobs[i].Priority > jobs[j].Priority {
					return false
				}
				// Equal, continue to next criterion

			case "priority_desc":
				if jobs[i].Priority > jobs[j].Priority {
					return true
				} else if jobs[i].Priority < jobs[j].Priority {
					return false
				}
				// Equal, continue to next criterion

			case "updated_asc":
				if jobs[i].GetLastUpdateDate() < jobs[j].GetLastUpdateDate() {
					return true
				} else if jobs[i].GetLastUpdateDate() > jobs[j].GetLastUpdateDate() {
					return false
				}
				// Equal, continue to next criterion

			case "updated_desc":
				if jobs[i].GetLastUpdateDate() > jobs[j].GetLastUpdateDate() {
					return true
				} else if jobs[i].GetLastUpdateDate() < jobs[j].GetLastUpdateDate() {
					return false
				}
				// Equal, continue to next criterion

			case "topic_asc":
				result = strings.Compare(jobs[i].Topic, jobs[j].Topic)
				if result < 0 {
					return true
				} else if result > 0 {
					return false
				}
				// Equal, continue to next criterion

			case "topic_desc":
				result = strings.Compare(jobs[j].Topic, jobs[i].Topic)
				if result < 0 {
					return true
				} else if result > 0 {
					return false
				}
				// Equal, continue to next criterion

			default:
				// Unknown criterion, skip to next one
				continue
			}
		}

		// All criteria were equal: creation time, then id, so that the order is the same
		// for every request
		if ci, cj := jobs[i].GetCreationTimestamp(), jobs[j].GetCreationTimestamp(); ci != cj {
			return ci < cj
		}
		return bytes.Compare(jobs[i].JobUUID[:], jobs[j].JobUUID[:]) < 0
	})
}

// Helper function for case-insensitive string contains
func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
