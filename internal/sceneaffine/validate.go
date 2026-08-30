// Package sceneaffine owns the small, runtime-safe Scene3D parent-matrix
// contract shared by the host scene package and the client VM.
package sceneaffine

import "math"

const determinantEpsilon = 1e-12

// ValidParentMatrix reports whether values are a finite, safely invertible,
// column-major affine 4x4 matrix.
func ValidParentMatrix(values []float64) bool {
	_, ok := InverseLinear(values)
	return ok
}

// InverseLinear returns the column-major inverse of the linear 3x3 basis. The
// determinant test is normalized by the largest coefficient, and each
// cofactor is divided before scale so representable extreme inverses survive.
func InverseLinear(values []float64) ([9]float64, bool) {
	var inverse [9]float64
	if len(values) != 16 {
		return inverse, false
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return inverse, false
		}
	}
	if values[3] != 0 || values[7] != 0 || values[11] != 0 || values[15] != 1 {
		return inverse, false
	}
	maximum := 0.0
	for _, index := range [...]int{0, 1, 2, 4, 5, 6, 8, 9, 10} {
		maximum = math.Max(maximum, math.Abs(values[index]))
	}
	if maximum == 0 || math.IsInf(1/maximum, 0) {
		return inverse, false
	}
	a, b, c := values[0]/maximum, values[4]/maximum, values[8]/maximum
	d, e, f := values[1]/maximum, values[5]/maximum, values[9]/maximum
	g, h, i := values[2]/maximum, values[6]/maximum, values[10]/maximum
	c00, c01, c02 := e*i-f*h, f*g-d*i, d*h-e*g
	c10, c11, c12 := c*h-b*i, a*i-c*g, b*g-a*h
	c20, c21, c22 := b*f-c*e, c*d-a*f, a*e-b*d
	determinant := a*c00 + b*c01 + c*c02
	if math.Abs(determinant) <= determinantEpsilon || math.IsNaN(determinant) || math.IsInf(determinant, 0) {
		return inverse, false
	}
	inverse = [9]float64{
		c00 / determinant / maximum, c01 / determinant / maximum, c02 / determinant / maximum,
		c10 / determinant / maximum, c11 / determinant / maximum, c12 / determinant / maximum,
		c20 / determinant / maximum, c21 / determinant / maximum, c22 / determinant / maximum,
	}
	nonZero := false
	for _, value := range inverse {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return [9]float64{}, false
		}
		nonZero = nonZero || value != 0
	}
	return inverse, nonZero
}
