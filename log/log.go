package log

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Logger *zap.Logger
var SugarLogger *zap.SugaredLogger
var LoggerConfig *zap.Config

// InitLogger builds the logger. An invalid configuration is reported (and returned): the
// caller stops with an error.
func InitLogger(loggerConfig *zap.Config) error {
	logger, err := Build(loggerConfig)
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

// Build builds a logger from its configuration. zap gives no default to the encoder
// functions missing from the configuration, and the JSON encoder calls the caller encoder
// without checking it: without "callerEncoder", the first line logged stopped the server.
// It is "short", as for an empty "callerEncoder".
func Build(loggerConfig *zap.Config) (*zap.Logger, error) {
	if loggerConfig.EncoderConfig.EncodeCaller == nil {
		loggerConfig.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder
	}
	return loggerConfig.Build()
}
