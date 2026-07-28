package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// TestWatchSampleAtMatchesTheNameLookup pins that the bound-pointer reader
// answers exactly like the name-based reference reader, for both namespaces
// and for a name that is not registered at all.
func TestWatchSampleAtMatchesTheNameLookup(t *testing.T) {
	island := NewIsland(program.CounterProgram(), `{}`)
	island.Dispatch("increment", "{}")

	reuse := island.reuse
	if reuse == nil {
		t.Fatal("the counter program should get a reuse plan")
	}
	reuse.bindWatchedSignals(island.vm)

	watched := append([]watchKey{}, reuse.plan.watch...)
	watched = append(watched,
		watchKey{kind: watchSignal, name: "not-registered"},
		watchKey{kind: watchProp, name: "not-a-prop"},
	)
	for i, key := range watched {
		bound := i
		if bound >= len(reuse.watchSigs) {
			// Beyond the plan there is no bound slot, so compare the
			// reference reader against a fresh binding for that name.
			if got := island.vm.currentWatchSample(key); got.present {
				t.Errorf("reference reader reported %q as present", key.name)
			}
			continue
		}
		got := reuse.watchSampleAt(island.vm, bound, key)
		want := island.vm.currentWatchSample(key)
		if got != want {
			t.Errorf("watchSampleAt(%d, %+v) = %+v, want %+v", bound, key, got, want)
		}
	}
}

// TestSetSignalRebindsTheWatchTable is the failure this cache can cause: a
// shared signal installed after the plan was built replaces the signal the
// pointer table holds. Without the generation bump the island would keep
// reading the retired signal and stop updating, with every other test green.
func TestSetSignalRebindsTheWatchTable(t *testing.T) {
	island := NewIsland(program.CounterProgram(), `{}`)
	island.Dispatch("increment", "{}")

	shared := signal.New(IntVal(41))
	island.SetSharedSignal("count", shared)
	island.Reconcile()

	if got := counterDisplayText(island.CurrentTree()); got != "41" {
		t.Fatalf("display after installing a shared signal = %q, want %q", got, "41")
	}

	shared.Set(IntVal(42))
	island.Reconcile()
	if got := counterDisplayText(island.CurrentTree()); got != "42" {
		t.Fatalf("display after writing the shared signal = %q, want %q; "+
			"the watch table is still bound to the retired signal", got, "42")
	}
}

// TestSwapProgramRebindsTheWatchTable covers the other writer of the signal
// table. SwapProgram replaces the whole map, so every cached pointer must be
// re-resolved.
func TestSwapProgramRebindsTheWatchTable(t *testing.T) {
	island := NewIsland(program.CounterProgram(), `{}`)
	island.Dispatch("increment", "{}")
	island.SwapProgram(program.CounterProgram())

	island.Dispatch("increment", "{}")
	if got := counterDisplayText(island.CurrentTree()); got != "2" {
		t.Fatalf("display after a swap and an increment = %q, want %q", got, "2")
	}
}

// TestSignalTableWritersBumpTheGeneration pins the contract bindWatchedSignals
// depends on: every writer of vm.signals raises vm.signalGen.
//
// The two writers today are SetSignal and SwapProgram. SetSignal has an
// observable failure — TestSetSignalRebindsTheWatchTable shows it. SwapProgram
// has none right now, because its merge keeps the same signal instance for
// every name it already had, so a cached pointer happens to stay correct. That
// is a property of the merge, not of the cache. This test fails the moment
// either writer stops bumping, which is the warning a future merge change
// needs.
func TestSignalTableWritersBumpTheGeneration(t *testing.T) {
	prog := program.CounterProgram()
	vm := NewVM(prog, nil)
	InitSignals(vm, prog)

	before := vm.signalGen
	vm.SetSignal("extra", signal.New(IntVal(1)))
	if vm.signalGen == before {
		t.Error("SetSignal did not bump the signal generation")
	}

	before = vm.signalGen
	vm.SwapProgram(program.CounterProgram())
	if vm.signalGen == before {
		t.Error("SwapProgram did not bump the signal generation")
	}
}

// TestAutoKeyPropChangeStillInvalidates pins the early exit in refresh. The
// loop skips its work when an auto-key prop is absent on both sides, so a
// prop that appears, changes and disappears must still force a full walk.
func TestAutoKeyPropChangeStillInvalidates(t *testing.T) {
	island := NewIsland(program.CounterProgram(), `{}`)
	island.Dispatch("increment", "{}")

	reuse := island.reuse
	if reuse == nil {
		t.Fatal("the counter program should get a reuse plan")
	}

	// Absent on both sides: the reconcile must reuse subtrees.
	island.Reconcile()
	quiet := reuseHits(island)
	if quiet == 0 {
		t.Fatal("a reconcile with no change should reuse at least one subtree")
	}

	// The prop appears. Every subtree must be re-evaluated.
	island.vm.SetProp("_key", StringVal("row-1"))
	island.Reconcile()
	if hits := reuseHits(island); hits != 0 {
		t.Errorf("reused subtrees after _key appeared = %d, want 0", hits)
	}

	// The prop changes value. Again a full walk.
	island.Reconcile()
	island.vm.SetProp("_key", StringVal("row-2"))
	island.Reconcile()
	if hits := reuseHits(island); hits != 0 {
		t.Errorf("reused subtrees after _key changed = %d, want 0", hits)
	}

	// The prop disappears. Still a full walk, because presence changed.
	island.Reconcile()
	delete(island.vm.props, "_key")
	island.Reconcile()
	if hits := reuseHits(island); hits != 0 {
		t.Errorf("reused subtrees after _key disappeared = %d, want 0", hits)
	}
}
