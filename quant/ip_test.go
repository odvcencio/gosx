package quant

import (
	"math"
	"math/rand"
	"testing"
)

func TestIPQuantizerUnbiased(t *testing.T) {
	dim := 384
	rng := rand.New(rand.NewSource(99))
	x := randomUnitVector(dim, rng)
	y := randomUnitVector(dim, rng)

	var trueIP float64
	for i := range x {
		trueIP += float64(x[i]) * float64(y[i])
	}

	// Average over many independently-seeded quantizers
	var sumEstimate float64
	trials := 200
	for trial := 0; trial < trials; trial++ {
		q := NewIPWithSeed(dim, 3, int64(trial+100))
		qx := q.Quantize(x)
		estimate := q.InnerProduct(qx, y)
		sumEstimate += float64(estimate)
	}
	avgEstimate := sumEstimate / float64(trials)
	if math.Abs(avgEstimate-trueIP) > 0.05 {
		t.Errorf("IP avg estimate %.4f vs true IP %.4f", avgEstimate, trueIP)
	}
}

func TestIPQuantizerDistortion(t *testing.T) {
	dim := 384
	rng := rand.New(rand.NewSource(99))
	x := randomUnitVector(dim, rng)
	y := randomUnitVector(dim, rng)

	var trueIP float64
	for i := range x {
		trueIP += float64(x[i]) * float64(y[i])
	}

	var sumSqErr float64
	trials := 200
	for trial := 0; trial < trials; trial++ {
		q := NewIPWithSeed(dim, 2, int64(trial+200))
		qx := q.Quantize(x)
		estimate := q.InnerProduct(qx, y)
		err := float64(estimate) - trueIP
		sumSqErr += err * err
	}
	avgDistortion := sumSqErr / float64(trials)
	bound := 0.56 / float64(dim) // paper value for b=2
	if avgDistortion > bound*2.0 {
		t.Errorf("IP distortion %.6f exceeds 2x bound %.6f", avgDistortion, bound)
	}
}

func TestIPQuantizerDimAndBitWidth(t *testing.T) {
	q := NewIPWithSeed(256, 3, 42)
	if q.Dim() != 256 {
		t.Errorf("Dim() = %d want 256", q.Dim())
	}
	if q.BitWidth() != 3 {
		t.Errorf("BitWidth() = %d want 3", q.BitWidth())
	}
	if q.Seed() != 42 {
		t.Errorf("Seed() = %d want 42", q.Seed())
	}
}

func TestIPQuantizerPreparedQuery(t *testing.T) {
	dim := 128
	q := NewIPWithSeed(dim, 3, 42)
	rng := rand.New(rand.NewSource(99))
	x := randomUnitVector(dim, rng)
	y := randomUnitVector(dim, rng)

	qx := q.Quantize(x)
	direct := q.InnerProduct(qx, y)
	pq := q.PrepareQuery(y)
	prepared := q.InnerProductPrepared(qx, pq)

	if math.Abs(float64(direct-prepared)) > 1e-5 {
		t.Errorf("prepared %.6f != direct %.6f", prepared, direct)
	}
}
