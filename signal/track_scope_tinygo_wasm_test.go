//go:build tinygo && wasm

package signal

import "testing"

func TestTinyGoSharedTrackingWindowsCanCloseOutOfOrder(t *testing.T) {
	outer, middle, inner := pushTracker(0), pushTracker(0), pushTracker(0)
	source := New(1)
	source.Get()
	var first, second, last, reusedDeps []dependency
	popTracker(outer, &first)
	valid := currentTracker(0) == inner && inner.parent == middle && middle.parent == nil
	reused := pushTracker(0)
	valid = valid && reused.parent == inner
	popTracker(reused, &reusedDeps)
	popTracker(middle, &second)
	valid = valid && currentTracker(0) == inner && inner.parent == nil
	popTracker(inner, &last)
	if !valid || currentTracker(0) != nil || openTrackers.Load() != 0 {
		t.Fatal("shared tracking windows lost their ownership or stayed active")
	}
	if len(first) != 0 || len(second) != 0 || len(reusedDeps) != 0 || len(last) != 1 || last[0].source != source {
		t.Fatal("shared window reuse changed a live dependency list")
	}
	recycleDependencies(last)
}
