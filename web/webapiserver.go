package web

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
	"github.com/gofiber/fiber/v2/middleware/monitor"
	"github.com/gofiber/fiber/v2/middleware/pprof"
	"github.com/gofiber/swagger"
	"github.com/qri-io/jsonschema"
	"go.uber.org/zap"

	"github.com/nbigot/minijob/config"
	"github.com/nbigot/minijob/constants"
	_ "github.com/nbigot/minijob/docs"
	"github.com/nbigot/minijob/fiberprometheus"
	"github.com/nbigot/minijob/log"
	"github.com/nbigot/minijob/service"
)

type WebAPIServer struct {
	service            service.IService
	funcShutdownServer func()
	funcRestartServer  func()
	app                *fiber.App
	appConfig          *config.Config
	schema             jsonschema.Schema
	schemaMu           sync.Mutex // serializes the validations (see validateJSON)
}

func (w *WebAPIServer) GetFiberPrometheus() *fiberprometheus.FiberPrometheus {
	return w.service.GetMetrics().GetFiberPrometheus()
}

func (w *WebAPIServer) AddRoutes(app *fiber.App) {
	app.Use(recoverPanics)
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
		apiObservability.Get("/jobs/processing", w.GetJobsMetrics)
		apiObservability.Get("/jobs/recent", w.GetRecentJobs)
		apiObservability.Get("/jobs/stalled", w.GetStalledJobs)
		apiObservability.Get("/jobs/activity", w.GetJobsCumulativeEventCount)
		apiObservability.Get("/jobs/topics", w.GetJobsMetricsTopics)
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
		apiSwagger.Get("*", swaggerHandler())
		apiSwagger.Get("*", swaggerHandler(swagger.Config{
			URL:         "swagger.json",
			DeepLinking: false,
		}))
	}
}

func (w *WebAPIServer) ShutdownServer() {
	// the service is stopped after the HTTP server (see webserver.Server), so that the
	// requests in progress can finish
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
		registerSchemaKeywords()
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

// noFetchContext is already canceled: the validator would download remote "$ref" schemas
// while answering a request; with this context such a download fails at once, like an
// unreachable reference.
var noFetchContext = func() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}()

// validateJSON validates a request body against the schema. The validator changes the
// schema and its global registry while it validates, so validations run one at a time.
// The errors are sorted by field path (the validator's order changed from one request to
// the next); errors on the same path keep their order.
func (w *WebAPIServer) validateJSON(payload []byte) ([]jsonschema.KeyError, error) {
	keyErrors, err := w.validateBytes(payload)
	sort.SliceStable(keyErrors, func(i, j int) bool {
		return keyErrors[i].PropertyPath < keyErrors[j].PropertyPath
	})
	return keyErrors, err
}

func (w *WebAPIServer) validateBytes(payload []byte) ([]jsonschema.KeyError, error) {
	w.schemaMu.Lock()
	defer w.schemaMu.Unlock() // also when the validator panics
	return w.schema.ValidateBytes(noFetchContext, payload)
}

// recoverPanics answers 500 when a request handler panics: the panic ended the server.
func recoverPanics(c *fiber.Ctx) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Logger.Error(
				"Panic while answering a request",
				zap.String("topic", "server"),
				zap.String("method", "recoverPanics"),
				zap.String("request", c.Method()+" "+c.Path()),
				zap.Any("panic", r),
				zap.Stack("stack"),
			)
			err = fiber.ErrInternalServerError
		}
	}()
	return c.Next()
}

var registerSchemaKeywordsOnce sync.Once

// registerSchemaKeywords loads the JSON schema keywords once, with the draft-07 keyword
// "definitions" as an alias of "$defs": the validator lists "definitions" as unsupported,
// so every "$ref": "#/definitions/..." failed to resolve and rejected every request.
func registerSchemaKeywords() {
	registerSchemaKeywordsOnce.Do(func() {
		jsonschema.LoadDraft2019_09()
		jsonschema.RegisterKeyword("definitions", jsonschema.NewDefs)
	})
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
