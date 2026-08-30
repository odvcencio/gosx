package sceneaffine

import (
	"math"
	"testing"
)

func TestValidParentMatrixContract(t *testing.T) {
	valid := []float64{
		-2e-6, 0, 0, 0,
		1e-6, 3e-6, 0, 0,
		0, 0, 4e-6, 0,
		5, 6, 7, 1,
	}
	if !ValidParentMatrix(valid) {
		t.Fatal("finite, invertible affine shear/reflection was rejected because of absolute scale")
	}
	mutations := map[string]func([]float64){
		"projective bottom row": func(matrix []float64) { matrix[3] = 1 },
		"singular linear part":  func(matrix []float64) { matrix[4] = matrix[0]; matrix[5] = 0 },
		"non-finite":            func(matrix []float64) { matrix[8] = math.Inf(1) },
	}
	for name, mutate := range mutations {
		matrix := append([]float64(nil), valid...)
		mutate(matrix)
		if ValidParentMatrix(matrix) {
			t.Errorf("%s matrix was accepted", name)
		}
	}
	if ValidParentMatrix(valid[:15]) {
		t.Fatal("short matrix was accepted")
	}
}

func TestValidParentMatrixScaleInvariantExtremes(t *testing.T) {
	const nearMax = 9e307
	for name, matrix := range map[string][]float64{
		"uniform large": {
			1e150, 0, 0, 0, 0, 1e150, 0, 0, 0, 0, 1e150, 0, 0, 0, 0, 1,
		},
		"uniform small": {
			1e-150, 0, 0, 0, 0, 1e-150, 0, 0, 0, 0, 1e-150, 0, 0, 0, 0, 1,
		},
		"sheared reflected large": {
			-2e150, 0, 0, 0, 1e150, 3e150, 0, 0, 0, 0, 4e150, 0, 5, 6, 7, 1,
		},
		"sheared reflected small": {
			-2e-150, 0, 0, 0, 1e-150, 3e-150, 0, 0, 0, 0, 4e-150, 0, 5, 6, 7, 1,
		},
		"near max finite": {
			nearMax, -nearMax, 0, 0, nearMax, nearMax, 0, 0, 0, 0, nearMax, 0, 0, 0, 0, 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if !ValidParentMatrix(matrix) {
				t.Fatal("valid finite affine matrix was rejected")
			}
		})
	}
}
