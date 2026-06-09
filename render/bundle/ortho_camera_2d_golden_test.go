package bundle

import (
	"math"
	"testing"
)

// TestComputeOrthoCamera2DMVP_Golden pins the native 2D-board camera math AND
// is the cross-language contract with the 16a JS renderer: the helper
// sceneMat4Ortho2DViewProj (client/js/bootstrap-src/11-scene-math.js) must
// produce proj*view equal to this native MVP. The JS side was independently
// node-verified (ORTHO2D_JS_MATH_OK) against these same values; this test
// guards the native half so the two can never silently diverge.
func TestComputeOrthoCamera2DMVP_Golden(t *testing.T) {
	approx := func(got, want float32, name string) {
		if math.Abs(float64(got-want)) > 1e-5 {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}

	// zoom=1, pan=0, 200x100 → ortho scale 2/200, 2/100; identity translation.
	m := computeOrthoCamera2DMVP(OrthoCamera2D(1, 0, 0, 200, 100), 200, 100)
	approx(m[0], 0.01, "c1 m0")
	approx(m[5], 0.02, "c1 m5")
	approx(m[10], -1, "c1 m10")
	approx(m[15], 1, "c1 m15")
	approx(m[12], 0, "c1 m12")
	approx(m[13], 0, "c1 m13")
	approx(m[14], 0, "c1 m14")

	// pan=(10,20) → MVP translation = proj-scaled (-panX, -panY).
	m = computeOrthoCamera2DMVP(OrthoCamera2D(1, 10, 20, 200, 100), 200, 100)
	approx(m[0], 0.01, "c2 m0")
	approx(m[5], 0.02, "c2 m5")
	approx(m[12], -0.1, "c2 m12")
	approx(m[13], -0.4, "c2 m13")

	// zoom=2 → half-extents halve → ortho scale doubles.
	m = computeOrthoCamera2DMVP(OrthoCamera2D(2, 0, 0, 200, 100), 200, 100)
	approx(m[0], 0.02, "c3 m0")
	approx(m[5], 0.04, "c3 m5")
}
