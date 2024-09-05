package web

import (
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/nbigot/minijob/constants"
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/web/apierror"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"
)

// Home godoc
// @Summary Home page
// @Description Home page
// @ID utils-home
// @Produce plain
// @Tags Utils
// @Success 200 {string}
// @Router / [get]
func (w *WebAPIServer) Home(c *fiber.Ctx) error {
	return c.SendString("Welcome to MiniJob!")
}

// Ping godoc
// @Summary Ping server
// @Description Ping server
// @ID utils-ping
// @Produce plain
// @Tags Utils
// @Success 200 {string} string "ok"
// @Router /ping [get]
func (w *WebAPIServer) Ping(c *fiber.Ctx) error {
	return c.SendString("ok")
}

// Healthcheck godoc
// @Summary Healthcheck server
// @Description Healthcheck server
// @ID utils-healthcheck
// @Produce plain
// @Tags Utils
// @Success 200 {string} string "ok"
// @Router /healthcheck [get]
func (w *WebAPIServer) Healthcheck(c *fiber.Ctx) error {
	if w.service.Healthcheck() {
		return c.SendString("ok")
	}

	return c.SendStatus(fiber.StatusServiceUnavailable)
}

// Metrics godoc
// @Summary get server metrics
// @Description get server metrics
// @ID utils-metrics-get
// @Produce plain
// @Tags Utils
// @Success 200 {string} string "ok"
// @Router /metrics [get]
func (w *WebAPIServer) Metrics(c *fiber.Ctx) error {
	// /!\ do not set metricName here, this is a special route
	prometheusHandler := fasthttpadaptor.NewFastHTTPHandler(promhttp.Handler())
	prometheusHandler(c.Context())
	return nil
}

// ComputeMetrics godoc
// @Summary Compute server metrics
// @Description Compute server metrics
// @ID utils-metrics-compute
// @Produce plain
// @Tags Utils
// @Success 200 {string} string "ok"
// @Router /computemetrics [post]
func (w *WebAPIServer) ComputeMetrics(c *fiber.Ctx) error {
	c.Locals("metricName", "ComputeMetrics")

	if err := w.service.ComputeMetrics(); err == nil {
		return c.SendString("ok")
	}

	return c.SendStatus(fiber.StatusServiceUnavailable)
}

func (w *WebAPIServer) GetJobUUIDFromParameter(c *fiber.Ctx) (job.JobUUID, *apierror.APIError) {
	jobUuid, err := uuid.Parse(c.Params(constants.JobUuidParam))
	if err != nil {
		// missing or invalid parameter
		vErr := apierror.ValidationError{FailedField: constants.JobUuidParam, Tag: "parameter", Value: c.Params(constants.JobUuidParam)}
		return jobUuid, &apierror.APIError{
			Message:          "invalid job uuid",
			Code:             constants.ErrorInvalidJobUuid,
			HttpCode:         fiber.StatusBadRequest,
			ValidationErrors: []*apierror.ValidationError{&vErr},
			Err:              err,
		}
	}

	return jobUuid, nil
}

func (w *WebAPIServer) GetJobUUIDFromQuery(c *fiber.Ctx) (*job.JobUUID, *apierror.APIError) {
	value := c.Query(constants.JobUuidParam)
	if value == "" {
		// parameter missing or empty value in query
		return nil, nil
	}

	jobUuid, err := uuid.Parse(value)
	if err != nil {
		// missing or invalid parameter
		vErr := apierror.ValidationError{FailedField: constants.JobUuidParam, Tag: "parameter", Value: c.Params(constants.JobUuidParam)}
		return nil, &apierror.APIError{
			Message:          "invalid job uuid",
			Code:             constants.ErrorInvalidJobUuid,
			HttpCode:         fiber.StatusBadRequest,
			ValidationErrors: []*apierror.ValidationError{&vErr},
			Err:              err,
		}
	}

	return &jobUuid, nil
}

func (w *WebAPIServer) GetUintParameterFromQuery(c *fiber.Ctx, name string, defaultValue uint, minValue uint, maxValue uint) (uint, *apierror.APIError) {
	value := defaultValue
	strValue := c.Query(name)
	if strValue != "" {
		// convert the string to an unsigned integer
		value64, err := strconv.ParseUint(strValue, 10, 32)
		value = uint(value64)
		if err != nil {
			apiErr := apierror.APIError{
				Message:  "invalid value for query parameter: " + name,
				Code:     constants.ErrorInvalidParameterValue,
				HttpCode: fiber.StatusBadRequest,
			}
			return 0, &apiErr
		}
		if value < minValue {
			apiErr := apierror.APIError{
				Message:  fmt.Sprintf("invalid value for query parameter: %s, found %s, minimum allowed value is %d", name, strValue, minValue),
				Code:     constants.ErrorInvalidParameterValue,
				HttpCode: fiber.StatusBadRequest,
			}
			return 0, &apiErr
		}
		if value > maxValue {
			apiErr := apierror.APIError{
				Message:  fmt.Sprintf("invalid value for query parameter: %s, found %s, maximum allowed value is %d", name, strValue, maxValue),
				Code:     constants.ErrorInvalidParameterValue,
				HttpCode: fiber.StatusBadRequest,
			}
			return 0, &apiErr
		}
	}

	return value, nil
}
