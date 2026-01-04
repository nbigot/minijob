package web

import (
	"fmt"
	"runtime"

	"github.com/gofiber/fiber/v2"
)

// ResourceMetric represents a single resource metric with label and value
type ResourceMetric struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ResourceBackendProvider represents backend provider type
type ResourceBackendProvider struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ResourceUsage represents system resource usage metrics
type ResourceUsage struct {
	Memory  ResourceMetric          `json:"memory"`
	Disk    ResourceMetric          `json:"disk"`
	Backend ResourceBackendProvider `json:"backend"`
}

// JSONResultGetSystemResources represents the API response for system resources
type JSONResultGetSystemResources struct {
	Code          int           `json:"code" example:"200"`
	Message       string        `json:"message" example:"success"`
	ResourceUsage ResourceUsage `json:"resourceUsage"`
}

// GetSystemInfo godoc
// @Summary Get system information
// @Description Get system information including version, uptime, and runtime details
// @ID system-get-info
// @Produce json
// @Tags System
// @success 200 {object} web.JSONResultGetSystemInfo{} "successful operation"
// @Router /api/v1/system/info [get]
func (w *WebAPIServer) GetSystemInfo(c *fiber.Ctx) error {
	return c.JSON(
		JSONResultGetSystemInfo{
			Code:    fiber.StatusOK,
			Message: "success",
			SystemInfo: struct {
				Version     string `json:"version"`
				Uptime      int64  `json:"uptime"`
				Hostname    string `json:"hostname"`
				Environment string `json:"environment"`
			}{
				Version:     w.service.GetVersion(),
				Uptime:      w.service.GetUptime(),
				Hostname:    w.service.GetHostname(),
				Environment: w.service.GetEnvironment(),
			},
		},
	)
}

// GetSystemHealth godoc
// @Summary Get system health status
// @Description Get system health status and basic diagnostics
// @ID system-get-health
// @Produce plain
// @Tags System
// @Success 200 {string} string "ok"
// @Router /api/v1/system/health [get]
func (w *WebAPIServer) GetSystemHealth(c *fiber.Ctx) error {
	if w.service.Healthcheck() {
		return c.SendStatus(fiber.StatusOK)
	}

	return c.SendStatus(fiber.StatusServiceUnavailable)
}

// GetSystemResources godoc
// @Summary Get system resource usage
// @Description Get system resource usage including memory and CPU statistics
// @ID system-get-resources
// @Produce json
// @Tags System
// @success 200 {object} web.JSONResultGetSystemResources{} "successful operation"
// @Router /api/v1/system/resources [get]
func (w *WebAPIServer) GetSystemResources(c *fiber.Ctx) error {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Calculate memory usage in megabytes
	memUsage := fmt.Sprintf("%dMB", memStats.Alloc/(1024*1024))

	// Determine backend provider type
	backendProvider := w.service.GetBackendProvider()

	// Calculate disk usage (static value as example)
	diskUsage := fmt.Sprintf("%dMB", backendProvider.GetDiskUsage()/(1024*1024))

	resourceUsage := ResourceUsage{
		Memory: ResourceMetric{
			Label: "Memory Usage",
			Value: memUsage,
		},
		Disk: ResourceMetric{
			Label: "Disk Usage",
			Value: diskUsage,
		},
		Backend: ResourceBackendProvider{
			Label: "Backend Provider",
			Value: backendProvider.GetType(),
		},
	}

	return c.JSON(
		JSONResultGetSystemResources{
			Code:          fiber.StatusOK,
			Message:       "success",
			ResourceUsage: resourceUsage,
		},
	)
}
