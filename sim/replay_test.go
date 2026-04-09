package sim

import (
	"testing"

	"github.com/odvcencio/gosx/hub"
)

func TestReplayRecordAndPlayback(t *testing.T) {
	// Record 2 frames with inputs
	rec := newReplayRecorder()

	inputs1 := map[string]Input{
		"p1": {Data: []byte("attack")},
	}
	rec.Record(1, inputs1)

	inputs2 := map[string]Input{
		"p1": {Data: []byte("block")},
		"p2": {Data: []byte("jump")},
	}
	rec.Record(2, inputs2)

	log := rec.Finish()

	// Verify frame count
	if len(log.Frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(log.Frames))
	}

	// Verify input count per frame
	if len(log.Frames[0].Inputs) != 1 {
		t.Fatalf("expected 1 input in frame 0, got %d", len(log.Frames[0].Inputs))
	}
	if len(log.Frames[1].Inputs) != 2 {
		t.Fatalf("expected 2 inputs in frame 1, got %d", len(log.Frames[1].Inputs))
	}

	// Verify deep copy: mutate original, replay should be unaffected
	inputs1["p1"] = Input{Data: []byte("MUTATED")}
	if string(log.Frames[0].Inputs["p1"].Data) != "attack" {
		t.Fatalf("replay log was mutated, got %q", string(log.Frames[0].Inputs["p1"].Data))
	}

	// Play through a mock sim to verify replay works
	s := &mockSim{}
	h := hub.New("replay-test")
	r := New(h, s, Options{})
	_ = r // runner created successfully

	for _, frame := range log.Frames {
		s.Tick(frame.Inputs)
	}
	if s.ticks != 2 {
		t.Fatalf("expected 2 ticks from replay, got %d", s.ticks)
	}
}
