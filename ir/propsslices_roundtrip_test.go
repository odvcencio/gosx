package ir

import (
	"encoding/json"
	"testing"
)

// TestComponentPropsSlicesDecodesAbsentFromOlderPrograms proves the
// compatibility rule PropsSlices' doc comment states, matching
// ComponentSyntax's and PropsPaths' established zero-value convention: a
// Component encoded before this field existed decodes into the current
// type with PropsSlices absent (nil), not an error and not a
// present-but-empty map that would compare unequal to the legacy zero
// value. legacyComponentJSON is a hand-written encoding of a pre-#182/#184
// Component — every field this change adds is absent, exactly as a program
// serialized before PropsSlices existed would be.
func TestComponentPropsSlicesDecodesAbsentFromOlderPrograms(t *testing.T) {
	const legacyComponentJSON = `{
		"Name": "Row",
		"PropsType": "RowProps",
		"PropsName": "props",
		"PropsFields": {"Breakdown": "[]BreakdownRow"},
		"Syntax": 1,
		"Root": 3
	}`
	var comp Component
	if err := json.Unmarshal([]byte(legacyComponentJSON), &comp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if comp.PropsSlices != nil {
		t.Fatalf("PropsSlices = %#v, want nil (absent) for a program serialized before this field existed", comp.PropsSlices)
	}
	if comp.Name != "Row" || comp.PropsFields["Breakdown"] != "[]BreakdownRow" || comp.Syntax != ComponentSyntaxStrict {
		t.Fatalf("legacy fields did not decode unchanged: %#v", comp)
	}
}

// TestComponentPropsSlicesRoundTripsThroughJSON proves the new field
// itself round-trips: a Component with a populated PropsSlices, including
// a nested (dot-joined) Reads key, encodes and decodes back to an equal
// value, alongside its PropsFields root-type entry.
func TestComponentPropsSlicesRoundTripsThroughJSON(t *testing.T) {
	comp := Component{
		Name:      "Row",
		PropsType: "RowProps",
		PropsName: "props",
		PropsFields: map[string]string{
			"Breakdown": "[]BreakdownRow",
		},
		PropsSlices: map[string]SlicePropSchema{
			"Breakdown": {
				Elem: "BreakdownRow",
				Reads: map[string]string{
					"Label":      "string",
					"Stat.Label": "string",
				},
			},
		},
		Syntax: ComponentSyntaxStrict,
	}
	data, err := json.Marshal(comp)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded Component
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	schema, ok := decoded.PropsSlices["Breakdown"]
	if !ok {
		t.Fatalf("PropsSlices did not round-trip: %#v", decoded.PropsSlices)
	}
	if schema.Elem != "BreakdownRow" {
		t.Fatalf("PropsSlices[Breakdown].Elem = %q, want BreakdownRow", schema.Elem)
	}
	if schema.Reads["Label"] != "string" || schema.Reads["Stat.Label"] != "string" {
		t.Fatalf("PropsSlices[Breakdown].Reads did not round-trip: %#v", schema.Reads)
	}
	if decoded.Name != comp.Name || decoded.PropsFields["Breakdown"] != "[]BreakdownRow" {
		t.Fatalf("Component did not round-trip: %#v", decoded)
	}
}
