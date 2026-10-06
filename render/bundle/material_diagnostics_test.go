package bundle

import (
	"m31labs.dev/gosx/engine"
	"testing"
)

func TestRendererReportsAndResetsCustomMaterialFallback(t *testing.T) {
	r, err := New(Config{Device: newFakeDevice(), Surface: fakeSurface{}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	b := engine.RenderBundle{Materials: []engine.RenderMaterial{{Kind: "custom", ShaderLayout: map[string]any{
		"programs": map[string]any{"metal": engine.ShaderProgram{Source: "authored metal"}},
	}}}}
	if err := r.Frame(b, 32, 32, 0); err != nil {
		t.Fatal(err)
	}
	stats := r.Stats()
	if len(stats.MaterialFallbacks) != 1 || stats.MaterialFallbacks[0].Code != "scene.native.custom_material_fallback" {
		t.Fatalf("missing truth: %+v", stats)
	}
	stats.MaterialFallbacks[0].Code = "changed"
	if r.Stats().MaterialFallbacks[0].Code == "changed" {
		t.Fatal("stats leaked mutable diagnostics")
	}
	if err := r.Frame(engine.RenderBundle{}, 32, 32, 0); err != nil {
		t.Fatal(err)
	}
	if len(r.Stats().MaterialFallbacks) != 0 {
		t.Fatal("fallback truth survived unrelated frame")
	}
}
