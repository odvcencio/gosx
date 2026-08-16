package ir

import (
	"encoding/json"
	"testing"
)

// TestComponentPropsPathsDecodesAbsentFromOlderPrograms proves the
// compatibility rule PropsPaths' doc comment states, matching
// ComponentSyntax's established zero-value convention: a Component encoded
// before this field existed decodes into the current type with PropsPaths
// absent (nil), not an error and not a present-but-empty map that would
// compare unequal to the legacy zero value. legacyComponentJSON is a
// hand-written encoding of a pre-#183 Component — every field this change
// adds is absent, exactly as a program serialized before PropsPaths existed
// would be.
func TestComponentPropsPathsDecodesAbsentFromOlderPrograms(t *testing.T) {
	const legacyComponentJSON = `{
		"Name": "Card",
		"PropsType": "CardProps",
		"PropsName": "props",
		"PropsFields": {"Tone": "string"},
		"Syntax": 1,
		"Root": 3
	}`
	var comp Component
	if err := json.Unmarshal([]byte(legacyComponentJSON), &comp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if comp.PropsPaths != nil {
		t.Fatalf("PropsPaths = %#v, want nil (absent) for a program serialized before this field existed", comp.PropsPaths)
	}
	if comp.Name != "Card" || comp.PropsFields["Tone"] != "string" || comp.Syntax != ComponentSyntaxStrict {
		t.Fatalf("legacy fields did not decode unchanged: %#v", comp)
	}
}

// TestComponentPropsPathsRoundTripsThroughJSON proves the new field itself
// round-trips: a Component with a populated PropsPaths encodes and decodes
// back to an equal value, alongside its PropsFields root-type entry.
func TestComponentPropsPathsRoundTripsThroughJSON(t *testing.T) {
	comp := Component{
		Name:        "Row",
		PropsType:   "RowProps",
		PropsName:   "props",
		PropsFields: map[string]string{"Player": "Player"},
		PropsPaths:  map[string]string{"Player.Name": "string"},
		Syntax:      ComponentSyntaxStrict,
	}
	data, err := json.Marshal(comp)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded Component
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if decoded.PropsPaths["Player.Name"] != "string" {
		t.Fatalf("PropsPaths did not round-trip: %#v", decoded.PropsPaths)
	}
	if decoded.Name != comp.Name || decoded.PropsFields["Player"] != "Player" {
		t.Fatalf("Component did not round-trip: %#v", decoded)
	}
}
