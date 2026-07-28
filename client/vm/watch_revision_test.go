package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// Tests for the revision fast path in islandReuse.refresh.
//
// refresh used to read every watched signal on every reconcile, and each read
// takes a mutex inside Signal.Get. The fast path asks the signal's write
// counter first and skips the read when nothing wrote it.
//
// The failure mode is a missed write: the island keeps rendering the previous
// value and nothing reports an error. FuzzIslandReuseMatchesFullEval is the
// broad guard, because it compares every reconcile against a full evaluation.
// The tests here pin the two ends the fuzz target cannot reach directly: that
// the fast path fires at all, and that a write from outside a handler still
// invalidates it.

// TestRefreshSkipsTheReadWhenNothingWrote pins that the fast path fires. If it
// stopped firing, every reconcile would go back to a lock and an unlock per
// watched signal and no test would notice.
func TestRefreshSkipsTheReadWhenNothingWrote(t *testing.T) {
	island := NewIsland(program.CounterProgram(), `{}`)
	island.Reconcile() // builds the analysis and primes the snapshot
	island.Reconcile()

	reuse := island.reuse
	if reuse == nil {
		t.Fatal("the counter program should get a reuse plan")
	}
	checked := 0
	for i, key := range reuse.plan.watch {
		if key.kind != watchSignal || reuse.watchSigs[i] == nil {
			continue
		}
		checked++
		rev, revValid := reuse.signalRevision(i)
		if !reuse.unwrittenSince(i, rev, revValid) {
			t.Errorf("watched signal %q took the slow path although nothing wrote it; "+
				"the refresh pays a mutex per signal per reconcile again", key.name)
		}
	}
	if checked == 0 {
		t.Fatal("the counter watches no signal, so the test proves nothing")
	}
}

// TestRefreshSeesAWriteFromOutsideAHandler is the guard the fast path needs.
// The counter's own handler is not the only writer: a shared signal is written
// by the bridge, by a host receiver and by another island. All of them go
// through Set, so all of them must move the revision.
func TestRefreshSeesAWriteFromOutsideAHandler(t *testing.T) {
	island := NewIsland(program.CounterProgram(), `{}`)
	island.Reconcile()
	island.Reconcile()

	island.vm.signals["count"].Set(IntVal(42))
	island.Reconcile()

	if got := counterDisplayText(island.prev); got != "42" {
		t.Fatalf("display after a write from outside a handler = %q, want %q; the "+
			"refresh trusted a revision that had already moved", got, "42")
	}
	if !treesEqual(island.prev, fullEvalTree(island)) {
		t.Fatal("the tree differs from a full evaluation")
	}
}

// TestRefreshDropsRevisionsWhenTheSignalIsReplaced pins the invalidation.
//
// A revision only means something against the signal it came from. Installing
// a shared signal swaps the pointer, and the fresh signal's counter starts at
// 0. An island that has taken no write yet stored 0 as well, so the two numbers
// collide on the very first install. That is the common case, not a corner: a
// bridge builds the shared signal with New and hands it over before any event.
func TestRefreshDropsRevisionsWhenTheSignalIsReplaced(t *testing.T) {
	island := NewIsland(program.CounterProgram(), `{}`)
	island.Reconcile() // builds the analysis and stores revision 0
	island.Reconcile()

	shared := signal.New(IntVal(7))
	if shared.Revision() != 0 {
		t.Fatal("a fresh signal no longer starts at revision 0; the collision this " +
			"test is built on is gone")
	}
	island.SetSharedSignal("count", shared)
	island.Reconcile()

	if got := counterDisplayText(island.prev); got != "7" {
		t.Fatalf("display after a shared signal was installed = %q, want %q; the "+
			"island trusted a revision it read from the retired signal", got, "7")
	}

	shared.Set(IntVal(9))
	island.Reconcile()
	if got := counterDisplayText(island.prev); got != "9" {
		t.Fatalf("display after the shared signal changed = %q, want %q", got, "9")
	}
	if !treesEqual(island.prev, fullEvalTree(island)) {
		t.Fatal("the tree differs from a full evaluation")
	}
}

// TestRefreshIgnoresAWriteThatRestoredTheSameValue pins that the revision is a
// hint, not the answer.
//
// A Value cannot be compared, so signal.New installs no equality test and every
// Set counts as a write. A handler that writes back the value a signal already
// held therefore moves the revision. The refresh must still compare the payload
// and report no change, or a form that revalidates on every keystroke would
// re-evaluate its whole subtree for nothing.
func TestRefreshIgnoresAWriteThatRestoredTheSameValue(t *testing.T) {
	island := NewIsland(program.CounterProgram(), `{}`)
	// Two increments first, so the stored revision is not 0. A test that
	// primed on an untouched island would compare 0 against 0 and pass even
	// if the payload comparison started counting the revision.
	island.Dispatch("increment", "{}")
	island.Dispatch("increment", "{}")

	sig := island.vm.signals["count"]
	if sig.Revision() == 0 {
		t.Fatal("the increments did not move the revision, so the stored number is 0 " +
			"and the test proves nothing")
	}
	before := sig.Revision()
	sig.Set(sig.Get())
	if sig.Revision() == before {
		t.Fatal("the write did not move the revision; the fixture no longer models " +
			"a signal without an equality test")
	}

	if dirty := island.reuse.refresh(island.vm); dirty != 0 {
		t.Fatalf("write set = %b, want 0; a write that restored the same value must "+
			"not re-evaluate the subtrees that read it", dirty)
	}
}

// TestRefreshReReadsACompositeSignal pins the one value shape the revision
// cannot cover. OpFieldSet writes through the map the signal holds without any
// Set at all, so the counter stands still while the content changes. sampleValue
// marks a composite unstable, and the fast path refuses an unstable sample.
func TestRefreshReReadsACompositeSignal(t *testing.T) {
	island := NewIsland(objectSignalProgram(), `{}`)
	fields := map[string]Value{"label": StringVal("first")}
	island.vm.SetSignal("state", signal.New(ObjectVal(fields)))
	island.Reconcile()
	island.Reconcile()

	reuse := island.reuse
	if reuse == nil {
		t.Fatal("the object program should get a reuse plan")
	}
	for i, key := range reuse.plan.watch {
		if key.kind != watchSignal || reuse.watchSigs[i] == nil {
			continue
		}
		rev, revValid := reuse.signalRevision(i)
		if reuse.unwrittenSince(i, rev, revValid) {
			t.Fatalf("watched signal %q took the fast path although it holds a "+
				"composite; an in-place field write would go unseen", key.name)
		}
	}

	// Mutate through the shared map, exactly as OpFieldSet does.
	fields["label"] = StringVal("second")
	island.Reconcile()
	if got := island.prev.Nodes[1].Text; got != "second" {
		t.Fatalf("text after an in-place field write = %q, want %q", got, "second")
	}
}
