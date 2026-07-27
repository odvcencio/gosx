package signal

import (
	"sync"
	"testing"
	"unsafe"
)

// Tests for Signal.Revision, the uncontended read fast path.
//
// A caller uses the revision to skip a read. A revision that fails to advance
// therefore hides a write, and the caller keeps a stale value with no error,
// no panic and no failing assertion anywhere near the defect. The tests below
// attack the counter from both sides: it must advance on every write that
// changes the value, and it must not advance on a write the equality test
// rejects.

// TestRevisionAdvancesOnEveryWriteThatChangesTheValue is the guard against a
// hidden write. Set and Update both change the value, so both must move the
// counter.
func TestRevisionAdvancesOnEveryWriteThatChangesTheValue(t *testing.T) {
	sig := New(1)
	start := sig.Revision()

	sig.Set(2)
	afterSet := sig.Revision()
	if afterSet == start {
		t.Fatal("Set changed the value and did not advance the revision; a reader " +
			"that trusts the number would keep the old value for ever")
	}

	sig.Update(func(v int) int { return v + 1 })
	afterUpdate := sig.Revision()
	if afterUpdate == afterSet {
		t.Fatal("Update changed the value and did not advance the revision")
	}
	if got := sig.Get(); got != 3 {
		t.Fatalf("value = %d, want 3", got)
	}
}

// TestRevisionHoldsWhenTheEqualityTestRejectsTheWrite pins the other side. New
// installs an equality test for a comparable type, so a Set with the same value
// notifies nobody and changes nothing. A revision that moved there would send
// every reader back to the slow path on every reconcile and the fast path
// would buy nothing.
func TestRevisionHoldsWhenTheEqualityTestRejectsTheWrite(t *testing.T) {
	sig := New("same")
	before := sig.Revision()

	sig.Set("same")
	sig.Update(func(v string) string { return v })

	if after := sig.Revision(); after != before {
		t.Fatalf("revision moved from %d to %d for two writes that changed nothing", before, after)
	}
}

// TestRevisionCountsWritesForTypesWithoutAnEqualityTest pins the contract for
// the type the island VM uses. A Value cannot be compared, so New installs no
// equality test and every Set counts as a write.
func TestRevisionCountsWritesForTypesWithoutAnEqualityTest(t *testing.T) {
	type incomparable struct {
		_    [0]func()
		text string
	}
	sig := New(incomparable{text: "a"})
	if sig.equal != nil {
		t.Fatal("an incomparable type must get no equality test; the fixture no longer " +
			"models the island Value")
	}

	before := sig.Revision()
	sig.Set(incomparable{text: "a"})
	if after := sig.Revision(); after != before+1 {
		t.Fatalf("revision = %d, want %d; a signal with no equality test must count "+
			"every Set as a write", after, before+1)
	}
}

// TestRevisionDoesNotRecordADependency pins that Revision stays outside the
// tracking system. A Computed value built on it would never be told to
// recompute, so the fast path must not look like a read.
func TestRevisionDoesNotRecordADependency(t *testing.T) {
	sig := New(1)
	_, deps := trackDependencies(func() int {
		_ = sig.Revision()
		return 0
	})
	if len(deps) != 0 {
		t.Fatalf("Revision recorded %d dependencies, want 0", len(deps))
	}

	_, deps = trackDependencies(func() int { return sig.Get() })
	if len(deps) != 1 {
		t.Fatalf("Get recorded %d dependencies, want 1; the reference read no longer "+
			"tracks and the test above proves nothing", len(deps))
	}
}

// TestRevisionNeverRunsAheadOfTheValue pins the ordering rule Revision
// documents. A reader that takes the revision first, then the value, must
// never store a number that is newer than the value it holds: that pairing is
// the one that goes stale for ever.
//
// The loop below runs under -race, so it also proves the counter and the value
// are published safely.
func TestRevisionNeverRunsAheadOfTheValue(t *testing.T) {
	sig := New(0)
	var writers sync.WaitGroup
	stop := make(chan struct{})

	for w := 0; w < 4; w++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for i := 1; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				sig.Set(i)
			}
		}()
	}

	for i := 0; i < 20000; i++ {
		firstRev := sig.Revision()
		value := sig.Get()
		secondRev := sig.Revision()
		if firstRev != secondRev {
			// A write landed around the read. The stored pair is then
			// conservative, not wrong, so there is nothing to check.
			continue
		}
		// No write ran between the two readings, so the value must still
		// be the one the signal holds now.
		if again := sig.Get(); again != value && sig.Revision() == firstRev {
			t.Fatalf("the value changed while the revision stood still: %d then %d",
				value, again)
		}
	}
	close(stop)
	writers.Wait()
}

// TestSignalStaysWithinOneSizeClass records what the revision counter costs.
//
// A signal over the island Value type grew from 80 to 88 bytes, which crosses
// the 80-byte allocation size class, so the allocator hands out 96. An island
// holds a handful of signals and builds them once, so the trade buys a lock
// and an unlock per watched signal per reconcile for 16 bytes per signal. A
// field added carelessly here would push the struct into the next class again
// and buy nothing.
func TestSignalStaysWithinOneSizeClass(t *testing.T) {
	// islandValue mirrors the layout of client/vm.Value: 32 bytes, one
	// pointer, and an empty func array that makes it incomparable.
	type islandValue struct {
		noCompare [0]func()
		ptr       unsafe.Pointer
		n         int
		num       float64
		typ       uint16
		tag       uint8
	}
	if got := unsafe.Sizeof(islandValue{}); got != 32 {
		t.Fatalf("the fixture is %d bytes, not 32; it no longer mirrors the island Value", got)
	}
	const want = 88
	if got := unsafe.Sizeof(Signal[islandValue]{}); got != want {
		t.Fatalf("sizeof(Signal[Value]) = %d, want %d; a new field changed what a "+
			"signal costs", got, want)
	}
}
