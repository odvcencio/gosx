package signal

import (
	"sync"
	"sync/atomic"
)

// Batch coalesces multiple signal updates into a single notification pass.
//
// # Goroutine contract
//
// A batch belongs to the goroutine that opened it. While a Batch is open, no
// other goroutine may write a signal. A write from another goroutine would join
// the open queue and fire later, on the batching goroutine, instead of firing
// for its own caller.
//
// The batch depth is deliberately global rather than keyed by goroutine. Go
// offers no cheap goroutine identity: runtime.Stack is the only portable source,
// and it formats a traceback in 1.1 microseconds at shallow stack depth and 3 to
// 4 microseconds at the depth this code runs. Batch wraps every dispatched
// island event, so paying that per notification multiplied the whole event
// budget by 4.1, measured at 1,148 nanoseconds before and 4,654 nanoseconds
// after on BenchmarkDispatchIncrementCounter in client/vm.
//
// Moving the read to Batch entry does not help. The question a notification asks
// is "is the caller the owner", which needs the caller's identity, not the
// owner's. A table of open batches does not help either, because a comparison
// needs a key for the caller.
//
// So the contract is documented, and a best-effort detector reports a breach.
//
// # What the detector catches
//
// While a batch is open, batchNotify only appends to the queue. It never calls a
// subscriber, so it cannot nest on one goroutine. Two notifications in flight at
// once therefore prove that two goroutines are writing signals inside somebody's
// batch, and the package panics.
//
// The detector is best effort, in the same sense as the Go runtime's concurrent
// map write check. A write from a second goroutine that overlaps no other write
// goes undetected and still joins the open queue. Do not write signals from a
// second goroutine while a Batch is open.
//
// Dependency tracking does not make this trade. See track.go: an ordinary Get
// pays one atomic load, so the tracker stays keyed by goroutine identity.
var (
	batchMu    sync.Mutex
	batchDepth int
	batchQueue []Subscriber
	// openBatches lets a notification skip the mutex when nobody batches.
	openBatches atomic.Int32
	// notifyInFlight counts the notifications running inside an open batch.
	notifyInFlight atomic.Int32
)

// Batch runs fn and defers all subscriber notifications until fn returns.
// Nested batches are supported; notifications fire when the outermost batch
// exits. Notifications also fire when fn panics, so a panic cannot leave the
// package batching forever.
func Batch(fn func()) {
	openBatches.Add(1)
	batchMu.Lock()
	batchDepth++
	batchMu.Unlock()

	defer func() {
		batchMu.Lock()
		batchDepth--
		var queue []Subscriber
		if batchDepth <= 0 {
			batchDepth = 0
			queue = batchQueue
			batchQueue = nil
		}
		batchMu.Unlock()
		openBatches.Add(-1)

		for _, sub := range queue {
			sub()
		}
	}()

	fn()
}

// batchNotify calls subs now, or queues them when a batch is open.
func batchNotify(subs []Subscriber) {
	if len(subs) == 0 {
		return
	}
	if openBatches.Load() > 0 {
		if notifyInFlight.Add(1) > 1 {
			notifyInFlight.Add(-1)
			panic("signal: two goroutines wrote signals while a Batch was open; " +
				"a Batch belongs to the goroutine that opened it")
		}
		batchMu.Lock()
		batching := batchDepth > 0
		if batching {
			batchQueue = append(batchQueue, subs...)
		}
		batchMu.Unlock()
		// Release the count before any subscriber runs, so a subscriber that
		// writes a signal cannot look like a second goroutine.
		notifyInFlight.Add(-1)
		if batching {
			return
		}
	}

	for _, sub := range subs {
		sub()
	}
}
