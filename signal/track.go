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
	trackerStacks map[uint64]*dependencyTracker
	openTrackers  atomic.Int32
	trackerPool   = sync.Pool{New: func() any { return new(dependencyTracker) }}
)

type dependencyTracker struct {
	mu     sync.Mutex
	parent *dependencyTracker
	deps   []dependency
	seen   map[Subscribable]struct{}
}

type dependency struct {
	source      Subscribable
	revision    uint64
	unsubscribe func()
}

// recordDependency adds s to the innermost tracking window of the calling
// goroutine. It returns at once when no window is open anywhere, so an ordinary
// Get pays one atomic load.
func recordDependency(s Subscribable, revision uint64) {
	if openTrackers.Load() == 0 {
		return
	}
	gid := goroutineID()

	trackerMu.Lock()
	t := trackerStacks[gid]
	trackerMu.Unlock()
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	// Most derived values read only a few sources. A short scan avoids a
	// hash table for those windows; larger dependency sets keep O(1) lookup.
	if len(t.deps) < 8 {
		for _, dep := range t.deps {
			if dep.source == s {
				return
			}
		}
	} else {
		if len(t.seen) == 0 {
			if t.seen == nil {
				t.seen = make(map[Subscribable]struct{})
			}
			for _, dep := range t.deps {
				t.seen[dep.source] = struct{}{}
			}
		}
		if _, found := t.seen[s]; found {
			return
		}
		t.seen[s] = struct{}{}
	}
	t.deps = append(t.deps, dependency{source: s, revision: revision})
}

// trackDependencies runs fn and reports every signal that fn read on the
// calling goroutine. Nested windows are supported. The returned dependencies
// belong to the caller; only cleared tracking scratch is shared through a pool.
func trackDependencies[T any](fn func() T) (T, []dependency) {
	t := trackerPool.Get().(*dependencyTracker)
	gid := goroutineID()

	openTrackers.Add(1)
	trackerMu.Lock()
	if trackerStacks == nil {
		trackerStacks = make(map[uint64]*dependencyTracker)
	}
	t.parent = trackerStacks[gid]
	trackerStacks[gid] = t
	trackerMu.Unlock()

	// Pop and clear the window even when fn panics. Neither subsequent reads
	// nor pooled scratch may retain a dependency of an abandoned computation.
	defer func() {
		trackerMu.Lock()
		if t.parent == nil {
			delete(trackerStacks, gid)
		} else {
			trackerStacks[gid] = t.parent
		}
		trackerMu.Unlock()
		openTrackers.Add(-1)
		t.mu.Lock()
		t.parent = nil
		if cap(t.deps) > 128 {
			// An unusually large computation must not permanently enlarge
			// the scratch retained by ordinary, short tracking windows.
			t.deps = nil
			t.seen = nil
		} else {
			clear(t.deps)
			t.deps = t.deps[:0]
			clear(t.seen)
		}
		t.mu.Unlock()
		trackerPool.Put(t)
	}()

	val := fn()
	t.mu.Lock()
	deps := append([]dependency(nil), t.deps...)
	t.mu.Unlock()
	return val, deps
}
