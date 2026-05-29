package capability

import "testing"

func TestPresenceResolverGLSLOnly(t *testing.T) {
	r := PresenceResolver{}
	got := r.Serves(CustomMaterialSources{GLSL: true, WGSL: false})
	if !got[BackendWebGL] {
		t.Fatalf("expected webgl=true for GLSL-only material, got %v", got)
	}
	if got[BackendWebGPU] {
		t.Fatalf("expected webgpu=false for GLSL-only material, got %v", got)
	}
}

func TestPresenceResolverBothLanguages(t *testing.T) {
	r := PresenceResolver{}
	got := r.Serves(CustomMaterialSources{GLSL: true, WGSL: true})
	if !got[BackendWebGL] {
		t.Fatalf("expected webgl=true for GLSL+WGSL material, got %v", got)
	}
	if !got[BackendWebGPU] {
		t.Fatalf("expected webgpu=true for GLSL+WGSL material, got %v", got)
	}
}
