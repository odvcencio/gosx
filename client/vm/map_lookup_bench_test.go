// Slice Y.B — benchmarks for OpMapLookup. The Y.B handler runs once per
// comma-ok map index in lowered Go (`v, ok := m[k]`), so its cost is
// scaled by handler invocation count. Target: a single lookup against
// a 100-entry map should run in well under 1 µs so a draw handler that
// fires 60 times/second with a few dozen lookups never approaches
// frame budget.
//
// The earlier version of BenchmarkMapLookupHit pointed the lookup at an
// OpComposite, so every iteration re-materialized the whole 100-entry map and
// the benchmark reported about 14 µs — the cost of building the map, not of
// reading it. The lookup benchmarks below hold the map in a prop, which is what
// a real handler does, and a separate benchmark keeps the materialization cost
// visible under its own name.

package vm

import (
	"strconv"
	"testing"

	"m31labs.dev/gosx/island/program"
)

// BenchmarkMapLookupHit measures a present-key lookup against a 100-entry
// ObjectVal held in a prop. The loop times Eval plus the ObjectVal wrapper.
func BenchmarkMapLookupHit(b *testing.B) {
	machine, lookupID := newBenchLookupVM(100, "key_50")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got := machine.Eval(lookupID)
		if !got.Map()["ok"].Truth() {
			b.Fatalf("expected hit, got %+v", got)
		}
	}
}

// BenchmarkMapLookupMiss measures the absent-key path. It should be
// indistinguishable from the hit path — both branches do one map-Fields read
// plus the wrapper allocation.
func BenchmarkMapLookupMiss(b *testing.B) {
	machine, lookupID := newBenchLookupVM(100, "key_999")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got := machine.Eval(lookupID)
		if got.Map()["ok"].Truth() {
			b.Fatalf("expected miss, got %+v", got)
		}
	}
}

// BenchmarkMapCompositeThenLookup100 keeps the old shape under an honest name:
// it materializes a 100-entry map composite and then reads one key.
func BenchmarkMapCompositeThenLookup100(b *testing.B) {
	prog, lookupID := buildBenchLookup(100, "key_50")
	machine := NewVM(prog, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got := machine.Eval(lookupID)
		if !got.Map()["ok"].Truth() {
			b.Fatalf("expected hit, got %+v", got)
		}
	}
}

// TestMapLookupStaysCheap pins the allocation count of one lookup.
//
// The doc comment above states a sub-microsecond target. Wall time flakes on a
// shared machine, so the assertion pins the allocation count instead: the
// lookup must not touch the map's size. Two allocations cover the result
// ObjectVal — the Fields map plus its bucket — and nothing else.
func TestMapLookupStaysCheap(t *testing.T) {
	machine, lookupID := newBenchLookupVM(100, "key_50")
	// Warm the VM so first-call lazy work does not land in the measurement.
	machine.Eval(lookupID)

	const maxAllocs = 3
	got := testing.AllocsPerRun(200, func() {
		result := machine.Eval(lookupID)
		if !result.Map()["ok"].Truth() {
			t.Fatal("expected a hit")
		}
	})
	if got > maxAllocs {
		t.Errorf("OpMapLookup against a 100-entry map allocated %.0f objects, want at most %d; "+
			"a lookup must not scale with map size", got, maxAllocs)
	}
}

// newBenchLookupVM builds a VM whose single OpMapLookup reads a 100-entry map
// held in the "m" prop. This is the shape a lowered handler produces.
func newBenchLookupVM(n int, key string) (*VM, program.ExprID) {
	prog := &program.Program{}
	mapID := program.ExprID(len(prog.Exprs))
	prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpPropGet, Value: "m", Type: program.TypeAny})
	keyID := program.ExprID(len(prog.Exprs))
	prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpLitString, Value: key, Type: program.TypeString})
	lookupID := program.ExprID(len(prog.Exprs))
	prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpMapLookup, Operands: []program.ExprID{mapID, keyID}})

	fields := make(map[string]Value, n)
	for i := 0; i < n; i++ {
		fields["key_"+strconv.Itoa(i)] = IntVal(i)
	}
	return NewVM(prog, map[string]Value{"m": ObjectVal(fields)}), lookupID
}

// buildBenchLookup pre-builds a program whose single OpMapLookup
// targets an N-entry map composite literal. Returns the program and
// the ExprID of the lookup.
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
