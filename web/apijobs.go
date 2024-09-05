package web

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/utils"
	"github.com/nbigot/minijob/constants"
	"github.com/nbigot/minijob/service"
	"github.com/nbigot/minijob/web/apierror"
)

// GetAllJobs godoc
// @Summary Get all jobs
// @Description Get all jobs
// @ID jobs-get-all
// @Produce json
// @Tags Jobs
// @Success 200 {object}
// @Router /api/v1/jobs/ [get]
func (w *WebAPIServer) GetAllJobs(c *fiber.Ctx) error {
	c.Locals("metricName", "GetAllJobs")

	jobs, err := w.service.GetAllJobs()
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
		JSONResult{
			Code:    fiber.StatusOK,
			Message: "success",
			Data:    jobs,
		},
	)
}

// DeleteAllJobs godoc
// @Summary Delete all jobs
// @Description Delete all jobs
// @ID jobs-delete-all
// @Produce json
// @Tags Jobs
// @Success 200 {object}
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
// @Success 200 {object} JobResponse "Job details"
// @Failure 400 {object} ErrorResponse "Invalid UUID"
// @Failure 404 {object} ErrorResponse "Job not found"
// @Failure 500 {object} ErrorResponse "Invalid job"
// @Router /api/v1/job/{jobuuid} [get]
func (w *WebAPIServer) GetJob(c *fiber.Ctx) error {
	c.Locals("metricName", "GetJob")

	jobUUID, err1 := w.GetJobUUIDFromParameter(c)
	if err1 != nil {
		return c.Status(err1.HttpCode).JSON(err1)
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
		JSONResult{
			Code:    fiber.StatusOK,
			Message: "success",
			Data:    job,
		},
	)
}

// CreateJob godoc
// @Summary Create job
// @Description Create job
// @ID job-create
// @Produce json
// @Tags Jobs
// @Success 200 {object}
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
			Err:      err,
		}
		return apiErr.HTTPResponse(c)
	}
	return c.JSON(job)
}

// DeleteJob godoc
// @Summary Delete job
// @Description Delete job
// @ID job-delete
// @Produce json
// @Tags Jobs
// @Success 200 {object}
// @Router /api/v1/job/{jobuuid} [delete]
func (w *WebAPIServer) DeleteJob(c *fiber.Ctx) error {
	c.Locals("metricName", "DeleteJob")

	jobUUID, errApi := w.GetJobUUIDFromParameter(c)
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
// @Success 200 {object}
// @Router /api/v1/job/{jobuuid}/clone [post]
func (w *WebAPIServer) CloneJob(c *fiber.Ctx) error {
	c.Locals("metricName", "CloneJob")

	jobUUID, errApi := w.GetJobUUIDFromParameter(c)
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

	return c.JSON(job)
}

// PullJob godoc
// @Summary Pull one or multiple jobs
// @Description Pull one or multiple jobs
// @ID job-pull
// @Produce json
// @Tags Jobs
// @Success 200 {object}
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
		JSONResult{
			Code:    fiber.StatusOK,
			Message: "success",
			Data:    res,
		},
	)
}

func (w *WebAPIServer) GetPullJobRequest(c *fiber.Ctx) (*service.RequestPullJobs, *apierror.APIError) {
	// get request parameters from the query context
	var apiErr *apierror.APIError

	// numJobs of jobs to pull at once (default 1)
	var numJobs uint
	if numJobs, apiErr = w.GetUintParameterFromQuery(c, constants.NumJobsQueryParam, 1, 1, 100); apiErr != nil {
		return nil, apiErr
	}

	// visibilityTimeout parameter to hide the job for a specific duration
	var visibilityTimeout uint
	if visibilityTimeout, apiErr = w.GetUintParameterFromQuery(c, constants.VisibilityTimeoutQueryParam, w.appConfig.Jobs.DefaultVisibilityTimeout, 1, w.appConfig.Jobs.MaxVisibilityTimeout); apiErr != nil {
		return nil, apiErr
	}

	// waitTimeSeconds parameter enables long-poll (default 0)
	var waitTimeSeconds uint
	if waitTimeSeconds, apiErr = w.GetUintParameterFromQuery(c, constants.WaitTimeSecondsQueryParam, 0, 0, 60); apiErr != nil {
		return nil, apiErr
	}

	// topic to pull the job from (if empty pull from any topic)
	topic := c.Query("topic")

	// job uuid to pull (if empty pull any job)
	jobUUID, errApi := w.GetJobUUIDFromQuery(c)
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
// @Summary Cancel a job
// @Description Cancel a job
// @ID job-cancel
// @Produce json
// @Tags Jobs
// @Success 200 {object}
// @Router /api/v1/job/{jobuuid}/cancel [post]
func (w *WebAPIServer) CancelJob(c *fiber.Ctx) error {
	c.Locals("metricName", "CancelJob")

	jobUUID, errApi := w.GetJobUUIDFromParameter(c)
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
// @Success 200 {object}
// @Router /api/v1/job/{jobuuid}/succeed [post]
func (w *WebAPIServer) SetJobAsSuccessful(c *fiber.Ctx) error {
	c.Locals("metricName", "SetJobAsSuccessful")

	jobUUID, errApi := w.GetJobUUIDFromParameter(c)
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
// @Success 200 {object}
// @Router /api/v1/job/{jobuuid}/fail [post]
func (w *WebAPIServer) FailJob(c *fiber.Ctx) error {
	c.Locals("metricName", "FailJob")

	jobUUID, errApi := w.GetJobUUIDFromParameter(c)
	if errApi != nil {
		return errApi.HTTPResponse(c)
	}

	err := w.service.FailJob(jobUUID)
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
// @Success 200 {object}
// @Router /api/v1/job/{jobuuid}/visibilitytimeout [post]
func (w *WebAPIServer) ChangeVisibilityTimeoutJob(c *fiber.Ctx) error {
	c.Locals("metricName", "ChangeVisibilityTimeoutJob")

	jobUUID, errApi := w.GetJobUUIDFromParameter(c)
	if errApi != nil {
		return errApi.HTTPResponse(c)
	}

	// visibilityTimeout parameter to hide the job for a specific duration
	var apiErr *apierror.APIError
	var visibilityTimeout uint
	if visibilityTimeout, apiErr = w.GetUintParameterFromQuery(c, constants.VisibilityTimeoutQueryParam, w.appConfig.Jobs.DefaultVisibilityTimeout, 1, w.appConfig.Jobs.MaxVisibilityTimeout); apiErr != nil {
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

// JobsMonitor godoc
// @Summary Monitor jobs
// @Description Monitor jobs
// @ID job-monitor
// @Produce json
// @Tags Jobs
// @Success 200 {object}
// @Router /api/v1/jobs/monitor [get]
func (w *WebAPIServer) JobsMonitor(c *fiber.Ctx) error {
	c.Locals("metricName", "JobsMonitor")

	// display all jobs in an html table
	// the table should have the following columns:
	// - Job UUID
	// - Job Name
	// - Job Status
	// - Job Start Time
	// - Job End Time
	// - Job Duration
	// - Job Properties
	// each row should have a button to delete the job
	// each row should have a button to clone the job
	// each row should have a button to view the job
	// each row should have a button to view the job logs
	// each row should have a button to view the job properties
	// each cell should have a background color based on relevant information (status, duration, ...) (e.g. red for failed, green for success, yellow for running, etc.)
	// the overall page should like an excel sheet
	// TODO
	return c.JSON(
		JSONResultSuccess{
			Code:    fiber.StatusOK,
			Message: "success",
		},
	)
}
