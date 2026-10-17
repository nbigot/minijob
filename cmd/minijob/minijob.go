package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/nbigot/minijob/config"
	"github.com/nbigot/minijob/jobbackendprovider/registry"
	"github.com/nbigot/minijob/log"
	"github.com/nbigot/minijob/service"
	"github.com/nbigot/minijob/web"
	"github.com/nbigot/minijob/web/webserver"
	"go.uber.org/zap"
)

// This variable is set at compile time with ldflags arg
//
// Example:
//
//	$ go build -ldflags="-X 'main.Version=v1.0.0'" cmd/minijob/minijob.go
var Version = "v0.0.0"

func argparse() string {
	showVersion := flag.Bool("version", false, "Show version")
	configFilePath := flag.String("config", "config.yaml", "Filepath to config.yaml")
	flag.Parse()

	if *showVersion {
		fmt.Printf("Version: %s\n", Version)
		os.Exit(0)
	}

	return *configFilePath
}

func WithFiberLogger() webserver.ServerOption {
	return func(s *webserver.Server) {
		if s.GetWebConfig().Logs.Enable {
			s.GetApp().Use(logger.New(web.GetFiberLogger()))
		}
	}
}

func WithCors() webserver.ServerOption {
	return func(s *webserver.Server) {
		if s.GetWebConfig().Cors.Enable {
			handler, err := newCors(corsConfig(s.GetWebConfig().Cors.AllowOrigins, s.GetWebConfig().Cors.AllowHeaders))
			if err != nil {
				s.SetInitError(err)
				return
			}
			s.GetApp().Use(handler)
		}
	}
}

// corsConfig is the CORS configuration of the server. The origins are trimmed: Fiber looks
// for a wildcard ("https://*.example.com") before trimming each entry, so a wildcard
// entry after ", " never matched.
func corsConfig(allowOrigins, allowHeaders string) cors.Config {
	origins := strings.Split(allowOrigins, ",")
	for i, origin := range origins {
		origins[i] = strings.TrimSpace(origin)
	}
	return cors.Config{AllowOrigins: strings.Join(origins, ","), AllowHeaders: allowHeaders}
}

// newCors creates the CORS middleware. Fiber panics on an invalid origin: the panic is
// returned as an error, so that the server stops with a message instead of crashing.
func newCors(config cors.Config) (handler fiber.Handler, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	return cors.New(config), nil
}

func WithPrometheus() webserver.ServerOption {
	return func(s *webserver.Server) {
		if s.GetWebConfig().Metrics.Enable {
			app := s.GetApp()
			fp := s.GetWebAPIServer().GetFiberPrometheus()
			fp.RegisterAt(app, "/metrics")
			fp.SetSkipPaths([]string{
				"/ping",
				"/healthcheck",
				"/livez",
				"/readyz",
				"/api/v1/admin/server/restart",
				"/api/v1/admin/server/shutdown",
			}) // Optional: Remove some paths from metrics
			_ = app.Use(fp.Middleware)
		}
	}
}

func WithAPIRoutes() webserver.ServerOption {
	return func(s *webserver.Server) {
		s.GetWebAPIServer().AddRoutes(s.GetApp())
	}
}

func RunServer(appConfig *config.Config) (bool, error) {
	var err error
	var apiServer *webserver.Server

	log.Logger.Info(
		"Server version",
		zap.String("topic", "server"),
		zap.String("version", Version),
	)

	if err = registry.Initialize(); err != nil {
		log.Logger.Error("Startup error", zap.String("topic", "backend providers"), zap.Error(err))
		return false, err
	}
	defer registry.Finalize()

	svc, err := service.CreateAndInitService(appConfig, Version)
	if err != nil {
		return false, err
	}
	go func() {
		if err := svc.Run(); err != nil {
			log.Logger.Error("Error running service", zap.Error(err))
		}
	}()
	defer func() {
		if err := svc.Finalize(); err != nil {
			log.Logger.Error("Error finalizing API server", zap.Error(err))
		}
	}()

	// create api server (HTTP server)
	apiServer = webserver.NewServer(log.Logger, web.GetFiberConfig(), appConfig, svc)
	if err = apiServer.Initialize(context.Background(), WithFiberLogger(), WithCors(), WithPrometheus(), WithAPIRoutes()); err != nil {
		log.Logger.Error("Cannot initialize server", zap.String("topic", "server"), zap.Error(err))
		return false, err
	}
	defer apiServer.Finalize()
	err = apiServer.Start()

	switch {
	case err == nil:
		log.Logger.Info("Server stopped", zap.String("topic", "server"))
		return false, nil
	case errors.Is(err, webserver.ErrRequestRestart):
		log.Logger.Info("Restarting server ...", zap.String("topic", "server"))
		return true, nil
	default:
		log.Logger.Error("Server stopped with error", zap.String("topic", "server"))
		return false, err
	}
}

// @title MiniJob API
// @version 1.0
// @description This documentation describes MiniJob API
// @license.name MIT
// @license.url https://github.com/nbigot/minijob/blob/main/LICENSE
// @host 127.0.0.1:8080
// @BasePath /
func main() {
	os.Exit(run(argparse()))
}

// run starts the server, and starts it again with a reloaded configuration when a restart
// is requested. It returns the exit status: 0 after a normal stop, 1 after an error.
func run(configFilePath string) int {
	for {
		appConfig, err := config.LoadConfig(configFilePath)
		if err != nil {
			fmt.Println(err)
			return 1
		}

		restartServer, serverErr := RunServer(appConfig)
		if serverErr != nil {
			log.Logger.Info("End program", zap.String("topic", "server"))
			return 1
		}
		if !restartServer {
			log.Logger.Info("End program", zap.String("topic", "server"))
			return 0
		}
	}
}
