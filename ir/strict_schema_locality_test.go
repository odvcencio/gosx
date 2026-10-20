package ir_test

import (
	"errors"
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
)

// TestStrictPropsSchemaMustBeSameFileCarriesTheReason covers gosx#230 ask
// 1. The rule itself does not change — the file renderer resolves a strict
// component's props from schema data the IR carries, and the lowerer builds
// that data from the one .gsx file it is given — but the diagnostic now
// says why a sibling .go declaration cannot serve, instead of leaving the
// author to guess that a same-package type should have been reachable.
func TestStrictPropsSchemaMustBeSameFileCarriesTheReason(t *testing.T) {
	for _, tc := range []struct {
		name    string
		source  string
		message string
	}{
		{
			name: "props struct declared elsewhere",
			source: `package app

component Card(props: CardProps) {
	return <span>{props.Title}</span>
}
`,
			message: "whose struct schema is not declared in this .gsx file",
		},
		{
			name: "nested struct declared elsewhere",
			source: `package app

type CardProps struct {
	Team TeamView
}

component Card(props: CardProps) {
	return <span>{props.Team.Name}</span>
}
`,
			message: "struct TeamView is not declared in this .gsx file",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parse(t, []byte(tc.source))
			if err == nil {
				t.Fatal("Lower unexpectedly accepted a schema declared outside this .gsx file")
			}
			var diagErr *ir.DiagnosticsError
			if !errors.As(err, &diagErr) {
				t.Fatalf("error %v is not an *ir.DiagnosticsError", err)
			}
			if len(diagErr.Diagnostics) != 1 {
				t.Fatalf("got %d diagnostics, want 1: %v", len(diagErr.Diagnostics), err)
			}
			diag := diagErr.Diagnostics[0]
			if !strings.Contains(diag.Message, tc.message) {
				t.Fatalf("message = %q, want to contain %q", diag.Message, tc.message)
			}
			for _, want := range []string{
				"ir.Lower reads one .gsx file",
				"let the .go converter keep its own type",
			} {
				if !strings.Contains(diag.Hint, want) {
					t.Fatalf("hint = %q, want to contain %q", diag.Hint, want)
				}
			}
		})
	}
}

// TestStrictHopFailuresWithoutASchemaLocalityRemedyCarryNoHint keeps the
// hint honest: a path defect that has nothing to do with where a type is
// declared must not suggest moving a declaration.
func TestStrictHopFailuresWithoutASchemaLocalityRemedyCarryNoHint(t *testing.T) {
	_, err := parse(t, []byte(`package app

type CardProps struct {
	Title string
}

component Card(props: CardProps) {
	return <span>{props.Title.Extra}</span>
}
`))
	if err == nil {
		t.Fatal("Lower unexpectedly accepted a selector through a scalar field")
	}
	var diagErr *ir.DiagnosticsError
	if !errors.As(err, &diagErr) {
		t.Fatalf("error %v is not an *ir.DiagnosticsError", err)
	}
	for _, diag := range diagErr.Diagnostics {
		if diag.Hint != "" {
			t.Fatalf("diagnostic %q carries hint %q, want none", diag.Message, diag.Hint)
		}
	}
}
