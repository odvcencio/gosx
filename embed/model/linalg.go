package model

import "math"

// matMul computes out = A @ B, where A is m×k and B is k×n (row-major).
// out must be pre-allocated with m*n elements.
func matMul(out, a, b []float32, m, k, n int) {
	for i := 0; i < m; i++ {
		for j := 0; j < n; j++ {
			var sum float64
			for l := 0; l < k; l++ {
				sum += float64(a[i*k+l]) * float64(b[l*n+j])
			}
			out[i*n+j] = float32(sum)
		}
	}
}

// matMulTransB computes out = A @ B^T, where A is m×k and B is n×k (row-major).
// Equivalent to multiplying A by the transpose of B.
// out must be pre-allocated with m*n elements.
func matMulTransB(out, a, b []float32, m, k, n int) {
	for i := 0; i < m; i++ {
		for j := 0; j < n; j++ {
			var sum float64
			for l := 0; l < k; l++ {
				sum += float64(a[i*k+l]) * float64(b[j*k+l])
			}
			out[i*n+j] = float32(sum)
		}
	}
}

// layerNorm applies layer normalization to x with learnable scale (gamma) and
// shift (beta). eps=1e-5 for numerical stability. dim is the length of x, gamma,
// beta, and out.
func layerNorm(out, x, gamma, beta []float32, dim int) {
	// Compute mean.
	var mean float64
	for i := 0; i < dim; i++ {
		mean += float64(x[i])
	}
	mean /= float64(dim)

	// Compute variance.
	var variance float64
	for i := 0; i < dim; i++ {
		d := float64(x[i]) - mean
		variance += d * d
	}
	variance /= float64(dim)

	invStd := 1.0 / math.Sqrt(variance+1e-5)

	for i := 0; i < dim; i++ {
		normalized := (float64(x[i]) - mean) * invStd
		out[i] = float32(normalized*float64(gamma[i]) + float64(beta[i]))
	}
}

// gelu applies the GELU activation function element-wise:
//
//	gelu(x) = x * 0.5 * (1 + tanh(sqrt(2/pi) * (x + 0.044715*x^3)))
func gelu(out, x []float32) {
	const c = 0.7978845608028654 // sqrt(2/pi)
	for i, v := range x {
		xf := float64(v)
		inner := c * (xf + 0.044715*xf*xf*xf)
		out[i] = float32(xf * 0.5 * (1.0 + math.Tanh(inner)))
	}
}

// softmax computes the softmax of x[:n] into out[:n].
// Uses max subtraction for numerical stability. No-op for n==0.
func softmax(out, x []float32, n int) {
	if n == 0 {
		return
	}

	// Find max for numerical stability.
	max := x[0]
	for i := 1; i < n; i++ {
		if x[i] > max {
			max = x[i]
		}
	}

	var sum float64
	for i := 0; i < n; i++ {
		e := math.Exp(float64(x[i]) - float64(max))
		out[i] = float32(e)
		sum += e
	}

	for i := 0; i < n; i++ {
		out[i] = float32(float64(out[i]) / sum)
	}
}

// l2Normalize normalizes x to unit length, writing the result to out.
// If the squared norm is below 1e-12 (zero or near-zero vector), out is zeroed.
func l2Normalize(out, x []float32) {
	var sum float64
	for _, v := range x {
		sum += float64(v) * float64(v)
	}
	if sum < 1e-12 {
		for i := range out {
			out[i] = 0
		}
		return
	}
	invNorm := 1.0 / math.Sqrt(sum)
	for i, v := range x {
		out[i] = float32(float64(v) * invNorm)
	}
}

// addBias adds bias to x in-place, row by row.
// x is rows×cols (row-major), bias has length cols.
func addBias(x, bias []float32, rows, cols int) {
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			x[i*cols+j] += bias[j]
		}
	}
}

// meanPool averages the rows of x (seq×dim) into out (dim).
func meanPool(out, x []float32, seq, dim int) {
	for j := 0; j < dim; j++ {
		out[j] = 0
	}
	for i := 0; i < seq; i++ {
		for j := 0; j < dim; j++ {
			out[j] += x[i*dim+j]
		}
	}
	invSeq := float32(1.0 / float64(seq))
	for j := 0; j < dim; j++ {
		out[j] *= invSeq
	}
}
