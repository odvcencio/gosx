package scene

import (
	_ "embed"
	"strings"
	"testing"

	"m31labs.dev/selena/result"
)

// glow.sel.json is the artifact `selena artifact -targets wgsl,gles
// -specular-mrt` writes for testdata/selena/glow.sel. These tests carry no build
// tag, so the js/wasm test run proves the artifact path works without the
// compiler; TestSelenaArtifactsMatchSources keeps the file in step with its
// source.
//
//go:embed testdata/selena/glow.sel.json
var glowArtifactJSON []byte

func glowArtifact(t *testing.T) SelenaArtifact {
	t.Helper()
	file, err := DecodeSelenaArtifact(glowArtifactJSON)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func TestMaterialFromSelenaArtifactBuildsBrowserMaterial(t *testing.T) {
	file := glowArtifact(t)
	material, layout, err := MaterialFromSelenaArtifact(file, SelenaMaterialOptions{
		SpecularMRT: true,
		Standard:    StandardMaterial{Color: "#336699"},
		Uniforms:    map[string]any{"gain": 2.0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if layout.Material != "GlowTinted" {
		t.Fatalf("default material = %q, want the last material", layout.Material)
	}
	if material.ShaderBackend != "selena" || material.Color != "#336699" {
		t.Fatalf("backend %q color %q", material.ShaderBackend, material.Color)
	}
	if !strings.Contains(material.VertexGLSL, "#version 300 es") || !strings.Contains(material.FragmentWGSL, "fn fragmentMain") {
		t.Fatal("missing WebGL2 or WebGPU program")
	}
	if material.SpecularFragmentGLSL == "" || material.SpecularFragmentWGSL == "" {
		t.Fatal("specular variants were dropped")
	}
	if material.Uniforms["gain"] != 2.0 {
		t.Fatalf("gain override = %v", material.Uniforms["gain"])
	}
	if tint, ok := material.Uniforms["tint"].([]float32); !ok || len(tint) != 3 {
		t.Fatalf("tint default = %#v, want the descriptor default", material.Uniforms["tint"])
	}
	if material.ShaderLayout["material"] != "GlowTinted" {
		t.Fatalf("shader layout material = %v", material.ShaderLayout["material"])
	}

	base, _, err := MaterialFromSelenaArtifact(file, SelenaMaterialOptions{Material: "Glow"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := base.Uniforms["tint"]; ok {
		t.Fatal("the base material picked up the derived material's tint")
	}
}

func TestSelenaBundleFromArtifactKeepsOrder(t *testing.T) {
	file := glowArtifact(t)
	bundle, err := SelenaBundleFromArtifact(file, SelenaMaterialOptions{Material: "Glow"}, SelenaMaterialOptions{Material: "GlowTinted"})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle) != 2 || bundle[0].Name != "Glow" || bundle[1].Name != "GlowTinted" {
		t.Fatalf("bundle = %+v", bundle)
	}
	defaults, err := SelenaBundleFromArtifact(file)
	if err != nil || len(defaults) != 1 || defaults[0].Name != "GlowTinted" {
		t.Fatalf("default bundle = %+v, %v", defaults, err)
	}
}

func TestMaterialFromSelenaArtifactRejectsMismatches(t *testing.T) {
	file := glowArtifact(t)
	for name, opts := range map[string]SelenaMaterialOptions{
		"missing material": {Material: "Nope"},
		"missing target":   {Targets: []result.Target{result.TargetMetal}},
	} {
		if _, _, err := MaterialFromSelenaArtifact(file, opts); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	plain := file
	plain.Options.SpecularMRT = false
	if _, _, err := MaterialFromSelenaArtifact(plain, SelenaMaterialOptions{SpecularMRT: true}); err == nil || !strings.Contains(err.Error(), "specular") {
		t.Fatalf("specular request against a plain artifact = %v", err)
	}
	if _, err := DecodeSelenaArtifact([]byte(`{"schema":"selena.artifact.v0"}`)); err == nil {
		t.Fatal("decoded a wrong schema")
	}
}
