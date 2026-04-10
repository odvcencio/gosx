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

func TestTonemapIRLegacyProps(t *testing.T) {
	ir := TonemapIR{Mode: "aces", Exposure: 1.5}
	got := ir.legacyProps()
	if got["kind"] != "toneMapping" {
		t.Errorf(`kind = %v, want "toneMapping"`, got["kind"])
	}
	if got["exposure"] != 1.5 {
		t.Errorf("exposure = %v, want 1.5", got["exposure"])
	}
}

func TestBloomIRLegacyProps(t *testing.T) {
	ir := BloomIR{Threshold: 0.7, Strength: 0.6, Radius: 12}
	got := ir.legacyProps()
	if got["kind"] != "bloom" {
		t.Errorf(`kind = %v, want "bloom"`, got["kind"])
	}
	if got["threshold"] != 0.7 {
		t.Errorf("threshold = %v, want 0.7", got["threshold"])
	}
	if got["intensity"] != 0.6 {
		t.Errorf("intensity (Strength) = %v, want 0.6", got["intensity"])
	}
	if got["radius"] != 12.0 {
		t.Errorf("radius = %v, want 12", got["radius"])
	}
}

func TestVignetteIRLegacyProps(t *testing.T) {
	ir := VignetteIR{Intensity: 0.8}
	got := ir.legacyProps()
	if got["kind"] != "vignette" {
		t.Errorf(`kind = %v, want "vignette"`, got["kind"])
	}
	if got["intensity"] != 0.8 {
		t.Errorf("intensity = %v, want 0.8", got["intensity"])
	}
}

func TestColorGradeIRLegacyProps(t *testing.T) {
	ir := ColorGradeIR{Exposure: 1.1, Contrast: 1.2, Saturation: 0.9}
	got := ir.legacyProps()
	if got["kind"] != "colorGrade" {
		t.Errorf(`kind = %v, want "colorGrade"`, got["kind"])
	}
	if got["exposure"] != 1.1 {
		t.Errorf("exposure = %v, want 1.1", got["exposure"])
	}
	if got["contrast"] != 1.2 {
		t.Errorf("contrast = %v, want 1.2", got["contrast"])
	}
	if got["saturation"] != 0.9 {
		t.Errorf("saturation = %v, want 0.9", got["saturation"])
	}
}

func TestPostFXSceneIR(t *testing.T) {
	pfx := PostFX{Effects: []PostEffect{
		Bloom{Threshold: 0.7, Strength: 0.6, Radius: 12},
		Tonemap{Mode: TonemapACES, Exposure: 1.1},
	}}
	irs := pfx.sceneIR()
	if len(irs) != 2 {
		t.Fatalf("got %d IRs, want 2", len(irs))
	}
	if _, ok := irs[0].(BloomIR); !ok {
		t.Errorf("irs[0] = %T, want BloomIR", irs[0])
	}
	if _, ok := irs[1].(TonemapIR); !ok {
		t.Errorf("irs[1] = %T, want TonemapIR", irs[1])
	}
}

func TestPropsPostFXRoundTrip(t *testing.T) {
	p := Props{
		PostFX: PostFX{Effects: []PostEffect{
			Bloom{Threshold: 0.7, Strength: 0.6, Radius: 12},
			Tonemap{Mode: TonemapACES, Exposure: 1.1},
		}},
	}
	ir := p.SceneIR()
	if len(ir.PostEffects) != 2 {
		t.Fatalf("ir.PostEffects len = %d, want 2", len(ir.PostEffects))
	}

	bundle := ir.legacyProps()
	rawList, ok := bundle["postEffects"].([]map[string]any)
	if !ok {
		t.Fatalf("bundle.postEffects type = %T, want []map[string]any", bundle["postEffects"])
	}
	if len(rawList) != 2 {
		t.Fatalf("postEffects len = %d, want 2", len(rawList))
	}
	if rawList[0]["kind"] != "bloom" {
		t.Errorf(`postEffects[0].kind = %v, want "bloom"`, rawList[0]["kind"])
	}
	if rawList[1]["kind"] != "toneMapping" {
		t.Errorf(`postEffects[1].kind = %v, want "toneMapping"`, rawList[1]["kind"])
	}
}

func TestPropsNoPostFXEmitsNothing(t *testing.T) {
	p := Props{} // no PostFX
	ir := p.SceneIR()
	if len(ir.PostEffects) != 0 {
		t.Errorf("empty PostFX should produce empty PostEffects, got %d", len(ir.PostEffects))
	}
	bundle := ir.legacyProps()
	if bundle == nil {
		return // empty scene, no bundle, also fine
	}
	if _, exists := bundle["postEffects"]; exists {
		t.Errorf("empty PostFX should not emit postEffects key in bundle")
	}
}

func TestEnvironmentTonemapMigrationSynthesizesEffect(t *testing.T) {
	// A scene that uses the legacy Environment.ToneMapping path with NO
	// explicit PostFX.Effects should get a synthesized Tonemap effect at
	// compile time so the post-processor takes over from the inline shader.
	p := Props{
		Environment: Environment{
			ToneMapping: "aces",
			Exposure:    1.2,
		},
	}
	ir := p.SceneIR()
	if len(ir.PostEffects) != 1 {
		t.Fatalf("expected 1 synthesized PostEffect, got %d", len(ir.PostEffects))
	}
	tm, ok := ir.PostEffects[0].(TonemapIR)
	if !ok {
		t.Fatalf("synthesized effect type = %T, want TonemapIR", ir.PostEffects[0])
	}
	if tm.Mode != "aces" {
		t.Errorf("synthesized Mode = %q, want aces", tm.Mode)
	}
	if tm.Exposure != 1.2 {
		t.Errorf("synthesized Exposure = %v, want 1.2", tm.Exposure)
	}
}

func TestEnvironmentTonemapMigrationRespectsExplicitTonemap(t *testing.T) {
	// If the user already declared a Tonemap effect, don't synthesize a
	// second one — the explicit declaration wins.
	p := Props{
		Environment: Environment{
			ToneMapping: "aces",
			Exposure:    1.2,
		},
		PostFX: PostFX{Effects: []PostEffect{
			Tonemap{Mode: TonemapACES, Exposure: 0.7}, // user override
		}},
	}
	ir := p.SceneIR()
	if len(ir.PostEffects) != 1 {
		t.Fatalf("expected 1 PostEffect, got %d", len(ir.PostEffects))
	}
	tm := ir.PostEffects[0].(TonemapIR)
	// Float32 → float64 widens with precision drift; compare with tolerance.
	if diff := tm.Exposure - 0.7; diff < -1e-6 || diff > 1e-6 {
		t.Errorf("explicit Tonemap should have won, Exposure = %v want ~0.7", tm.Exposure)
	}
}

func TestEnvironmentTonemapMigrationNoOpWhenEmpty(t *testing.T) {
	// No legacy field set, no explicit PostFX → no PostEffects.
	p := Props{}
	ir := p.SceneIR()
	if len(ir.PostEffects) != 0 {
		t.Errorf("expected 0 PostEffects, got %d", len(ir.PostEffects))
	}
}
