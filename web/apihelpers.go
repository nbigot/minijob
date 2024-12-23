package web

import (
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/nbigot/minijob/constants"
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/web/apierror"
)

func GetJobUUIDFromParameter(c *fiber.Ctx) (job.JobUUID, *apierror.APIError) {
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

func GetJobUUIDFromQuery(c *fiber.Ctx) (*job.JobUUID, *apierror.APIError) {
	value := c.Query(constants.JobUuidParam)
	if value == "" {
		// parameter missing or empty value in query
		return nil, nil
	}

	jobUuid, err := uuid.Parse(value)
	if err != nil {
		// missing or invalid parameter
		vErr := apierror.ValidationError{FailedField: constants.JobUuidParam, Tag: "parameter", Value: value}
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

func GetUintParameterFromQuery(c *fiber.Ctx, name string, defaultValue uint, minValue uint, maxValue uint) (uint, *apierror.APIError) {
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
