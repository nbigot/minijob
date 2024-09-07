package web

import "github.com/gofiber/fiber/v2"

// GetLockedResources godoc
// @Summary Get locked Resources
// @Description Get locked Resources
// @ID resources-get-locked
// @Produce json
// @Tags Resources
// @success 200 {object} web.JSONResultGetLockedResources{} "successful operation"
// @Router /api/v1/resources/locked [get]
func (w *WebAPIServer) GetLockedResources(c *fiber.Ctx) error {
	c.Locals("metricName", "GetLockedResources")

	lockedResources, err := w.service.GetLockedResources()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(err)
	}
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
