package inspect

import (
	"encoding/json"
	"testing"

	"m31labs.dev/gosx/scene"
)

// TestInspectBackendIntentDerivesFromVerdict proves the surface report's
// BackendIntent reflects the SceneIR's backendCaps verdict rather than the
// legacy hardcoded triple.
//
// A pickable scene yields [webgpu webgl]: both GPU backends implement
// gpu-picking, and Canvas2D does not, so the required-feature rule drops
// Canvas2D. The legacy fallback triple ends in "canvas", so a report that
// stops at two GPU backends can only have come from the verdict.
//
// This test asserted [webgl] alone until gpu-picking shipped on WebGPU. See
// the FeatureGPUPicking comment in scene/capability/capability.go.
func TestInspectBackendIntentDerivesFromVerdict(t *testing.T) {
	pickable := true
	props := scene.Props{Graph: scene.NewGraph(scene.Mesh{
		ID:       "m",
		Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1},
		Material: scene.StandardMaterial{Color: "#fff"},
		Pickable: &pickable,
	})}
	data, err := json.Marshal(props.SceneIR())
	if err != nil {
		t.Fatalf("marshal SceneIR: %v", err)
	}

	report, err := InspectJSON("pickable.scene.json", data, Options{})
	if err != nil {
		t.Fatalf("InspectJSON: %v", err)
	}
	got := report.Surface.BackendIntent
	want := []string{"webgpu", "webgl"}
	if len(got) != len(want) {
		t.Fatalf("expected BackendIntent == %v from verdict, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected BackendIntent == %v from verdict, got %v", want, got)
		}
	}
}

// TestInspectBackendIntentFallback confirms the legacy triple still appears
// when a document carries no backendCaps (e.g. a hand-authored fixture).
func TestInspectBackendIntentFallback(t *testing.T) {
	data := []byte(`{"objects":[{"id":"a","kind":"cube"}]}`)
	report, err := InspectJSON("plain.scene.json", data, Options{})
	if err != nil {
		t.Fatalf("InspectJSON: %v", err)
	}
	got := report.Surface.BackendIntent
	want := []string{"webgpu", "webgl", "canvas"}
	if len(got) != len(want) {
		t.Fatalf("fallback BackendIntent = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fallback BackendIntent = %v, want %v", got, want)
		}
	}
}
