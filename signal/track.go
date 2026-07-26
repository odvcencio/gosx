package signal

import (
	"sync"
	"sync/atomic"
)

// Dependency tracking for automatic signal dependency collection.
//
// A tracking window belongs to the goroutine that opened it. The tracker stack
// is therefore keyed by goroutine identity: a read on goroutine A never lands
// in a window that goroutine B opened.
var (
	trackerMu     sync.Mutex
	trackerStacks map[uint64][]*dependencyTracker
	openTrackers  atomic.Int32
)

type dependencyTracker struct {
	mu   sync.Mutex
	deps []Subscribable
	seen map[Subscribable]bool
}

// recordDependency adds s to the innermost tracking window of the calling
// goroutine. It returns at once when no window is open anywhere, so an ordinary
// Get pays one atomic load.
func recordDependency(s Subscribable) {
	if openTrackers.Load() == 0 {
		return
	}
	gid := goroutineID()

	trackerMu.Lock()
	stack := trackerStacks[gid]
	if len(stack) == 0 {
		trackerMu.Unlock()
		return
	}
	t := stack[len(stack)-1]
	trackerMu.Unlock()

	t.mu.Lock()
	if !t.seen[s] {
		t.seen[s] = true
		t.deps = append(t.deps, s)
	}
	t.mu.Unlock()
}

// trackDependencies runs fn and reports every signal that fn read on the
// calling goroutine. Nested windows are supported.
func trackDependencies[T any](fn func() T) (T, []Subscribable) {
	t := &dependencyTracker{
		seen: make(map[Subscribable]bool),
	}
	gid := goroutineID()

	openTrackers.Add(1)
	trackerMu.Lock()
	if trackerStacks == nil {
		trackerStacks = make(map[uint64][]*dependencyTracker)
	}
	trackerStacks[gid] = append(trackerStacks[gid], t)
	trackerMu.Unlock()

	// Pop the window even when fn panics. A leaked window would silently
	// attach every later read to a dead computed value.
	defer func() {
		trackerMu.Lock()
		stack := trackerStacks[gid]
		if n := len(stack); n > 0 {
			stack = stack[:n-1]
			if len(stack) == 0 {
				delete(trackerStacks, gid)
			} else {
				trackerStacks[gid] = stack
			}
		}
		trackerMu.Unlock()
		openTrackers.Add(-1)
	}()

	val := fn()
	return val, t.deps
}
