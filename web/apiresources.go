package web

import (
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/nbigot/minijob/constants"
	"github.com/nbigot/minijob/web/apierror"
)

// GetLockedResources godoc
// @Summary Get locked Resources
// @Description Get locked Resources
// @ID resources-get-locked
// @Produce json
// @Tags Resources
// @success 200 {object} web.JSONResultGetLockedResources{} "successful operation"
// @Router /api/v1/resources [get]
func (w *WebAPIServer) GetLockedResources(c *fiber.Ctx) error {
	c.Locals("metricName", "GetLockedResources")

	lockedResources := w.service.GetLockedResources()
	return c.JSON(
		JSONResultGetLockedResources{
			Code:      fiber.StatusOK,
			Message:   "success",
			Resources: lockedResources,
		},
	)
}

// UnlockAllResources godoc
// @Summary Unlock all resources
// @Description Unlock all resources
// @ID resources-unlock-all
// @Produce json
// @Tags Resources
// @success 200 {object} web.JSONResultSuccess{} "successful operation"
// @Router /api/v1/resources/unlock [post]
func (w *WebAPIServer) UnlockAllResources(c *fiber.Ctx) error {
	c.Locals("metricName", "UnlockAllResources")

	err := w.service.UnlockAllResources()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(err)
	}
	return c.JSON(
		JSONResultSuccess{
			Code:    fiber.StatusOK,
			Message: "success",
		},
	)
}

// UnlockResource godoc
// @Summary Unlock a single resource
// @Description Unlock a single resource
// @ID resource-unlock
// @Produce json
// @Tags Resources
// @success 200 {object} web.JSONResultSuccess{} "successful operation"
// @Failure 404 {object} web.JSONResult "resource not found"
// @Router /api/v1/resource/{resourceName}/unlock [post]
func (w *WebAPIServer) UnlockResource(c *fiber.Ctx) error {
	c.Locals("metricName", "UnlockResource")
	// the resource name is the path parameter; the "resourcename" query parameter, when
	// present, takes precedence (it was the only one read by previous versions)
	resourceName := c.Query(constants.ResourceNameQueryParam)
	if resourceName == "" {
		resourceName = decodePathSegment(c.Params(constants.ResourceNameQueryParam))
	}

	if err := w.service.UnlockResource(strings.Clone(resourceName)); err != nil {
		if apiErr, ok := err.(*apierror.APIError); ok {
			return apiErr.HTTPResponse(c)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(err)
	}

	return c.JSON(
		JSONResultSuccess{
			Code:    fiber.StatusOK,
			Message: "success",
		},
	)
}

// decodePathSegment decodes the %XX sequences of a raw path segment (the server keeps paths
// percent-encoded). Invalid sequences are kept as they are, invalid UTF-8 is replaced.
func decodePathSegment(s string) string {
	unhex := func(c byte) (byte, bool) {
		switch {
		case '0' <= c && c <= '9':
			return c - '0', true
		case 'a' <= c && c <= 'f':
			return c - 'a' + 10, true
		case 'A' <= c && c <= 'F':
			return c - 'A' + 10, true
		}
		return 0, false
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi, ok1 := unhex(s[i+1])
			lo, ok2 := unhex(s[i+2])
			if ok1 && ok2 {
				out = append(out, hi<<4|lo)
				i += 2
				continue
			}
		}
		out = append(out, s[i])
	}
	if utf8.Valid(out) {
		return string(out)
	}
	return strings.ToValidUTF8(string(out), string(utf8.RuneError))
}
