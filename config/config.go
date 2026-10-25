package config

import (
	"errors"
	"fmt"
	"io"

	"github.com/nbigot/minijob/log"
	"github.com/nbigot/minijob/retrypolicy"

	"os"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type WebServerConfig struct {
	HTTP struct {
		Enable  bool   `yaml:"enable"`
		Address string `yaml:"address"`
	}
	Logs struct {
		Enable bool `yaml:"enable"`
	}
	Cors struct {
		Enable       bool   `yaml:"enable"`
		AllowOrigins string `yaml:"allowOrigins"`
		AllowHeaders string `yaml:"allowHeaders"`
	}
	Monitor struct {
		Enable bool `yaml:"enable"`
	}
	Metrics struct {
		Enable bool `yaml:"enable"`
	}
	Swagger struct {
		Enable bool `yaml:"enable"`
	}
}

type ConsulConfig struct {
	Enable bool `yaml:"enable"`
	Server struct {
		Address               string `yaml:"address"`               // Consul server address of the service (required)
		Port                  int    `yaml:"port"`                  // Consul server port of the service (required)
		Token                 string `yaml:"token"`                 // Consul token (optional)
		ReplaceExistingChecks bool   `yaml:"replaceExistingChecks"` // Replace existing checks (default: false)
		// Interval string   `yaml:"interval"`
		// Timeout  string   `yaml:"timeout"`
	} `yaml:"server"`
	Service struct {
		ID        string   `yaml:"id"`        // ID of the service (required)
		Name      string   `yaml:"name"`      // Name of the service (required)
		Tags      []string `yaml:"tags"`      // Tags of the service (optional)
		Namespace string   `yaml:"namespace"` // Consul namespace (optional) (enterprise only)
		Partition string   `yaml:"partition"` // Consul partition (optional) (enterprise only)
	} `yaml:"service"`
	HealthCheck struct {
		Enable   bool   `yaml:"enable"`
		CheckId  string `yaml:"checkId"`
		Interval string `yaml:"interval"`
		Timeout  string `yaml:"timeout"`
		URL      string `yaml:"url"`
	} `yaml:"healthCheck"`
}

type Config struct {
	Environment string `yaml:"environment" example:"development"` // Environment name (development, staging, production, etc.)
	Backend     struct {
		Type         string     `yaml:"type" example:"redis"` // Type of backend to use (redis, inMemory)
		LoggerConfig zap.Config `yaml:"logger"`
		LogVerbosity int        `yaml:"logVerbosity"`
		Redis        struct {
			// https://github.com/Go-SQL-Driver/MySQL/?tab=readme-ov-file#dsn-data-source-name
			URL            string `yaml:"url" example:"redis://user:password@localhost:6379/3?dial_timeout=3&db=1&read_timeout=6s&max_retries=2&client_name=minijob"`
			MaxJobs        uint   `yaml:"maxJobs" example:"1000"`
			FinishedJobTTL uint   `yaml:"finishedJobTTL" example:"3600"` // Time to live for finished jobs in seconds
		} `yaml:"redis"`
		InMemory struct {
			MaxJobs                 uint   `yaml:"maxJobs" example:"1000"`
			WriteFrequency          int    `yaml:"writeFrequency" example:"300"`
			EnablePersistentStorage bool   `yaml:"enablePersistentStorage"`
			Directory               string `yaml:"directory"`
			Filename                string `yaml:"filename"`
		} `yaml:"inMemory"`
	}
	Jobs struct {
		// BulkFlushFrequency        int  `yaml:"bulkFlushFrequency"`
		// BulkMaxSize               int  `yaml:"bulkMaxSize"`
		// ChannelBufferSize         int  `yaml:"channelBufferSize"`
		// MaxIteratorsPerStream     int  `yaml:"maxAllowedIteratorsPerStream"`
		// MaxMessagePerGetOperation uint `yaml:"maxMessagePerGetOperation"`
		LogVerbosity   int  `yaml:"logVerbosity"`
		MaxAllowedJobs uint `yaml:"maxAllowedJobs" example:"25"` // Max number of jobs allowed (0 means no limit)
		JsonSchema     struct {
			Enable bool   `yaml:"enable"` // Enable JSON schema validation
			Path   string `yaml:"path"`   // Path to JSON schema file
		} `yaml:"jsonSchema"`
		DefaultVisibilityTimeout uint `yaml:"defaultVisibilityTimeout"` // Default duration (in seconds) to keep the job hidden from the queue after it is fetched.
		MaxVisibilityTimeout     uint `yaml:"maxVisibilityTimeout"`     // Maximum duration (in seconds) to keep the job hidden from the queue after it is fetched.
		RetentionPolicy          struct {
			Enable    bool `yaml:"enable"`    // Enable retention policy
			Interval  int  `yaml:"interval"`  // Interval in seconds to check for expired jobs
			MaxJobAge int  `yaml:"maxJobAge"` // The duration to keep the job in the backend after it has been completed
			MaxJobs   int  `yaml:"maxJobs"`   // The maximum number of jobs to keep in the backend after it has been completed
		} `yaml:"retentionPolicy"`
		RetryPolicy retrypolicy.RetryPolicy `yaml:"retryPolicy"`
	} `yaml:"jobs"`
	LoggerConfig zap.Config `yaml:"logger"`
	EventsLogger struct {
		Enable   bool   `yaml:"enable"`
		FilePath string `yaml:"filepath"`
	} `yaml:"eventsLogger"`
	WebServer WebServerConfig `yaml:"webserver"`
	Consul    ConsulConfig    `yaml:"consul"`
}

// Visibility timeouts (in seconds) when the configuration has none: those of the templates.
const (
	defaultVisibilityTimeout = 60
	maxVisibilityTimeout     = 86400
)

// setVisibilityTimeouts gives values to the visibility timeouts missing from the
// configuration: when they were 0, a job pulled without visibility timeout was failed at the
// next check, and a pull with one was refused. The default visibility timeout is at most
// the maximum.
func (c *Config) setVisibilityTimeouts() error {
	if c.Jobs.MaxVisibilityTimeout == 0 {
		c.Jobs.MaxVisibilityTimeout = maxVisibilityTimeout
	}
	if c.Jobs.DefaultVisibilityTimeout == 0 {
		c.Jobs.DefaultVisibilityTimeout = min(defaultVisibilityTimeout, c.Jobs.MaxVisibilityTimeout)
	}
	if c.Jobs.DefaultVisibilityTimeout > c.Jobs.MaxVisibilityTimeout {
		return fmt.Errorf("jobs.defaultVisibilityTimeout (%d) is greater than jobs.maxVisibilityTimeout (%d)",
			c.Jobs.DefaultVisibilityTimeout, c.Jobs.MaxVisibilityTimeout)
	}
	return nil
}

func LoadConfig(filename string) (*Config, error) {
	var configuration Config

	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("error while opening configuration file %s : %s", filename, err.Error())
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Logger.Error(
				"Failed to close config file",
				zap.String("topic", "config"),
				zap.String("method", "LoadConfig"),
				zap.String("filename", filename),
				zap.Error(err),
			)
		}
	}()

	decoder := yaml.NewDecoder(file)
	err = decoder.Decode(&configuration)
	if err == nil {
		// Decode reads one document: the file must end there. When the first key is
		// indented and the next ones are not, the first document ends after the first key.
		var rest yaml.Node
		if err = decoder.Decode(&rest); err == nil {
			err = errors.New("only one YAML document is allowed")
		} else if errors.Is(err, io.EOF) {
			err = nil
		}
	}
	if err != nil {
		return nil, fmt.Errorf("error while parsing configuration file %s : %s", filename, err.Error())
	}
	if err := configuration.setVisibilityTimeouts(); err != nil {
		return nil, fmt.Errorf("invalid configuration file %s : %s", filename, err.Error())
	}

	if err := log.InitLogger(&configuration.LoggerConfig); err != nil {
		return nil, err
	}
	log.Logger.Info(
		"Configuration loaded",
		zap.String("topic", "server"),
		zap.String("method", "LoadConfig"),
		zap.String("filename", filename),
	)

	return &configuration, nil
}
