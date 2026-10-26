//go:build tinygo && wasm

package signal

// TinyGo browser goroutine identity is always zero. Keep its one shared scope
// directly instead of allocating and consulting a map with a constant key.
// Tracking scope access is protected by trackerMu.
var sharedTracker *dependencyTracker

// TinyGo already looks up its shared scope without formatting a goroutine
// traceback. Keep its existing single-lock dependency collection path.
func dependencyAlreadyRecorded(Subscribable) bool { return false }

func currentTracker(uint64) *dependencyTracker          { return sharedTracker }
func storeTracker(_ uint64, tracker *dependencyTracker) { sharedTracker = tracker }
