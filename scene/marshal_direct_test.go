package scene

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestSceneIRDirectMarshalMatchesLegacy verifies that encoding SceneIR
// directly via json.Marshal (reflection over tagged struct fields) gives
// the same semantic output as going through sceneIR.legacyProps() +
// json.Marshal(map). This is the pinning test for the Props.MarshalJSON
// fast path rewrite: if the two produce the same object tree, we can
// swap to the direct path and skip the map round-trip entirely.
func TestSceneIRDirectMarshalMatchesLegacy(t *testing.T) {
	props := benchMixedScene()
	sceneIR := props.SceneIR()

	directBytes, err := json.Marshal(sceneIR)
	if err != nil {
		t.Fatalf("direct Marshal(sceneIR) failed: %v", err)
	}
	legacyBytes, err := json.Marshal(sceneIR.legacyProps())
	if err != nil {
		t.Fatalf("Marshal(sceneIR.legacyProps) failed: %v", err)
	}

	var direct, legacy any
	if err := json.Unmarshal(directBytes, &direct); err != nil {
		t.Fatalf("unmarshal direct: %v\nraw: %s", err, directBytes)
	}
	if err := json.Unmarshal(legacyBytes, &legacy); err != nil {
		t.Fatalf("unmarshal legacy: %v\nraw: %s", err, legacyBytes)
	}
	if !reflect.DeepEqual(direct, legacy) {
		t.Fatalf("direct vs legacy mismatch\n direct: %s\n legacy: %s", directBytes, legacyBytes)
	}
}
