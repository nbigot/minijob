package web

import (
	"fmt"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
	"github.com/gofiber/fiber/v2/middleware/monitor"
	"github.com/gofiber/fiber/v2/middleware/pprof"
	"github.com/gofiber/swagger"
	"github.com/qri-io/jsonschema"

	"github.com/nbigot/minijob/config"
	"github.com/nbigot/minijob/constants"
	_ "github.com/nbigot/minijob/docs"
	"github.com/nbigot/minijob/fiberprometheus"
	"github.com/nbigot/minijob/service"
)

type WebAPIServer struct {
	service            service.IService
	funcShutdownServer func()
	funcRestartServer  func()
	app                *fiber.App
	appConfig          *config.Config
	schema             jsonschema.Schema
}

func (w *WebAPIServer) GetFiberPrometheus() *fiberprometheus.FiberPrometheus {
	return w.service.GetMetrics().GetFiberPrometheus()
}

func (w *WebAPIServer) AddRoutes(app *fiber.App) {
	app.Get("/", w.Home)

	// Optimization: order of routes registration matters for performance
	// Please register most used routes first
	api := app.Group("/api/v1")

	apiJob := api.Group("/job")
	apiJob.Get("/:"+constants.JobUuidParam, w.GetJob)
	apiJob.Post("/", w.CreateJob)
	apiJob.Post("/pull", w.PullJob)
	apiJob.Post("/:"+constants.JobUuidParam+"/succeed", w.SetJobAsSuccessful)
	apiJob.Post("/:"+constants.JobUuidParam+"/cancel", w.CancelJob)
	apiJob.Post("/:"+constants.JobUuidParam+"/fail", w.FailJob)
	apiJob.Post("/:"+constants.JobUuidParam+"/visibilitytimeout", w.ChangeVisibilityTimeoutJob)
	apiJob.Post("/:"+constants.JobUuidParam+"/clone", w.CloneJob)
	apiJob.Delete("/:"+constants.JobUuidParam, w.DeleteJob)

	apiJobs := api.Group("/jobs")
	apiJobs.Get("/", w.GetJobs)
	apiJobs.Get("/all", w.GetAllJobs)
	apiJobs.Get("/oldest", w.GetOldestJobs)
	apiJobs.Get("/stalled", w.GetStalledJobs)
	apiJobs.Get("/recent", w.GetRecentJobs)
	apiJobs.Delete("/queued", w.DeleteQueuedJobs)
	apiJobs.Delete("/", w.DeleteAllJobs)

	apiTopics := api.Group("/topics")
	apiTopics.Get("/", w.GetTopics)

	apiResources := api.Group("/resources")
	apiResources.Get("/", w.GetLockedResources)
	apiResources.Post("/unlock", w.UnlockAllResources)

	apiResource := api.Group("/resource")
	apiResource.Post("/:"+constants.ResourceNameQueryParam+"/unlock", w.UnlockResource)

	apiAdmin := api.Group("/admin")
	apiAdmin.Post("/server/shutdown", w.ApiServerShutdown)
	apiAdmin.Post("/server/restart", w.ApiServerRestart)

	apiSystem := api.Group("/system")
	apiSystem.Get("/info", w.GetSystemInfo)
	apiSystem.Get("/health", w.GetSystemHealth)
	apiSystem.Get("/resources", w.GetSystemResources)

	if w.appConfig.WebServer.Metrics.Enable {
		apiObservability := api.Group("/metrics")
		apiObservability.Get("/jobs/topics", w.GetJobsMetricsTopics)
		apiObservability.Get("/jobs/stalled", w.GetStalledJobs)
		apiObservability.Get("/resources", w.GetResourcesMetrics)
		apiObservability.Get("/topics", w.GetTopicsStats)
	}

	// Add healthcheck
	app.Get("/ping", w.Ping)
	app.Get("/healthcheck", w.Healthcheck)

	healthcheckConfig := healthcheck.Config{
		LivenessProbe: func(c *fiber.Ctx) bool {
			return true
		},
		ReadinessProbe: func(c *fiber.Ctx) bool {
			return w.service.Healthcheck()
		},
		LivenessEndpoint:  "/livez",
		ReadinessEndpoint: "/readyz",
	}
	app.Use(healthcheck.New(healthcheckConfig))

	if w.appConfig.WebServer.Monitor.Enable {
		app.Get("/monitor", monitor.New())
		app.Use(pprof.New())
	}

	if w.appConfig.WebServer.Swagger.Enable {
		// example of url to swagger gui: "http://127.0.0.1:8080/docs/index.html"
		// url to swagger json document: "http://127.0.0.1:8080/docs/doc.json"
		// Create swagger routes group.
		apiSwagger := app.Group("/docs")

		// Routes for GET method:
		apiSwagger.Get("*", swagger.HandlerDefault)
		apiSwagger.Get("*", swagger.New(swagger.Config{
			URL:         "swagger.json",
			DeepLinking: false,
		}))
	}
}

func (w *WebAPIServer) ShutdownServer() {
	_ = w.service.Stop()
	w.funcShutdownServer()
}

func (w *WebAPIServer) RestartServer() {
	w.funcRestartServer()
}

// ApiServerShutdown godoc
// @Summary Shutdown server
// @Description Shutdown server
// @ID server-shutdown
// @Accept json
// @Produce json
// @Tags Admin
// @success 200 {object} web.JSONResultSuccess{} "successful operation"
// @Router /api/v1/admin/server/shutdown [post]
func (w *WebAPIServer) ApiServerShutdown(c *fiber.Ctx) error {
	w.ShutdownServer()
	return c.JSON(
		JSONResultSuccess{
			Code:    fiber.StatusOK,
			Message: "success",
		},
	)
}

// ApiServerRestart godoc
// @Summary Restart server
// @Description Restart server
// @ID server-restart
// @Accept json
// @Produce json
// @Tags Admin
// @success 200 {object} web.JSONResultSuccess{} "successful operation"
// @Router /api/v1/admin/server/restart [post]
func (w *WebAPIServer) ApiServerRestart(c *fiber.Ctx) error {
	w.RestartServer()
	return c.JSON(
		JSONResultSuccess{
			Code:    fiber.StatusOK,
			Message: "success",
		},
	)
}

func (w *WebAPIServer) GetFiberApp() *fiber.App {
	return w.app
}

func (w *WebAPIServer) Init() error {
	if w.appConfig.Jobs.JsonSchema.Enable {
		// Read the JSON schema file
		schemaBytes, err := os.ReadFile(w.appConfig.Jobs.JsonSchema.Path)
		if err != nil {
			return fmt.Errorf("error reading schema file: %v", err)
		}

		// Parse the JSON schema
		w.schema = jsonschema.Schema{}
		if err := w.schema.UnmarshalJSON(schemaBytes); err != nil {
			return fmt.Errorf("error unmarshaling schema: %v", err)
		}
	} else {
		// Because json schema validation is disabled, set schema to be empty
		// Needed when server restarts and config has changed
		w.schema = jsonschema.Schema{}
	}
	return nil
}

func NewWebAPIServer(appConfig *config.Config, fiberConfig fiber.Config, service service.IService, funcShutdownServer func(), funcRestartServer func()) *WebAPIServer {
	return &WebAPIServer{
		service:            service,
		funcShutdownServer: funcShutdownServer,
		funcRestartServer:  funcRestartServer,
		app:                fiber.New(fiberConfig),
		appConfig:          appConfig,
	}
}
