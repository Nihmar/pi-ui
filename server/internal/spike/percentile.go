package spike

import (
	"math"
	"sort"
)

// percentile returns the nearest-rank percentile of sorted samples: the smallest
// sample that is at or above p percent of the data. With fewer than 100 samples a
// linear interpolation would suggest a precision the measurement does not have,
// which is exactly what the acceptance matrix thresholds tolerate.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	switch {
	case p <= 0:
		return sorted[0]
	case p >= 100:
		return sorted[len(sorted)-1]
	}
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// percentileOf copies and sorts samples before taking the percentile, so callers
// can keep the raw sample order they measured in.
func percentileOf(samples []float64, p float64) float64 {
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	return percentile(sorted, p)
}

// maxFloat returns the largest sample, or 0 for an empty slice.
func maxFloat(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	best := samples[0]
	for _, sample := range samples[1:] {
		if sample > best {
			best = sample
		}
	}
	return best
}
