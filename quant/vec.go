package quant

import (
	"math"
	"math/rand"
)

func vecNorm(v []float32) float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return float32(math.Sqrt(sum))
}

func randomUnitVector(dim int, rng *rand.Rand) []float32 {
	v := make([]float32, dim)
	var normSq float64
	for i := range v {
		g := float32(rng.NormFloat64())
		v[i] = g
		normSq += float64(g) * float64(g)
	}
	scale := float32(1.0 / math.Sqrt(normSq))
	for i := range v {
		v[i] *= scale
	}
	return v
}
