package route

import (
	"strings"
	"testing"
)

// nestedTestTeam stands in for a nested struct a .gsx file declares, and
// nestedTestConverterTeam for the differently named type a sibling .go
// converter builds. gosx#230's conflict was that a nested type had to be
// both at once: declared only in the .gsx file (the lowerer resolves hops
// through the same-file schema) and identical to what the .go file
// constructs (the old boundary compared type names).
type nestedTestTeam struct {
	Abbreviation string
	Tone         string
}

type nestedTestConverterTeam struct {
	Abbreviation string
	Tone         string
	Name         string
}

// TestRequireStrictSpreadStructFieldProvesStructurally covers gosx#230 ask
// 2 at the boundary itself: a spread's nested struct field is proved by the
// leaves the renderer reads, so a differently named source type carrying
// those leaves is admitted.
func TestRequireStrictSpreadStructFieldProvesStructurally(t *testing.T) {
	value := nestedTestConverterTeam{Abbreviation: "NE", Tone: "red", Name: "Patriots"}
	got, err := requireStrictSpreadStructField(value, "nestedTestTeam", map[string]string{
		"Abbreviation": "string",
		"Tone":         "string",
	})
	if err != nil {
		t.Fatalf("requireStrictSpreadStructField: %v", err)
	}
	if got != any(value) {
		t.Fatalf("returned %#v, want %#v", got, value)
	}
}

// TestRequireStrictSpreadStructFieldFailsClosed proves structural does not
// mean unproved. Every shape that cannot supply the leaves the renderer
// reads is still rejected.
func TestRequireStrictSpreadStructFieldFailsClosed(t *testing.T) {
	paths := map[string]string{"Abbreviation": "string"}
	for _, tc := range []struct {
		name  string
		value any
		paths map[string]string
		want  string
	}{
		{
			name:  "nil value",
			value: nil,
			paths: paths,
			want:  "value is nil",
		},
		{
			name:  "map cannot prove field coverage",
			value: map[string]any{"Abbreviation": "NE"},
			paths: paths,
			want:  "want a struct carrying the fields nestedTestTeam declares",
		},
		{
			name:  "pointer is not the struct",
			value: &nestedTestConverterTeam{Abbreviation: "NE"},
			paths: paths,
			want:  "want a struct carrying the fields nestedTestTeam declares",
		},
		{
			name:  "missing read field",
			value: struct{ City string }{City: "Foxborough"},
			paths: paths,
			want:  "field Abbreviation not found",
		},
		{
			name:  "read field of the wrong type",
			value: struct{ Abbreviation int }{Abbreviation: 1},
			paths: paths,
			want:  "path nestedTestTeam.Abbreviation",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := requireStrictSpreadStructField(tc.value, "nestedTestTeam", tc.paths)
			if err == nil {
				t.Fatalf("requireStrictSpreadStructField(%#v) unexpectedly accepted", tc.value)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want to contain %q", err, tc.want)
			}
		})
	}
}

// TestRequireStrictStructValueKeepsNominalCheckForNamedAttributes pins the
// deliberate asymmetry gosx#230 introduces. A named attribute at a strict
// call site has a generated-Go twin — transpile emits a composite literal
// whose field type the Go compiler proves exactly — so the file renderer
// keeps the nominal check there and stays in step with that twin. Only a
// spread, which has no twin when it comes from a legacy caller, relaxes.
func TestRequireStrictStructValueKeepsNominalCheckForNamedAttributes(t *testing.T) {
	value := nestedTestConverterTeam{Abbreviation: "NE", Tone: "red"}
	_, err := requireStrictStructValue(value, "nestedTestTeam", map[string]string{"Abbreviation": "string"})
	if err == nil {
		t.Fatal("requireStrictStructValue unexpectedly accepted a differently named struct")
	}
	if want := "want exact struct nestedTestTeam"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
	matching := nestedTestTeam{Abbreviation: "NE", Tone: "red"}
	if _, err := requireStrictStructValue(matching, "nestedTestTeam", map[string]string{"Abbreviation": "string"}); err != nil {
		t.Fatalf("requireStrictStructValue: %v", err)
	}
}
