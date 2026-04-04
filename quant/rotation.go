package quant

import (
	"math"
	"math/rand"
)

// generateRotation creates a d×d orthogonal matrix via Householder QR
// decomposition of a random Gaussian matrix. Computed in float64 for
// numerical stability, stored as float32 row-major.
func generateRotation(dim int, rng *rand.Rand) []float32 {
	a := make([]float64, dim*dim)
	for i := range a {
		a[i] = rng.NormFloat64()
	}
	q := householderQR(a, dim)
	result := make([]float32, dim*dim)
	for i, v := range q {
		result[i] = float32(v)
	}
	return result
}

func householderQR(a []float64, dim int) []float64 {
	q := make([]float64, dim*dim)
	for i := 0; i < dim; i++ {
		q[i*dim+i] = 1.0
	}
	for k := 0; k < dim-1; k++ {
		x := make([]float64, dim-k)
		for i := k; i < dim; i++ {
			x[i-k] = a[i*dim+k]
		}
		normX := 0.0
		for _, v := range x {
			normX += v * v
		}
		normX = math.Sqrt(normX)
		if normX < 1e-15 {
			continue
		}
		sign := 1.0
		if x[0] < 0 {
			sign = -1.0
		}
		x[0] += sign * normX
		normV := 0.0
		for _, v := range x {
			normV += v * v
		}
		normV = math.Sqrt(normV)
		if normV < 1e-15 {
			continue
		}
		for i := range x {
			x[i] /= normV
		}
		for j := k; j < dim; j++ {
			dot := 0.0
			for i := k; i < dim; i++ {
				dot += x[i-k] * a[i*dim+j]
			}
			for i := k; i < dim; i++ {
				a[i*dim+j] -= 2 * x[i-k] * dot
			}
		}
		for i := 0; i < dim; i++ {
			dot := 0.0
			for j := k; j < dim; j++ {
				dot += q[i*dim+j] * x[j-k]
			}
			for j := k; j < dim; j++ {
				q[i*dim+j] -= 2 * dot * x[j-k]
			}
		}
	}
	return q
}

// rotate computes dst = matrix · src.
func rotate(dst, src, matrix []float32, dim int) {
	for i := 0; i < dim; i++ {
		var sum float32
		row := i * dim
		for j := 0; j < dim; j++ {
			sum += matrix[row+j] * src[j]
		}
		dst[i] = sum
	}
}

// rotateInverse computes dst = matrixᵀ · src.
func rotateInverse(dst, src, matrix []float32, dim int) {
	for i := 0; i < dim; i++ {
		dst[i] = 0
	}
	for i := 0; i < dim; i++ {
		row := i * dim
		s := src[i]
		for j := 0; j < dim; j++ {
			dst[j] += matrix[row+j] * s
		}
	}
}
