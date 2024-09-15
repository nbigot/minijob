package redisprovider

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/nbigot/minijob/config"
	"github.com/nbigot/minijob/constants"
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/jobbackendprovider"
	"github.com/nbigot/minijob/web/apierror"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type RedisJobBackendProvider struct {
	// implements IJobBackendProvider interface
	logger         *zap.Logger
	logVerbosity   int
	mu             sync.Mutex
	maxJobs        uint // maxJobs is the maximum number of jobs that can be stored in the backend
	finishedJobTTL uint // FinishedJobTTL is the time to live for finished jobs in seconds
	redisClient    *redis.Client
	redisOptions   *redis.Options
	wg             sync.WaitGroup
	stopChan       chan bool
	notifChan      chan jobbackendprovider.Event
	ctx            context.Context
}

func (p *RedisJobBackendProvider) Init(notifChan chan jobbackendprovider.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ctx = context.Background()
	p.notifChan = notifChan

	p.redisClient = redis.NewClient(p.redisOptions)

	// Enable keyspace notifications for the key
	err := p.redisClient.ConfigSet(p.ctx, "notify-keyspace-events", "KEA").Err()
	if err != nil {
		return &apierror.APIError{
			Message:  "cannot configure redis",
			Code:     constants.ErrorDatabaseUnavailable,
			HttpCode: fiber.StatusServiceUnavailable,
			Err:      errors.New("failed to enable keyspace notifications"),
		}
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

func (p *RedisJobBackendProvider) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// send stop notification
	p.stopChan <- true
	// wait for the Run function to finish
	p.wg.Wait()

	// close redis client
	if err := p.redisClient.Close(); err != nil {
		p.logger.Error("Error closing Redis client", zap.Error(err))
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

func (p *RedisJobBackendProvider) LoadJobs() (job.JobMap, error) {
	// load all jobs from redis
	jobMap := make(job.JobMap)
	jobKeys, err := p.redisClient.Keys(p.ctx, "job:*").Result()
	if err != nil {
		p.logger.Error("Error getting job keys from Redis", zap.Error(err))
		return nil, &apierror.APIError{
			Message:  "cannot load jobs",
			Code:     constants.ErrorDatabaseUnavailable,
			HttpCode: fiber.StatusServiceUnavailable,
			Err:      errors.New("error getting job keys from Redis"),
		}
	}

	for _, jobKey := range jobKeys {
		jobJSON, err := p.redisClient.Get(p.ctx, jobKey).Result()
		if err != nil {
			p.logger.Error("Error getting job from Redis", zap.Error(err))
			return nil, &apierror.APIError{
				Message:  "cannot load job",
				Code:     constants.ErrorDatabaseUnavailable,
				HttpCode: fiber.StatusServiceUnavailable,
				Err:      errors.New("error getting job from Redis"),
			}
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

func (p *RedisJobBackendProvider) OnJobDeleted(jobUUID job.JobUUID) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// delete job from redis
	if err := p.redisClient.Del(p.ctx, JobUUID2RedisKey(jobUUID)).Err(); err != nil {
		p.logger.Error("Error deleting job from Redis", zap.Error(err))
		return err
	}

	return nil
}

func (p *RedisJobBackendProvider) OnJobStarted(j *job.Job) error {
	return p.SaveJob(j)
}

func (p *RedisJobBackendProvider) OnJobSucceeded(j *job.Job) error {
	if err := p.SaveJob(j); err != nil {
		return err
	}
	return p.SetJobTTL(j)
}

func (p *RedisJobBackendProvider) OnJobTimeout(j *job.Job) error {
	return p.SaveJob(j)
}

func (p *RedisJobBackendProvider) OnJobCreated(j *job.Job) error {
	return p.SaveJob(j)
}

func (p *RedisJobBackendProvider) OnJobEnqueued(j *job.Job) error {
	return p.SaveJob(j)
}

func (p *RedisJobBackendProvider) SaveJob(j *job.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// serialize job info into a JSON string
	jobJSON, err := j.ToJSON()
	if err != nil {
		p.logger.Error("Error serializing job info into JSON", zap.Error(err))
		return err
	}

	// save job to redis
	if err := p.redisClient.Set(p.ctx, JobUUID2RedisKey(j.JobUUID), jobJSON, 0).Err(); err != nil {
		p.logger.Error("Error saving job to Redis", zap.Error(err))
		return err
	}

	return nil
}

func (p *RedisJobBackendProvider) SetJobTTL(j *job.Job) error {
	if p.finishedJobTTL == 0 {
		// no TTL set (keep job forever in redis)
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// set TTL for job
	duration := time.Duration(p.finishedJobTTL) * time.Second
	if err := p.redisClient.Expire(p.ctx, JobUUID2RedisKey(j.JobUUID), duration).Err(); err != nil {
		p.logger.Error("Error setting TTL for job in Redis", zap.Error(err))
		return err
	}

	return nil
}

func (p *RedisJobBackendProvider) OnJobsDeleted() error {
	// delete all jobs from redis
	p.mu.Lock()
	defer p.mu.Unlock()

	// delete all keys starting with "job:"
	jobKeys, err := p.redisClient.Keys(p.ctx, "job:*").Result()
	if err != nil {
		p.logger.Error("Error getting job keys from Redis", zap.Error(err))
		return err
	}

	for _, jobKey := range jobKeys {
		if err := p.redisClient.Del(p.ctx, jobKey).Err(); err != nil {
			p.logger.Error("Error deleting job from Redis", zap.Error(err))
			return err
		}
	}

	return nil
}

func (p *RedisJobBackendProvider) OnAllResourcesUnlocked() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// TODO
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventAllResourcesUnlocked})
	return nil
}

func (p *RedisJobBackendProvider) OnResourceUnlocked(j *job.Job, resource string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// TODO
	p.NotifyChange(jobbackendprovider.Event{Type: jobbackendprovider.EventResourceUnlocked, JobUUID: j.JobUUID, Resource: &resource})
	return nil
}

func (p *RedisJobBackendProvider) Healthcheck() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	// not redis client is not set yet then assume it is healthy
	if p.redisClient == nil {
		return true
	}

	if _, err := p.redisClient.Ping(p.ctx).Result(); err != nil {
		p.logger.Error("Error pinging Redis", zap.Error(err))
		return false
	}

	return true
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

	p.notifChan <- event
}

func (p *RedisJobBackendProvider) Run() error {
	// this function must be called in a goroutine
	p.wg.Add(1)

	// listen to redis notifications
	// https://redis.io/topics/notifications
	// https://pkg.go.dev/github.com/go-redis/redis/v9#example-Client.Subscribe
	// https://pkg.go.dev/github.com/go-redis/redis/v9#example-Client.PSubscribe

	key := "ev:job:*"
	pubsub := p.redisClient.Subscribe(p.ctx, "__keyspace@0__:"+key)
	defer pubsub.Close()

	ch := pubsub.Channel()

	fmt.Printf("Listening for changes to key: %s\n", key)

	for {
		select {
		case msg := <-ch:
			if msg.Payload == "set" {
				value, err := p.redisClient.Get(p.ctx, key).Result()
				if err != nil {
					p.logger.Info("Error getting value for key", zap.String("key", key), zap.Error(err))
					continue
				}
				fmt.Printf("New value for key %s: %s\n", key, value)
			}
		case <-p.stopChan:
			p.wg.Done()
			return nil
		}
	}

	// // subscribe to all channels
	// pubsub := s.redisClient.PSubscribe(s.ctx, "chan:job:event:*")
	// defer pubsub.Close()

	// // Wait for confirmation that subscription is created before publishing anything.
	// _, err := pubsub.Receive(s.ctx)
	// if err != nil {
	// 	s.logger.Error("Error subscribing to Redis", zap.Error(err))
	// 	return err
	// }

	// // Go channel which receives messages.
	// ch := pubsub.Channel()

	// // Consume messages.
	// for msg := range ch {
	// 	s.logger.Info("Received message", zap.String("channel", msg.Channel), zap.String("payload", msg.Payload))
	// }

	// // listen to notifications

	// // listen to notifChan

	// // process events

	// // TODO
	// for {
	// 	select {
	// 	case event := <-s.notifChan:
	// 		switch event.Type {
	// 		case "create":
	// 			// job, err := svc.bp.GetStreamInfo(event.JobUUID)
	// 			// if err != nil {
	// 			// 	svc.logger.Error(
	// 			// 		"Error while getting stream info",
	// 			// 		zap.String("topic", "job"),
	// 			// 		zap.String("method", "Run"),
	// 			// 		zap.String("JobUUID", event.JobUUID.String()),
	// 			// 		zap.Error(err),
	// 			// 	)
	// 			// 	continue
	// 			// }
	// 			// if job == nil {
	// 			// 	svc.logger.Error(
	// 			// 		"Stream not found",
	// 			// 		zap.String("topic", "job"),
	// 			// 		zap.String("method", "Run"),
	// 			// 		zap.String("JobUUID", event.JobUUID.String()),
	// 			// 	)
	// 			// 	continue
	// 			// }
	// 		}
	// 	}
	// }
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
		stopChan:       make(chan bool, 1),
	}, nil
}
