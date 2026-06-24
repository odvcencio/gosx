package anim

import (
	"math"
	"testing"

	"m31labs.dev/gosx/engine"
)

func TestEvaluateBonePaletteSamplesTranslation(t *testing.T) {
	clip := engine.RenderAnimation{
		Name:     "move",
		Duration: 1,
		Channels: []engine.RenderAnimationChannel{{
			TargetID: "root",
			Property: "translation",
			Times:    []float64{0, 1},
			Values:   []float64{0, 0, 0, 10, 4, 2},
		}},
	}
	got, err := EvaluateBonePalette(clip, 0.25, []Joint{{ID: "root", Parent: -1}})
	if err != nil {
		t.Fatalf("EvaluateBonePalette: %v", err)
	}
	want := []float32{2.5, 1, 0.5}
	for i, idx := range []int{12, 13, 14} {
		if abs32(got[idx]-want[i]) > 1e-5 {
			t.Fatalf("translation component %d = %v, want %v", i, got[idx], want[i])
		}
	}
}

func TestEvaluateBonePaletteComposesParentChild(t *testing.T) {
	clip := engine.RenderAnimation{
		Name: "hierarchy",
		Channels: []engine.RenderAnimationChannel{
			{
				TargetID: "root",
				Property: "translation",
				Times:    []float64{0},
				Values:   []float64{1, 0, 0},
			},
			{
				TargetID: "child",
				Property: "translation",
				Times:    []float64{0},
				Values:   []float64{0, 2, 0},
			},
		},
	}
	joints := []Joint{
		{ID: "root", Parent: -1},
		{ID: "child", Parent: 0},
	}
	got, err := EvaluateBonePalette(clip, 0, joints)
	if err != nil {
		t.Fatalf("EvaluateBonePalette: %v", err)
	}
	child := got[16:32]
	if child[12] != 1 || child[13] != 2 || child[14] != 0 {
		t.Fatalf("child world translation = [%v %v %v], want [1 2 0]", child[12], child[13], child[14])
	}
}

func TestEvaluateBonePaletteStepInterpolation(t *testing.T) {
	clip := engine.RenderAnimation{
		Channels: []engine.RenderAnimationChannel{{
			TargetID:      "root",
			Property:      "scale",
			Interpolation: "STEP",
			Times:         []float64{0, 1},
			Values:        []float64{1, 1, 1, 3, 3, 3},
		}},
	}
	got, err := EvaluateBonePalette(clip, 0.75, []Joint{{ID: "root", Parent: -1}})
	if err != nil {
		t.Fatalf("EvaluateBonePalette: %v", err)
	}
	if got[0] != 1 || got[5] != 1 || got[10] != 1 {
		t.Fatalf("step scale before next key = [%v %v %v], want [1 1 1]", got[0], got[5], got[10])
	}
}

func TestEvaluateBonePaletteNormalizesRotation(t *testing.T) {
	s := math.Sqrt2 / 2
	clip := engine.RenderAnimation{
		Channels: []engine.RenderAnimationChannel{{
			TargetID: "root",
			Property: "rotation",
			Times:    []float64{0, 1},
			Values:   []float64{0, 0, 0, 1, 0, 0, s, s},
		}},
	}
	got, err := EvaluateBonePalette(clip, 1, []Joint{{ID: "root", Parent: -1}})
	if err != nil {
		t.Fatalf("EvaluateBonePalette: %v", err)
	}
	if abs32(got[0]) > 1e-5 || abs32(got[1]-1) > 1e-5 || abs32(got[4]+1) > 1e-5 || abs32(got[5]) > 1e-5 {
		t.Fatalf("rotation matrix first columns = [%v %v %v %v], want 90-degree Z rotation", got[0], got[1], got[4], got[5])
	}
}

// TestEvaluateBonePaletteSlerpOffMidpoint verifies that bone rotation uses true
// spherical linear interpolation (slerp) rather than normalized-lerp (nlerp).
// nlerp and slerp agree at t=0, t=0.5, and t=1, so the test samples at t=0.25
// with a wide 170° arc — far enough that nlerp produces a measurably wrong angle.
//
// q0 = identity, q1 = 170° about X.
// slerp(q0,q1,0.25) == 42.5° about X → cos(42.5°)≈0.737277, sin(42.5°)≈0.675590
// nlerp(q0,q1,0.25) gives ≈35.77° about X → cos≈0.811, sin≈0.585 (wrong)
func TestEvaluateBonePaletteSlerpOffMidpoint(t *testing.T) {
	// 170° half-angle = 85°
	halfRad := 85.0 * math.Pi / 180.0
	sinH := math.Sin(halfRad) // ≈ 0.9961947
	cosH := math.Cos(halfRad) // ≈ 0.0871557

	clip := engine.RenderAnimation{
		Name:     "slerp-test",
		Duration: 1,
		Channels: []engine.RenderAnimationChannel{{
			TargetID:      "joint0",
			Property:      "rotation",
			Interpolation: "LINEAR",
			Times:         []float64{0, 1},
			// identity then 170° about X: [x,y,z,w] each frame
			Values: []float64{0, 0, 0, 1, sinH, 0, 0, cosH},
		}},
	}
	joints := []Joint{{ID: "joint0", Parent: -1, Scale: [3]float32{1, 1, 1}}}
	got, err := EvaluateBonePalette(clip, 0.25, joints)
	if err != nil {
		t.Fatalf("EvaluateBonePalette: %v", err)
	}

	// composeTRS writes into column-major mat4:
	//   m[5]  = cos(42.5°) ≈  0.737277
	//   m[6]  = sin(42.5°) ≈  0.675590
	//   m[9]  = -sin(42.5°) ≈ -0.675590
	//   m[10] = cos(42.5°) ≈  0.737277
	cos425 := float32(math.Cos(42.5 * math.Pi / 180.0))
	sin425 := float32(math.Sin(42.5 * math.Pi / 180.0))
	const tol = 1e-5
	checks := []struct {
		idx  int
		want float32
		name string
	}{
		{5, cos425, "m[5]=cos(42.5°)"},
		{6, sin425, "m[6]=sin(42.5°)"},
		{9, -sin425, "m[9]=-sin(42.5°)"},
		{10, cos425, "m[10]=cos(42.5°)"},
	}
	for _, c := range checks {
		if abs32(got[c.idx]-c.want) > tol {
			t.Errorf("slerp at t=0.25: %s got %v, want %v (diff %v)", c.name, got[c.idx], c.want, got[c.idx]-c.want)
		}
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
