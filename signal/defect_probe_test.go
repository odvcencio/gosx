package signal

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestComputedGetInsideSubscriberDoesNotDeadlock proves that Computed.Get must
// not hold its own mutex while it notifies subscribers. A subscriber that reads
// the computed value is the most ordinary usage there is.
func TestComputedGetInsideSubscriberDoesNotDeadlock(t *testing.T) {
	base := New(1)
	doubled := Derive(func() int { return base.Get() * 2 })
	defer doubled.Stop()

	var seen atomic.Int64
	doubled.Subscribe(func() { seen.Store(int64(doubled.Get())) })

	done := make(chan struct{})
	go func() {
		defer close(done)
		base.Set(21)
		_ = doubled.Get()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Set deadlocked: a subscriber that calls Computed.Get re-entered c.mu")
	}
	if got := seen.Load(); got != 42 {
		t.Fatalf("subscriber saw %d, want 42", got)
	}
}

// TestComputedNotifiesOncePerDependencyChange proves that one base Set must
// produce exactly one subscriber call per computed value.
func TestComputedNotifiesOncePerDependencyChange(t *testing.T) {
	base := New(1)
	doubled := Derive(func() int { return base.Get() * 2 })
	defer doubled.Stop()

	var calls atomic.Int32
	doubled.Subscribe(func() { calls.Add(1) })

	base.Set(2)
	if got := doubled.Get(); got != 4 {
		t.Fatalf("computed = %d, want 4", got)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("got %d subscriber calls for one base Set, want 1", got)
	}
}

// TestComputedSwitchesDependenciesAndStopFreezesTheLastValue pins the complete
// computed lifecycle current main exposes to island state: a recompute replaces
// stale dependency subscriptions, and Stop detaches the active set without
// retroactively changing the last published value.
func TestComputedSwitchesDependenciesAndStopFreezesTheLastValue(t *testing.T) {
	useLeft := New(true)
	left := New(1)
	right := New(10)
	selected := Derive(func() int {
		if useLeft.Get() {
			return left.Get()
		}
		return right.Get()
	})

	var calls atomic.Int32
	selected.Subscribe(func() { calls.Add(1) })

	left.Set(2)
	right.Set(20)
	if got := selected.Get(); got != 2 {
		t.Fatalf("selected left value = %d, want 2", got)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("active/inactive writes produced %d notifications, want 1", got)
	}

	useLeft.Set(false)
	left.Set(3)
	right.Set(30)
	if got := selected.Get(); got != 30 {
		t.Fatalf("selected right value = %d, want 30", got)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("dependency switch produced %d notifications, want 3", got)
	}

	selected.Stop()
	right.Set(40)
	if got := selected.Get(); got != 30 {
		t.Fatalf("stopped computed value = %d, want frozen value 30", got)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("stopped computed received %d notifications, want 3", got)
	}
}

// TestSignalSuppressesIdenticalValueByDefault proves that a signal over a
// comparable type must not notify when the value does not change.
func TestSignalSuppressesIdenticalValueByDefault(t *testing.T) {
	s := New(1)
	var calls atomic.Int32
	s.Subscribe(func() { calls.Add(1) })

	s.Set(1)
	s.Set(1)
	s.Set(1)
	if got := calls.Load(); got != 0 {
		t.Fatalf("got %d notifications for three identical Set calls, want 0", got)
	}

	s.Set(2)
	if got := calls.Load(); got != 1 {
		t.Fatalf("got %d notifications after a real change, want 1", got)
	}
}

// TestSignalUpdateSuppressesIdenticalValueByDefault covers the Update path.
func TestSignalUpdateSuppressesIdenticalValueByDefault(t *testing.T) {
	s := New("stable")
	var calls atomic.Int32
	s.Subscribe(func() { calls.Add(1) })

	s.Update(func(v string) string { return v })
	if got := calls.Load(); got != 0 {
		t.Fatalf("got %d notifications for an identity Update, want 0", got)
	}
}

// TestBatchDetectsConcurrentWriteFromAnotherGoroutine proves that the package
// reports a breach of the Batch goroutine contract instead of silently swallowing
// the other goroutine's notification.
//
// The test drives the detector directly. Two real goroutines would have to
// overlap inside a few nanoseconds of queue append, which no test can force, so
// the probe holds the in-flight count that a concurrent notification would hold.
func TestBatchDetectsConcurrentWriteFromAnotherGoroutine(t *testing.T) {
	s := New(0)
	s.Subscribe(func() {})

	inBatch := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		Batch(func() {
			close(inBatch)
			<-release
		})
	}()
	defer func() {
		close(release)
		<-done
	}()
	<-inBatch

	// Stand in for a notification that another goroutine is running right now.
	notifyInFlight.Add(1)
	defer notifyInFlight.Add(-1)

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		s.Set(1)
	}()
	if recovered == nil {
		t.Fatal("a concurrent write during a Batch went undetected")
	}
	message, ok := recovered.(string)
	if !ok || !strings.Contains(message, "Batch belongs to the goroutine") {
		t.Fatalf("panic value = %v, want the Batch contract message", recovered)
	}
}

// TestBatchCoalescesOwnGoroutineWrites locks the behaviour every real caller
// depends on: many writes inside one Batch produce one notification pass, and
// the notifications fire when the batch exits.
func TestBatchCoalescesOwnGoroutineWrites(t *testing.T) {
	s := New(0)
	var calls atomic.Int32
	s.Subscribe(func() { calls.Add(1) })

	Batch(func() {
		s.Set(1)
		s.Set(2)
		s.Set(3)
		if got := calls.Load(); got != 0 {
			t.Fatalf("got %d notifications inside the batch, want 0", got)
		}
	})
	if got := calls.Load(); got != 3 {
		t.Fatalf("got %d notifications after the batch, want 3", got)
	}
	if s.Get() != 3 {
		t.Fatalf("value = %d, want 3", s.Get())
	}
}

// TestBatchNotifiesWithNoBatchOpen proves the fast path still notifies.
func TestBatchNotifiesWithNoBatchOpen(t *testing.T) {
	s := New(0)
	var calls atomic.Int32
	s.Subscribe(func() { calls.Add(1) })
	s.Set(1)
	if got := calls.Load(); got != 1 {
		t.Fatalf("got %d notifications with no batch open, want 1", got)
	}
}

// TestTrackingDoesNotCaptureOtherGoroutineReads proves that the dependency
// tracker must be scoped to the goroutine that opened the tracking window.
func TestTrackingDoesNotCaptureOtherGoroutineReads(t *testing.T) {
	tracked := New(1)
	foreign := New(100)

	inWindow := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var computed *Computed[int]
	go func() {
		defer close(done)
		computed = Derive(func() int {
			close(inWindow)
			<-release
			return tracked.Get()
		})
	}()

	<-inWindow
	_ = foreign.Get()
	close(release)
	<-done
	defer computed.Stop()

	var calls atomic.Int32
	computed.Subscribe(func() { calls.Add(1) })
	foreign.Set(200)
	if got := calls.Load(); got != 0 {
		t.Fatalf("computed got %d notifications from a signal it never read, want 0", got)
	}
}

// TestBatchReleasesDepthOnPanic proves that a panic inside Batch must not leave
// the package permanently batching.
func TestBatchReleasesDepthOnPanic(t *testing.T) {
	func() {
		defer func() { _ = recover() }()
		Batch(func() { panic("boom") })
	}()

	s := New(0)
	var calls atomic.Int32
	s.Subscribe(func() { calls.Add(1) })
	s.Set(1)
	if got := calls.Load(); got != 1 {
		t.Fatalf("got %d notifications after a panicking Batch, want 1", got)
	}
}

// TestConcurrentSetsWithoutBatchNeverPanic guards the detector against false
// alarms. Many goroutines writing signals with no batch open is ordinary use and
// must stay silent.
func TestConcurrentSetsWithoutBatchNeverPanic(t *testing.T) {
	const goroutines = 16
	signals := make([]*Signal[int], goroutines)
	var calls atomic.Int64
	for i := range signals {
		signals[i] = New(0)
		signals[i].Subscribe(func() { calls.Add(1) })
	}

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 1; i <= 500; i++ {
				signals[g].Set(i)
				_ = signals[(g+1)%goroutines].Get()
			}
		}(g)
	}
	wg.Wait()
	if got := calls.Load(); got != goroutines*500 {
		t.Fatalf("got %d notifications, want %d", got, goroutines*500)
	}
}

// TestNestedBatchStillCoalescesToTheOutermostExit locks nested batch semantics.
func TestNestedBatchStillCoalescesToTheOutermostExit(t *testing.T) {
	s := New(0)
	var calls atomic.Int32
	s.Subscribe(func() { calls.Add(1) })

	Batch(func() {
		s.Set(1)
		Batch(func() {
			s.Set(2)
		})
		if got := calls.Load(); got != 0 {
			t.Fatalf("inner batch exit fired %d notifications, want 0", got)
		}
		s.Set(3)
	})
	if got := calls.Load(); got != 3 {
		t.Fatalf("got %d notifications after the outer batch, want 3", got)
	}
}
