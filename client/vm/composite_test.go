// Slice Y.A — composite-literal evaluator tests. These exercise the
// new OpComposite handler in vm.go directly against handcrafted
// programs (no lowerer round-trip). Lowerer-end tests live in
// ir/golower/composite_lit_test.go and assert end-to-end behavior; the
// tests here pin the operand encoding so a future encoding tweak
// breaks loudly at the VM seam first.
package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

// TestOpCompositeStructPositional builds a struct Value from a
// positional-style expression: the lowerer pre-emits the field name
// keys, the VM stores them in Fields, and a follow-up OpIndex with
// the field-name key reads them back.
func TestOpCompositeStructPositional(t *testing.T) {
	// Program shape:
	//   exprs:
	//     0: "X" (string lit)
	//     1: 3.5 (float lit)
	//     2: "Y" (string lit)
	//     3: 1.25 (float lit)
	//     4: OpComposite("struct:vec2", [0,1,2,3])
	exprs := []program.Expr{
		{Op: program.OpLitString, Value: "X", Type: program.TypeString},
		{Op: program.OpLitFloat, Value: "3.5", Type: program.TypeFloat},
		{Op: program.OpLitString, Value: "Y", Type: program.TypeString},
		{Op: program.OpLitFloat, Value: "1.25", Type: program.TypeFloat},
		{
			Op:       program.OpComposite,
			Value:    "struct:vec2",
			Operands: []program.ExprID{0, 1, 2, 3},
		},
	}
	prog := &program.Program{Exprs: exprs}
	machine := NewVM(prog, nil)
	got := machine.Eval(4)
	if got.Fields == nil {
		t.Fatalf("OpComposite struct: Fields nil; got %+v", got)
	}
	if got.Fields["X"].Num != 3.5 {
		t.Errorf("Fields[X] = %f, want 3.5", got.Fields["X"].Num)
	}
	if got.Fields["Y"].Num != 1.25 {
		t.Errorf("Fields[Y] = %f, want 1.25", got.Fields["Y"].Num)
	}
}

// TestOpCompositeSliceMaterialization confirms a slice literal becomes
// an array Value whose Items reflect the pair order.
func TestOpCompositeSliceMaterialization(t *testing.T) {
	// Program: ["alpha", "beta", "gamma"] via OpComposite("slice", ...).
	exprs := []program.Expr{
		{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},
		{Op: program.OpLitString, Value: "alpha", Type: program.TypeString},
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},
		{Op: program.OpLitString, Value: "beta", Type: program.TypeString},
		{Op: program.OpLitInt, Value: "2", Type: program.TypeInt},
		{Op: program.OpLitString, Value: "gamma", Type: program.TypeString},
		{
			Op:       program.OpComposite,
			Value:    "slice",
			Operands: []program.ExprID{0, 1, 2, 3, 4, 5},
		},
	}
	prog := &program.Program{Exprs: exprs}
	machine := NewVM(prog, nil)
	got := machine.Eval(6)
	if len(got.Items) != 3 {
		t.Fatalf("Items length = %d, want 3", len(got.Items))
	}
	if got.Items[0].Str != "alpha" || got.Items[2].Str != "gamma" {
		t.Errorf("Items = %+v, want [alpha beta gamma]", got.Items)
	}
}

// TestOpCompositeMapMaterialization confirms a map literal becomes an
// object Value whose Fields keys reflect the evaluated key expressions.
func TestOpCompositeMapMaterialization(t *testing.T) {
	exprs := []program.Expr{
		{Op: program.OpLitString, Value: "x", Type: program.TypeString},
		{Op: program.OpLitFloat, Value: "1.5", Type: program.TypeFloat},
		{Op: program.OpLitString, Value: "y", Type: program.TypeString},
		{Op: program.OpLitFloat, Value: "2.5", Type: program.TypeFloat},
		{
			Op:       program.OpComposite,
			Value:    "map",
			Operands: []program.ExprID{0, 1, 2, 3},
		},
	}
	prog := &program.Program{Exprs: exprs}
	machine := NewVM(prog, nil)
	got := machine.Eval(4)
	if got.Fields == nil || len(got.Fields) != 2 {
		t.Fatalf("Fields = %+v, want 2 entries", got.Fields)
	}
	if got.Fields["x"].Num != 1.5 || got.Fields["y"].Num != 2.5 {
		t.Errorf("Fields = %+v, want {x:1.5, y:2.5}", got.Fields)
	}
}

// TestOpCompositeEmptyStruct verifies an empty composite still
// materializes a Fields map (so downstream IndexVal reads zero rather
// than nil-deref).
func TestOpCompositeEmptyStruct(t *testing.T) {
	exprs := []program.Expr{
		{Op: program.OpComposite, Value: "struct:vec2"},
	}
	prog := &program.Program{Exprs: exprs}
	machine := NewVM(prog, nil)
	got := machine.Eval(0)
	if got.Fields == nil {
		t.Fatalf("empty struct should still allocate Fields, got %+v", got)
	}
	if len(got.Fields) != 0 {
		t.Errorf("empty struct Fields = %d entries, want 0", len(got.Fields))
	}
}

// TestOpCompositeOddOperandsDiagnostic ensures a malformed program
// (odd operand count) returns the zero Value and records a structured
// diagnostic instead of panicking.
func TestOpCompositeOddOperandsDiagnostic(t *testing.T) {
	exprs := []program.Expr{
		{Op: program.OpLitString, Value: "X", Type: program.TypeString},
		{
			Op:       program.OpComposite,
			Value:    "struct:vec2",
			Operands: []program.ExprID{0}, // odd — invalid
		},
	}
	prog := &program.Program{Exprs: exprs}
	machine := NewVM(prog, nil)
	got := machine.Eval(1)
	if got.Fields != nil {
		t.Errorf("malformed OpComposite should fall back to zero Any, got %+v", got)
	}
	diags := machine.Diagnostics()
	if len(diags) != 1 || diags[0].Code != "invalid_composite" {
		t.Errorf("expected invalid_composite diagnostic, got %+v", diags)
	}
}

// TestOpCompositeUnknownKindDiagnostic ensures an unrecognized kind tag
// records a structured diagnostic.
func TestOpCompositeUnknownKindDiagnostic(t *testing.T) {
	exprs := []program.Expr{
		{Op: program.OpComposite, Value: "tuple"},
	}
	prog := &program.Program{Exprs: exprs}
	machine := NewVM(prog, nil)
	got := machine.Eval(0)
	if got.Fields != nil || got.Items != nil {
		t.Errorf("unknown OpComposite kind should fall back to zero Any, got %+v", got)
	}
	diags := machine.Diagnostics()
	if len(diags) != 1 || diags[0].Code != "invalid_composite" {
		t.Errorf("expected invalid_composite diagnostic, got %+v", diags)
	}
}
