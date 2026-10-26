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
	openTrackers  atomic.Int32
	spareTrackers *dependencyTracker
	spareCount    int
)

type dependencyTracker struct {
	gid    uint64
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
	count := openTrackers.Load()
	if count == 0 {
		return
	}
	if count == 1 && dependencyAlreadyRecorded(s) {
		return
	}
	gid := goroutineID()

	trackerMu.Lock()
	t := currentTracker(gid)
	if t == nil {
		trackerMu.Unlock()
		return
	}

	// Most derived values read only a few sources. A short scan avoids a
	// hash table for those windows; larger dependency sets keep O(1) lookup.
	if len(t.deps) < 8 {
		for _, dep := range t.deps {
			if dep.source == s {
				trackerMu.Unlock()
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
			trackerMu.Unlock()
			return
		}
		t.seen[s] = true
	}
	t.deps = append(t.deps, dependency{source: s, revision: revision})
	trackerMu.Unlock()
}

// trackDependencies runs fn and reports every signal that fn read on the
// calling goroutine. Nested windows are supported. The returned dependencies
// belong to the caller; recycle only retired lists with no remaining readers.
func trackDependencies[T any](fn func() T) (value T, deps []dependency) {
	t := pushTracker(goroutineID())
	defer popTracker(t, &deps)
	return fn(), nil
}

// Keep scratch management outside the generic evaluator so each value type
// shares the same implementation in the browser runtime.
func pushTracker(gid uint64) *dependencyTracker {
	openTrackers.Add(1)
	trackerMu.Lock()
	t := spareTrackers
	if t == nil {
		t = new(dependencyTracker)
	} else {
		spareTrackers = t.parent
		spareCount--
	}
	t.gid = gid
	t.parent = currentTracker(gid)
	storeTracker(gid, t)
	trackerMu.Unlock()
	return t
}

// Close the window even when fn panics. Transfer its dependency storage to
// the caller; cached windows cannot retain or overwrite a returned list.
func popTracker(t *dependencyTracker, deps *[]dependency) {
	trackerMu.Lock()
	*deps = t.deps
	t.deps = nil
	gid := t.gid
	head := currentTracker(gid)
	if head != t {
		// Runtimes without goroutine IDs share one scope. If overlapping
		// windows finish out of order, detach this window before caching it.
		// Each pushed window remains linked until its matching pop.
		for head.parent != t {
			head = head.parent
		}
		head.parent = t.parent
	} else {
		storeTracker(gid, t.parent)
	}
	// Cache only small, cleared windows. Oversized or excess windows can be
	// collected instead of keeping their backing arrays or source graphs.
	if cap(*deps) <= 128 && spareCount < 8 {
		clear(t.seen)
		t.parent = spareTrackers
		spareTrackers = t
		spareCount++
	}
	trackerMu.Unlock()
	openTrackers.Add(-1)
}

// Recycle only retired lists, after their subscriptions have been reconciled.
// Cached lists are cleared and bounded by the existing tracking-window cache.
func recycleDependencies(deps []dependency) {
	if cap(deps) > 128 {
		return
	}
	trackerMu.Lock()
	if t := spareTrackers; t != nil && t.deps == nil {
		clear(deps)
		t.deps = deps[:0]
	}
	trackerMu.Unlock()
}

// A stopped computation may still be reading its dependency snapshot in a
// refresh. Dispose its subscriptions, but only recycle storage with no reader.
func retireDependencies(deps []dependency, recycle bool) {
	for i := range deps {
		deps[i].unsubscribe()
	}
	if recycle {
		recycleDependencies(deps)
	}
}
