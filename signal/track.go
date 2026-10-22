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
	spareTrackers *dependencyTracker
	spareCount    int
)

type dependencyTracker struct {
	parent *dependencyTracker
	deps   []dependency
	seen   map[Subscribable]bool
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
	defer trackerMu.Unlock()
	t := trackerStacks[gid]
	if t == nil {
		return
	}

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
				t.seen = make(map[Subscribable]bool)
			}
			for _, dep := range t.deps {
				t.seen[dep.source] = true
			}
		}
		if t.seen[s] {
			return
		}
		t.seen[s] = true
	}
	t.deps = append(t.deps, dependency{source: s, revision: revision})
}

// trackDependencies runs fn and reports every signal that fn read on the
// calling goroutine. Nested windows are supported. The returned dependencies
// belong to the caller; only cleared tracking scratch is cached for reuse.
func trackDependencies[T any](fn func() T) (T, []dependency) {
	gid := goroutineID()
	t := pushTracker(gid)
	defer popTracker(gid, t)
	val := fn()
	return val, snapshotDependencies(t)
}

// Keep scratch management outside the generic evaluator so each value type
// shares the same implementation in the browser runtime.
func pushTracker(gid uint64) *dependencyTracker {
	openTrackers.Add(1)
	trackerMu.Lock()
	defer trackerMu.Unlock()
	t := spareTrackers
	if t == nil {
		t = new(dependencyTracker)
	} else {
		spareTrackers = t.parent
		spareCount--
	}
	if trackerStacks == nil {
		trackerStacks = make(map[uint64]*dependencyTracker)
	}
	t.parent = trackerStacks[gid]
	trackerStacks[gid] = t
	return t
}

func snapshotDependencies(t *dependencyTracker) []dependency {
	trackerMu.Lock()
	defer trackerMu.Unlock()
	return append([]dependency(nil), t.deps...)
}

// Pop and clear the window even when fn panics. Neither subsequent reads
// nor cached scratch may retain a dependency of an abandoned computation.
func popTracker(gid uint64, t *dependencyTracker) {
	trackerMu.Lock()
	if t.parent == nil {
		delete(trackerStacks, gid)
	} else {
		trackerStacks[gid] = t.parent
	}
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
	// A typed cache avoids interface boxing in the small browser runtime.
	// Bound retained windows as well as each window's scratch capacity.
	if spareCount < 8 {
		t.parent = spareTrackers
		spareTrackers = t
		spareCount++
	}
	trackerMu.Unlock()
	openTrackers.Add(-1)
}
