package web

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/nbigot/minijob/constants"
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/web/apierror"
	"github.com/valyala/fasthttp"
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

// DecodedBody returns the request body decoded according to its Content-Encoding, as
// c.Body() does (same encodings, decoded in the order listed; an unknown encoding ends the
// decoding; a body that cannot be decoded is replaced by the error message), but a decoded
// body larger than maxSize is refused with fasthttp.ErrBodyTooLarge. c.Body() decodes
// without a limit: a few kilobytes of nested gzip ("gzip, gzip") made the server allocate
// gigabytes, while the body limit of the server only applies to the bytes received.
func DecodedBody(c *fiber.Ctx, maxSize int) ([]byte, error) {
	req := c.Request()
	encodingHeader := ""
	for key, value := range req.Header.All() {
		if string(key) == fiber.HeaderContentEncoding {
			encodingHeader = string(value)
		}
	}
	if encodingHeader == "" {
		return req.Body(), nil
	}

	encodings := strings.Split(encodingHeader, ",")
	var original, body []byte
	defer func() {
		// each encoding is decoded from the request body: put the received body back
		if original != nil {
			req.SetBodyRaw(original)
		}
	}()
	for i, encoding := range encodings {
		var err error
		switch strings.TrimLeft(encoding, " ") {
		case "gzip":
			body, err = req.BodyGunzipWithLimit(maxSize)
		case "br", "brotli":
			body, err = req.BodyUnbrotliWithLimit(maxSize)
		case "deflate":
			body, err = req.BodyInflateWithLimit(maxSize)
		default:
			if len(encodings) == 1 {
				return req.Body(), nil
			}
			return body, nil
		}
		if errors.Is(err, fasthttp.ErrBodyTooLarge) {
			return nil, err
		}
		if err != nil {
			return []byte(err.Error()), nil
		}
		if i < len(encodings)-1 {
			if original == nil {
				original = append([]byte{}, req.Body()...)
			}
			req.SetBodyRaw(body)
		}
	}
	return body, nil
}
