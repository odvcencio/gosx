package headless

import (
	"math"
	"testing"
)

func TestAffineNormalScaleAndSingularContract(t *testing.T) {
	n := normalize3([3]float32{1, 2, 3})
	for _, scale := range []float32{1e-30, 1, 1e30} {
		m := identityMat4()
		m[0] = -2 * scale
		m[5] = 3 * scale
		m[10] = 4 * scale
		matrix, orientation := affineNormalMatrix(m)
		got := transformAffineNormal(matrix, n)
		want := normalize3([3]float32{-n[0] / 2, n[1] / 3, n[2] / 4})
		if orientation != -1 {
			t.Errorf("scale=%g orientation=%g", scale, orientation)
		}
		for i := range got {
			if math.Abs(float64(got[i]-want[i])) > 1e-6 {
				t.Errorf("scale=%g normal=%v want=%v", scale, got, want)
			}
		}
	}
	for _, m := range [][16]float32{{}, {1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1e-13, 0, 0, 0, 0, 1}} {
		matrix, orientation := affineNormalMatrix(m)
		got := transformAffineNormal(matrix, n)
		if orientation != 1 {
			t.Errorf("singular orientation=%g", orientation)
		}
		for i := range got {
			if math.Abs(float64(got[i]-n[i])) > 1e-6 {
				t.Errorf("singular normal=%v want=%v", got, n)
			}
		}
	}
}
