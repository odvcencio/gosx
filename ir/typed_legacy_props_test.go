package ir_test

import (
	"testing"

	"m31labs.dev/gosx/ir"
)

// --- gosx#240: the typed legacy component category --------------------------

// lowerTypedLegacySource compiles source and returns its program, failing on
// any diagnostic.
func lowerTypedLegacySource(t *testing.T, source string) *ir.Program {
	t.Helper()
	prog, err := parse(t, []byte(source))
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}
	return prog
}

// componentByName returns the named component of prog, or fails.
func componentByName(t *testing.T, prog *ir.Program, name string) ir.Component {
	t.Helper()
	for _, comp := range prog.Components {
		if comp.Name == name {
			return comp
		}
	}
	t.Fatalf("component %s not found in %#v", name, prog.Components)
	return ir.Component{}
}

// TestLowerRecordsTypedLegacyPropsSchema proves gosx#240's first claim:
// PropsFields and PropsPaths population is no longer gated on the strict
// spelling. A legacy component whose props parameter names a struct declared
// in the same file now carries the same two maps a strict component carries,
// built by the same collector from the same reads.
func TestLowerRecordsTypedLegacyPropsSchema(t *testing.T) {
	prog := lowerTypedLegacySource(t, `package app

type Team struct {
	Abbreviation string
}

type RowProps struct {
	Team Team
	Tone string
}

func Row(props RowProps) Node {
	return <div class={"tone-" + props.Tone}>{props.Team.Abbreviation}</div>
}
`)
	row := componentByName(t, prog, "Row")
	if row.Syntax != ir.ComponentSyntaxLegacy {
		t.Fatalf("Row.Syntax = %d, want %d", row.Syntax, ir.ComponentSyntaxLegacy)
	}
	if !row.PropsTyped {
		t.Fatal("Row.PropsTyped = false, want true")
	}
	if row.PropsFields["Tone"] != "string" || row.PropsFields["Team"] != "Team" {
		t.Fatalf("Row.PropsFields = %#v", row.PropsFields)
	}
	if row.PropsPaths["Team.Abbreviation"] != "string" {
		t.Fatalf("Row.PropsPaths = %#v", row.PropsPaths)
	}
}

// TestLowerClassifiesTypedLegacyRegardlessOfDeclarationOrder pins the
// classification to the whole file rather than to file order, the same
// property collectStrictSchemas' two passes give a strict component's own
// reads (gosx#182/#184 M-3). A props struct declared BELOW its component
// must classify exactly as one declared above it.
func TestLowerClassifiesTypedLegacyRegardlessOfDeclarationOrder(t *testing.T) {
	prog := lowerTypedLegacySource(t, `package app

func Row(props RowProps) Node {
	return <div>{props.Tone}</div>
}

type RowProps struct {
	Tone string
}
`)
	row := componentByName(t, prog, "Row")
	if !row.PropsTyped {
		t.Fatal("Row.PropsTyped = false, want true")
	}
	if row.PropsFields["Tone"] != "string" {
		t.Fatalf("Row.PropsFields = %#v", row.PropsFields)
	}
}

// TestLowerLeavesUntypedLegacyComponentsUntyped enumerates every shape that
// is NOT retrofittable. Each keeps the v0.48 map binding and the gosx#229
// diagnostic, because none of them names a struct this file declares.
func TestLowerLeavesUntypedLegacyComponentsUntyped(t *testing.T) {
	for _, tc := range []struct {
		name        string
		declaration string
	}{
		{
			name: "props any",
			declaration: `func Row(props any) Node {
	return <div>row</div>
}`,
		},
		{
			name: "an attribute list",
			declaration: `func Row(props AttrList) Node {
	return <div>row</div>
}`,
		},
		{
			name: "a struct declared in another file",
			declaration: `func Row(props ExternalProps) Node {
	return <div>row</div>
}`,
		},
		{
			name: "an anonymous struct",
			declaration: `func Row(props struct{ Tone string }) Node {
	return <div>row</div>
}`,
		},
		{
			name: "a parameter the renderer never binds",
			declaration: `type RowProps struct {
	Tone string
}

func Row(attrs RowProps) Node {
	return <div>row</div>
}`,
		},
		{
			name: "no parameter at all",
			declaration: `func Row() Node {
	return <div>row</div>
}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog := lowerTypedLegacySource(t, "package app\n\n"+tc.declaration+"\n")
			row := componentByName(t, prog, "Row")
			if row.PropsTyped {
				t.Fatalf("Row.PropsTyped = true, want false (%#v)", row)
			}
			if len(row.PropsFields) != 0 || len(row.PropsPaths) != 0 {
				t.Fatalf("Row carries a schema it cannot have: fields=%#v paths=%#v", row.PropsFields, row.PropsPaths)
			}
		})
	}
}

// TestLowerLeavesStrictComponentsPropsTypedFalse holds the field to its one
// meaning. Syntax already reports that a strict component's props are typed,
// so PropsTyped answers only the legacy question and no consumer has two
// ways to ask the same thing.
func TestLowerLeavesStrictComponentsPropsTypedFalse(t *testing.T) {
	prog := lowerTypedLegacySource(t, `package app

type RowProps struct {
	Tone string
}

component Row(props: RowProps) {
	return <div>{props.Tone}</div>
}
`)
	row := componentByName(t, prog, "Row")
	if row.Syntax != ir.ComponentSyntaxStrict {
		t.Fatalf("Row.Syntax = %d, want %d", row.Syntax, ir.ComponentSyntaxStrict)
	}
	if row.PropsTyped {
		t.Fatal("Row.PropsTyped = true, want false for a strict component")
	}
}
