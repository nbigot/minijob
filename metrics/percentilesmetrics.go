package metrics

import (
	"fmt"
)

type PercentilesMetrics struct {
	CptPercentiles     int
	PercentileLabels   []string
	BucketsPercentiles []float64
	Values             []int64
}

func (m *PercentilesMetrics) Init() {
	m.CptPercentiles = len(m.BucketsPercentiles)
	m.Values = make([]int64, m.CptPercentiles)
	m.PercentileLabels = make([]string, m.CptPercentiles)
	for i, percentile := range m.BucketsPercentiles {
		m.PercentileLabels[i] = fmt.Sprintf("p%d", int(percentile*100.0))
		m.Values[i] = 0
	}
}

func (m *PercentilesMetrics) Reset() {
	for i := range m.CptPercentiles {
		m.Values[i] = 0
	}
}

func (m *PercentilesMetrics) Compute(inputData *[]int64) {
	m.Reset()
	Percentiles(inputData, &m.BucketsPercentiles, &m.Values)
}

func NewPercentilesMetrics(bucketsPercentiles []float64) *PercentilesMetrics {
	m := PercentilesMetrics{
		BucketsPercentiles: bucketsPercentiles,
	}
	m.Init()
	return &m
}
