package log

import (
	"bytes"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v3"
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

// memorySink receives the lines logged to "memory://".
type memorySink struct{ bytes.Buffer }

func (*memorySink) Sync() error  { return nil }
func (*memorySink) Close() error { return nil }

func TestInitLoggerWithoutCallerEncoder(t *testing.T) {
	// zap gives no default to the encoder functions missing from the configuration: with
	// "callerKey" and no "callerEncoder", the JSON encoder called a nil function at the
	// first line logged, which stopped the server.
	sink := &memorySink{}
	require.NoError(t, zap.RegisterSink("memory", func(*url.URL) (zap.Sink, error) { return sink, nil }))
	var conf zap.Config
	require.NoError(t, yaml.Unmarshal([]byte(`
level: "info"
disableCaller: false
encoding: "json"
encoderConfig:
    messageKey: "message"
    callerKey: "caller"
outputPaths: ["memory://"]
errorOutputPaths: ["stderr"]
`), &conf))

	require.NoError(t, InitLogger(&conf))
	assert.Contains(t, sink.String(), `"caller":"log/log.go:`)
}
