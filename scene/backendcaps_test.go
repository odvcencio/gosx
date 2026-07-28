package scene

import (
	"encoding/json"
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

func backendSet(backends []capability.Backend) map[capability.Backend]bool {
	out := map[capability.Backend]bool{}
	for _, b := range backends {
		out[b] = true
	}
	return out
}

func degradedContains(features []capability.Feature, want capability.Feature) bool {
	for _, f := range features {
		if f == want {
			return true
		}
	}
	return false
}

// Test 1: an IBL scene degrades on webgpu+canvas2d but all backends stay
// capable (ibl is not a hard-gate feature in DefaultPolicy).
func TestSceneIRBackendCapsIBL(t *testing.T) {
	props := Props{Environment: Environment{EnvironmentMap: "env.hdr"}}
	ir := props.SceneIR()
	if ir.BackendCaps == nil {
		t.Fatalf("expected BackendCaps to be set on SceneIR")
	}
	got := backendSet(ir.BackendCaps.Capable)
	for _, want := range []capability.Backend{capability.BackendWebGPU, capability.BackendWebGL, capability.BackendCanvas2D} {
		if !got[want] {
			t.Fatalf("expected %q to be capable, got Capable=%v", want, ir.BackendCaps.Capable)
		}
	}
	if !degradedContains(ir.BackendCaps.Degraded[capability.BackendWebGPU], capability.FeatureIBL) {
		t.Fatalf("expected Degraded[webgpu] to contain ibl, got %v", ir.BackendCaps.Degraded)
	}
}

// Test 2: a pickable mesh follows the phase-correct gpu-picking Matrix row.
// The capability slice has only the shared WebGL raycast path; the later
// runtime slice flips WebGPU true when its ID-buffer implementation lands.
func TestSceneIRBackendCapsPickableTracksMatrix(t *testing.T) {
	pickable := true
	props := Props{Graph: NewGraph(Mesh{
		ID:       "m",
		Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
		Material: StandardMaterial{Color: "#fff"},
		Pickable: &pickable,
	})}
	ir := props.SceneIR()
	if ir.BackendCaps == nil {
		t.Fatalf("expected BackendCaps to be set on SceneIR")
	}
	got := backendSet(ir.BackendCaps.Capable)
	wantWebGPU := capability.Matrix[capability.FeatureGPUPicking][capability.BackendWebGPU]
	if got[capability.BackendWebGPU] != wantWebGPU {
		t.Fatalf("webgpu capability must match Matrix=%v, got Capable=%v", wantWebGPU, ir.BackendCaps.Capable)
	}
	if !got[capability.BackendWebGL] {
		t.Fatalf("expected webgl capable for gpu-picking, got %v", ir.BackendCaps.Capable)
	}
	if got[capability.BackendCanvas2D] {
		t.Fatalf("expected canvas2d excluded for gpu-picking, got %v", ir.BackendCaps.Capable)
	}
	if wantWebGPU && degradedContains(ir.BackendCaps.Degraded[capability.BackendWebGPU], capability.FeatureGPUPicking) {
		t.Fatalf("supported webgpu picking must not be degraded, got %v", ir.BackendCaps.Degraded)
	}
}

// Water sim is now implemented on both WebGPU and WebGL2 (WebGPU stays
// primary); only canvas2d is excluded by the required-feature gate.
func TestSceneIRBackendCapsWaterSimulationCapableOnGPU(t *testing.T) {
	props := Props{Graph: NewGraph(WaterSystem{ID: "pool-water"})}
	ir := props.SceneIR()
	if ir.BackendCaps == nil {
		t.Fatalf("expected BackendCaps to be set on SceneIR")
	}
	got := backendSet(ir.BackendCaps.Capable)
	if len(ir.BackendCaps.Capable) != 2 || !got[capability.BackendWebGPU] || !got[capability.BackendWebGL] {
		t.Fatalf("expected Capable == [webgpu webgl], got %v", ir.BackendCaps.Capable)
	}
	if got[capability.BackendCanvas2D] {
		t.Fatalf("expected canvas2d excluded for water sim, got %v", ir.BackendCaps.Capable)
	}
}

func TestSceneIRBackendCapsWaterObjectTextureReason(t *testing.T) {
	props := Props{Graph: NewGraph(WaterSystem{
		ID:                      "pool-water",
		ActiveObject:            "float-sphere",
		ObjectKind:              "Sphere",
		ObjectTextureResolution: 512,
	})}
	ir := props.SceneIR()
	if ir.BackendCaps == nil {
		t.Fatalf("expected BackendCaps to be set on SceneIR")
	}
	got := backendSet(ir.BackendCaps.Capable)
	if !got[capability.BackendWebGPU] || !got[capability.BackendWebGL] {
		t.Fatalf("expected webgpu+webgl capable for water object texture pass, got %v", ir.BackendCaps.Capable)
	}
	for _, reason := range ir.BackendCaps.Reasons {
		if reason.Feature == capability.FeatureWaterObjectTexturePass && reason.Excludes == capability.BackendWebGL {
			t.Fatalf("did not expect webgl exclusion now that WebGL implements water, got %+v", ir.BackendCaps.Reasons)
		}
		if reason.Feature == capability.FeatureWaterObjectTexturePass && reason.Excludes == capability.BackendCanvas2D {
			return
		}
	}
	t.Fatalf("expected canvas2d exclusion reason for water object texture pass, got %+v", ir.BackendCaps.Reasons)
}

func TestSceneIRBackendCapsWaterObjectTextureBudgetReason(t *testing.T) {
	props := Props{Graph: NewGraph(WaterSystem{
		ID:                       "pool-water",
		ObjectTexturePixelBudget: 3145728,
	})}
	ir := props.SceneIR()
	if ir.BackendCaps == nil {
		t.Fatalf("expected BackendCaps to be set on SceneIR")
	}
	for _, reason := range ir.BackendCaps.Reasons {
		if reason.Feature == capability.FeatureWaterObjectTexturePass && reason.Excludes == capability.BackendCanvas2D {
			return
		}
	}
	t.Fatalf("expected canvas2d exclusion reason for water object texture budget, got %+v", ir.BackendCaps.Reasons)
}

func TestSceneIRBackendCapsWaterObjectMeshShadowReason(t *testing.T) {
	props := Props{Graph: NewGraph(WaterSystem{
		ID:           "pool-water",
		ActiveObject: "TorusKnot",
		ObjectKind:   "compound",
	})}
	ir := props.SceneIR()
	if ir.BackendCaps == nil {
		t.Fatalf("expected BackendCaps to be set on SceneIR")
	}
	got := backendSet(ir.BackendCaps.Capable)
	if !got[capability.BackendWebGPU] || !got[capability.BackendWebGL] {
		t.Fatalf("expected webgpu+webgl capable for water object mesh shadow pass, got %v", ir.BackendCaps.Capable)
	}
	if got[capability.BackendCanvas2D] {
		t.Fatalf("canvas2d must fall out of a water scene, got %v", ir.BackendCaps.Capable)
	}

	// The mesh-shadow pass DEGRADES WebGL2; it does not exclude it. WebGL2
	// rasterizes no caster geometry, so it shades the object shadow from an
	// analytic primitive and the shadow shape is wrong. That is a degraded image
	// of the scene, not a different scene, and excluding WebGL2 would leave a
	// browser without WebGPU holding nothing. See
	// scene/capability/water_shadow_test.go.
	//
	// canvas2d still falls out, but on the two REQUIRED water features rather
	// than on the shadow pass. Assert both halves, because the interesting
	// property is which feature carries which verdict.
	var webglDegraded, canvasExcluded bool
	for _, reason := range ir.BackendCaps.Reasons {
		if reason.Feature == capability.FeatureWaterObjectMeshShadowPass {
			if reason.Excludes != "" {
				t.Fatalf("the mesh-shadow pass is droppable and must exclude nobody, got %+v", reason)
			}
			if reason.Degrades == capability.BackendWebGL {
				webglDegraded = true
			}
		}
		switch reason.Feature {
		case capability.FeatureWaterSim, capability.FeatureWaterObjectTexturePass:
			if reason.Excludes == capability.BackendCanvas2D {
				canvasExcluded = true
			}
		}
	}
	if !webglDegraded {
		t.Fatalf("expected webgl DEGRADED by the mesh shadow pass, got %+v", ir.BackendCaps.Reasons)
	}
	if !canvasExcluded {
		t.Fatalf("expected canvas2d excluded by a required water feature, got %+v", ir.BackendCaps.Reasons)
	}
	if degraded := ir.BackendCaps.Degraded[capability.BackendWebGL]; len(degraded) != 1 || degraded[0] != capability.FeatureWaterObjectMeshShadowPass {
		t.Fatalf("webgl must be degraded by the mesh shadow pass alone, got %v", ir.BackendCaps.Degraded)
	}
}

// Test 3: backendCaps round-trips through the serialized scene payload.
func TestSceneIRBackendCapsSerializes(t *testing.T) {
	props := Props{Environment: Environment{EnvironmentMap: "env.hdr"}}
	data, err := json.Marshal(props.SceneIR())
	if err != nil {
		t.Fatalf("marshal SceneIR: %v", err)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		t.Fatalf("unmarshal probe: %v", err)
	}
	if _, ok := probe["backendCaps"]; !ok {
		t.Fatalf("expected serialized scene JSON to contain \"backendCaps\"; got keys %v", probe)
	}

	var caps capability.BackendCaps
	type doc struct {
		BackendCaps *capability.BackendCaps `json:"backendCaps"`
	}
	var d doc
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("unmarshal doc: %v", err)
	}
	if d.BackendCaps == nil {
		t.Fatalf("expected BackendCaps non-nil after round-trip")
	}
	caps = *d.BackendCaps
	got := backendSet(caps.Capable)
	for _, want := range []capability.Backend{capability.BackendWebGPU, capability.BackendWebGL, capability.BackendCanvas2D} {
		if !got[want] {
			t.Fatalf("expected %q capable after round-trip, got %v", want, caps.Capable)
		}
	}
}

func TestRequiredBackendsMapping(t *testing.T) {
	yes := true
	if got := requiredBackends(Props{RequireWebGL: &yes}); len(got) != 1 || got[0] != capability.BackendWebGL {
		t.Fatalf("RequireWebGL: expected [webgl], got %v", got)
	}
	if got := requiredBackends(Props{RequiredCapabilities: RequireWebGPU()}); len(got) != 1 || got[0] != capability.BackendWebGPU {
		t.Fatalf("RequireWebGPU marker: expected [webgpu], got %v", got)
	}
	if got := requiredBackends(Props{}); got != nil {
		t.Fatalf("no gate: expected nil, got %v", got)
	}
}
