// Slice Y.B — benchmark for OpMapLookup. The Y.B handler runs once per
// comma-ok map index in lowered Go (`v, ok := m[k]`), so its cost is
// scaled by handler invocation count. Target: a single lookup against
// a 100-entry map should run in well under 1 µs so a draw handler that
// fires 60 times/second with a few dozen lookups never approaches
// frame budget.

package vm

import (
	"strconv"
	"testing"

	"m31labs.dev/gosx/island/program"
)

// BenchmarkMapLookupHit measures the steady-state cost of a present-key
// lookup against a 100-entry ObjectVal-backed map. The setup pre-builds
// the lookup program once so the bench loop measures only Eval + the
// ObjectVal wrapper allocation.
func BenchmarkMapLookupHit(b *testing.B) {
	prog, lookupID := buildBenchLookup(100, "key_50")
	vm := NewVM(prog, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got := vm.Eval(lookupID)
		if got.Fields["ok"].Bool != true {
			b.Fatalf("expected hit, got %+v", got)
		}
	}
}

// BenchmarkMapLookupMiss measures the absent-key path. Should be
// indistinguishable from the hit path — both branches do one
// map-Fields read plus the wrapper allocation.
func BenchmarkMapLookupMiss(b *testing.B) {
	prog, lookupID := buildBenchLookup(100, "key_999")
	vm := NewVM(prog, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got := vm.Eval(lookupID)
		if got.Fields["ok"].Bool != false {
			b.Fatalf("expected miss, got %+v", got)
		}
	}
}

// buildBenchLookup pre-builds a program whose single OpMapLookup
// targets an N-entry map composite literal. Returns the program and
// the ExprID of the lookup so the bench loop only times Eval.
func buildBenchLookup(n int, key string) (*program.Program, program.ExprID) {
	prog := &program.Program{}
	var entryIDs []program.ExprID
	for i := 0; i < n; i++ {
		k := program.ExprID(len(prog.Exprs))
		prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpLitString, Value: "key_" + strconv.Itoa(i), Type: program.TypeString})
		v := program.ExprID(len(prog.Exprs))
		prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpLitInt, Value: strconv.Itoa(i), Type: program.TypeInt})
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
