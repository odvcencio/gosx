package ir

import (
	"encoding/json"
	"testing"
)

// TestComponentAcceptsChildrenDecodesFalseFromOlderPrograms proves the
// compatibility rule AcceptsChildren's doc comment states, matching
// ComponentSyntax's, PropsPaths', and PropsSlices' zero-value convention: a
// Component encoded before this field existed decodes with AcceptsChildren
// false. False is not a fallback here, it is the correct historical answer —
// children were rejected at every strict callee then, so such a program
// accepted none.
func TestComponentAcceptsChildrenDecodesFalseFromOlderPrograms(t *testing.T) {
	const legacyComponentJSON = `{
		"Name": "Panel",
		"PropsType": "PanelProps",
		"PropsName": "props",
		"PropsFields": {"Title": "string"},
		"Syntax": 1,
		"Root": 4
	}`
	var comp Component
	if err := json.Unmarshal([]byte(legacyComponentJSON), &comp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if comp.AcceptsChildren {
		t.Fatal("AcceptsChildren = true, want false for a program serialized before this field existed")
	}
	if comp.Name != "Panel" || comp.PropsFields["Title"] != "string" || comp.Syntax != ComponentSyntaxStrict {
		t.Fatalf("legacy fields did not decode unchanged: %#v", comp)
	}
}

// TestComponentAcceptsChildrenRoundTripsThroughJSON proves the new field
// itself survives an encode and decode cycle.
func TestComponentAcceptsChildrenRoundTripsThroughJSON(t *testing.T) {
	comp := Component{
		Name:            "Panel",
		PropsType:       "PanelProps",
		PropsName:       "props",
		AcceptsChildren: true,
		Syntax:          ComponentSyntaxStrict,
	}
	data, err := json.Marshal(comp)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded Component
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !decoded.AcceptsChildren {
		t.Fatalf("AcceptsChildren did not round-trip: %#v", decoded)
	}
	if decoded.Name != comp.Name || decoded.Syntax != comp.Syntax {
		t.Fatalf("Component did not round-trip: %#v", decoded)
	}
}
