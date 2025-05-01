package service

import (
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/nbigot/minijob/config"
	"github.com/nbigot/minijob/constants"
	"github.com/nbigot/minijob/event"
	"github.com/nbigot/minijob/eventlogger"
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/jobbackendprovider"
	"github.com/nbigot/minijob/jobbackendprovider/registry"
	"github.com/nbigot/minijob/log"
	"github.com/nbigot/minijob/metrics"
	"github.com/nbigot/minijob/pq"

	"github.com/nbigot/minijob/web/apierror"

	"go.uber.org/zap"
)

type Service struct {
	// implements IService interface
	jobs                        job.JobMap                             // hashmap of jobs
	pendingJobs                 job.JobMap                             // hashmap of pending jobs
	pqRetryJobs                 pq.PriorityQueue[*job.Job]             // priority queue of retry jobs
	pqDelayedJobs               pq.PriorityQueue[*job.Job]             // priority queue of delayed jobs
	mu                          sync.Mutex                             // to ensure safe concurrent manipulation of jobs
	muPullJobs                  sync.Mutex                             // to ensure safe concurrent job pulling
	mapMutex                    sync.RWMutex                           // mutex to protect hashmap of jobs
	checkJobsRetentionRunning   atomic.Value                           // atomic value to track if the check jobs retention is running
	checkJobsVisibilityRunning  atomic.Value                           // atomic value to track if the check jobs visibility is running
	checkJobsDelayRunning       atomic.Value                           // atomic value to track if the check delayed jobs is running
	notifChanBP                 chan jobbackendprovider.Event          // notification channel for job backend provider
	notifyTryEnqueuePendingJobs chan struct{}                          // channel to notify the TryEnqueuePendingJobs function
	metrics                     metrics.IServiceMetrics                // service to manage metrics
	eventLogger                 *eventlogger.EventLoggerObserver       // event logger
	eventNotifier               event.IServiceEventNotifier            // service to manage events
	wg                          sync.WaitGroup                         // wg is a wait group to wait for the Run function to finish
	stopChan                    chan struct{}                          // stopChan is a channel to stop the Run function
	running                     atomic.Bool                            // Add this to track if the service is running
	bp                          jobbackendprovider.IJobBackendProvider // backend provider
	conf                        *config.Config                         // configuration
	logger                      *zap.Logger                            // logger
	resourcesManager            *ResourcesManager                      // resources manager
}

func (svc *Service) Init() error {
	var err error

	svc.running.Store(false)
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
	svc.eventNotifier.Init()

	err = svc.LoadJobs()
	if err != nil {
		log.Logger.Error("Error while loading jobs",
			zap.String("topic", "service"),
			zap.String("method", "Init"),
			zap.Error(err),
		)
		return err
	}

	return nil
}

func (svc *Service) GetJobsCount() uint {
	svc.mapMutex.RLock()
	defer svc.mapMutex.RUnlock()
	return uint(len(svc.jobs))
}

func (svc *Service) UpdateResourcesLockedCountMetric(topic string, inc int) {
	svc.metrics.UpdateResourcesLockedCountMetric(topic, inc)
}

func (svc *Service) LoadJobs() error {
	svc.mapMutex.RLock()
	defer svc.mapMutex.RUnlock()

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
	for _, j := range svc.jobs {
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
	svc.mu.Lock()
	defer svc.mu.Unlock()

	var err error

	// check if a job can be created
	if svc.conf.Jobs.MaxAllowedJobs > 0 && uint(svc.GetJobsCount()) >= svc.conf.Jobs.MaxAllowedJobs {
		err = errors.New("cannot create job, limit reached")
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

	var j *job.Job
	j, err = job.NewJobFromRequest(payload)
	if err != nil {
		svc.logger.Error(
			"Cannot create job",
			zap.String("topic", "service"),
			zap.String("method", "CreateJob"),
			zap.Error(err),
		)
		return nil, err
	}

	return svc.FinalizeJobCreation(j)
}

func (svc *Service) FinalizeJobCreation(j *job.Job) (*job.Job, error) {
	now := time.Now().UnixMilli()
	newJobUuid, err := svc.GenerateNewJobUuid()
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

	svc.mapMutex.Lock()
	svc.jobs[j.JobUUID] = j
	svc.mapMutex.Unlock()

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
		svc.SetJobStateToDelayed(j)
	} else {
		svc.SetJobStateToPending(j)
	}

	return j, nil
}

func (svc *Service) SetJobStateToDelayed(j *job.Job) {
	// delay the job by adding a history event
	j.AddHistoryEvent(job.JobEventDelay, time.Now().UnixMilli())
	// the job is delayed
	svc.pqDelayedJobs.Enqueue(j, -j.StartAfter)
	// send a notification that the job has been delayed
	svc.eventNotifier.NotifyJobEvent(j, event.ServiceEventJobDelayed)
}

func (svc *Service) SetJobStateToPending(j *job.Job) {
	// add a history event
	j.AddHistoryEvent(job.JobEventPending, time.Now().UnixMilli())
	// the job is pending
	svc.pendingJobs[j.JobUUID] = j
	svc.notifyTryEnqueuePendingJobs <- struct{}{}
	// send a notification that the job is pending
	svc.eventNotifier.NotifyJobEvent(j, event.ServiceEventJobPending)
}

func (svc *Service) TryEnqueuePendingJobs() {
	// This function check if a pending job can be enqueued
	// if a job is pending and the resources are available the job is enqueued
	svc.mu.Lock()
	defer svc.mu.Unlock()

	// optimization: if there are no pending jobs, return
	cptPendingJobs := len(svc.pendingJobs)
	if cptPendingJobs == 0 {
		return
	}

	// create a list of pending jobs from variable svc.pendingJobs for sorting
	lockedResourcesCopy := svc.resourcesManager.GetLockedResources()
	pendingJobs := make([]*job.Job, 0, len(svc.pendingJobs))
	for _, j := range svc.pendingJobs {
		if !requiresLockedResources(lockedResourcesCopy, j) { // BUG: fatal error: concurrent map read and map write
			pendingJobs = append(pendingJobs, j)
		}
	}

	// sort the pending jobs by (priority, creation timestamp)
	sort.Slice(pendingJobs, func(i, j int) bool {
		if pendingJobs[i].Priority == pendingJobs[j].Priority {
			return pendingJobs[i].GetCreationTimestamp() < pendingJobs[j].GetCreationTimestamp()
		}
		return pendingJobs[i].Priority > pendingJobs[j].Priority
	})

	// try to enqueue the pending jobs (may fail if resources are not available)
	for _, j := range pendingJobs {
		_ = svc.EnqueueJob(j)
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
	for {
		// get the first job from the priority queue with priority >= minPriority
		// here the priority is the retry time (negative value)
		j, priority, ok := svc.pqRetryJobs.DequeueWithPriority(minPriority)
		if !ok {
			break
		}
		if err := svc.RetryJob(j); err != nil {
			// the job cannot be retried
			// so enqueue it back in the priority queue
			svc.pqRetryJobs.Enqueue(j, priority)
			// retry later
			return
		}
	}
}

func (svc *Service) RetryJob(j *job.Job) error {
	// Retry the job immediately
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

	return nil
}

func (svc *Service) lockResources(j *job.Job) *apierror.APIError {
	// Lock the resources required by the job
	if err := svc.resourcesManager.LockResources(j); err != nil {
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
	svc.UpdateResourcesLockedCountMetric(j.Topic, len(j.LockResources)) // TODO

	return nil
}

func (svc *Service) EnqueueJob(j *job.Job) error {
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
	if apiError := svc.lockResources(j); apiError != nil {
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

	return nil
}

func (svc *Service) GenerateNewJobUuid() (job.JobUUID, error) {
	// ensure new job uuid is unique
	svc.mapMutex.RLock()
	defer svc.mapMutex.RUnlock()

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
	var err error

	j, err := svc.GetJob(jobUUID)
	if j == nil {
		apiErr := apierror.APIError{
			Message:  "job not found",
			Code:     constants.ErrorCantDeleteJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
			Err:      err,
		}
		svc.logger.Error(
			"Cannot delete job",
			zap.String("topic", "service"),
			zap.String("method", "DeleteJob"),
			zap.String("JobUUID", jobUUID.String()),
			zap.Error(err),
		)
		return &apiErr
	}

	svc.UnlockJobResources(j)

	// delete the job (add a history event)
	j.AddHistoryEvent(job.JobEventDelete, time.Now().UnixMilli())

	svc.mapMutex.Lock()

	// Remove the job from the retry queue (if it is in the retry queue)
	svc.pqRetryJobs.Remove(j)

	// Remove the job from the delayed queue (if it is in the delayed queue)
	svc.pqDelayedJobs.Remove(j)

	// delete uuid from hashmap
	delete(svc.jobs, jobUUID)

	// Remove the job from the pending jobs (if it is in the pending jobs)
	delete(svc.pendingJobs, jobUUID)

	svc.mapMutex.Unlock()

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

	return nil
}

func (svc *Service) DeleteAllJobs() error {
	svc.mapMutex.Lock()
	defer svc.mapMutex.Unlock()

	svc.pqRetryJobs.Clear()
	svc.pqDelayedJobs.Clear()
	svc.jobs = make(job.JobMap)
	svc.pendingJobs = make(job.JobMap)
	svc.UnlockAllResources()
	svc.eventNotifier.NotifyEvent(event.ServiceEventJobDeletedAll)
	return nil
}

func (svc *Service) UnlockJobResources(j *job.Job) {
	cptJobLockResources := len(j.LockResources)
	if cptJobLockResources == 0 {
		return
	}

	svc.resourcesManager.UnlockResources(j)

	// Update the resources locked count metric
	svc.UpdateResourcesLockedCountMetric(j.Topic, -cptJobLockResources)

	for _, resource := range j.LockResources {
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

	// pull any job from the queue

	// prevent the case where multiple workers pull the same job
	svc.muPullJobs.Lock()
	defer svc.muPullJobs.Unlock()

	// try to find the best job candidates
	var candidates []*job.Job
	var err error

	waitTime := req.WaitTimeSeconds
	for {
		// note: when no candidate is found, err is not nil
		candidates, err = svc.FindBestJobCandidates(req)
		if len(candidates) > 0 {
			// at least one job candidate is ready to start
			break
		}
		if waitTime == 0 {
			// no job candidate found in the time limit
			svc.eventNotifier.NotifyTopicEvent(event.ServiceEventEmptyPollReceived, req.Topic)
			return nil, err
		}
		time.Sleep(1 * time.Second)
		waitTime--
	}

	res := &ResponsePullJobs{
		Jobs: make([]*job.Job, 0),
	}

	// try to start the jobs from the candidates
	// note: at this point at least one job candidate should be able to start
	for _, j := range candidates {
		if err := svc.StartJob(j.JobUUID, req); err != nil {
			// rare but might happen if a previous job from candidates has locked resources,
			// or when multiple workers attempt to start a job,
			// but the server assigns the same job to all of them

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
		res.Jobs = append(res.Jobs, j)
	}

	if len(res.Jobs) == 0 {
		svc.eventNotifier.NotifyTopicEvent(event.ServiceEventEmptyPollReceived, req.Topic)

		return nil, &apierror.APIError{
			Message:  "no job found that is ready or matches the criteria",
			Code:     constants.ErrorCantPullAnyJob,
			HttpCode: fiber.StatusNotFound,
			JobUUID:  job.JobUUID{},
		}
	}

	return res, nil
}

func (svc *Service) FindBestJobCandidates(req *RequestPullJobs) ([]*job.Job, error) {
	// find the best job candidates to start
	svc.mapMutex.RLock()
	defer svc.mapMutex.RUnlock()

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

	// second step: sort the jobs by (priority, creation timestamp)
	sort.Slice(bestCandidates, func(i, j int) bool {
		if bestCandidates[i].Priority == bestCandidates[j].Priority {
			return bestCandidates[i].GetCreationTimestamp() < bestCandidates[j].GetCreationTimestamp()
		}
		return bestCandidates[i].Priority > bestCandidates[j].Priority
	})

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

	svc.mapMutex.RLock()
	if j, found := svc.jobs[jobUUID]; found {
		svc.mapMutex.RUnlock()
		if err := svc.StartJob(jobUUID, req); err != nil {
			return nil, err
		}

		return &ResponsePullJobs{
			Jobs: []*job.Job{j},
		}, nil
	}
	svc.mapMutex.RUnlock()

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

	var err error

	// get the job
	j, err := svc.GetJob(jobUUID)
	if j == nil {
		apiErr := apierror.APIError{
			Message:  "job not found",
			Code:     constants.ErrorCantStartJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
			Err:      err,
		}
		svc.logger.Error(
			"Cannot start job",
			zap.String("topic", "service"),
			zap.String("method", "StartJob"),
			zap.String("JobUUID", jobUUID.String()),
			zap.Error(err),
		)

		return &apiErr
	}

	// check if the job is able to be started (from its state)
	if j.GetState() != job.JobQueued {
		// This occurs when the job is already started,
		// for instance, when multiple workers attempt to start a job,
		// but the server assigns the same job to all of them.
		apiErr := apierror.APIError{
			Message:  "job already started",
			Code:     constants.ErrorCantStartJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
			Err:      err,
		}
		svc.logger.Error(
			"Cannot start job",
			zap.String("topic", "service"),
			zap.String("method", "StartJob"),
			zap.String("JobUUID", jobUUID.String()),
			zap.Error(err),
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
	var err error

	// get the job
	j, err := svc.GetJob(jobUUID)
	if j == nil {
		apiErr := apierror.APIError{
			Message:  "job not found",
			Code:     constants.ErrorCantCloneJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
			Err:      err,
		}
		svc.logger.Error(
			"Cannot clone job",
			zap.String("topic", "service"),
			zap.String("method", "CloneJob"),
			zap.String("JobUUID", jobUUID.String()),
			zap.Error(err),
		)

		return nil, &apiErr
	}

	clone := j.Clone()
	return svc.FinalizeJobCreation(clone)
}

func (svc *Service) SetJobAsSuccessful(jobUUID job.JobUUID) error {
	var err error

	// get the job
	j, err := svc.GetJob(jobUUID)
	if j == nil {
		apiErr := apierror.APIError{
			Message:  "job not found",
			Code:     constants.ErrorCantSetJobAsSuccessful,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
			Err:      err,
		}
		svc.logger.Error(
			"Cannot set job as successful",
			zap.String("topic", "service"),
			zap.String("method", "SetJobAsSuccessful"),
			zap.String("JobUUID", jobUUID.String()),
			zap.Error(err),
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
			Err:      err,
		}
		svc.logger.Error(
			"Cannot set job as successful",
			zap.String("topic", "service"),
			zap.String("method", "SetJobAsSuccessful"),
			zap.String("JobUUID", jobUUID.String()),
			zap.Error(err),
		)

		return &apiErr
	}

	// unlock the resources
	svc.UnlockJobResources(j)

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

	svc.notifyTryEnqueuePendingJobs <- struct{}{}
	return nil
}

func (svc *Service) CancelJob(jobUUID job.JobUUID) error {
	// cancel a job that is running and put it back in the queue
	var err error

	// get the job
	j, err := svc.GetJob(jobUUID)
	if j == nil {
		apiErr := apierror.APIError{
			Message:  "job not found",
			Code:     constants.ErrorCantCancelJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
			Err:      err,
		}
		svc.logger.Error(
			"Cannot cancel job",
			zap.String("topic", "service"),
			zap.String("method", "CancelJob"),
			zap.String("JobUUID", jobUUID.String()),
			zap.Error(err),
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
			Err:      err,
		}
		svc.logger.Error(
			"Cannot cancel job",
			zap.String("topic", "service"),
			zap.String("method", "CancelJob"),
			zap.String("JobUUID", jobUUID.String()),
			zap.Error(err),
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

	svc.notifyTryEnqueuePendingJobs <- struct{}{}
	return nil
}

func (svc *Service) FailJob(jobUUID job.JobUUID) (reachedMaxRetry bool, err error) {
	// Mark a running job as failed

	// get the job
	j, err := svc.GetJob(jobUUID)
	if j == nil {
		apiErr := apierror.APIError{
			Message:  "job not found",
			Code:     constants.ErrorCantFailJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
			Err:      err,
		}
		svc.logger.Error(
			"Cannot set job as failed",
			zap.String("topic", "service"),
			zap.String("method", "FailJob"),
			zap.String("JobUUID", jobUUID.String()),
			zap.Error(err),
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
			Err:      err,
		}
		svc.logger.Error(
			"Cannot set job as failed",
			zap.String("topic", "service"),
			zap.String("method", "FailJob"),
			zap.String("JobUUID", jobUUID.String()),
			zap.Error(err),
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
		return svc.OnJobFailedTooManyTimes(j)
	}

	// The job has not yet failed too many times
	delay := svc.conf.Jobs.RetryPolicy.BackoffStrategy.ComputeDelay(j.GetCountFaillures())

	// Check if the minimum duration between each retry is 0
	if delay == 0 {
		// Retry the job immediately
		return false, svc.RetryJob(j)
	}

	// compute the next retry time (current time + duration between retry)
	now := time.Now().UnixMilli()
	nextRetryTime := now + delay.Milliseconds()
	return false, svc.RetryJobLater(j, nextRetryTime)
}

func (svc *Service) OnJobFailedTooManyTimes(j *job.Job) (reachedMaxRetry bool, err error) {
	// the job has failed too many times, set it as terminated
	now := time.Now().UnixMilli()
	j.AddHistoryEvent(job.JobEventTerminate, now)

	// unlock the resources
	svc.UnlockJobResources(j)

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

	svc.notifyTryEnqueuePendingJobs <- struct{}{}
	return true, nil
}

func (svc *Service) RetryJobLater(j *job.Job, nextRetryTime int64) error {
	if svc.conf.Jobs.LogVerbosity > 1 {
		svc.logger.Info(
			"Job added to the retry queue",
			zap.String("topic", "service"),
			zap.String("method", "FailJob"),
			zap.String("JobUUID", j.JobUUID.String()),
			zap.String("NextRetryTime", time.UnixMilli(nextRetryTime).String()),
		)
	}

	// TODO: il faut unlock les resources car si le delay > 10 alors tous les autres jobs ayant besoin de ces resources seront bloqués pendant 10 secondes
	// set job state to JobHidden
	//j.SetState(job.JobHidden)
	now := time.Now().UnixMilli()
	j.AddHistoryEvent(job.JobEventHide, now)

	// unlock the resources
	svc.UnlockJobResources(j)

	// notify event
	svc.eventNotifier.NotifyJobEvent(j, event.ServiceEventJobHidden)

	// Add the job to a waiting list (priority queue) for retry
	// Tip: the priority queue is sorted by the next retry time,
	// the job with the smallest next retry time is at the top,
	// that's why we use a negative value for the next retry time
	svc.pqRetryJobs.Enqueue(j, -nextRetryTime)

	return nil
}

func (svc *Service) ChangeVisibilityTimeoutJob(jobUUID job.JobUUID, visibilityTimeout uint) error {
	// get the job
	j, err := svc.GetJob(jobUUID)
	if j == nil {
		apiErr := apierror.APIError{
			Message:  "job not found",
			Code:     constants.ErrorCantCloneJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
			Err:      err,
		}
		svc.logger.Error(
			"Cannot change job visibility timeout",
			zap.String("topic", "service"),
			zap.String("method", "ChangeVisibilityTimeoutJob"),
			zap.String("JobUUID", jobUUID.String()),
			zap.Error(err),
		)

		return &apiErr
	}

	j.VisibilityTimeout = visibilityTimeout

	return nil
}

func (svc *Service) GetJob(uuid job.JobUUID) (*job.Job, error) {
	svc.mapMutex.RLock()
	defer svc.mapMutex.RUnlock()

	if s, found := svc.jobs[uuid]; found {
		return s, nil
	}

	return nil, nil
}

func (svc *Service) GetJobsUUIDs() job.JobUUIDList {
	svc.mapMutex.RLock()

	uuids := make([]job.JobUUID, 0, len(svc.jobs))
	for k := range svc.jobs {
		uuids = append(uuids, k)
	}

	svc.mapMutex.RUnlock()
	return uuids
}

func (svc *Service) GetLogger() *zap.Logger {
	return svc.logger
}

func (svc *Service) Finalize() error {
	return svc.Stop()
}

func (svc *Service) Stop() error {
	if !svc.running.Load() {
		// service is not running
		return nil
	}

	// send stop notification
	close(svc.stopChan)
	// wait for the Run function to finish
	svc.wg.Wait()
	// notify the observers that the service has stopped
	svc.eventNotifier.Shutdown()
	return nil
}

func (svc *Service) GetAllJobs() []*job.Job {
	// convert the jobs hashmap into a slice
	svc.mapMutex.RLock()
	defer svc.mapMutex.RUnlock()

	jobs := make([]*job.Job, 0, len(svc.jobs))
	for _, job := range svc.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

func (svc *Service) GetLockedResources() job.LockedResources {
	// get the locked resources currently in use by the jobs
	return svc.resourcesManager.GetLockedResources()
}

func (svc *Service) UnlockAllResources() error {
	// this is used to unlock all resources in case of a blocking situation
	// it is not used in normal operation (it is a safety net)
	svc.resourcesManager.UnlockAllResources()
	return svc.bp.OnAllResourcesUnlocked()
}

func (svc *Service) Healthcheck() bool {
	return svc.bp.Healthcheck()
}

func (svc *Service) GetMetrics() metrics.IServiceMetrics {
	return svc.metrics
}

func (svc *Service) GetJobsTopics() []string {
	return svc.metrics.GetJobsTopics()
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

	svc.DeleteOldCompletedJobs()
}

func (svc *Service) UpdateJobStatistics() {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	// Update the job statistics
	svc.metrics.UpdateJobStatistics(svc.jobs)
}

func (svc *Service) DeleteOldCompletedJobs() {
	// check if the retention policy is enabled
	if !svc.conf.Jobs.RetentionPolicy.Enable {
		return
	}

	// filter the completed jobs
	svc.mapMutex.RLock()
	completedJobs := make([]*job.Job, 0, len(svc.jobs))
	for _, job := range svc.jobs {
		if job.IsCompleted() {
			completedJobs = append(completedJobs, job)
		}
	}
	svc.mapMutex.RUnlock()

	// Sort jobs by the oldest event in their history
	sort.Slice(completedJobs, func(i, j int) bool {
		return completedJobs[i].GetLastUpdateDate() < completedJobs[j].GetLastUpdateDate()
	})

	// check if the number of completed remaining jobs is too high

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
			if err := svc.DeleteJob(j.JobUUID); err != nil {
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

	// Create a list of jobs that have been running for too long
	svc.mapMutex.RLock()
	now := time.Now().UnixMilli()
	runningForTooLongJobs := make([]*job.Job, 0, len(svc.jobs))
	for _, j := range svc.jobs {
		// Check if the job is running
		if j.GetState() == job.JobRunning {
			// Check if the job is running for too long
			if now-j.GetLastUpdateDate() >= int64(j.VisibilityTimeout*1000) {
				runningForTooLongJobs = append(runningForTooLongJobs, j)
			}
		}
	}
	svc.mapMutex.RUnlock()

	svc.mu.Unlock()

	for _, j := range runningForTooLongJobs {
		svc.logger.Info(
			"Job is running for too long",
			zap.String("method", "CheckJobsVisibilityTimeout"),
			zap.String("JobUUID", string(j.JobUUID.String())),
		)
		_, _ = svc.FailJob(j.JobUUID)
	}

	if len(runningForTooLongJobs) > 0 {
		svc.notifyTryEnqueuePendingJobs <- struct{}{}
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

	// Check for delayed jobs

	// loop over the delayed job queue
	// note: the priority queue is ordered by the retry time,
	// so the first job in the delayed queue is not ready to be started yet
	// then the others are not ready too
	minPriority := -time.Now().UnixMilli()
	for {
		// get the first job from the priority queue with priority >= minPriority
		// here the priority is the retry time (negative value)
		j, _, ok := svc.pqDelayedJobs.DequeueWithPriority(minPriority)
		if !ok {
			break
		}
		// the job is ready to change it's state into JobPending
		svc.SetJobStateToPending(j)
	}

	svc.mu.Unlock()
}

func (svc *Service) Run() error {
	if !svc.running.CompareAndSwap(false, true) {
		return errors.New("service is already running")
	}
	defer svc.running.Store(false)

	defer func() {
		log.Logger.Info(
			"Service stopped",
			zap.String("topic", "service"),
			zap.String("method", "Run"),
		)
	}()

	// this function must be called in a goroutine
	svc.wg.Add(1)
	defer svc.wg.Done()

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

	// note: calling methods on the service should be done in a goroutine
	// to avoid blocking the main goroutine,
	// specially to be able to process channel events (svc.notifChanBP)
	for {
		select {
		case <-svc.notifyTryEnqueuePendingJobs:
			go svc.TryEnqueuePendingJobs()
		case <-pendingJobsTicker.C:
			svc.notifyTryEnqueuePendingJobs <- struct{}{}
		case <-jobRetryTicker.C:
			go svc.AttemptJobRetries()
		case <-jobVisibilityTicket.C:
			go svc.CheckJobsVisibilityTimeout()
		case <-jobDelayedTicker.C:
			go svc.CheckDelayedJobs()
		case <-jobRetentionTicker.C:
			go svc.CheckJobsRetention()
		case <-jobStatsTicker.C:
			go svc.UpdateJobStatistics()
		case <-svc.stopChan:
			// Channel was closed, time to stop
			// received a signal to stop the service
			// stop the tickers
			pendingJobsTicker.Stop()
			jobRetryTicker.Stop()
			jobVisibilityTicket.Stop()
			jobRetentionTicker.Stop()
			// stop and wait for the job backend provider to end (synchronous)
			_ = svc.bp.Stop()
			// stop the service itself
			// must be ok (synchronous)
			// svc.wg.Wait()
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
				jobRetentionTicker.Stop()
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
	// Helper function to check if a job requires locked resources
	for _, r := range job.LockResources { // BUG: fatal error: concurrent map read and map write
		if _, found := lockedResources[r]; found {
			return true
		}
	}

	return false
}

func NewService(logger *zap.Logger, conf *config.Config) (*Service, error) {
	bp, err := registry.NewJobBackendProvider(conf)
	if err != nil {
		return nil, err
	}
	return &Service{
		logger:                      logger,
		conf:                        conf,
		bp:                          bp,
		jobs:                        make(job.JobMap),
		pendingJobs:                 make(job.JobMap),
		notifChanBP:                 make(chan jobbackendprovider.Event, 1000),
		notifyTryEnqueuePendingJobs: make(chan struct{}, 1000),
		stopChan:                    make(chan struct{}),
		metrics:                     metrics.NewSericeMetrics(conf.WebServer.Metrics.Enable),
		eventLogger:                 eventlogger.NewEventLoggerObserver(conf.EventsLogger.FilePath),
		eventNotifier:               event.NewServiceEventNotifier(),
		resourcesManager:            NewResourcesManager(logger),
	}, nil
}

func CreateAndInitService(conf *config.Config) (*Service, error) {
	var err error

	svc, err := NewService(log.Logger, conf)
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
