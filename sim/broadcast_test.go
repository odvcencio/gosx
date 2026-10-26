package sim

import (
	"bytes"
	"testing"

	"m31labs.dev/gosx/hub"
)

func TestBinarySnapshotRoundTrip(t *testing.T) {
	b := &binarySnapshotBroadcaster{every: 3}
	h := hub.New("bin")
	states := [][]byte{
		[]byte(`{"a":1,"pad":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx","b":1}`),
		[]byte(`{"a":2,"pad":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx","b":1}`),
		[]byte(`{"a":2,"pad":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx","b":9}`),
		[]byte(`{"a":2,"pad":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx","b":7}`),
		[]byte(`short`),
	}
	for i, st := range states {
		b.Broadcast(h, uint64(i+1), st)
		var delta []byte
		if b.since > 1 || (i > 0 && b.epoch == 1) {
			delta = encodeBinaryDelta(b.epoch, uint64(i+1), b.state, st)
		}
		if delta != nil {
			_, f, got, err := DecodeBinarySnapshot(delta, b.baseline)
			if err != nil || f != uint64(i+1) || !bytes.Equal(got, st) {
				t.Fatalf("delta %d: %v %q", i, err, got)
			}
			if len(delta) >= len(st) {
				t.Fatalf("delta not smaller")
			}
		}
	}
	if b.epoch < 2 {
		t.Fatalf("expected a second keyframe, epoch=%d", b.epoch)
	}
	if _, _, st, err := DecodeBinarySnapshot(b.baseline, nil); err != nil || string(st) != "short" {
		t.Fatalf("keyframe decode: %v %q", err, st)
	}
	if _, _, _, err := DecodeBinarySnapshot([]byte{1}, nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunnerUsesCustomBroadcasterAndRingOptions(t *testing.T) {
	var frames []uint64
	r := New(hub.New("custom"), &sumSim{}, Options{
		RingSize:      4,
		SnapshotEvery: 2,
		Broadcaster:   BroadcasterFunc(func(_ *hub.Hub, f uint64, _ []byte) { frames = append(frames, f) }),
	})
	for i := 0; i < 6; i++ {
		r.tickOnce()
	}
	if len(frames) != 6 {
		t.Fatalf("frames=%v", frames)
	}
	for f, want := range map[uint64]bool{1: false, 2: true, 3: false, 4: true, 6: true} {
		if _, ok := r.SnapshotAt(f); ok != want {
			t.Fatalf("SnapshotAt(%d)=%v want %v", f, ok, want)
		}
	}
}

type countingSnapSim struct {
	sumSim
	snaps int
}

func (s *countingSnapSim) Snapshot() []byte { s.snaps++; return s.sumSim.Snapshot() }

func TestRingDisabledSkipsPerTickSnapshot(t *testing.T) {
	s := &countingSnapSim{}
	r := New(hub.New("noring"), s, Options{RingSize: RingDisabled, DisableReplay: true})
	for i := 0; i < 5; i++ {
		r.tickOnce()
	}
	if s.snaps != 0 {
		t.Fatalf("snapshots=%d", s.snaps)
	}
	if _, ok := r.SnapshotAt(5); ok {
		t.Fatal("ring should be disabled")
	}
}
