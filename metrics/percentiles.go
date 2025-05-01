package metrics

import "math"

// Percentiles calculates percentile values from a sorted array of int64 values
// inputData: pointer to sorted slice of int64 values
// percentiles: pointer to slice of float64 values between 0.0 and 1.0
// outputData: pointer to pre-allocated slice to store results (must be same length as percentiles)
func Percentiles(inputData *[]int64, percentiles *[]float64, outputData *[]int64) {
	lenInputData := len(*inputData)

	// If input data is empty, set all percentiles to 0
	if lenInputData == 0 {
		for i := range *percentiles {
			(*outputData)[i] = 0
		}
		return
	}

	// Calculate each percentile value
	for i, percentile := range *percentiles {
		// Calculate the index of the percentile in the sorted list
		// Subtract 1 because array is 0-indexed
		index := int(math.Ceil(float64(lenInputData) * percentile))
		if index > 0 {
			index--
		}

		// Set the result directly at the correct index
		(*outputData)[i] = (*inputData)[index]
	}
}
