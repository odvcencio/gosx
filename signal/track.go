package signal

import "sync"

// Dependency tracking for automatic signal dependency collection.

var (
	trackerMu     sync.Mutex
	trackerStack  []*dependencyTracker
	activeTracker *dependencyTracker
)

type dependencyTracker struct {
	mu   sync.Mutex
	deps []Subscribable
	seen map[Subscribable]bool
}

func recordDependency(s Subscribable) {
	trackerMu.Lock()
	t := activeTracker
	trackerMu.Unlock()

	if t == nil {
		return
	}

	t.mu.Lock()
	if !t.seen[s] {
		t.seen[s] = true
		t.deps = append(t.deps, s)
	}
	t.mu.Unlock()
}

func trackDependencies[T any](fn func() T) (T, []Subscribable) {
	t := &dependencyTracker{
		seen: make(map[Subscribable]bool),
	}

	trackerMu.Lock()
	trackerStack = append(trackerStack, t)
	activeTracker = t
	trackerMu.Unlock()

	val := fn()

	trackerMu.Lock()
	trackerStack = trackerStack[:len(trackerStack)-1]
	if len(trackerStack) > 0 {
		activeTracker = trackerStack[len(trackerStack)-1]
	} else {
		activeTracker = nil
	}
	trackerMu.Unlock()

	return val, t.deps
}
