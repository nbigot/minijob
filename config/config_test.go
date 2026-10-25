package config

import (
	"os"
	"path/filepath"
	"strings"
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

// loadTemplate loads the load test template with the given replacements.
func loadTemplate(t *testing.T, replacements ...string) (*Config, error) {
	t.Helper()
	template, err := os.ReadFile(filepath.Join("..", "config-templates", "standard", "config", "config.loadtest.yaml"))
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "config.yaml")
	text := strings.NewReplacer(replacements...).Replace(string(template))
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	return LoadConfig(path)
}

func TestVisibilityTimeoutDefaults(t *testing.T) {
	// Without these keys, both were 0: a pull without visibility timeout gave the job 0 s
	// (it failed at the next check), and a pull with one was refused ("maximum allowed value
	// is 0").
	const defaultLine = "    defaultVisibilityTimeout: 30  # Default visibility timeout in seconds (30 sec)\n"
	const maxLine = "    maxVisibilityTimeout: 600 # Maximum visibility timeout in seconds (10 minutes)\n"
	visibility := func(conf *Config) [2]uint {
		return [2]uint{conf.Jobs.DefaultVisibilityTimeout, conf.Jobs.MaxVisibilityTimeout}
	}

	conf, err := loadTemplate(t)
	require.NoError(t, err)
	assert.Equal(t, [2]uint{30, 600}, visibility(conf))
	conf, err = loadTemplate(t, defaultLine, "", maxLine, "")
	require.NoError(t, err)
	assert.Equal(t, [2]uint{60, 86400}, visibility(conf), "the values of the templates")
	conf, err = loadTemplate(t, defaultLine, "", maxLine, "    maxVisibilityTimeout: 20\n")
	require.NoError(t, err)
	assert.Equal(t, [2]uint{20, 20}, visibility(conf), "a missing default is at most the maximum")
	conf, err = loadTemplate(t, maxLine, "")
	require.NoError(t, err)
	assert.Equal(t, [2]uint{30, 86400}, visibility(conf))

	_, err = loadTemplate(t, defaultLine, "    defaultVisibilityTimeout: 700\n")
	assert.ErrorContains(t, err, "jobs.defaultVisibilityTimeout (700) is greater than jobs.maxVisibilityTimeout (600)")
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

func TestConfigFileIsReadToTheEnd(t *testing.T) {
	// The YAML decoder reads one document: when the first key was indented and the next
	// ones were not, the first document ended after the first key and the rest of the file
	// was ignored without an error (in-memory backend, no job limit...).
	_, err := loadTemplate(t, "\nenvironment: \"loadtest\"\n", "\n    environment: \"loadtest\"\n")
	assert.ErrorContains(t, err, "error while parsing configuration file")
	assert.ErrorContains(t, err, "did not find expected <document start>")
	_, err = loadTemplate(t, "\njobs:\n", "\n---\njobs:\n")
	assert.ErrorContains(t, err, "only one YAML document is allowed")

	// the end of the document
	template, err := os.ReadFile(filepath.Join("..", "config-templates", "standard", "config", "config.loadtest.yaml"))
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, append(template, "...\n# end\n"...), 0o600))
	conf, err := LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "loadtest", conf.Environment)
	assert.Equal(t, "minijob", conf.LoggerConfig.InitialFields["app"], "the last key")
}
