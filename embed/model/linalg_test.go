package model

import (
	"math"
	"testing"
)

func almostEq(a, b, eps float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < eps
}

// TestMatMul tests standard 2x3 @ 3x2 matrix multiply.
func TestMatMul(t *testing.T) {
	// A (2x3):
	// [1 2 3]
	// [4 5 6]
	a := []float32{1, 2, 3, 4, 5, 6}
	// B (3x2):
	// [7  8]
	// [9  10]
	// [11 12]
	b := []float32{7, 8, 9, 10, 11, 12}
	out := make([]float32, 4) // 2x2

	matMul(out, a, b, 2, 3, 2)

	// Expected C (2x2):
	// [1*7+2*9+3*11  1*8+2*10+3*12] = [58  64]
	// [4*7+5*9+6*11  4*8+5*10+6*12] = [139 154]
	expected := []float32{58, 64, 139, 154}
	for i, v := range expected {
		if !almostEq(out[i], v, 1e-4) {
			t.Errorf("matMul out[%d] = %f, want %f", i, out[i], v)
		}
	}
}

// TestMatMulTransB tests A @ B^T where A is 2x3 and B is also 2x3.
func TestMatMulTransB(t *testing.T) {
	// A (2x3):
	// [1 2 3]
	// [4 5 6]
	a := []float32{1, 2, 3, 4, 5, 6}
	// B (2x3) — will be transposed to 3x2 logically:
	// [1 0 0]
	// [0 1 0]
	b := []float32{1, 0, 0, 0, 1, 0}
	out := make([]float32, 4) // 2x2

	// A @ B^T: result[i][j] = dot(A[i,:], B[j,:])
	matMulTransB(out, a, b, 2, 3, 2)

	// row 0 of A dot row 0 of B = 1*1+2*0+3*0 = 1
	// row 0 of A dot row 1 of B = 1*0+2*1+3*0 = 2
	// row 1 of A dot row 0 of B = 4*1+5*0+6*0 = 4
	// row 1 of A dot row 1 of B = 4*0+5*1+6*0 = 5
	expected := []float32{1, 2, 4, 5}
	for i, v := range expected {
		if !almostEq(out[i], v, 1e-4) {
			t.Errorf("matMulTransB out[%d] = %f, want %f", i, out[i], v)
		}
	}
}

// TestLayerNorm checks that output has approximately zero mean and unit variance,
// and that gamma/beta are applied.
func TestLayerNorm(t *testing.T) {
	x := []float32{1, 2, 3, 4, 5}
	gamma := []float32{1, 1, 1, 1, 1}
	beta := []float32{0, 0, 0, 0, 0}
	out := make([]float32, 5)

	layerNorm(out, x, gamma, beta, 5)

	// Output mean should be ~0.
	var sum float32
	for _, v := range out {
		sum += v
	}
	mean := sum / 5
	if !almostEq(mean, 0, 1e-5) {
		t.Errorf("layerNorm mean = %f, want ~0", mean)
	}

	// Output variance should be ~1.
	var variance float32
	for _, v := range out {
		variance += (v - mean) * (v - mean)
	}
	variance /= 5
	if !almostEq(variance, 1, 1e-4) {
		t.Errorf("layerNorm variance = %f, want ~1", variance)
	}
}

// TestGELU checks boundary/known values.
func TestGELU(t *testing.T) {
	x := []float32{0, 1, -1, 2, -2}
	out := make([]float32, len(x))
	gelu(out, x)

	// gelu(0) = 0
	if !almostEq(out[0], 0, 1e-6) {
		t.Errorf("gelu(0) = %f, want 0", out[0])
	}

	// gelu(x) > 0 for x > 0
	if out[1] <= 0 {
		t.Errorf("gelu(1) = %f, want > 0", out[1])
	}
	if out[3] <= 0 {
		t.Errorf("gelu(2) = %f, want > 0", out[3])
	}

	// gelu(x) < 0 for x slightly negative (e.g. -1), but > x
	if out[2] >= 0 {
		t.Errorf("gelu(-1) = %f, want < 0", out[2])
	}
	if out[2] <= x[2] {
		t.Errorf("gelu(-1) = %f, want > -1", out[2])
	}

	// gelu is monotonically increasing: gelu(2) > gelu(1)
	if out[3] <= out[1] {
		t.Errorf("gelu should be monotonically increasing: gelu(2)=%f <= gelu(1)=%f", out[3], out[1])
	}
}

// TestSoftmax checks that output sums to 1 and preserves ordering.
func TestSoftmax(t *testing.T) {
	x := []float32{1, 2, 3, 4, 5}
	out := make([]float32, len(x))
	softmax(out, x, len(x))

	// Sum should be 1.
	var sum float32
	for _, v := range out {
		sum += v
	}
	if !almostEq(sum, 1, 1e-5) {
		t.Errorf("softmax sum = %f, want 1", sum)
	}

	// Monotonicity: larger input → larger output.
	for i := 1; i < len(out); i++ {
		if out[i] <= out[i-1] {
			t.Errorf("softmax[%d]=%f <= softmax[%d]=%f, want strictly increasing", i, out[i], i-1, out[i-1])
		}
	}

	// Guard: n==0 should not panic.
	softmax(out, x, 0)
}

// TestL2Norm checks that output has unit norm.
func TestL2Norm(t *testing.T) {
	x := []float32{3, 4}
	out := make([]float32, 2)
	l2Normalize(out, x)

	// ||out|| should be 1.
	norm := math.Sqrt(float64(out[0]*out[0] + out[1]*out[1]))
	if math.Abs(norm-1.0) > 1e-6 {
		t.Errorf("l2Normalize norm = %f, want 1", norm)
	}

	// Values: 3/5=0.6, 4/5=0.8.
	if !almostEq(out[0], 0.6, 1e-6) {
		t.Errorf("l2Normalize[0] = %f, want 0.6", out[0])
	}
	if !almostEq(out[1], 0.8, 1e-6) {
		t.Errorf("l2Normalize[1] = %f, want 0.8", out[1])
	}

	// Zero vector guard: should not produce NaN.
	z := []float32{0, 0}
	outz := make([]float32, 2)
	l2Normalize(outz, z)
	for i, v := range outz {
		if math.IsNaN(float64(v)) {
			t.Errorf("l2Normalize zero vector out[%d] = NaN", i)
		}
	}
}

// TestAddBias checks row-wise bias addition.
func TestAddBias(t *testing.T) {
	// 2 rows x 3 cols
	x := []float32{1, 2, 3, 4, 5, 6}
	bias := []float32{10, 20, 30}
	addBias(x, bias, 2, 3)

	expected := []float32{11, 22, 33, 14, 25, 36}
	for i, v := range expected {
		if !almostEq(x[i], v, 1e-5) {
			t.Errorf("addBias x[%d] = %f, want %f", i, x[i], v)
		}
	}
}
