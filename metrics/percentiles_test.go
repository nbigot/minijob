package metrics

import (
	"math"
	"reflect"
	"testing"
)

func TestPercentiles(t *testing.T) {
	tests := []struct {
		name        string
		inputData   []int64
		percentiles []float64
		want        []int64
	}{
		{
			name:        "Empty input",
			inputData:   []int64{},
			percentiles: []float64{0.5, 0.9, 0.99},
			want:        []int64{0, 0, 0}, // Default value for empty input
		},
		{
			name:        "Single value",
			inputData:   []int64{100},
			percentiles: []float64{0.1, 0.5, 0.9},
			want:        []int64{100, 100, 100}, // All percentiles should return the same value
		},
		{
			name:        "Typical case",
			inputData:   []int64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100},
			percentiles: []float64{0.0, 0.25, 0.5, 0.75, 1.0},
			want:        []int64{10, 30, 50, 80, 100}, // Expected values for each percentile
		},
		{
			name:        "Edge percentiles",
			inputData:   []int64{15, 20, 35, 40, 50},
			percentiles: []float64{0.0, 1.0},
			want:        []int64{15, 50}, // Min and max
		},
		{
			name:        "Non-sorted input",
			inputData:   []int64{50, 20, 30, 40, 10}, // Intentionally unsorted
			percentiles: []float64{0.5, 0.8},
			want:        []int64{30, 40}, // Function assumes sorted input
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputCopy := make([]int64, len(tt.inputData))
			copy(inputCopy, tt.inputData)

			got := make([]int64, len(tt.percentiles))
			Percentiles(&inputCopy, &tt.percentiles, &got)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Percentile() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test edge cases and potential issues
func TestPercentilesEdgeCases(t *testing.T) {
	t.Run("Large dataset", func(t *testing.T) {
		// Create a large dataset
		input := make([]int64, 10000)
		for i := range input {
			input[i] = int64(i)
		}

		percentiles := []float64{0.01, 0.5, 0.99}
		output := []int64{0, 0, 0}

		Percentiles(&input, &percentiles, &output)

		// Expected: close to 1%, 50% and 99% of the array length
		expected := []int64{100, 5000, 9900}

		// Allow for some imprecision in large datasets
		for i, val := range output {
			if math.Abs(float64(val-expected[i])) > 10.0 {
				t.Errorf("Large dataset percentile[%f] = %d, expected around %d",
					percentiles[i], val, expected[i])
			}
		}
	})
}
