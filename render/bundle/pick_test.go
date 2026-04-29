package bundle

import "testing"

func TestQueuePickDestroysUnsubmittedStaging(t *testing.T) {
	old := &fakeBuffer{}
	r := &Renderer{
		pendingPick: &pickRequest{
			staging: old,
		},
	}

	r.QueuePick(4, 8, func(uint32) {})

	if !old.destroyed {
		t.Fatal("old unsubmitted staging buffer was not destroyed")
	}
	if r.pendingPick == nil || r.pendingPick.x != 4 || r.pendingPick.y != 8 {
		t.Fatalf("replacement pick = %#v", r.pendingPick)
	}
}

func TestQueuePickRetiresSubmittedStaging(t *testing.T) {
	old := &fakeBuffer{}
	req := &pickRequest{
		staging:     old,
		submitFrame: true,
	}
	r := &Renderer{pendingPick: req}

	r.QueuePick(4, 8, func(uint32) {})

	if old.destroyed {
		t.Fatal("submitted staging buffer was destroyed before readback cleanup")
	}
	if len(r.retiredPicks) != 1 || r.retiredPicks[0] != req {
		t.Fatalf("retired picks = %#v, want old request", r.retiredPicks)
	}
}

func TestQueuePickLeavesInFlightStagingOwnedByReadback(t *testing.T) {
	old := &fakeBuffer{}
	r := &Renderer{
		pendingPick: &pickRequest{
			staging:  old,
			inFlight: true,
		},
	}

	r.QueuePick(4, 8, func(uint32) {})

	if old.destroyed {
		t.Fatal("in-flight staging buffer was destroyed by replacement")
	}
	if len(r.retiredPicks) != 0 {
		t.Fatalf("retired picks = %#v, want none for in-flight request", r.retiredPicks)
	}
}
