package anim

import (
	"math"
	"testing"

	"github.com/odvcencio/gosx/engine"
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

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
