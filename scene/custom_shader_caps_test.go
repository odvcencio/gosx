package scene

import (
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

// hasCapReason returns true if reasons contains a CapReason matching the given
// feature and excluded backend.
func hasExcludeReason(reasons []capability.CapReason, feat capability.Feature, excl capability.Backend) bool {
	for _, r := range reasons {
		if r.Feature == feat && r.Excludes == excl {
			return true
		}
	}
	return false
}

// TestCustomShaderGLSLOnlyExcludesWebGPU: a Mesh with a CustomMaterial that
// provides ONLY GLSL should exclude webgpu from Capable and add a
// custom-shader/Excludes:webgpu reason.
func TestCustomShaderGLSLOnlyExcludesWebGPU(t *testing.T) {
	props := Props{
		Graph: NewGraph(Mesh{
			ID:       "glsl-only",
			Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
			Material: CustomMaterial{
				VertexGLSL:   "void main(){}",
				FragmentGLSL: "void main(){}",
			},
		}),
	}
	ir := props.SceneIR()
	if ir.BackendCaps == nil {
		t.Fatalf("expected BackendCaps to be non-nil")
	}

	bs := backendSet(ir.BackendCaps.Capable)
	if bs[capability.BackendWebGPU] {
		t.Errorf("expected webgpu to be excluded from Capable, got Capable=%v", ir.BackendCaps.Capable)
	}
	if !bs[capability.BackendWebGL] {
		t.Errorf("expected webgl to remain Capable, got Capable=%v", ir.BackendCaps.Capable)
	}
	if !bs[capability.BackendCanvas2D] {
		t.Errorf("expected canvas2d to remain Capable, got Capable=%v", ir.BackendCaps.Capable)
	}
	if !hasExcludeReason(ir.BackendCaps.Reasons, capability.FeatureCustomShader, capability.BackendWebGPU) {
		t.Errorf("expected Reasons to contain custom-shader/Excludes:webgpu, got %v", ir.BackendCaps.Reasons)
	}
}

// TestCustomShaderBothLanguagesKeepsWebGPU: a CustomMaterial with both GLSL
// and WGSL should leave webgpu in Capable.
func TestCustomShaderBothLanguagesKeepsWebGPU(t *testing.T) {
	props := Props{
		Graph: NewGraph(Mesh{
			ID:       "both-languages",
			Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
			Material: CustomMaterial{
				VertexGLSL:   "void main(){}",
				FragmentGLSL: "void main(){}",
				VertexWGSL:   "@vertex fn main() -> @builtin(position) vec4f { return vec4f(0,0,0,1); }",
				FragmentWGSL: "@fragment fn main() -> @location(0) vec4f { return vec4f(1,0,0,1); }",
			},
		}),
	}
	ir := props.SceneIR()
	if ir.BackendCaps == nil {
		t.Fatalf("expected BackendCaps to be non-nil")
	}

	bs := backendSet(ir.BackendCaps.Capable)
	if !bs[capability.BackendWebGPU] {
		t.Errorf("expected webgpu to remain Capable for GLSL+WGSL material, got Capable=%v", ir.BackendCaps.Capable)
	}
	if !bs[capability.BackendWebGL] {
		t.Errorf("expected webgl to remain Capable for GLSL+WGSL material, got Capable=%v", ir.BackendCaps.Capable)
	}
}

// TestPlainSceneUnaffectedByShaderResolver: a plain scene with no custom
// material should still have all three backends capable.
func TestPlainSceneUnaffectedByShaderResolver(t *testing.T) {
	props := Props{
		Graph: NewGraph(Mesh{
			ID:       "plain-box",
			Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
			Material: StandardMaterial{Color: "#fff"},
		}),
	}
	ir := props.SceneIR()
	if ir.BackendCaps == nil {
		t.Fatalf("expected BackendCaps to be non-nil")
	}

	bs := backendSet(ir.BackendCaps.Capable)
	for _, b := range []capability.Backend{capability.BackendWebGPU, capability.BackendWebGL, capability.BackendCanvas2D} {
		if !bs[b] {
			t.Errorf("expected %q to be Capable in a plain scene, got Capable=%v", b, ir.BackendCaps.Capable)
		}
	}
}
