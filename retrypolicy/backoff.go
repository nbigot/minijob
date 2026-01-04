package retrypolicy

import (
	"math"
	"time"
)

type BackoffStrategyType int

const (
	Constant BackoffStrategyType = iota
	Linear
	Exponential
)

// BackoffStrategy defines the type for backoff strategies
type BackoffStrategy struct {
	//
	// The backoff strategy is a strategy that allows to increase the time between each retry.
	// The time between each retry is calculated as follow:
	//   backoff = base * (factor ^ (retry - 1))
	// where:
	//   - base is the base time between each retry
	//   - factor is the factor to increase the time between each retry
	//   - retry is the number of the current retry
	//
	// Example:
	//   base = 1
	//   factor = 2
	//   retry = 1 => backoff = 1 * (2 ^ 0) = 1
	//   retry = 2 => backoff = 1 * (2 ^ 1) = 2
	//   retry = 3 => backoff = 1 * (2 ^ 2) = 4
	//
	Type        BackoffStrategyType `yaml:"type"`                        // Type of backoff strategy
	Base        uint                `yaml:"base" example:"1000"`         // Base time between each retry (in milliseconds)
	Factor      float64             `yaml:"factor" example:"2.0"`        // Factor to increase the time between each retry
	MaxDuration uint                `yaml:"maxDuration" example:"60000"` // Maximum duration between each retry (in milliseconds)
}

// ComputeDelay calculates the delay before the next retry based on the count of failures and the backoff strategy
func (bs *BackoffStrategy) ComputeDelay(countFailures uint) time.Duration {
	switch bs.Type {
	case Constant:
		return time.Duration(bs.Base) * time.Millisecond
	case Linear:
		return time.Duration(min(bs.MaxDuration, countFailures*bs.Base)) * time.Millisecond
	case Exponential:
		return time.Duration(min(float64(bs.MaxDuration), float64(bs.Base)*math.Pow(bs.Factor, float64(countFailures)))) * time.Millisecond
	default:
		return 0 * time.Millisecond
	}
}
