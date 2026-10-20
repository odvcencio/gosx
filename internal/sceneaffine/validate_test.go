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

func TestInverseLinearExtremeAnisotropyAndThreshold(t *testing.T) {
	for name, testCase := range map[string]struct {
		matrix []float64
		valid  bool
	}{
		"anisotropic tiny diagonal": {matrix: []float64{
			1e-297, 0, 0, 0, 0, 2e-303, 0, 0, 0, 0, 2e-303, 0, 0, 0, 0, 1,
		}, valid: true},
		"anisotropic tiny shear": {matrix: []float64{
			1e-297, 0, 0, 0, 5e-298, 2e-303, 0, 0, -4e-298, 1e-303, 2e-303, 0, 0, 0, 0, 1,
		}, valid: true},
		"anisotropic tiny transposed shear": {matrix: []float64{
			1e-297, 5e-298, -4e-298, 0, 0, 2e-303, 1e-303, 0, 0, 0, 2e-303, 0, 0, 0, 0, 1,
		}, valid: true},
		"anisotropic tiny reflected shear": {matrix: []float64{
			-1e-297, 0, 0, 0, 5e-298, 2e-303, 0, 0, -4e-298, 1e-303, 2e-303, 0, 0, 0, 0, 1,
		}, valid: true},
		"just above normalized determinant threshold": {matrix: []float64{
			1e-297, 0, 0, 0, 0, 1e-303, 0, 0, 0, 0, 1.000001e-303, 0, 0, 0, 0, 1,
		}, valid: true},
		"just below normalized determinant threshold": {matrix: []float64{
			1e-297, 0, 0, 0, 0, 1e-303, 0, 0, 0, 0, 0.999999e-303, 0, 0, 0, 0, 1,
		}},
		"unrepresentable inverse coefficient": {matrix: []float64{
			1e-308, 0, 0, 0, 0, 1e-308, 0, 0, 0, 0, 2e-320, 0, 0, 0, 0, 1,
		}},
	} {
		t.Run(name, func(t *testing.T) {
			inverse, ok := InverseLinear(testCase.matrix)
			if ok != testCase.valid || ValidParentMatrix(testCase.matrix) != testCase.valid {
				t.Fatalf("validity = inverse %v parent %v, want %v", ok, ValidParentMatrix(testCase.matrix), testCase.valid)
			}
			if !ok {
				return
			}
			nonZero := false
			for index, value := range inverse {
				if math.IsNaN(value) || math.IsInf(value, 0) {
					t.Fatalf("inverse[%d] is non-finite: %v", index, value)
				}
				nonZero = nonZero || value != 0
			}
			if !nonZero {
				t.Fatal("accepted inverse is all zero")
			}
		})
	}
}
