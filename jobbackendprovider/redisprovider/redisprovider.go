package redisprovider

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/nbigot/minijob/config"
	"github.com/nbigot/minijob/constants"
	"github.com/nbigot/minijob/event"
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/jobbackendprovider"
	"github.com/nbigot/minijob/lifecycle"
	"github.com/nbigot/minijob/web/apierror"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	jobKeyPattern = "job:*" // keys of the jobs
	scanCount     = 1000    // keys asked per SCAN call
	mgetCount     = 500     // keys read per MGET call
)

// RedisJobBackendProvider stores each job in a Redis string (key job:<uuid>, value the job
// JSON). The changes are written by a writer goroutine, in order (see writer).
type RedisJobBackendProvider struct {
	// implements IJobBackendProvider & IServiceEventObserver interfaces
	logger         *zap.Logger
	logVerbosity   int
	maxJobs        uint // maxJobs is the maximum number of jobs that can be stored in the backend
	finishedJobTTL uint // FinishedJobTTL is the time to live for finished jobs in seconds
	redisClient    *redis.Client
	redisOptions   *redis.Options
	writer         *writer
	lc             *lifecycle.Lifecycle // runs Run once, stops it once
	closeOnce      sync.Once
	notifChan      chan jobbackendprovider.Event
	ctx            context.Context
	restoreFlag    bool // restoreFlag is a flag to indicate if the provider is in restore mode
}

func (p *RedisJobBackendProvider) SetNotifChan(notifChan chan jobbackendprovider.Event) {
	p.notifChan = notifChan
}

func (p *RedisJobBackendProvider) Init() error {
	p.ctx = context.Background()

	p.redisClient = redis.NewClient(p.redisOptions)
	p.writer = newWriter(p.logger, p.redisClient)

	// Keyspace notifications are enabled for the other clients of the database; minijob
	// does not use them, and a managed Redis may refuse the command: a warning only.
	if err := p.redisClient.ConfigSet(p.ctx, "notify-keyspace-events", "KEA").Err(); err != nil {
		p.logger.Warn(
			"Cannot enable Redis keyspace notifications",
			zap.String("topic", "backendProvider"),
			zap.String("method", "Init"),
			zap.Error(err),
		)
	}
	// var err error

	// if err = s.ClearJobs(); err != nil {
	// 	return err
	// }

	// if err = s.catalog.Init(); err != nil {
	// 	return err
	// }

	return nil
}

func (p *RedisJobBackendProvider) Shutdown() {
	_ = p.Stop()
}

func (p *RedisJobBackendProvider) Stop() error {
	// stop Run, write the pending changes, then close the client (once: Stop is called by
	// the service and again by the observers shutdown)
	p.lc.Stop()
	var err error
	p.closeOnce.Do(func() {
		if p.writer != nil {
			p.writer.stop()
		}
		if p.redisClient == nil {
			return
		}
		if err = p.redisClient.Close(); err != nil {
			p.logger.Error("Error closing Redis client", zap.Error(err))
		}
	})
	if err != nil {
		return err
	}

	// var err error
	// if err = s.catalog.Stop(); err != nil {
	// 	return err
	// }

	// if err = s.ClearJobs(); err != nil {
	// 	return err
	// }

	return nil
}

func (p *RedisJobBackendProvider) SetRestoreFlag(enabled bool) {
	p.restoreFlag = enabled
}

func (p *RedisJobBackendProvider) JobExists(jobUUID job.JobUUID) (bool, error) {
	// check if job exists in redis
	exists, err := p.redisClient.Exists(p.ctx, JobUUID2RedisKey(jobUUID)).Result()
	if err != nil {
		p.logger.Error("Error checking if job exists in Redis", zap.Error(err))
		return false, &apierror.APIError{
			Message:  "cannot create job",
			Code:     constants.ErrorDatabaseUnavailable,
			HttpCode: fiber.StatusServiceUnavailable,
			JobUUID:  jobUUID,
			Err:      errors.New("error checking if job exists in Redis"),
		}
	}

	return exists > 0, nil
}

// LoadJobs reads every job (SCAN, then MGET), then starts the writer.
func (p *RedisJobBackendProvider) LoadJobs() (job.JobMap, error) {
	jobKeys, err := p.scanJobKeys()
	if err != nil {
		p.logger.Error("Error getting job keys from Redis", zap.Error(err))
		return nil, &apierror.APIError{
			Message:  "cannot load jobs",
			Code:     constants.ErrorDatabaseUnavailable,
			HttpCode: fiber.StatusServiceUnavailable,
			Err:      errors.New("error getting job keys from Redis"),
		}
	}

	jobMap, err := p.getJobs(jobKeys)
	if err != nil {
		return nil, err
	}
	p.writer.start()
	return jobMap, nil
}

// scanJobKeys returns the job keys, sorted (SCAN may return a key more than once).
func (p *RedisJobBackendProvider) scanJobKeys() ([]string, error) {
	var keys []string
	iter := p.redisClient.Scan(p.ctx, 0, jobKeyPattern, scanCount).Iterator()
	for iter.Next(p.ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	slices.Sort(keys)
	return slices.Compact(keys), nil
}

// getJobs reads the jobs of the keys; a key that expired since the scan is skipped.
func (p *RedisJobBackendProvider) getJobs(keys []string) (job.JobMap, error) {
	jobMap := make(job.JobMap, len(keys))
	for start := 0; start < len(keys); start += mgetCount {
		values, err := p.redisClient.MGet(p.ctx, keys[start:min(start+mgetCount, len(keys))]...).Result()
		if err != nil {
			p.logger.Error("Error getting job from Redis", zap.Error(err))
			return nil, &apierror.APIError{
				Message:  "cannot load job",
				Code:     constants.ErrorDatabaseUnavailable,
				HttpCode: fiber.StatusServiceUnavailable,
				Err:      errors.New("error getting job from Redis"),
			}
		}

		for _, value := range values {
			jobJSON, found := value.(string)
			if !found {
				continue
			}
			j, err := job.NewJob([]byte(jobJSON))
			if err != nil {
				p.logger.Error("Error load job from JSON", zap.Error(err))
				return nil, &apierror.APIError{
					Message:  "cannot load job",
					Code:     constants.ErrorCantCreateJob,
					HttpCode: fiber.StatusBadRequest,
					Err:      err,
				}
			}
			jobMap[j.JobUUID] = j
		}
	}

	return jobMap, nil
}

// func (s *RedisJobBackendProvider) SaveStreamCatalog() error {
// 	return s.catalog.SaveStreamCatalog()
// }

// func (s *RedisJobBackendProvider) OnJobCreated(info *types.JobUUID) error {
// 	s.mu.Lock()
// 	defer s.mu.Unlock()

// 	if inMemoryStream, err := NewInMemoryStream(info, s.maxRecordsByStream, s.maxSizeInBytes); err != nil {
// 		return err
// 	} else {
// 		s.inMemoryJobs[info.UUID] = inMemoryStream
// 	}

// 	return s.catalog.OnJobCreated(info)
// }

// func (s *RedisJobBackendProvider) LoadJobsFromUUIDs(jobUUIDs types.JobUUIDList) (types.StreamInfoList, error) {
// 	infos := make(types.StreamInfoList, len(jobUUIDs))
// 	for idx, jobUUID := range jobUUIDs {
// 		if info, err := s.GetStreamInfo(jobUUID); err != nil {
// 			return nil, err
// 		} else {
// 			infos[idx] = info
// 		}
// 	}
// 	return infos, nil
// }

// func (s *RedisJobBackendProvider) GetStreamInfo(jobUUID types.JobUUID) (*types.Job, error) {
// 	return s.catalog.GetStreamInfo(jobUUID)
// }

// func (s *RedisJobBackendProvider) ClearJobs() error {
// 	s.mu.Lock()
// 	defer s.mu.Unlock()

// 	s.inMemoryJobs = make(map[types.JobUUID]*InMemoryStream, 0)
// 	return nil
// }

func (p *RedisJobBackendProvider) OnJobCanceled(j *job.Job) error {
	return p.SaveJob(j)
}

func (p *RedisJobBackendProvider) OnJobFailed(j *job.Job) error {
	return p.SaveJob(j)
}

func (p *RedisJobBackendProvider) OnJobHidden(j *job.Job) error {
	return p.SaveJob(j)
}

func (p *RedisJobBackendProvider) OnJobTerminated(j *job.Job) error {
	return p.SaveJob(j)
}

func (p *RedisJobBackendProvider) OnJobDeleted(jobUUID job.JobUUID) error {
	p.writer.push(writeOp{key: JobUUID2RedisKey(jobUUID), del: true})
	return nil
}

func (p *RedisJobBackendProvider) OnJobStarted(j *job.Job) error {
	return p.SaveJob(j)
}

func (p *RedisJobBackendProvider) OnJobSucceeded(j *job.Job) error {
	// the job is kept finishedJobTTL seconds (0: forever)
	return p.saveJob(j, time.Duration(p.finishedJobTTL)*time.Second)
}

func (p *RedisJobBackendProvider) OnJobTimeout(j *job.Job) error {
	return p.SaveJob(j)
}

func (p *RedisJobBackendProvider) OnJobCreated(j *job.Job) error {
	return p.SaveJob(j)
}

func (p *RedisJobBackendProvider) OnJobDelayed(j *job.Job) error {
	return p.SaveJob(j)
}

func (p *RedisJobBackendProvider) OnJobPending(j *job.Job) error {
	return p.SaveJob(j)
}

func (p *RedisJobBackendProvider) OnJobEnqueued(j *job.Job) error {
	return p.SaveJob(j)
}

func (p *RedisJobBackendProvider) SaveJob(j *job.Job) error {
	return p.saveJob(j, 0)
}

// saveJob queues the write of the job as it is now (the caller holds the service lock).
func (p *RedisJobBackendProvider) saveJob(j *job.Job, ttl time.Duration) error {
	// serialize job info into a JSON string
	jobJSON, err := j.ToJSON()
	if err != nil {
		p.logger.Error("Error serializing job info into JSON", zap.Error(err))
		return err
	}

	p.writer.push(writeOp{key: JobUUID2RedisKey(j.JobUUID), value: jobJSON, ttl: ttl})
	return nil
}

func (p *RedisJobBackendProvider) OnJobsDeleted() error {
	// delete all keys starting with "job:"
	p.writer.push(writeOp{delAll: true})
	return nil
}

// WaitWrites returns once the changes made before the call were written, or failed once
// (they are then retried in the background).
func (p *RedisJobBackendProvider) WaitWrites() {
	if p.writer != nil {
		p.writer.wait()
	}
}

func (p *RedisJobBackendProvider) OnAllResourcesUnlocked() error {
	// TODO
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventAllResourcesUnlocked})
	return nil
}

func (p *RedisJobBackendProvider) OnResourceUnlocked(j *job.Job, resource string) error {
	// TODO
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventResourceUnlocked, JobUUID: j.JobUUID, Resource: &resource})
	return nil
}

// Healthcheck is false when Redis cannot be reached or when changes could not be written
// yet (they are retried).
func (p *RedisJobBackendProvider) Healthcheck() bool {
	// not redis client is not set yet then assume it is healthy
	if p.redisClient == nil {
		return true
	}

	if _, err := p.redisClient.Ping(p.ctx).Result(); err != nil {
		p.logger.Error("Error pinging Redis", zap.Error(err))
		return false
	}

	return p.writer.healthy()
}

func (p *RedisJobBackendProvider) NotifyChange(event jobbackendprovider.Event) {
	if p.logVerbosity >= 2 {
		p.logger.Debug(
			"notify event",
			zap.String("topic", "backendProvider"),
			zap.String("method", "NotifyChange"),
			zap.Any("event", event),
		)
	}

	// informational only: never block the caller (the service holds its lock)
	select {
	case p.notifChan <- event:
	default:
	}
}

func (p *RedisJobBackendProvider) Run() error {
	// this function must be called in a goroutine; the changes are written by the writer
	// (started by LoadJobs), Run only waits for the stop
	if !p.lc.Begin() {
		return errors.New("provider is already running or stopped")
	}
	defer p.lc.End()

	<-p.lc.Stopping()
	return nil
}

func (p *RedisJobBackendProvider) NotifyEvent(ev event.ServiceEventType) {
	if p.restoreFlag {
		return
	}

	switch ev {
	case event.ServiceEventJobDeletedAll:
		_ = p.OnJobsDeleted() // Ignore error
	}
}

func (p *RedisJobBackendProvider) NotifyTopicEvent(ev event.ServiceEventType, topic string) {
}

func (p *RedisJobBackendProvider) NotifyJobEvent(j *job.Job, ev event.ServiceEventType) {
	if p.restoreFlag {
		return
	}

	switch ev {
	case event.ServiceEventJobCreated:
		_ = p.OnJobCreated(j)
	case event.ServiceEventJobDelayed:
		_ = p.OnJobDelayed(j)
	case event.ServiceEventJobPending:
		_ = p.OnJobPending(j)
	case event.ServiceEventJobEnqueued:
		_ = p.OnJobEnqueued(j)
	case event.ServiceEventJobDeleted:
		_ = p.OnJobDeleted(j.JobUUID)
	case event.ServiceEventJobStarted:
		_ = p.OnJobStarted(j)
	case event.ServiceEventJobSucceeded:
		_ = p.OnJobSucceeded(j)
	case event.ServiceEventJobCanceled:
		_ = p.OnJobCanceled(j)
	case event.ServiceEventJobFailed:
		_ = p.OnJobFailed(j)
	case event.ServiceEventJobHidden:
		_ = p.OnJobHidden(j)
	case event.ServiceEventJobTerminated:
		_ = p.OnJobTerminated(j)
	}
}

func (p *RedisJobBackendProvider) GetDiskUsage() int64 {
	return 0
}

func (p *RedisJobBackendProvider) GetType() string {
	return "Redis"
}

func JobUUID2RedisKey(jobUUID job.JobUUID) string {
	return "job:" + jobUUID.String()
}

func NewRedisJobBackendProvider(logger *zap.Logger, conf *config.Config) (jobbackendprovider.IJobBackendProvider, error) {
	if conf.Backend.Redis.MaxJobs == 0 {
		return nil, fmt.Errorf("invalid value for configuration backend.redis.maxJobs: %d", conf.Backend.Redis.MaxJobs)
	}

	options, err := redis.ParseURL(conf.Backend.Redis.URL)
	if err != nil {
		return nil, fmt.Errorf("error while parsing redis URL: %s", err.Error())
	}

	return &RedisJobBackendProvider{
		logger:         logger,
		logVerbosity:   conf.Backend.LogVerbosity,
		maxJobs:        conf.Backend.Redis.MaxJobs,
		finishedJobTTL: uint(max(0, conf.Backend.Redis.FinishedJobTTL)),
		redisOptions:   options,
		lc:             lifecycle.New(),
		restoreFlag:    false,
	}, nil
}

func (p *RedisJobBackendProvider) GetMaxJobs() uint {
	return p.maxJobs
}
