package worklet

import "testing"

func TestStateTracksReadyPlayAndPosition(t *testing.T) {
	var s State
	if s.Apply(Event{Type: MsgReady}); !s.Ready {
		t.Fatal("not ready")
	}
	s.Apply(Event{Type: MsgState, Playing: true})
	if !s.Playing {
		t.Fatal("not playing")
	}
	if !s.Apply(Event{Type: MsgMessage, Frames: 4096}) {
		t.Fatal("a message batch must ask the page to return the buffer")
	}
	if s.Frames != 4096 {
		t.Fatalf("frames = %v", s.Frames)
	}
	s.Apply(Event{Type: MsgState, Playing: false})
	if s.Playing {
		t.Fatal("still playing after stop")
	}
}

func TestStateFaultStopsPlaybackAndErrorClosesNode(t *testing.T) {
	s := State{Ready: true, Playing: true}
	s.Apply(Event{Type: MsgFault, Fault: 42})
	if s.Fault == nil || *s.Fault != 42 || s.Playing {
		t.Fatalf("state after fault = %+v", s)
	}
	s.Apply(Event{Type: MsgError, Err: "audio: render failed"})
	if !s.Closed || s.Error != "audio: render failed" {
		t.Fatalf("state after error = %+v", s)
	}
}

func TestStateOnlyMessageBatchesNeedTheBufferBack(t *testing.T) {
	for _, typ := range []string{MsgReady, MsgState, MsgFault, MsgError, MsgReject, MsgSwapped, MsgTiming} {
		var s State
		if s.Apply(Event{Type: typ}) {
			t.Errorf("%q must not request a buffer return", typ)
		}
	}
}

func TestStateRecordsUnderruns(t *testing.T) {
	var s State
	s.Apply(Event{Type: MsgTiming, Underruns: 3})
	if s.Underruns != 3 {
		t.Fatalf("underruns = %d", s.Underruns)
	}
}

func TestSplitRecords(t *testing.T) {
	recs, err := SplitRecords(make([]byte, 48), 16)
	if err != nil || len(recs) != 3 || len(recs[1]) != 16 {
		t.Fatalf("recs=%d err=%v", len(recs), err)
	}
	if _, err := SplitRecords(make([]byte, 17), 16); err == nil {
		t.Fatal("expected error for a partial record")
	}
	if _, err := SplitRecords(nil, 0); err == nil {
		t.Fatal("expected error for size 0")
	}
}

func TestCheckCommands(t *testing.T) {
	b := Backend{CommandBytes: 24}
	if err := b.CheckCommands(make([]byte, 48)); err != nil {
		t.Fatal(err)
	}
	if err := b.CheckCommands(make([]byte, 25)); err == nil {
		t.Fatal("partial command accepted")
	}
	if err := b.CheckCommands(nil); err == nil {
		t.Fatal("empty batch accepted")
	}
}
