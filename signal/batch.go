package signal

import "sync"

// Batch coalesces multiple signal updates into a single notification pass.

var (
	batchMu    sync.Mutex
	batchDepth int
	batchQueue []Subscriber
)

// Batch runs fn and defers all subscriber notifications until fn returns.
// Nested batches are supported; notifications fire when the outermost batch exits.
func Batch(fn func()) {
	batchMu.Lock()
	batchDepth++
	batchMu.Unlock()

	fn()

	batchMu.Lock()
	batchDepth--
	if batchDepth == 0 {
		queue := batchQueue
		batchQueue = nil
		batchMu.Unlock()
		for _, sub := range queue {
			sub()
		}
		return
	}
	batchMu.Unlock()
}

func batchNotify(subs []Subscriber) {
	batchMu.Lock()
	if batchDepth > 0 {
		batchQueue = append(batchQueue, subs...)
		batchMu.Unlock()
		return
	}
	batchMu.Unlock()

	for _, sub := range subs {
		sub()
	}
}
