package log

import (
	"fmt"

	"go.uber.org/zap"
)

var Logger *zap.Logger
var SugarLogger *zap.SugaredLogger
var LoggerConfig *zap.Config

// InitLogger builds the logger. An invalid configuration is reported (and returned): the
// caller stops with an error.
func InitLogger(loggerConfig *zap.Config) error {
	logger, err := loggerConfig.Build()
	if err != nil {
		fmt.Println("Error while initializing logger")
		return err
	}
	LoggerConfig = loggerConfig
	Logger = logger
	SugarLogger = Logger.Sugar()
	Logger.Info(
		"Initialize logger",
		zap.String("topic", "server"),
		zap.String("method", "InitLogger"),
	)
	return nil
}
