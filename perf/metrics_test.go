package perf

import (
	"math"
	"testing"
)

func TestComputeFrameStats(t *testing.T) {
	durations := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	fs := ComputeFrameStats(durations)

	if fs.Count != 10 {
		t.Fatalf("Count: got %d, want 10", fs.Count)
	}
	if fs.Max != 10 {
		t.Fatalf("Max: got %f, want 10", fs.Max)
	}
	// Mean = 55/10 = 5.5
	if math.Abs(fs.Mean-5.5) > 0.01 {
		t.Fatalf("Mean: got %f, want 5.5", fs.Mean)
	}
	// P50 = median of [1..10] with linear interpolation at index 4.5 → 5.5
	if math.Abs(fs.P50-5.5) > 0.01 {
		t.Fatalf("P50: got %f, want 5.5", fs.P50)
	}
	// P95 at rank = 0.95*9 = 8.55 → sorted[8] + 0.55*(sorted[9]-sorted[8]) = 9 + 0.55 = 9.55
	if math.Abs(fs.P95-9.55) > 0.1 {
		t.Fatalf("P95: got %f, want ~9.55", fs.P95)
	}
	// P99 at rank = 0.99*9 = 8.91 → sorted[8] + 0.91*(sorted[9]-sorted[8]) = 9 + 0.91 = 9.91
	if math.Abs(fs.P99-9.91) > 0.1 {
		t.Fatalf("P99: got %f, want ~9.91", fs.P99)
	}
}

func TestComputeFrameStatsEmpty(t *testing.T) {
	fs := ComputeFrameStats(nil)
	if fs.Count != 0 {
		t.Fatalf("Count: got %d, want 0", fs.Count)
	}
	if fs.P50 != 0 || fs.P95 != 0 || fs.P99 != 0 || fs.Max != 0 || fs.Mean != 0 {
		t.Fatalf("expected zero FrameStats, got %+v", fs)
	}
}

func TestComputeFrameStatsSingle(t *testing.T) {
	fs := ComputeFrameStats([]float64{42.0})
	if fs.Count != 1 {
		t.Fatalf("Count: got %d, want 1", fs.Count)
	}
	if fs.P50 != 42.0 {
		t.Fatalf("P50: got %f, want 42.0", fs.P50)
	}
	if fs.P95 != 42.0 {
		t.Fatalf("P95: got %f, want 42.0", fs.P95)
	}
	if fs.P99 != 42.0 {
		t.Fatalf("P99: got %f, want 42.0", fs.P99)
	}
	if fs.Max != 42.0 {
		t.Fatalf("Max: got %f, want 42.0", fs.Max)
	}
	if fs.Mean != 42.0 {
		t.Fatalf("Mean: got %f, want 42.0", fs.Mean)
	}
}
