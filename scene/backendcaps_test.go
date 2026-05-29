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

// Test 2: a pickable mesh forces webgl-only (gpu-picking is required by
// DefaultPolicy and only webgl implements it).
func TestSceneIRBackendCapsPickableForcesWebGL(t *testing.T) {
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
	if len(ir.BackendCaps.Capable) != 1 || ir.BackendCaps.Capable[0] != capability.BackendWebGL {
		t.Fatalf("expected Capable == [webgl], got %v", ir.BackendCaps.Capable)
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
