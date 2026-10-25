//go:build !tinygo || !wasm

package signal

var trackerStacks map[uint64]*dependencyTracker

// A hint for the last map update, not the calling goroutine's identity. A
// stale hint just misses in the map and uses normal ownership lookup.
var trackerLookupHint uint64

// A repeated read cannot change tracking when the sole active window already
// holds its source. Its owner would keep the first revision; an unrelated
// goroutine would record nothing. Neither case needs an identity lookup.
func dependencyAlreadyRecorded(source Subscribable) bool {
	trackerMu.Lock()
	defer trackerMu.Unlock()
	// Counts increase before a window is published and decrease after removal,
	// so this check cannot overlook another window that can already read.
	if openTrackers.Load() != 1 || len(trackerStacks) != 1 {
		return false
	}
	tracker := trackerStacks[trackerLookupHint]
	if tracker == nil {
		return false
	}
	if len(tracker.seen) != 0 {
		return tracker.seen[source]
	}
	for _, dep := range tracker.deps {
		if dep.source == source {
			return true
		}
	}
	return false
}

// Tracking scope access is protected by trackerMu.
func currentTracker(gid uint64) *dependencyTracker { return trackerStacks[gid] }

func storeTracker(gid uint64, tracker *dependencyTracker) {
	trackerLookupHint = gid
	if tracker == nil {
		delete(trackerStacks, gid)
		return
	}
	if trackerStacks == nil {
		trackerStacks = make(map[uint64]*dependencyTracker)
	}
	trackerStacks[gid] = tracker
}
