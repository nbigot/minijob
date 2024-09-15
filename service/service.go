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
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/jobbackendprovider"
	"github.com/nbigot/minijob/jobbackendprovider/registry"
	"github.com/nbigot/minijob/log"

	"github.com/nbigot/minijob/web/apierror"

	"go.uber.org/zap"
)

type Service struct {
	// implements IService interface
	jobs            job.JobMap                             // hashmap of jobs
	pendingJobs     job.JobMap                             // hashmap of pending jobs
	lockedResources job.LockedResources                    // map of locked resources
	mu              sync.Mutex                             // to ensure safe concurrent manipulation of jobs
	mapMutex        sync.RWMutex                           // mutex to protect hashmap of jobs
	notifChanBP     chan jobbackendprovider.Event          // notification channel for job backend provider
	notifChanSvc    chan ServiceEvent                      // notification channel for metrics
	metrics         ServiceMetrics                         // metrics
	wg              sync.WaitGroup                         // wg is a wait group to wait for the Run function to finish
	stopChan        chan struct{}                          // stopChan is a channel to stop the Run function
	running         atomic.Bool                            // Add this to track if the service is running
	bp              jobbackendprovider.IJobBackendProvider // backend provider
	conf            *config.Config                         // configuration
	logger          *zap.Logger                            // logger
}

func (svc *Service) Init() error {
	var err error

	if err = svc.bp.Init(svc.notifChanBP); err != nil {
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

	return nil
}

func (svc *Service) GetJobsCount() uint {
	svc.mapMutex.RLock()
	defer svc.mapMutex.RUnlock()
	return uint(len(svc.jobs))
}

func (svc *Service) LoadJobs() error {
	svc.mapMutex.RLock()
	defer svc.mapMutex.RUnlock()

	var err error
	svc.jobs, err = svc.bp.LoadJobs()
	if err != nil {
		return err
	}

	// compute metrics
	svc.lockedResources = svc.ComputeLockedResources(svc.jobs)
	svc.metrics.ResourcesLockedCount = uint(len(svc.lockedResources))

	svc.metrics.JobsCount = uint(len(svc.jobs))
	svc.pendingJobs = make(job.JobMap)
	var jobState job.JobState
	for _, j := range svc.jobs {
		if jobState, err = j.GetState(); err != nil {
			return err
		}
		svc.metrics.JobsCounterCreated++
		switch jobState {
		case job.JobPending:
			svc.metrics.JobsCounterPending++
			svc.pendingJobs[j.JobUUID] = j
		case job.JobQueued:
			svc.metrics.JobsCounterQueued++
		case job.JobRunning:
			svc.metrics.JobsCounterRunning++
		case job.JobSucceeded:
			svc.metrics.JobsCounterSucceeded++
		case job.JobFailed:
			svc.metrics.JobsCounterFailed++
		case job.JobCanceled:
			svc.metrics.JobsCounterCanceled++
		}
		svc.metrics.JobsCounterFaillure += j.GetCountFaillures()
	}

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
	svc.pendingJobs[j.JobUUID] = j
	svc.setJobMap(j.JobUUID, j)

	svc.logger.Info(
		"Job created",
		zap.String("topic", "service"),
		zap.String("method", "FinalizeJobCreation"),
		zap.String("JobUUID", j.JobUUID.String()),
	)

	// log the job value
	if svc.conf.Jobs.LogVerbosity > 1 {
		svc.logger.Info(
			"New job",
			zap.Any("Job", j),
		)
	}

	if err = svc.bp.OnJobCreated(j); err != nil {
		return nil, err
	}

	// send a notification to the channel that a new job has been created
	svc.metrics.JobsCount++
	svc.metrics.JobsCounterCreated++
	svc.metrics.JobsCounterPending++
	svc.notifChanSvc <- ServiceEvent{
		Type:    ServiceEventJobCreated,
		Metrics: svc.metrics,
		JobUUID: j.JobUUID,
	}

	// try to enqueue the job, it does not matter if this action fails (therefore ignore error),
	// this job will be started later eventually when the needed resources are available when the
	// other job that locks the resources is completed
	_ = svc.EnqueueJob(j)

	return j, nil
}

func (svc *Service) TryEnqueuePendingJobs() {
	// this function check if a pending job can be enqueued
	// if a job is pending and the resources are available the job is enqueued
	svc.mu.Lock()
	defer svc.mu.Unlock()

	// optimization: if there are no pending jobs, return
	if len(svc.pendingJobs) == 0 {
		return
	}

	// create a list of pending jobs from variable svc.pendingJobs for sorting
	pendingJobs := make([]*job.Job, 0, len(svc.pendingJobs))
	for _, j := range svc.pendingJobs {
		pendingJobs = append(pendingJobs, j)
	}

	// sort the pending jobs by (priority, creation date)
	sort.Slice(pendingJobs, func(i, j int) bool {
		if pendingJobs[i].Priority == pendingJobs[j].Priority {
			return pendingJobs[i].GetCreationDate() < pendingJobs[j].GetCreationDate()
		}
		return pendingJobs[i].Priority > pendingJobs[j].Priority
	})

	// try to enqueue the pending jobs
	for _, j := range pendingJobs {
		_ = svc.EnqueueJob(j)
	}
}

func (svc *Service) EnqueueJob(j *job.Job) error {
	// Attempt to enqueue the job.
	// If it fails, it means that the job is not ready to be enqueued due to resource unavailability.
	var err error

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

	// Check if the job can be enqueued
	if len(j.LockResources) > 0 {
		// Check if the resources are available
		for _, r := range j.LockResources {
			if _, found := svc.lockedResources[r]; found {
				return &apierror.APIError{
					Message:  "cannot enqueue job",
					Code:     constants.ErrorCantEnqueueJob,
					HttpCode: fiber.StatusPreconditionFailed,
					JobUUID:  j.JobUUID,
					Err:      errors.New("resource not available: " + r),
				}
			}
		}
	}

	// Lock the resources
	for _, r := range j.LockResources {
		svc.lockedResources[r] = j.JobUUID
	}

	// Enqueue the job by adding a history event
	j.AddHistoryEvent(job.JobEventEnqueue, time.Now().UnixMilli())

	// Remove the job from the pending jobs
	delete(svc.pendingJobs, j.JobUUID)

	// Notify the backend provider
	if err = svc.bp.OnJobEnqueued(j); err != nil {
		return err
	}

	// Update metrics
	svc.metrics.JobsCounterPending--
	svc.metrics.JobsCounterQueued++
	svc.notifChanSvc <- ServiceEvent{
		Type:    ServiceEventJobEnqueued,
		Metrics: svc.metrics,
		JobUUID: j.JobUUID,
	}

	svc.logger.Info(
		"Job enqueued",
		zap.String("topic", "service"),
		zap.String("method", "EnqueueJob"),
		zap.String("JobUUID", j.JobUUID.String()),
	)

	return nil
}

func (svc *Service) GenerateNewJobUuid() (job.JobUUID, error) {
	// ensure new job uuid is unique
	for {
		candidate, err := uuid.NewV7()
		if err != nil {
			return job.JobUUID(candidate), &apierror.APIError{
				Message:  "cannot create job",
				Code:     constants.ErrorCantCreateJob,
				HttpCode: fiber.StatusInternalServerError,
				Err:      err,
			}
		}
		exists, err2 := svc.bp.JobExists(candidate)
		if err2 != nil {
			return job.JobUUID(candidate), err2
		}
		if !exists {
			return job.JobUUID(candidate), nil
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

	if err = svc.UnlockJobResources(j); err != nil {
		return err
	}

	// Remove the job from the pending jobs (if it is in the pending jobs)
	delete(svc.pendingJobs, j.JobUUID)

	if err = svc.bp.OnJobDeleted(jobUUID); err != nil {
		return err
	}

	// delete uuid from hashmap
	svc.setJobMap(jobUUID, nil)

	svc.logger.Info(
		"Job deleted",
		zap.String("topic", "service"),
		zap.String("method", "DeleteJob"),
		zap.String("JobUUID", jobUUID.String()),
	)

	svc.metrics.JobsCount--
	svc.metrics.JobsCounterDeleted++
	return nil
}

func (svc *Service) DeleteAllJobs() error {
	svc.mapMutex.RLock()
	defer svc.mapMutex.RUnlock()

	if err := svc.bp.OnJobsDeleted(); err != nil {
		return err
	}

	cptJobsDeleted := uint(len(svc.jobs))
	svc.jobs = make(job.JobMap)
	svc.pendingJobs = make(job.JobMap)
	svc.lockedResources = make(job.LockedResources)
	svc.metrics.JobsCount -= cptJobsDeleted
	svc.metrics.JobsCounterDeleted += cptJobsDeleted
	return nil
}

func (svc *Service) UnlockJobResources(j *job.Job) error {
	for _, resource := range j.LockResources {
		// delete the resource from the locked resources
		delete(svc.lockedResources, resource)
		// notify the backend provider
		if err := svc.bp.OnResourceUnlocked(j, resource); err != nil {
			return err
		}
	}

	return nil
}

func (svc *Service) PullJobs(req *RequestPullJobs) (*ResponsePullJobs, error) {
	// pull one or multiple jobs from the queue and start it/them
	if req.JobUUID != nil {
		return svc.PullSpecificJob(req)
	}

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
			// rare but might happen if a previous job from candidates has locked resources

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

	return res, nil
}

func (svc *Service) FindBestJobCandidates(req *RequestPullJobs) ([]*job.Job, error) {
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
		state, _ := j.GetState()
		if state != job.JobQueued {
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

	// second step: sort the jobs by (priority, creation date)
	sort.Slice(bestCandidates, func(i, j int) bool {
		if bestCandidates[i].Priority == bestCandidates[j].Priority {
			return bestCandidates[i].GetCreationDate() < bestCandidates[j].GetCreationDate()
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

	if j, found := svc.jobs[jobUUID]; found {
		if err := svc.StartJob(jobUUID, req); err != nil {
			return nil, err
		}

		return &ResponsePullJobs{
			Jobs: []*job.Job{j},
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

	var err error

	// get the job
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
			"Cannot start job",
			zap.String("topic", "service"),
			zap.String("method", "StartJob"),
			zap.String("JobUUID", jobUUID.String()),
			zap.Error(err),
		)

		return &apiErr
	}

	// check if the job is able to be started (from its state)
	state, _ := j.GetState()
	if state != job.JobQueued {
		apiErr := apierror.APIError{
			Message:  "job already started once",
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

	// start the job (add a history event)
	j.AddHistoryEvent(job.JobEventStart, time.Now().UnixMilli())

	j.VisibilityTimeout = req.VisibilityTimeout

	// notify the backend provider
	if err = svc.bp.OnJobStarted(j); err != nil {
		return err
	}

	// update metrics
	svc.metrics.JobsCounterQueued--
	svc.metrics.JobsCounterRunning++
	svc.notifChanSvc <- ServiceEvent{
		Type:    ServiceEventJobStarted,
		Metrics: svc.metrics,
		JobUUID: j.JobUUID,
	}

	svc.logger.Info(
		"Job started",
		zap.String("topic", "service"),
		zap.String("method", "StartJob"),
		zap.String("JobUUID", jobUUID.String()),
	)

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
	state, _ := j.GetState()
	if state != job.JobRunning {
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
	if err = svc.UnlockJobResources(j); err != nil {
		return err
	}

	// finish the job (add a history event)
	j.AddHistoryEvent(job.JobEventSuccess, time.Now().UnixMilli())

	// notify the backend provider
	if err = svc.bp.OnJobSucceeded(j); err != nil {
		return err
	}

	// update metrics
	svc.metrics.JobsCounterRunning--
	svc.metrics.JobsCounterSucceeded++
	svc.notifChanSvc <- ServiceEvent{
		Type:    ServiceEventJobSucceeded,
		Metrics: svc.metrics,
		JobUUID: j.JobUUID,
	}

	svc.logger.Info(
		"Job succeeded",
		zap.String("topic", "service"),
		zap.String("method", "SetJobAsSuccessful"),
		zap.String("JobUUID", jobUUID.String()),
	)

	svc.TryEnqueuePendingJobs()

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
	state, _ := j.GetState()
	if state != job.JobRunning {
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

	// notify the backend provider
	if err = svc.bp.OnJobCanceled(j); err != nil {
		return err
	}

	// update metrics
	svc.metrics.JobsCounterRunning--
	svc.metrics.JobsCounterCanceled++
	svc.notifChanSvc <- ServiceEvent{
		Type:    ServiceEventJobCanceled,
		Metrics: svc.metrics,
		JobUUID: j.JobUUID,
	}

	svc.logger.Info(
		"Job canceled",
		zap.String("topic", "service"),
		zap.String("method", "CancelJob"),
		zap.String("JobUUID", jobUUID.String()),
	)

	// Enqueue the job by adding a history event
	j.AddHistoryEvent(job.JobEventEnqueue, time.Now().UnixMilli())

	// Notify the backend provider
	if err = svc.bp.OnJobEnqueued(j); err != nil {
		return err
	}

	// Update metrics
	svc.metrics.JobsCounterQueued++
	svc.notifChanSvc <- ServiceEvent{
		Type:    ServiceEventJobEnqueued,
		Metrics: svc.metrics,
		JobUUID: j.JobUUID,
	}

	svc.logger.Info(
		"Job enqueued",
		zap.String("topic", "service"),
		zap.String("method", "CancelJob"),
		zap.String("JobUUID", j.JobUUID.String()),
	)

	svc.TryEnqueuePendingJobs()

	return nil
}

func (svc *Service) FailJob(jobUUID job.JobUUID) error {
	return nil // TODO
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
		return errors.New("service is not running")
	}

	// send stop notification
	close(svc.stopChan)
	// wait for the Run function to finish
	svc.wg.Wait()
	return nil
}

func (svc *Service) setJobMap(jobUUID job.JobUUID, job *job.Job) {
	svc.mapMutex.Lock()
	if job == nil {
		delete(svc.jobs, jobUUID)
	} else {
		svc.jobs[jobUUID] = job
	}
	svc.mapMutex.Unlock()
}

func (svc *Service) GetAllJobs() ([]*job.Job, error) {
	// convert the jobs hashmap into a slice
	svc.mapMutex.RLock()
	defer svc.mapMutex.RUnlock()

	jobs := make([]*job.Job, 0, len(svc.jobs))
	for _, job := range svc.jobs {
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (svc *Service) ComputeLockedResources(jobs job.JobMap) job.LockedResources {
	lockedResources := job.LockedResources{}
	for _, j := range jobs {
		state, err := j.GetState()
		if err != nil {
			continue
		}
		switch state {
		case job.JobQueued, job.JobRunning:
			for _, r := range j.LockResources {
				lockedResources[r] = j.JobUUID
			}
		}
	}

	return lockedResources
}

func (svc *Service) GetLockedResources() (job.LockedResources, error) {
	// get the locked resources currently in use by the jobs
	return svc.lockedResources, nil
}

func (svc *Service) UnlockAllResources() error {
	// this is used to unlock all resources in case of a blocking situation
	// it is not used in normal operation (it is a safety net)
	svc.lockedResources = make(job.LockedResources)
	return svc.bp.OnAllResourcesUnlocked()
}

func (svc *Service) GetServiceEventChan() chan ServiceEvent {
	return svc.notifChanSvc
}

func (svc *Service) Healthcheck() bool {
	return svc.bp.Healthcheck()
}

func (svc *Service) ComputeMetrics() error {
	// svc.metrics            *metrics.Metrics
	return nil // TODO svc.bp.ComputeMetrics()
}

func (svc *Service) CheckJobsRetention() error {
	// Check if the retention policy is enabled
	if !svc.conf.Jobs.RetentionPolicy.Enable {
		return nil
	}

	svc.mapMutex.RLock()
	defer svc.mapMutex.RUnlock()

	// this function is called periodically to check the retention policy
	// if a job is completed and too old, it should be deleted
	now := time.Now().UnixMilli()
	defaultJobRetentionDurationMs := int64(max(0, svc.conf.Jobs.RetentionPolicy.MaxJobAge)) * 1000

	var jobsToDelete []job.JobUUID

	// check if the jobs are too old
	for _, j := range svc.jobs {
		if now-j.GetLastUpdateDate() >= defaultJobRetentionDurationMs {
			// flag the job for deletion
			jobsToDelete = append(jobsToDelete, j.JobUUID)
		}
	}

	// delete the old jobs
	for _, jobUUID := range jobsToDelete {
		if err := svc.DeleteJob(jobUUID); err != nil {
			return err
		}
	}

	// check if the number of completed remaining jobs is too high
	if svc.conf.Jobs.RetentionPolicy.MaxJobs <= 0 {
		return nil
	}

	// check if the number of jobs is too high
	cptCompletedJobsToDelete := len(svc.jobs) - svc.conf.Jobs.RetentionPolicy.MaxJobs
	if cptCompletedJobsToDelete <= 0 {
		return nil
	}

	// sort the jobs by last update date
	// then delete the oldest jobs
	// Extract completed jobs into a slice
	completedJobs := make([]*job.Job, 0, len(svc.jobs))
	for _, job := range svc.jobs {
		// only consider completed jobs
		if job.IsCompleted() {
			completedJobs = append(completedJobs, job)
		}
	}

	// Sort jobs by the oldest event in their history
	sort.Slice(completedJobs, func(i, j int) bool {
		return completedJobs[i].GetLastUpdateDate() < completedJobs[j].GetLastUpdateDate()
	})

	cptDeletedJobs := 0
	for _, j := range completedJobs {
		if err := svc.DeleteJob(j.JobUUID); err != nil {
			return err
		}
		cptDeletedJobs++
		if cptDeletedJobs >= cptCompletedJobsToDelete {
			break
		}
	}

	return nil
}

func (svc *Service) Watchdog() {
	// Check if the watchdog is enabled
	if !svc.conf.Watchdog.Enable {
		return
	}

	svc.mapMutex.RLock()
	defer svc.mapMutex.RUnlock()

	// TODO
	// if a job is too old and blocked by a resource unavailability, it should be deleted
	// if a job is too old and blocked by a resource unavailability, an alarm should be sent

	// For each job:
	// for _, j := range svc.jobs {

	// 	if j.IsTooOld() {
	// 		// check if the job is too old
	// 		// send an alarm
	// 		// TODO
	// 	}

	// 	if j.HasTooManyRetries() {
	// 		// check if the job has too many retries
	// 		// send an alarm
	// 		// TODO
	// 	}

	// 	state, err := j.GetState()
	// 	if err != nil {
	// 		return err
	// 	}

	// 	if state == job.JobStateStarted {
	// 		// check if the job is in state started
	// 		// send an alarm
	// 		// TODO

	// 		// check if the job is still running
	// 		// if the job is not running, send an alarm
	// 		// TODO

	// 	}

	// 	if j.IsRunningForTooLong() {
	// 		// check if the job is running for too long
	// 		// send an alarm
	// 		// TODO

	// 		// kill the job
	// 		// TODO
	// 	}
	// }

	// - check if the job is too old
	// - check if the job has too many retries
	// - check if the job is in state started
	// - if any of the above is true, send an alarm
	// - if the job is in state started, check if the job is still running
	// - if the job is not running, send an alarm
	// - if the job is running, check if the job is running for too long
	// - if the job is running for too long, send an alarm
	// - if the job is running for too long, kill the job

	// add an alarm to the alarm list

	// this function is called periodically to check the health of the service
	// if the service is not healthy, it should return an alarm
	// perform health check on jobs
	// if a job is stuck, return an alarm
	// Check for any job started long time ago,
	// assume it's interrupted or broken and notify it for eventual restart (visibility timeout).
	// watchdog alarm --> notify channel event --> restart (visibility timeout) job

	// job_max_duration_alarm_seconds := svc.conf.Jobs.MaxDurationAlarmSeconds

	// jobsStartedTooLongAgo := make([]job.JobUUID, 0)

	// jobsCreatedTooLongAgoAndBlockedByResourceInavailabilty := make([]job.JobUUID, 0)
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

	// create a ticker for the watchdog
	watchdogTicker := time.NewTicker(time.Duration(max(svc.conf.Watchdog.Interval, 5)) * time.Second)

	// create a ticker for the job retention policy
	jobRetentionTicker := time.NewTicker(time.Duration(max(svc.conf.Jobs.RetentionPolicy.Interval, 60)) * time.Second)

	// Run the job backend provider in a separate goroutine
	go svc.bp.Run()

	for {
		select {
		case <-pendingJobsTicker.C:
			svc.TryEnqueuePendingJobs()
		case <-watchdogTicker.C:
			svc.Watchdog()
		case <-jobRetentionTicker.C:
			_ = svc.CheckJobsRetention()
		case <-svc.stopChan:
			// Channel was closed, time to stop
			// received a signal to stop the service
			// stop the tickers
			pendingJobsTicker.Stop()
			watchdogTicker.Stop()
			jobRetentionTicker.Stop()
			// stop and wait for the job backend provider to end (synchronous)
			svc.bp.Stop()
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
				watchdogTicker.Stop()
				jobRetentionTicker.Stop()
				// stop and wait for the job backend provider to end (synchronous)
				svc.bp.Stop()
				// stop the main goroutine (the service is stopped)
				return errors.New("job backend provider received a fatal error")
			default:
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

func NewService(logger *zap.Logger, conf *config.Config) (*Service, error) {
	bp, err := registry.NewJobBackendProvider(conf)
	if err != nil {
		return nil, err
	}
	return &Service{
		logger:       logger,
		conf:         conf,
		bp:           bp,
		jobs:         make(job.JobMap),
		pendingJobs:  make(job.JobMap),
		notifChanBP:  make(chan jobbackendprovider.Event, 100),
		notifChanSvc: make(chan ServiceEvent, 100),
		stopChan:     make(chan struct{}),
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
