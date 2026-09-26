package spike

import (
	"math"
	"testing"
)

func TestPercentileEmptyAndSingle(t *testing.T) {
	if got := percentile(nil, 50); got != 0 {
		t.Fatalf("percentile(nil, 50) = %v, want 0", got)
	}
	if got := percentile([]float64{7}, 50); got != 7 {
		t.Fatalf("percentile([7], 50) = %v, want 7", got)
	}
	if got := percentile([]float64{7}, 100); got != 7 {
		t.Fatalf("percentile([7], 100) = %v, want 7", got)
	}
}

func TestPercentileNearestRank(t *testing.T) {
	samples := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	cases := []struct {
		p    float64
		want float64
	}{
		{0, 10},
		{1, 10},
		{10, 10},  // ceil(0.10*10)=1
		{11, 20},  // ceil(1.10)=2
		{50, 50},  // ceil(5)=5
		{89, 90},  // ceil(8.9)=9
		{90, 90},  // ceil(9)=9
		{95, 100}, // ceil(9.5)=10
		{100, 100},
	}
	for _, tc := range cases {
		if got := percentile(samples, tc.p); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("percentile(p=%v) = %v, want %v", tc.p, got, tc.want)
		}
	}
}

func TestPercentileOfDoesNotReorderInput(t *testing.T) {
	samples := []float64{30, 10, 20}
	if got := percentileOf(samples, 50); got != 20 {
		t.Fatalf("percentileOf = %v, want 20", got)
	}
	if samples[0] != 30 {
		t.Fatalf("percentileOf reordered its input: %v", samples)
	}
}

func TestMaxFloat(t *testing.T) {
	if got := maxFloat(nil); got != 0 {
		t.Fatalf("maxFloat(nil) = %v, want 0", got)
	}
	if got := maxFloat([]float64{3, 9, -1}); got != 9 {
		t.Fatalf("maxFloat = %v, want 9", got)
	}
}
