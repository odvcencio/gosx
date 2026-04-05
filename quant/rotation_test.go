package quant

import (
	"math"
	"math/rand"
	"testing"
)

func TestRotationOrthogonality(t *testing.T) {
	dims := []int{4, 16, 64, 384}
	if raceEnabled {
		dims = []int{4, 16, 64}
	}
	for _, dim := range dims {
		rng := rand.New(rand.NewSource(42))
		rot := generateRotation(dim, rng)
		for trial := 0; trial < 10; trial++ {
			x := randomUnitVector(dim, rng)
			y := make([]float32, dim)
			tmp := make([]float32, dim)
			rotate(y, x, rot, dim)
			rotateInverse(tmp, y, rot, dim)
			var errSq float64
			for i := range x {
				d := float64(tmp[i] - x[i])
				errSq += d * d
			}
			if errSq > 1e-4 {
				t.Errorf("dim=%d trial=%d: round-trip error %.6f", dim, trial, errSq)
			}
		}
	}
}

func TestRotationPreservesNorm(t *testing.T) {
	dim := 128
	rng := rand.New(rand.NewSource(99))
	rot := generateRotation(dim, rng)
	x := randomUnitVector(dim, rng)
	y := make([]float32, dim)
	rotate(y, x, rot, dim)
	var normSq float64
	for _, v := range y {
		normSq += float64(v) * float64(v)
	}
	if math.Abs(normSq-1.0) > 1e-4 {
		t.Errorf("rotated norm² = %.6f, want ≈ 1.0", normSq)
	}
}
