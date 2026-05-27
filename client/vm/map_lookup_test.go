// Slice Y.B — VM-level tests for the OpMapLookup opcode.
//
// These tests bypass the lowerer and construct programs by hand to
// verify that evalMapLookupExpr returns an ObjectVal with "value" and
// "ok" fields covering the three observable states:
//
//   - key present in the map  → value = stored, ok = true
//   - key absent from the map → value = zero,   ok = false
//   - target has no Fields    → value = zero,   ok = false + diagnostic
package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

// buildLookupProgram constructs a program that defines an OpMapLookup
// expression against an OpComposite map literal. Returns the ExprID of
// the lookup so callers can Eval it directly.
func buildLookupProgram(mapEntries [][2]string, key string) (*program.Program, program.ExprID) {
	prog := &program.Program{}
	// Build entries: each is (OpLitString key, OpLitFloat value).
	var entryIDs []program.ExprID
	for _, entry := range mapEntries {
		k := program.ExprID(len(prog.Exprs))
		prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpLitString, Value: entry[0], Type: program.TypeString})
		v := program.ExprID(len(prog.Exprs))
		prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpLitFloat, Value: entry[1], Type: program.TypeFloat})
		entryIDs = append(entryIDs, k, v)
	}
	mapID := program.ExprID(len(prog.Exprs))
	prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpComposite, Value: "map", Operands: entryIDs})
	keyID := program.ExprID(len(prog.Exprs))
	prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpLitString, Value: key, Type: program.TypeString})
	lookupID := program.ExprID(len(prog.Exprs))
	prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpMapLookup, Operands: []program.ExprID{mapID, keyID}})
	return prog, lookupID
}

func TestMapLookupPresent(t *testing.T) {
	prog, id := buildLookupProgram([][2]string{{"hit", "1.5"}, {"miss", "0"}}, "hit")
	vm := NewVM(prog, nil)
	got := vm.Eval(id)
	if got.Fields == nil {
		t.Fatalf("expected ObjectVal with Fields; got %+v", got)
	}
	if got.Fields["ok"].Bool != true {
		t.Errorf("ok = %v, want true", got.Fields["ok"].Bool)
	}
	if got.Fields["value"].Num != 1.5 {
		t.Errorf("value = %f, want 1.5", got.Fields["value"].Num)
	}
}

func TestMapLookupAbsent(t *testing.T) {
	prog, id := buildLookupProgram([][2]string{{"hit", "1.5"}}, "miss")
	vm := NewVM(prog, nil)
	got := vm.Eval(id)
	if got.Fields == nil {
		t.Fatalf("expected ObjectVal with Fields; got %+v", got)
	}
	if got.Fields["ok"].Bool != false {
		t.Errorf("ok = %v, want false", got.Fields["ok"].Bool)
	}
	// Missing-key value is the zero Any — Num is 0, Str is empty.
	v := got.Fields["value"]
	if v.Num != 0 || v.Str != "" {
		t.Errorf("absent value = %+v, want zero", v)
	}
}

// TestMapLookupOnNonMap exercises the diagnostic path when the target
// has no Fields map (e.g., an array, scalar, or zero Value). The lookup
// still returns the {value: zero, ok: false} pair so downstream OpIndex
// reads stay safe.
func TestMapLookupOnNonMap(t *testing.T) {
	prog := &program.Program{}
	// Build a non-map target — an array literal.
	itemKey := program.ExprID(len(prog.Exprs))
	prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
	itemVal := program.ExprID(len(prog.Exprs))
	prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpLitInt, Value: "42", Type: program.TypeInt})
	arrID := program.ExprID(len(prog.Exprs))
	prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpComposite, Value: "slice", Operands: []program.ExprID{itemKey, itemVal}})
	keyID := program.ExprID(len(prog.Exprs))
	prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpLitString, Value: "any", Type: program.TypeString})
	lookupID := program.ExprID(len(prog.Exprs))
	prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpMapLookup, Operands: []program.ExprID{arrID, keyID}})

	var diags []Diagnostic
	vm := NewVM(prog, nil)
	vm.diagnosticSink = func(d Diagnostic) { diags = append(diags, d) }
	got := vm.Eval(lookupID)
	if got.Fields == nil || got.Fields["ok"].Bool != false {
		t.Errorf("non-map lookup yielded %+v, want {value: zero, ok: false}", got)
	}
	if len(diags) == 0 {
		t.Errorf("expected diagnostic for non-map OpMapLookup target")
	}
	found := false
	for _, d := range diags {
		if d.Code == "map_lookup_non_map" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected map_lookup_non_map diagnostic; got %+v", diags)
	}
}

// TestMapLookupOperandCount verifies the requireOperands fast-path
// returns the {zero, false} pair when fewer than 2 operands are given.
func TestMapLookupOperandCount(t *testing.T) {
	prog := &program.Program{}
	// Single-operand OpMapLookup — malformed program.
	mapID := program.ExprID(len(prog.Exprs))
	prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpComposite, Value: "map"})
	lookupID := program.ExprID(len(prog.Exprs))
	prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpMapLookup, Operands: []program.ExprID{mapID}})

	vm := NewVM(prog, nil)
	got := vm.Eval(lookupID)
	if got.Fields == nil {
		t.Fatalf("expected ObjectVal even on operand-shortage; got %+v", got)
	}
	if got.Fields["ok"].Bool != false {
		t.Errorf("ok = %v, want false on operand-shortage", got.Fields["ok"].Bool)
	}
}
