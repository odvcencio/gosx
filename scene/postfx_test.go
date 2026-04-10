package scene

import "testing"

func TestPostEffectInterfaceImplementations(t *testing.T) {
	// Compile-time assertion that all four effect types satisfy PostEffect.
	var _ PostEffect = Tonemap{}
	var _ PostEffect = Bloom{}
	var _ PostEffect = Vignette{}
	var _ PostEffect = ColorGrade{}
}

func TestPostFXZeroValueIsEmpty(t *testing.T) {
	var pfx PostFX
	if len(pfx.Effects) != 0 {
		t.Errorf("zero PostFX should have empty Effects, got %d", len(pfx.Effects))
	}
}

func TestTonemapDefaultMode(t *testing.T) {
	tm := Tonemap{}
	if tm.Mode != TonemapACES {
		t.Errorf("Tonemap zero value Mode = %v, want TonemapACES (0)", tm.Mode)
	}
}
