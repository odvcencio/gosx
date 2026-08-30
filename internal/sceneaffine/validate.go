// Package sceneaffine owns the small, runtime-safe Scene3D parent-matrix
// contract shared by the host scene package and the client VM.
package sceneaffine

import "math"

const determinantEpsilon = 1e-12

// ValidParentMatrix reports whether values are a finite, invertible,
// column-major affine 4x4 matrix. The determinant test is normalized by the
// largest linear coefficient, so uniformly tiny or large transforms are not
// rejected merely because of their absolute scale.
func ValidParentMatrix(values []float64) bool {
	if len(values) != 16 {
		return false
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	if values[3] != 0 || values[7] != 0 || values[11] != 0 || values[15] != 1 {
		return false
	}
	maximum := 0.0
	for _, index := range [...]int{0, 1, 2, 4, 5, 6, 8, 9, 10} {
		maximum = math.Max(maximum, math.Abs(values[index]))
	}
	if maximum == 0 || math.IsInf(1/maximum, 0) {
		return false
	}
	a, b, c := values[0]/maximum, values[4]/maximum, values[8]/maximum
	d, e, f := values[1]/maximum, values[5]/maximum, values[9]/maximum
	g, h, i := values[2]/maximum, values[6]/maximum, values[10]/maximum
	determinant := a*(e*i-f*h) + b*(f*g-d*i) + c*(d*h-e*g)
	return math.Abs(determinant) > determinantEpsilon && !math.IsNaN(determinant) && !math.IsInf(determinant, 0)
}
