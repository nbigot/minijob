package registry

import (
	"fmt"

	"github.com/nbigot/minijob/config"
	"github.com/nbigot/minijob/jobbackendprovider"
	"github.com/nbigot/minijob/jobbackendprovider/inmemoryprovider"
	"github.com/nbigot/minijob/jobbackendprovider/mockprovider"
	"github.com/nbigot/minijob/jobbackendprovider/redisprovider"
	"go.uber.org/zap"
)

// Factory is used to register functions creating new job backend provider instances.
type Factory = func(logger *zap.Logger, conf *config.Config) (jobbackendprovider.IJobBackendProvider, error)

var registry = make(map[string]Factory)

func Register(name string, factory Factory) error {
	// Register a job backend provider into the registry
	if name == "" {
		return fmt.Errorf("error registering job backend provider: name cannot be empty")
	}

	if factory == nil {
		return fmt.Errorf("error registering job backend provider '%v': factory cannot be empty", name)
	}

	if _, found := registry[name]; found {
		return fmt.Errorf("error registering job backend provider '%v': already registered", name)
	}

	registry[name] = factory
	return nil
}

func GetFactory(name string) (Factory, error) {
	// Get the job backend provider factory from the registry for the given job backend provider type name
	if _, found := registry[name]; !found {
		return nil, fmt.Errorf("error creating job backend provider. No such provider type exists: '%v'", name)
	}
	return registry[name], nil
}

func Initialize() error {
	return SetupJobBackendProviders()
}

func Finalize() {
	FinalizeJobBackendProviders()
}

func SetupJobBackendProviders() error {
	// Registering job backend providers is hardcoded,
	// if you add a new type of job backend provider you must also register there.
	var err error

	err = Register("mock", mockprovider.NewMockJobBackendProvider)
	if err != nil {
		return err
	}

	err = Register("inMemory", inmemoryprovider.NewInMemoryJobBackendProvider)
	if err != nil {
		return err
	}

	err = Register("redis", redisprovider.NewRedisJobBackendProvider)
	if err != nil {
		return err
	}

	return nil
}

func FinalizeJobBackendProviders() {
	// clear registry map
	for k := range registry {
		delete(registry, k)
	}
}

func NewJobBackendProvider(conf *config.Config) (jobbackendprovider.IJobBackendProvider, error) {
	// find the specific job backend provider factory from the registry
	if factory, err := GetFactory(conf.Backend.Type); err != nil {
		return nil, err
	} else {
		// The provider has it's own logger
		spLogger, err := NewLogger(&conf.Backend.LoggerConfig)
		if err != nil {
			return nil, err
		}
		return factory(spLogger, conf)
	}
}

func NewLogger(loggerConfig *zap.Config) (*zap.Logger, error) {
	// Create a new logger
	logger, err := loggerConfig.Build()
	if err != nil {
		return nil, err
	}
	return logger, nil
}
