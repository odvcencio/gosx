//go:build !tinygo || !wasm

package signal

var trackerStacks map[uint64]*dependencyTracker

// Tracking scope access is protected by trackerMu.
func currentTracker(gid uint64) *dependencyTracker { return trackerStacks[gid] }

func storeTracker(gid uint64, tracker *dependencyTracker) {
	if tracker == nil {
		delete(trackerStacks, gid)
		return
	}
	if trackerStacks == nil {
		trackerStacks = make(map[uint64]*dependencyTracker)
	}
	trackerStacks[gid] = tracker
}
