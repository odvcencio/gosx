package quant

import (
	"math"
	"testing"
)

func TestCodebookMSEMatchesPaper(t *testing.T) {
	dim := 384
	expected := map[int]float64{
		1: 0.36,
		2: 0.117,
		3: 0.03,
		4: 0.009,
	}
	for bw, want := range expected {
		cb := computeCodebook(dim, bw)
		got := cb.expectedMSE(dim)
		ratio := got / want
		if ratio < 0.8 || ratio > 1.2 {
			t.Errorf("bw=%d: MSE=%.4f, want ≈ %.4f (ratio %.2f)", bw, got, want, ratio)
		}
	}
}

func TestCodebookCentroidsAreSorted(t *testing.T) {
	for bw := 1; bw <= 4; bw++ {
		cb := computeCodebook(384, bw)
		for i := 1; i < len(cb.centroids); i++ {
			if cb.centroids[i] <= cb.centroids[i-1] {
				t.Errorf("bw=%d: centroids not sorted at %d: %.4f <= %.4f",
					bw, i, cb.centroids[i], cb.centroids[i-1])
			}
		}
	}
}

func TestCodebookCentroidCount(t *testing.T) {
	for bw := 1; bw <= 4; bw++ {
		cb := computeCodebook(384, bw)
		want := 1 << uint(bw)
		if len(cb.centroids) != want {
			t.Errorf("bw=%d: got %d centroids want %d", bw, len(cb.centroids), want)
		}
		if len(cb.boundaries) != want-1 {
			t.Errorf("bw=%d: got %d boundaries want %d", bw, len(cb.boundaries), want-1)
		}
	}
}

func TestNearestCentroidBinarySearch(t *testing.T) {
	cb := computeCodebook(384, 2)
	for i, c := range cb.centroids {
		got := cb.nearestCentroid(c)
		if got != i {
			t.Errorf("nearestCentroid(%.4f) = %d, want %d", c, got, i)
		}
	}
}

func TestCodebookSymmetry(t *testing.T) {
	// Beta distribution is symmetric around 0, so centroids should be symmetric
	cb := computeCodebook(384, 2)
	n := len(cb.centroids)
	for i := 0; i < n/2; i++ {
		sum := float64(cb.centroids[i]) + float64(cb.centroids[n-1-i])
		if math.Abs(sum) > 0.01 {
			t.Errorf("centroids[%d]+centroids[%d] = %.4f, want ≈ 0", i, n-1-i, sum)
		}
	}
}
