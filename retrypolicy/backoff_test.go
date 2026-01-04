package retrypolicy

import (
	"testing"
	"time"
)

func TestComputeDelay(t *testing.T) {
	tests := []struct {
		name          string
		strategy      BackoffStrategy
		countFailures uint
		expectedDelay time.Duration
	}{
		{
			name: "Constant strategy",
			strategy: BackoffStrategy{
				Type: Constant,
				Base: 2000,
			},
			countFailures: 3,
			expectedDelay: 2 * time.Second,
		},
		{
			name: "Linear strategy",
			strategy: BackoffStrategy{
				Type:        Linear,
				Base:        2000,
				MaxDuration: 99000,
			},
			countFailures: 3,
			expectedDelay: 6 * time.Second,
		},
		{
			name: "Linear strategy (max duration)",
			strategy: BackoffStrategy{
				Type:        Linear,
				Base:        2000,
				MaxDuration: 5000,
			},
			countFailures: 3,
			expectedDelay: 5 * time.Second,
		},
		{
			name: "Exponential strategy",
			strategy: BackoffStrategy{
				Type:        Exponential,
				Base:        2000,
				MaxDuration: 99000,
				Factor:      2.0,
			},
			countFailures: 3,
			expectedDelay: 16 * time.Second, // 2x2^3 = 16
		},
		{
			name: "Exponential strategy (factor > 1)",
			strategy: BackoffStrategy{
				Type:        Exponential,
				Base:        1000,
				MaxDuration: 99000,
				Factor:      1.4,
			},
			countFailures: 4,
			expectedDelay: 3841 * time.Millisecond, // 1.4^4 = 8
		},
		{
			name: "Exponential strategy (max duration)",
			strategy: BackoffStrategy{
				Type:        Exponential,
				Base:        2000,
				MaxDuration: 1000,
				Factor:      2.0,
			},
			countFailures: 3,
			expectedDelay: 1 * time.Second,
		},
		{
			name: "Default case",
			strategy: BackoffStrategy{
				Type: 99, // Invalid type
				Base: 2000,
			},
			countFailures: 3,
			expectedDelay: 0 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay := tt.strategy.ComputeDelay(tt.countFailures)
			if delay != tt.expectedDelay {
				t.Errorf("expected %v, got %v", tt.expectedDelay, delay)
			}
		})
	}
}
