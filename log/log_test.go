package log

import (
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestInitLogger(t *testing.T) {
	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)

	InitLogger(&config)

	if Logger == nil {
		t.Error("Expected Logger to be initialized, but it is nil")
	}

	if SugarLogger == nil {
		t.Error("Expected SugarLogger to be initialized, but it is nil")
	}

	if LoggerConfig != &config {
		t.Error("Expected LoggerConfig to be set to the provided config")
	}
}

func TestInitLoggerWithInvalidConfig(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic for invalid logger config, but did not panic")
		}
	}()

	invalidConfig := zap.Config{}
	InitLogger(&invalidConfig)
}
