package sync

import "testing"

func TestStateShouldSendTracksInitializationHeadsAndPendingChanges(t *testing.T) {
	state := NewState()
	heads := [][32]byte{hashByte(1), hashByte(2)}

	if !state.ShouldSend(heads, false) {
		t.Fatal("expected initial sync to send heads")
	}

	state.NoteHeads(heads)
	if state.ShouldSend(heads, false) {
		t.Fatal("expected identical head set without changes to stay quiet")
	}
	if !state.ShouldSend(heads, true) {
		t.Fatal("expected pending changes to force a send")
	}

	nextHeads := [][32]byte{hashByte(2), hashByte(3)}
	if !state.ShouldSend(nextHeads, false) {
		t.Fatal("expected changed heads to force a send")
	}
}

func TestStateNoteHeadsClonesInput(t *testing.T) {
	state := NewState()
	heads := [][32]byte{hashByte(1), hashByte(2)}
	state.NoteHeads(heads)

	heads[0] = hashByte(9)
	if state.LastSentHeads[0] != hashByte(1) {
		t.Fatalf("expected stored heads to be cloned, got %#v", state.LastSentHeads)
	}
}

func TestStateShouldSendIgnoresHeadOrdering(t *testing.T) {
	state := NewState()
	initial := [][32]byte{hashByte(1), hashByte(2), hashByte(3)}
	state.NoteHeads(initial)

	reordered := [][32]byte{hashByte(3), hashByte(1), hashByte(2)}
	if state.ShouldSend(reordered, false) {
		t.Fatalf("expected reordered heads %#v to match last sent set %#v", reordered, state.LastSentHeads)
	}
}

func TestStateMarkKnownClearsSentHashes(t *testing.T) {
	state := NewState()
	hash := hashByte(9)

	state.MarkSent(hash)
	if !state.HasSent(hash) {
		t.Fatal("expected sent hash to be tracked")
	}

	state.MarkKnown(hash)
	if !state.HasKnown(hash) {
		t.Fatal("expected known hash to be tracked")
	}
	if state.HasSent(hash) {
		t.Fatal("expected marking a hash known to clear sent state")
	}
}

func TestStateMarkKnownClearsNeedState(t *testing.T) {
	state := NewState()
	hash := hashByte(9)

	state.MarkNeed(hash)
	state.MarkPeerNeed(hash)
	state.MarkKnown(hash)

	if len(state.Needed()) != 0 {
		t.Fatal("expected marking a hash known to clear local need state")
	}
	if state.HasPeerNeed(hash) {
		t.Fatal("expected marking a hash known to clear peer need state")
	}
}

func TestStateNeedForcesSendAndClonesSortedNeeds(t *testing.T) {
	state := NewState()
	heads := [][32]byte{hashByte(1)}
	state.NoteHeads(heads)
	if state.ShouldSend(heads, false) {
		t.Fatal("expected quiet state before needs are recorded")
	}

	state.MarkNeed(hashByte(9))
	state.MarkNeed(hashByte(3))
	if !state.ShouldSend(heads, false) {
		t.Fatal("expected local need to force a sync message")
	}
	needs := state.Needed()
	if len(needs) != 2 || needs[0] != hashByte(3) || needs[1] != hashByte(9) {
		t.Fatalf("needs = %#v, want sorted hashes 3,9", needs)
	}
	needs[0] = hashByte(1)
	if state.Needed()[0] != hashByte(3) {
		t.Fatal("expected Needed to return a cloned snapshot")
	}
}

func TestStatePeerNeedOverridesKnownAndSentState(t *testing.T) {
	state := NewState()
	hash := hashByte(7)
	state.MarkKnown(hash)
	state.MarkSent(hash)

	state.MarkPeerNeed(hash)
	if !state.HasPeerNeed(hash) {
		t.Fatal("expected peer need to be tracked")
	}
	if state.HasKnown(hash) || state.HasSent(hash) {
		t.Fatal("expected peer need to clear stale known/sent assumptions")
	}
}

func TestStatePeerBloomIsClonedAndQueryable(t *testing.T) {
	state := NewState()
	hash := hashByte(4)
	filter := NewBloomFilterForHashes([][32]byte{hash})
	state.MarkPeerBloom(filter)

	if !state.PeerMayHave(hash) {
		t.Fatal("expected peer bloom to report inserted hash")
	}
	filter = NewBloomFilterForHashes([][32]byte{hashByte(9)})
	if !state.PeerMayHave(hash) {
		t.Fatal("expected peer bloom to be cloned")
	}
	state.MarkPeerBloom(nil)
	if state.PeerMayHave(hash) {
		t.Fatal("expected clearing peer bloom to reset membership")
	}
}
