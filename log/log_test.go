package log

import (
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestInitLogger(t *testing.T) {
	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)

	if err := InitLogger(&config); err != nil {
		t.Fatal(err)
	}

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
	// An invalid configuration is an error (the program reports it and exits), not a panic.
	invalidConfig := zap.Config{}
	if err := InitLogger(&invalidConfig); err == nil {
		t.Error("Expected an error for an invalid logger config")
	}
}
