package web

import (
	"fmt"
	"sort"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/utils"
	"github.com/nbigot/minijob/constants"
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/metrics"
	"github.com/nbigot/minijob/service"
	"github.com/nbigot/minijob/web/apierror"
)

// convertJobsToResponses converts a slice of Job pointers to a slice of JobResponse pointers
func convertJobsToResponses(jobs []*job.Job) []*job.JobResponse {
	jobResponses := make([]*job.JobResponse, 0, len(jobs))
	for _, j := range jobs {
		jobResponses = append(jobResponses, j.ToResponse())
	}
	return jobResponses
}

// GetAllJobs godoc
// @Summary Get all jobs
// @Description Get all jobs
// @ID jobs-get-all
// @Produce json
// @Tags Jobs
// @success 200 {object} web.JSONResultGetAllJobs{} "successful operation"
// @Router /api/v1/jobs/all [get]
func (w *WebAPIServer) GetAllJobs(c *fiber.Ctx) error {
	c.Locals("metricName", "GetAllJobs")

	jobs := w.service.GetAllJobs()
	return c.JSON(
		JSONResultGetAllJobs{
			Code:    fiber.StatusOK,
			Message: "success",
			Jobs:    convertJobsToResponses(jobs),
		},
	)
}

// GetJobs godoc
// @Summary Get jobs with filtering, pagination and sorting
// @Description Get jobs with optional filtering by status/topic, pagination, and sorting
// @ID jobs-get-filtered
// @Produce json
// @Tags Jobs
// @Param page query int false "Page number (default 1, min 1)"
// @Param limit query int false "Number of jobs per page (default 10, min 1, max 1000)"
// @Param status query string false "Filter by job status (created, delayed, pending, queued, running, succeeded, failed, canceled, deleted, hidden)"
// @Param topic query string false "Filter by topic"
// @Param sort query string false "Sort criteria, comma-separated (created_asc, created_desc, priority_asc, priority_desc, updated_asc, updated_desc, topic_asc, topic_desc)"
// @Param search query string false "Search by job ID (partial match)"
// @success 200 {object} web.JSONResultGetJobs{} "successful operation"
// @Router /api/v1/jobs [get]
func (w *WebAPIServer) GetJobs(c *fiber.Ctx) error {
	c.Locals("metricName", "GetJobs")

	// Parse query parameters
	page, apiErr := GetUintParameterFromQuery(c, "page", 1, 1, 10000)
	if apiErr != nil {
		return apiErr.HTTPResponse(c)
	}

	limit, apiErr := GetUintParameterFromQuery(c, "limit", 10, 1, 1000)
	if apiErr != nil {
		return apiErr.HTTPResponse(c)
	}

	status := c.Query("status")
	topic := c.Query("topic")
	sort := c.Query("sort")
	search := c.Query("search")

	// Create request
	req := &service.GetJobsRequest{
		Page:   int(page),
		Limit:  int(limit),
		Status: status,
		Topic:  topic,
		Sort:   sort,
		Search: search,
	}

	// Get jobs from service
	response, err := w.service.GetJobs(req)
	if err != nil {
		// check if type of err is apierror.APIError
		if _, ok := err.(*apierror.APIError); ok {
			return err.(*apierror.APIError).HTTPResponse(c)
		}
		apiErr := &apierror.APIError{
			Message:  "cannot get jobs",
			Code:     constants.ErrorCantGetJobs,
			HttpCode: fiber.StatusInternalServerError,
			Err:      err,
		}
		return apiErr.HTTPResponse(c)
	}

	return c.JSON(JSONResultGetJobs{
		Code:       fiber.StatusOK,
		Message:    "success",
		Jobs:       convertJobsToResponses(response.Jobs),
		Total:      uint(response.Total),
		Page:       uint(response.Page),
		Limit:      uint(response.Limit),
		TotalPages: uint(response.TotalPages),
	})
}

// DeleteQueuedJobs godoc
// @Summary Delete queued jobs
// @Description Delete queued jobs
// @ID jobs-delete-queued
// @Produce json
// @Tags Jobs
// @success 200 {object} web.JSONResultSuccess{} "successful operation"
// @Router /api/v1/jobs/queued [delete]
func (w *WebAPIServer) DeleteQueuedJobs(c *fiber.Ctx) error {
	c.Locals("metricName", "DeleteQueuedJobs")

	err := w.service.DeleteQueuedJobs()
	if err != nil {
		// check if type of err is apierror.APIError
		if _, ok := err.(*apierror.APIError); ok {
			return err.(*apierror.APIError).HTTPResponse(c)
		}
		apiErr := apierror.APIError{
			Message:  "cannot delete queued jobs",
			Code:     constants.ErrorCantDeleteJob,
			HttpCode: fiber.StatusBadRequest,
			Err:      err,
		}
		return apiErr.HTTPResponse(c)
	}

	return c.JSON(
		JSONResultSuccess{
			Code:    fiber.StatusOK,
			Message: "success",
		},
	)
}

// DeleteAllJobs godoc
// @Summary Delete all jobs
// @Description Delete all jobs
// @ID jobs-delete-all
// @Produce json
// @Tags Jobs
// @success 200 {object} web.JSONResultSuccess{} "successful operation"
// @Router /api/v1/jobs/ [delete]
func (w *WebAPIServer) DeleteAllJobs(c *fiber.Ctx) error {
	c.Locals("metricName", "DeleteAllJobs")

	err := w.service.DeleteAllJobs()
	if err != nil {
		// check if type of err is apierror.APIError
		if _, ok := err.(*apierror.APIError); ok {
			return err.(*apierror.APIError).HTTPResponse(c)
		}
		apiErr := apierror.APIError{
			Message:  "cannot delete all jobs",
			Code:     constants.ErrorCantDeleteJob,
			HttpCode: fiber.StatusBadRequest,
			Err:      err,
		}
		return apiErr.HTTPResponse(c)
	}

	return c.JSON(
		JSONResultSuccess{
			Code:    fiber.StatusOK,
			Message: "success",
		},
	)
}

// GetJob godoc
// @Summary Get a job
// @Description Get a job
// @ID job-get
// @Produce json
// @Tags Jobs
// @Param jobuuid path string true "Job UUID"
// @success 200 {object} web.JSONResultGetJob{} "successful operation"
// @Failure 400 {object} apierror.APIError "Invalid UUID"
// @Failure 404 {object} apierror.APIError "Job not found"
// @Failure 500 {object} apierror.APIError "Invalid job"
// @Router /api/v1/job/{jobuuid} [get]
func (w *WebAPIServer) GetJob(c *fiber.Ctx) error {
	c.Locals("metricName", "GetJob")

	jobUUID, errApi := GetJobUUIDFromParameter(c)
	if errApi != nil {
		return c.Status(errApi.HttpCode).JSON(errApi)
	}
	job, err := w.service.GetJob(jobUUID)
	if err != nil {
		var apiErr apierror.APIError

		if job == nil {
			// unknown job
			apiErr = apierror.APIError{
				Message:  "job not found",
				Code:     constants.ErrorJobUuidNotFound,
				HttpCode: fiber.StatusBadRequest,
				JobUUID:  jobUUID,
				Err:      err,
			}
		} else {
			// invalid job
			apiErr = apierror.APIError{
				Message:  "cannot get job",
				Code:     constants.ErrorInvalidJobUuid,
				HttpCode: fiber.StatusInternalServerError,
				JobUUID:  jobUUID,
				Err:      err,
			}
		}

		return apiErr.HTTPResponse(c)
	}
	if job == nil {
		apiErr := apierror.APIError{
			Message:  "job not found",
			Code:     constants.ErrorJobUuidNotFound,
			HttpCode: fiber.StatusNotFound,
			JobUUID:  jobUUID,
		}
		return apiErr.HTTPResponse(c)
	}

	return c.JSON(
		JSONResultGetJob{
			Code:    fiber.StatusOK,
			Message: "success",
			Job:     job.ToResponse(),
		},
	)
}

// CreateJob godoc
// @Summary Create job
// @Description Create job
// @ID job-create
// @Accept json
// @Produce json
// @Tags Jobs
// @Param request body job.JobRequest true "Job creation request"
// @success 200 {object} web.JSONResultCreateJob{} "successful operation"
// @Failure 400 {object} apierror.APIError "Invalid input - content type, JSON body format or validation errors"
// @Router /api/v1/job/ [post]
func (w *WebAPIServer) CreateJob(c *fiber.Ctx) error {
	c.Locals("metricName", "CreateJob")

	// get request body as json string and ensure the header is application/json
	ctype := utils.ToLower(utils.UnsafeString(c.Request().Header.ContentType()))
	if ctype != "application/json" {
		apiErr := apierror.APIError{
			Message:  "invalid content type (must be application/json)",
			Code:     constants.ErrorInvalidContentType,
			HttpCode: fiber.StatusBadRequest,
		}
		return apiErr.HTTPResponse(c)
	}

	payload := c.Body()

	// validate request body against the json schema
	if w.appConfig.Jobs.JsonSchema.Enable {
		keyErrors, err := w.schema.ValidateBytes(c.Context(), payload)
		if err != nil {
			apiErr := apierror.APIError{
				Message:  "invalid json body format",
				Code:     constants.ErrorCantDeserializeJson,
				HttpCode: fiber.StatusBadRequest,
			}
			return apiErr.HTTPResponse(c)
		} else if len(keyErrors) > 0 {
			jsonErrors := make([]*apierror.ValidationError, len(keyErrors))
			for i, keyError := range keyErrors {
				jsonErrors[i] = &apierror.ValidationError{
					FailedField: keyError.PropertyPath,
					Tag:         keyError.Message,
					Value:       fmt.Sprintf("%v", keyError.InvalidValue),
				}
			}
			apiErr := apierror.APIError{
				Message:          "invalid json content",
				Code:             constants.ErrorInvalidJsonData,
				HttpCode:         fiber.StatusBadRequest,
				ValidationErrors: jsonErrors,
			}
			return apiErr.HTTPResponse(c)
		}
	}

	job, err := w.service.CreateJob(payload)
	if err != nil {
		// check if type of err is apierror.APIError
		if _, ok := err.(*apierror.APIError); ok {
			return err.(*apierror.APIError).HTTPResponse(c)
		}
		apiErr := apierror.APIError{
			Message:  "cannot create job",
			Code:     constants.ErrorCantCreateJob,
			HttpCode: fiber.StatusBadRequest,
			Details:  err.Error(),
			Err:      err,
		}
		return apiErr.HTTPResponse(c)
	}

	return c.JSON(
		JSONResultCreateJob{
			Code:    fiber.StatusOK,
			Message: "success",
			Job:     job.ToResponse(),
		},
	)
}

// DeleteJob godoc
// @Summary Delete job
// @Description Delete job
// @ID job-delete
// @Produce json
// @Tags Jobs
// @success 200 {object} web.JSONResultSuccess{} "successful operation"
// @Router /api/v1/job/{jobuuid} [delete]
func (w *WebAPIServer) DeleteJob(c *fiber.Ctx) error {
	c.Locals("metricName", "DeleteJob")

	jobUUID, errApi := GetJobUUIDFromParameter(c)
	if errApi != nil {
		return errApi.HTTPResponse(c)
	}

	err := w.service.DeleteJob(jobUUID)
	if err != nil {
		// check if type of err is apierror.APIError
		if _, ok := err.(*apierror.APIError); ok {
			return err.(*apierror.APIError).HTTPResponse(c)
		}
		apiErr := apierror.APIError{
			Message:  "cannot delete job",
			Code:     constants.ErrorCantDeleteJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
			Err:      err,
		}
		return apiErr.HTTPResponse(c)
	}

	return c.JSON(
		JSONResultSuccess{
			Code:    fiber.StatusOK,
			Message: "success",
		},
	)
}

// CloneJob godoc
// @Summary Clone job
// @Description Clone job
// @ID job-clone
// @Produce json
// @Tags Jobs
// @success 200 {object} web.JSONResultCloneJob{} "successful operation"
// @Router /api/v1/job/{jobuuid}/clone [post]
func (w *WebAPIServer) CloneJob(c *fiber.Ctx) error {
	c.Locals("metricName", "CloneJob")

	jobUUID, errApi := GetJobUUIDFromParameter(c)
	if errApi != nil {
		return errApi.HTTPResponse(c)
	}

	job, err := w.service.CloneJob(jobUUID)
	if err != nil {
		// check if type of err is apierror.APIError
		if _, ok := err.(*apierror.APIError); ok {
			return err.(*apierror.APIError).HTTPResponse(c)
		}
		apiErr := apierror.APIError{
			Message:  "cannot clone job",
			Code:     constants.ErrorCantCloneJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
			Err:      err,
		}
		return apiErr.HTTPResponse(c)
	}

	return c.JSON(
		JSONResultCloneJob{
			Code:    fiber.StatusOK,
			Message: "success",
			Job:     job.ToResponse(),
		},
	)
}

// PullJob godoc
// @Summary Pull one or multiple jobs
// @Description Pull one or multiple jobs
// @ID job-pull
// @Produce json
// @Tags Jobs
// @success 200 {object} web.JSONResultPullJob{} "successful operation"
// @Router /api/v1/job/pull [post]
func (w *WebAPIServer) PullJob(c *fiber.Ctx) error {
	c.Locals("metricName", "PullJob")

	req, apiErr := w.GetPullJobRequest(c)
	if apiErr != nil {
		return apiErr.HTTPResponse(c)
	}

	res, err := w.service.PullJobs(req)
	if err != nil {
		// check if type of err is apierror.APIError
		if _, ok := err.(*apierror.APIError); ok {
			return err.(*apierror.APIError).HTTPResponse(c)
		}
		apiErr := apierror.APIError{
			Message:  "cannot pull any job",
			Code:     constants.ErrorCantPullAnyJob,
			HttpCode: fiber.StatusBadRequest,
			Err:      err,
		}
		return apiErr.HTTPResponse(c)
	}

	return c.JSON(
		JSONResultPullJob{
			Code:    fiber.StatusOK,
			Message: "success",
			Jobs:    convertJobsToResponses(res.Jobs),
		},
	)
}

func (w *WebAPIServer) GetPullJobRequest(c *fiber.Ctx) (*service.RequestPullJobs, *apierror.APIError) {
	// get request parameters from the query context
	var apiErr *apierror.APIError

	// numJobs of jobs to pull at once (default 1)
	var numJobs uint
	if numJobs, apiErr = GetUintParameterFromQuery(c, constants.NumJobsQueryParam, 1, 1, 100); apiErr != nil {
		return nil, apiErr
	}

	// visibilityTimeout parameter to hide the job for a specific duration
	var visibilityTimeout uint
	if visibilityTimeout, apiErr = GetUintParameterFromQuery(c, constants.VisibilityTimeoutQueryParam, w.appConfig.Jobs.DefaultVisibilityTimeout, 1, w.appConfig.Jobs.MaxVisibilityTimeout); apiErr != nil {
		return nil, apiErr
	}

	// waitTimeSeconds parameter enables long-poll (default 0)
	var waitTimeSeconds uint
	if waitTimeSeconds, apiErr = GetUintParameterFromQuery(c, constants.WaitTimeSecondsQueryParam, 0, 0, 60); apiErr != nil {
		return nil, apiErr
	}

	// topic to pull the job from (if empty pull from any topic)
	topic := c.Query(constants.JobTopicParam)

	// job uuid to pull (if empty pull any job)
	jobUUID, errApi := GetJobUUIDFromQuery(c)
	if errApi != nil {
		return nil, errApi
	}
	if jobUUID != nil {
		// when the job uuid is provided, the numJobs must be 1
		numJobs = 1
	}

	req := service.RequestPullJobs{
		NumJobs:           numJobs,
		Topic:             topic,
		JobUUID:           jobUUID,
		VisibilityTimeout: visibilityTimeout,
		WaitTimeSeconds:   waitTimeSeconds,
	}

	return &req, nil
}

// CancelJob godoc
// @Summary Cancel a running job
// @Description Cancel a job that is running
// @ID job-cancel
// @Produce json
// @Tags Jobs
// @success 200 {object} web.JSONResultSuccess{} "successful operation"
// @Router /api/v1/job/{jobuuid}/cancel [post]
func (w *WebAPIServer) CancelJob(c *fiber.Ctx) error {
	c.Locals("metricName", "CancelJob")

	jobUUID, errApi := GetJobUUIDFromParameter(c)
	if errApi != nil {
		return errApi.HTTPResponse(c)
	}

	err := w.service.CancelJob(jobUUID)
	if err != nil {
		// check if type of err is apierror.APIError
		if _, ok := err.(*apierror.APIError); ok {
			return err.(*apierror.APIError).HTTPResponse(c)
		}
		apiErr := apierror.APIError{
			Message:  "cannot cancel job",
			Code:     constants.ErrorCantCancelJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
			Err:      err,
		}
		return apiErr.HTTPResponse(c)
	}

	return c.JSON(
		JSONResultSuccess{
			Code:    fiber.StatusOK,
			Message: "success",
		},
	)
}

// SetJobAsSuccessful godoc
// @Summary Set job as successful
// @Description Set job as successful
// @ID job-set-as-successful
// @Produce json
// @Tags Jobs
// @success 200 {object} web.JSONResultSuccess{} "successful operation"
// @Router /api/v1/job/{jobuuid}/succeed [post]
func (w *WebAPIServer) SetJobAsSuccessful(c *fiber.Ctx) error {
	c.Locals("metricName", "SetJobAsSuccessful")

	jobUUID, errApi := GetJobUUIDFromParameter(c)
	if errApi != nil {
		return errApi.HTTPResponse(c)
	}

	err := w.service.SetJobAsSuccessful(jobUUID)
	if err != nil {
		// check if type of err is apierror.APIError
		if _, ok := err.(*apierror.APIError); ok {
			return err.(*apierror.APIError).HTTPResponse(c)
		}
		apiErr := apierror.APIError{
			Message:  "cannot succeed job",
			Code:     constants.ErrorCantSetJobAsSuccessful,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
			Err:      err,
		}
		return apiErr.HTTPResponse(c)
	}

	return c.JSON(
		JSONResultSuccess{
			Code:    fiber.StatusOK,
			Message: "success",
		},
	)
}

// FailJob godoc
// @Summary Set job as failed
// @Description Set job as failed
// @ID job-set-as-failed
// @Produce json
// @Tags Jobs
// @success 200 {object} web.JSONResultSuccess{} "successful operation"
// @Router /api/v1/job/{jobuuid}/fail [post]
func (w *WebAPIServer) FailJob(c *fiber.Ctx) error {
	c.Locals("metricName", "FailJob")

	jobUUID, errApi := GetJobUUIDFromParameter(c)
	if errApi != nil {
		return errApi.HTTPResponse(c)
	}

	reachedMaxRetry, err := w.service.FailJob(jobUUID)
	if err != nil {
		// check if type of err is apierror.APIError
		if _, ok := err.(*apierror.APIError); ok {
			return err.(*apierror.APIError).HTTPResponse(c)
		}
		apiErr := apierror.APIError{
			Message:  "cannot fail job",
			Code:     constants.ErrorCantFailJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
			Err:      err,
		}
		return apiErr.HTTPResponse(c)
	}

	if reachedMaxRetry {
		return c.JSON(
			JSONResultSuccess{
				Code:    fiber.StatusOK,
				Message: "job reached max retry",
			},
		)
	}

	return c.JSON(
		JSONResultSuccess{
			Code:    fiber.StatusOK,
			Message: "success",
		},
	)
}

// ChangeVisibilityTimeoutJob godoc
// @Summary Change job visibility timeout
// @Description Change job visibility timeout
// @ID job-change-visibility-timeout
// @Produce json
// @Tags Jobs
// @success 200 {object} web.JSONResultSuccess{} "successful operation"
// @Router /api/v1/job/{jobuuid}/visibilitytimeout [post]
func (w *WebAPIServer) ChangeVisibilityTimeoutJob(c *fiber.Ctx) error {
	c.Locals("metricName", "ChangeVisibilityTimeoutJob")

	jobUUID, errApi := GetJobUUIDFromParameter(c)
	if errApi != nil {
		return errApi.HTTPResponse(c)
	}

	// visibilityTimeout parameter to hide the job for a specific duration
	var apiErr *apierror.APIError
	var visibilityTimeout uint
	if visibilityTimeout, apiErr = GetUintParameterFromQuery(c, constants.VisibilityTimeoutQueryParam, w.appConfig.Jobs.DefaultVisibilityTimeout, 1, w.appConfig.Jobs.MaxVisibilityTimeout); apiErr != nil {
		return apiErr.HTTPResponse(c)
	}

	err := w.service.ChangeVisibilityTimeoutJob(jobUUID, visibilityTimeout)
	if err != nil {
		// check if type of err is apierror.APIError
		if _, ok := err.(*apierror.APIError); ok {
			return err.(*apierror.APIError).HTTPResponse(c)
		}
		apiErr = &apierror.APIError{
			Message:  "cannot change job visibility timeout",
			Code:     constants.ErrorCantFailJob,
			HttpCode: fiber.StatusBadRequest,
			JobUUID:  jobUUID,
			Err:      err,
		}
		return apiErr.HTTPResponse(c)
	}

	return c.JSON(
		JSONResultSuccess{
			Code:    fiber.StatusOK,
			Message: "success",
		},
	)
}

// GetJobsMetrics godoc
// @Summary Get jobs metrics
// @Description Get jobs metrics
// @ID jobs-metrics
// @Produce json
// @Tags Utils
// @success 200 {object} web.JSONResultGetJobsMetrics{} "successful operation"
// @Router /api/v1/observability/metrics [get]
func (w *WebAPIServer) GetJobsMetrics(c *fiber.Ctx) error {
	c.Locals("metricName", "GetJobsMetrics")
	result := JSONResultGetJobsMetrics{
		Code:    fiber.StatusOK,
		Message: "success",
		Metrics: w.service.GetMetrics().(*metrics.ServiceMetrics).GetJobMetricsByTopicMap(),
	}

	// return the metrics as json
	return c.JSON(result)
}

// GetTopics godoc
// @Summary Get jobs topics
// @Description Get jobs topics
// @ID jobs-topics
// @Produce json
// @Tags Topics
// @success 200 {object} web.JSONResultGetTopics{} "successful operation"
// @Router /api/v1/topics [get]
func (w *WebAPIServer) GetTopics(c *fiber.Ctx) error {
	c.Locals("metricName", "GetTopics")
	return c.JSON(
		JSONResultGetTopics{
			Code:    fiber.StatusOK,
			Message: "success",
			Topics:  w.service.GetTopics(),
		},
	)
}

// GetTopicsStats godoc
// @Summary Get topics stats
// @Description Get topics stats
// @ID topics-stats
// @Produce json
// @Tags Topics
// @success 200 {object} web.JSONResultGetTopicsStats{} "successful operation"
// @Router /api/v1/metrics/topics [get]
func (w *WebAPIServer) GetTopicsStats(c *fiber.Ctx) error {
	c.Locals("metricName", "GetTopicsStats")
	return c.JSON(
		JSONResultGetTopicsStats{
			Code:    fiber.StatusOK,
			Message: "success",
			Topics:  w.service.GetMetrics().GetTopicsStats(),
		},
	)
}

// GetOldestJobs godoc
// @Summary Get oldest incomplete jobs
// @Description Get top N oldest jobs that are not completed, grouped by topic
// @ID jobs-get-old
// @Produce json
// @Tags Jobs
// @Param limit query int false "Number of jobs per topic (default 10, min 1, max 100)"
// @Param duration query int false "Minimum job duration in seconds (default 0, max 1 year)"
// @success 200 {object} web.JSONResultGetOldJobs{} "successful operation"
// @Router /api/v1/jobs/old [get]
func (w *WebAPIServer) GetOldestJobs(c *fiber.Ctx) error {
	c.Locals("metricName", "GetOldestJobs")

	// Get limit parameter, default to 10 if not specified
	limit, apiErr := GetUintParameterFromQuery(c, "limit", 10, 1, 100)
	if apiErr != nil {
		return apiErr.HTTPResponse(c)
	}

	// Get duration parameter, default to 0 if not specified (return all jobs)
	minDuration, apiErr := GetUintParameterFromQuery(c, "duration", 0, 0, 86400*365)
	if apiErr != nil {
		return apiErr.HTTPResponse(c)
	}
	minDurationMs := int64(minDuration) * 1000

	jobs := w.service.GetAllJobs()
	topicJobs := make(TopicOldJobsMap)
	now := time.Now().UnixMilli()

	for _, job := range jobs {
		if !job.IsCompleted() {
			jobDurationMs := now - job.GetCreationTimestamp()

			// Filter out jobs with duration less than minDuration
			if jobDurationMs < minDurationMs {
				continue
			}

			result := TopOldJobsResult{
				JobUUID:    job.JobUUID.String(),
				DurationMs: jobDurationMs,
			}

			topicJobs[job.Topic] = append(topicJobs[job.Topic], result)
		}
	}

	// Sort and limit topicsResults for each topic
	topicsResults := make(TopicOldJobsMap)
	for topic, jobs := range topicJobs {
		// Sort jobs by duration in descending order (oldest first)
		sort.Slice(jobs, func(i, j int) bool {
			return jobs[i].DurationMs > jobs[j].DurationMs
		})

		// Limit number of jobs per topic
		if len(jobs) > int(limit) {
			jobs = jobs[:limit]
		}

		topicsResults[topic] = jobs
	}

	return c.JSON(JSONResultGetOldJobs{
		Code:    fiber.StatusOK,
		Message: "success",
		Topics:  topicsResults,
	})
}

// GetResourcesMetrics godoc
// @Summary Get resources stats
// @Description Get resources stats
// @ID resources-stats
// @Produce json
// @Tags Resources
// @success 200 {object} web.JSONResultGetResourcesStats{} "successful operation"
// @Router /api/v1/metrics/resources [get]
func (w *WebAPIServer) GetResourcesMetrics(c *fiber.Ctx) error {
	c.Locals("metricName", "GetResourcesMetrics")
	return c.JSON(
		JSONResultGetResourcesStats{
			Code:      fiber.StatusOK,
			Message:   "success",
			Resources: w.service.GetMetrics().GetResourcesMetrics(),
		},
	)
}
