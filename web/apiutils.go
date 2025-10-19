package web

import (
	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"
)

// Home godoc
// @Summary Home page
// @Description Home page
// @ID utils-home
// @Produce plain
// @Tags Utils
// @Success 200 {string} string "Welcome to MiniJob!"
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
// @Success 200 {string} string "OK"
// @Router /ping [get]
func (w *WebAPIServer) Ping(c *fiber.Ctx) error {
	return c.SendStatus(fiber.StatusOK)
}

// Healthcheck godoc
// @Summary Healthcheck server
// @Description Healthcheck server
// @ID utils-healthcheck
// @Produce plain
// @Tags Utils
// @Success 200 {string} string "OK"
// @Router /healthcheck [get]
func (w *WebAPIServer) Healthcheck(c *fiber.Ctx) error {
	if w.service.Healthcheck() {
		return c.SendStatus(fiber.StatusOK)
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
