package quant

import (
	"math"
	"math/rand"
)

// generateGaussianMatrix creates a d×d matrix with i.i.d. N(0,1) entries as float32.
func generateGaussianMatrix(dim int, rng *rand.Rand) []float32 {
	m := make([]float32, dim*dim)
	for i := range m {
		m[i] = float32(rng.NormFloat64())
	}
	return m
}

// qjlProject computes sign(S · x) and packs sign bits into dst.
// Returns ||x||₂.
func qjlProject(signs []byte, x, projMatrix []float32, dim int) float32 {
	norm := vecNorm(x)
	for i := range signs {
		signs[i] = 0
	}
	for i := 0; i < dim; i++ {
		row := i * dim
		var dot float32
		for j := 0; j < dim; j++ {
			dot += projMatrix[row+j] * x[j]
		}
		if dot >= 0 {
			signs[i/8] |= 1 << uint(i%8)
		}
	}
	return norm
}

// qjlInnerProduct estimates <x, y> from QJL-quantized x and raw y.
// signs = sign(S·x), resNorm = ||x||, projMatrix = S
// Formula: √(π/2)/d · resNorm · Σᵢ sign_i · <sᵢ, y>
func qjlInnerProduct(signs []byte, resNorm float32, y, projMatrix []float32, dim int) float32 {
	scale := float32(math.Sqrt(math.Pi/2.0)) / float32(dim) * resNorm
	var sum float32
	for i := 0; i < dim; i++ {
		row := i * dim
		var dot float32
		for j := 0; j < dim; j++ {
			dot += projMatrix[row+j] * y[j]
		}
		signBit := signs[i/8] & (1 << uint(i%8))
		if signBit != 0 {
			sum += dot
		} else {
			sum -= dot
		}
	}
	return scale * sum
}
