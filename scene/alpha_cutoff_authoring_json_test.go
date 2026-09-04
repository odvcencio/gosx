package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStandardMaterialAlphaCutoffJSON pins the authoring wire contract so the
// default-marshal regression cannot hide: the json:",omitzero" tag must keep
// an omitted AlphaCutoff off the wire (a default StandardMaterial marshals
// cleanly), a numeric cutoff marshals to its number including 0, and an
// explicit disable marshals to null and round-trips.
func TestStandardMaterialAlphaCutoffJSON(t *testing.T) {
	raw, err := json.Marshal(StandardMaterial{})
	if err != nil {
		t.Fatalf("default StandardMaterial must marshal without an alphaCutoff field: %v", err)
	}
	if strings.Contains(string(raw), "alphaCutoff") {
		t.Fatalf("omitted alphaCutoff must not appear on the wire: %s", raw)
	}

	raw, err = json.Marshal(StandardMaterial{AlphaCutoff: Cutoff(0.5)})
	if err != nil {
		t.Fatalf("numeric alphaCutoff must marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"alphaCutoff":0.5`) {
		t.Fatalf("numeric alphaCutoff marshaled to %s", raw)
	}

	raw, err = json.Marshal(StandardMaterial{AlphaCutoff: Cutoff(0)})
	if err != nil {
		t.Fatalf("zero alphaCutoff must marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"alphaCutoff":0`) {
		t.Fatalf("zero alphaCutoff must stay present on the wire: %s", raw)
	}

	raw, err = json.Marshal(StandardMaterial{AlphaCutoff: CutoffDisabled()})
	if err != nil {
		t.Fatalf("disabled alphaCutoff must marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"alphaCutoff":null`) {
		t.Fatalf("disabled alphaCutoff must marshal to null: %s", raw)
	}

	var back StandardMaterial
	if err := json.Unmarshal([]byte(`{"alphaCutoff":null}`), &back); err != nil {
		t.Fatalf("null alphaCutoff must unmarshal: %v", err)
	}
	if !back.AlphaCutoff.Disabled() {
		t.Fatal("null alphaCutoff must round-trip as an explicit disable")
	}
}
