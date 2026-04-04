package quant

import (
	"math"
	"math/rand"
	"testing"
)

func TestQJLUnbiased(t *testing.T) {
	dim := 384
	rng := rand.New(rand.NewSource(42))
	x := randomUnitVector(dim, rng)
	y := randomUnitVector(dim, rng)

	var trueIP float64
	for i := range x {
		trueIP += float64(x[i]) * float64(y[i])
	}

	var sumEstimate float64
	trials := 5000
	for trial := 0; trial < trials; trial++ {
		proj := generateGaussianMatrix(dim, rand.New(rand.NewSource(int64(trial+100))))
		signs := make([]byte, (dim+7)/8)
		resNorm := qjlProject(signs, x, proj, dim)
		estimate := qjlInnerProduct(signs, resNorm, y, proj, dim)
		sumEstimate += float64(estimate)
	}
	avgEstimate := sumEstimate / float64(trials)
	if math.Abs(avgEstimate-trueIP) > 0.05 {
		t.Errorf("QJL average estimate %.4f vs true IP %.4f", avgEstimate, trueIP)
	}
}

func TestQJLVarianceBound(t *testing.T) {
	dim := 384
	rng := rand.New(rand.NewSource(42))
	x := randomUnitVector(dim, rng)
	y := randomUnitVector(dim, rng)

	var trueIP float64
	var yNormSq float64
	for i := range x {
		trueIP += float64(x[i]) * float64(y[i])
		yNormSq += float64(y[i]) * float64(y[i])
	}

	var sumSqErr float64
	trials := 5000
	for trial := 0; trial < trials; trial++ {
		proj := generateGaussianMatrix(dim, rand.New(rand.NewSource(int64(trial+200))))
		signs := make([]byte, (dim+7)/8)
		resNorm := qjlProject(signs, x, proj, dim)
		estimate := qjlInnerProduct(signs, resNorm, y, proj, dim)
		err := float64(estimate) - trueIP
		sumSqErr += err * err
	}
	variance := sumSqErr / float64(trials)
	bound := math.Pi / (2.0 * float64(dim)) * yNormSq
	if variance > bound*2.0 {
		t.Errorf("QJL variance %.6f exceeds 2x bound %.6f", variance, bound)
	}
}
