package sync

import "testing"

func TestStateShouldSendTracksInitializationHeadsAndPendingChanges(t *testing.T) {
	state := NewState()
	heads := [][32]byte{hashByte(1)}

	if !state.ShouldSend(heads, false) {
		t.Fatal("expected initial sync to send heads")
	}

	state.NoteHeads(heads)
	if state.ShouldSend(heads, false) {
		t.Fatal("expected identical heads without changes to stay quiet")
	}
	if !state.ShouldSend(heads, true) {
		t.Fatal("expected pending changes to force a send")
	}

	nextHeads := [][32]byte{hashByte(2)}
	if !state.ShouldSend(nextHeads, false) {
		t.Fatal("expected changed heads to force a send")
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
