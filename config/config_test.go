package config

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var templates = []string{
	filepath.Join("..", "config-templates", "standard", "config", "config.yaml"),
	filepath.Join("..", "config-templates", "standard", "config", "config.inmemory.yaml"),
	filepath.Join("..", "config-templates", "docker", "config", "config.yaml"),
}

func TestTemplatesRetryDelays(t *testing.T) {
	// The templates had base 2, factor 2 and maxDuration 60 with no type: a failed job was
	// retried after 2 ms (constant delay, in milliseconds), not after 2 s, 4 s...
	for _, path := range templates {
		conf, err := LoadConfig(path)
		require.NoError(t, err, path)
		backoff := conf.Jobs.RetryPolicy.BackoffStrategy
		assert.Equal(t, 2*time.Second, backoff.ComputeDelay(1), path)
		assert.Equal(t, 4*time.Second, backoff.ComputeDelay(2), path)
		assert.Equal(t, time.Minute, backoff.ComputeDelay(10), path)
	}
}
