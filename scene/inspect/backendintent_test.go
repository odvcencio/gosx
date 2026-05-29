package inspect

import (
	"encoding/json"
	"testing"

	"m31labs.dev/gosx/scene"
)

// TestInspectBackendIntentDerivesFromVerdict proves the surface report's
// BackendIntent reflects the SceneIR's backendCaps verdict (webgl-only for a
// pickable scene) rather than the legacy hardcoded triple.
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
	if len(got) != 1 || got[0] != "webgl" {
		t.Fatalf("expected BackendIntent == [webgl] from verdict, got %v", got)
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
