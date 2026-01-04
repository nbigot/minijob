package metrics

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPercentilesMetrics_Init(t *testing.T) {
	// Test with standard percentile buckets
	buckets := []float64{0.5, 0.75, 0.9, 0.95, 0.99}
	pm := PercentilesMetrics{
		BucketsPercentiles: buckets,
	}

	// Init should populate arrays and calculate labels
	pm.Init()

	// Check initialization results
	assert.Equal(t, len(buckets), pm.CptPercentiles, "CptPercentiles should match buckets length")
	assert.Equal(t, len(buckets), len(pm.Values), "Values array length should match buckets length")
	assert.Equal(t, len(buckets), len(pm.PercentileLabels), "PercentileLabels array length should match buckets length")

	// Check label formatting
	expectedLabels := []string{"p50", "p75", "p90", "p95", "p99"}
	for i, percentile := range buckets {
		expectedLabel := expectedLabels[i]
		assert.Equal(t, expectedLabel, pm.PercentileLabels[i], "Label format incorrect for percentile %f", percentile)
	}

	// Check values initialization
	for i := range pm.Values {
		assert.Equal(t, int64(0), pm.Values[i], "Values should be initialized to 0")
	}
}

func TestPercentilesMetrics_Reset(t *testing.T) {
	// Create metrics with non-zero values
	buckets := []float64{0.5, 0.9, 0.99}
	pm := NewPercentilesMetrics(buckets)

	// Set some values
	pm.Values[0] = 100
	pm.Values[1] = 200
	pm.Values[2] = 300

	// Reset values
	pm.Reset()

	// Check all values were reset to 0
	for i, val := range pm.Values {
		assert.Equal(t, int64(0), val, "Value at index %d should be reset to 0", i)
	}
}

func TestPercentilesMetrics_Compute(t *testing.T) {
	tests := []struct {
		name     string
		buckets  []float64
		input    []int64
		expected []int64
	}{
		{
			name:     "Empty input",
			buckets:  []float64{0.5, 0.9, 0.99},
			input:    []int64{},
			expected: []int64{0, 0, 0},
		},
		{
			name:     "Single value",
			buckets:  []float64{0.5, 0.9, 0.99},
			input:    []int64{42},
			expected: []int64{42, 42, 42},
		},
		{
			name:     "Multiple values",
			buckets:  []float64{0.0, 0.5, 1.0},
			input:    []int64{10, 20, 30, 40, 50},
			expected: []int64{10, 30, 50},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create percentiles metrics
			pm := NewPercentilesMetrics(tt.buckets)

			// Set some non-zero values to verify Reset is called
			for i := range pm.Values {
				pm.Values[i] = 999
			}

			// Compute percentiles
			inputCopy := make([]int64, len(tt.input))
			copy(inputCopy, tt.input)
			pm.Compute(&inputCopy)

			// Check results
			assert.Equal(t, tt.expected, pm.Values, "Computed percentile values don't match expected")
		})
	}
}

func TestNewPercentilesMetrics(t *testing.T) {
	// Test the factory function
	buckets := []float64{0.1, 0.5, 0.9}
	pm := NewPercentilesMetrics(buckets)

	// Verify object was properly initialized
	require.NotNil(t, pm, "NewPercentilesMetrics should return non-nil")
	assert.Equal(t, buckets, pm.BucketsPercentiles, "BucketsPercentiles should match input")
	assert.Equal(t, len(buckets), pm.CptPercentiles, "CptPercentiles should be initialized")
	assert.Equal(t, len(buckets), len(pm.Values), "Values array should be initialized")
	assert.Equal(t, len(buckets), len(pm.PercentileLabels), "PercentileLabels array should be initialized")

	// Check label format
	expectedLabels := []string{"p10", "p50", "p90"}
	assert.True(t, reflect.DeepEqual(expectedLabels, pm.PercentileLabels),
		"Labels should match expected format: got %v, expected %v", pm.PercentileLabels, expectedLabels)
}

func TestPercentilesMetrics_FixInReset(t *testing.T) {
	// This test specifically checks for the bug in the Reset function
	// where the loop uses 'range m.CptPercentiles' instead of 'range m.Values'
	buckets := []float64{0.5, 0.9, 0.99}
	pm := NewPercentilesMetrics(buckets)

	// Set non-zero values
	for i := range pm.Values {
		pm.Values[i] = int64(i + 100)
	}

	// Call Reset (which should reset all values to 0)
	pm.Reset()

	// Verify all values are properly reset
	for i, val := range pm.Values {
		assert.Equal(t, int64(0), val, "Reset failed to clear value at index %d", i)
	}
}
