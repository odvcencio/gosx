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
