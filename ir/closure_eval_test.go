package ir_test

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/program"
)

// Round-trip tests for Phase 4 closure syntax: parse a method-call expression
// with a `func(x){ ... }` closure argument, then evaluate the result through
// the island VM with concrete prop data to confirm the lowered ops actually
// produce the right runtime values.

func TestClosureRoundTripFilterByField(t *testing.T) {
	scope := &ir.ExprScope{Props: map[string]bool{"items": true}}
	exprs, rootID, err := ir.ParseExpr(`items.filter(func(i){ return i.active })`, scope)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	items := vm.ArrayVal([]vm.Value{
		vm.ObjectVal(map[string]vm.Value{"active": vm.BoolVal(true), "name": vm.StringVal("alpha")}),
		vm.ObjectVal(map[string]vm.Value{"active": vm.BoolVal(false), "name": vm.StringVal("beta")}),
		vm.ObjectVal(map[string]vm.Value{"active": vm.BoolVal(true), "name": vm.StringVal("gamma")}),
	})

	machine := vm.NewVM(&program.Program{Name: "round-trip", Exprs: exprs}, map[string]vm.Value{"items": items})
	got := machine.Eval(rootID)
	if got.Type != program.TypeAny {
		// ArrayVal carries TypeAny; the underlying Items slice is what matters.
	}
	if len(got.Items) != 2 {
		t.Fatalf("expected 2 active items, got %d (%+v)", len(got.Items), got.Items)
	}
	if got.Items[0].IndexVal(vm.StringVal("name")).Str != "alpha" {
		t.Fatalf("first kept item should be alpha, got %+v", got.Items[0])
	}
	if got.Items[1].IndexVal(vm.StringVal("name")).Str != "gamma" {
		t.Fatalf("second kept item should be gamma, got %+v", got.Items[1])
	}
}

func TestClosureRoundTripMapProject(t *testing.T) {
	scope := &ir.ExprScope{Props: map[string]bool{"items": true}}
	exprs, rootID, err := ir.ParseExpr(`items.map(func(i){ i.name })`, scope)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	items := vm.ArrayVal([]vm.Value{
		vm.ObjectVal(map[string]vm.Value{"name": vm.StringVal("one")}),
		vm.ObjectVal(map[string]vm.Value{"name": vm.StringVal("two")}),
	})

	machine := vm.NewVM(&program.Program{Name: "round-trip", Exprs: exprs}, map[string]vm.Value{"items": items})
	got := machine.Eval(rootID)
	if len(got.Items) != 2 {
		t.Fatalf("expected 2 projected items, got %d", len(got.Items))
	}
	if got.Items[0].Str != "one" || got.Items[1].Str != "two" {
		t.Fatalf("expected [one, two], got %+v", got.Items)
	}
}

func TestClosureRoundTripChainedFilterMap(t *testing.T) {
	scope := &ir.ExprScope{Props: map[string]bool{"items": true}}
	exprs, rootID, err := ir.ParseExpr(`items.filter(func(i){ return i.active }).map(func(i){ return i.name })`, scope)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	items := vm.ArrayVal([]vm.Value{
		vm.ObjectVal(map[string]vm.Value{"active": vm.BoolVal(true), "name": vm.StringVal("alpha")}),
		vm.ObjectVal(map[string]vm.Value{"active": vm.BoolVal(false), "name": vm.StringVal("beta")}),
		vm.ObjectVal(map[string]vm.Value{"active": vm.BoolVal(true), "name": vm.StringVal("gamma")}),
	})

	machine := vm.NewVM(&program.Program{Name: "round-trip", Exprs: exprs}, map[string]vm.Value{"items": items})
	got := machine.Eval(rootID)
	if len(got.Items) != 2 {
		t.Fatalf("expected 2 names, got %d (%+v)", len(got.Items), got.Items)
	}
	if got.Items[0].Str != "alpha" || got.Items[1].Str != "gamma" {
		t.Fatalf("expected [alpha, gamma], got %+v", got.Items)
	}
}

func TestClosureRoundTripFindByID(t *testing.T) {
	scope := &ir.ExprScope{Props: map[string]bool{"items": true}}
	exprs, rootID, err := ir.ParseExpr(`items.find(func(i){ return i.id == 2 })`, scope)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	items := vm.ArrayVal([]vm.Value{
		vm.ObjectVal(map[string]vm.Value{"id": vm.IntVal(1), "name": vm.StringVal("alpha")}),
		vm.ObjectVal(map[string]vm.Value{"id": vm.IntVal(2), "name": vm.StringVal("beta")}),
		vm.ObjectVal(map[string]vm.Value{"id": vm.IntVal(3), "name": vm.StringVal("gamma")}),
	})

	machine := vm.NewVM(&program.Program{Name: "round-trip", Exprs: exprs}, map[string]vm.Value{"items": items})
	got := machine.Eval(rootID)
	if got.IndexVal(vm.StringVal("name")).Str != "beta" {
		t.Fatalf("expected beta, got %+v", got)
	}
}
