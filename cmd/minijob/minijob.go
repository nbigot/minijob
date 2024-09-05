package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

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
			s.GetApp().Use(cors.New(cors.Config{
				AllowOrigins: s.GetWebConfig().Cors.AllowOrigins,
				AllowHeaders: s.GetWebConfig().Cors.AllowHeaders,
			}))
		}
	}
}

func WithPrometheus() webserver.ServerOption {
	return func(s *webserver.Server) {
		s.GetWebAPIServer().AddPrometheus(s.GetApp(), s.GetService().GetServiceEventChan())
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

	svc, err := service.CreateAndInitService(appConfig)
	if err != nil {
		return false, err
	}
	go svc.Run()
	defer svc.Finalize()

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
	// 127.0.0.1:443
	configFilePath := argparse()

	// start/restart the server forever (reason is reload config) unless an error occurs
	for {
		var appConfig *config.Config
		var err error
		if appConfig, err = config.LoadConfig(configFilePath); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		restartServer, serverErr := RunServer(appConfig)
		if !restartServer || serverErr != nil {
			log.Logger.Info("End program", zap.String("topic", "server"))
			os.Exit(0)
		}
		if serverErr != nil {
			os.Exit(1)
		}
	}
}
