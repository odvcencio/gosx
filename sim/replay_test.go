package sim

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"m31labs.dev/gosx/hub"
)

func TestRunnerDisableReplay(t *testing.T) {
	s := &mockSim{}
	r := New(hub.New("no-replay"), s, Options{DisableReplay: true})
	const ticks = 10000
	for i := 0; i < ticks; i++ {
		r.ReceiveInput(fmt.Sprintf("player-%d", i), Input{Data: []byte("attack")})
		r.tickOnce()
	}
	if r.recorder != nil || len(r.Replay().Frames) != 0 {
		t.Fatal("disabled replay retained an input log")
	}
	if s.ticks != ticks || r.Frame() != ticks {
		t.Fatalf("simulation stopped advancing: ticks=%d frame=%d", s.ticks, r.Frame())
	}
	if _, ok := r.snapshots.Get(ticks); !ok {
		t.Fatal("disabled replay stopped recording rollback snapshots")
	}
}

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

// sumSim accumulates input bytes and the RNG seed so a replay is checkable.
type sumSim struct{ sum int }

func (s *sumSim) Tick(in map[string]Input) {
	EachInput(in, func(_ string, i Input) {
		for _, b := range i.Data {
			s.sum = s.sum*31 + int(b)
		}
	})
	s.sum++
}
func (s *sumSim) Snapshot() []byte { return []byte(fmt.Sprint(s.sum)) }
func (s *sumSim) Restore(b []byte) { fmt.Sscan(string(b), &s.sum) }
func (s *sumSim) State() []byte    { return s.Snapshot() }

func TestReplayLogCarriesInitialSnapshotAndSeed(t *testing.T) {
	s := &sumSim{sum: 7}
	r := New(hub.New("replay-init"), s, Options{Seed: 99})
	for i := 0; i < 5; i++ {
		r.ReceiveInput("p1", Input{Data: []byte{byte('a' + i)}})
		r.tickOnce()
	}
	log := r.Replay()
	if string(log.Initial) != "7" || log.Seed != 99 || log.InitialFrame != 0 || log.Truncated {
		t.Fatalf("log header: initial=%q seed=%d frame=%d", log.Initial, log.Seed, log.InitialFrame)
	}
	fresh := &sumSim{}
	fresh.Restore(log.Initial)
	for _, f := range log.Frames {
		fresh.Tick(f.Inputs)
	}
	if fresh.sum != s.sum {
		t.Fatalf("replay diverged: %d != %d", fresh.sum, s.sum)
	}
}

func TestReplayMaxFramesBoundsLog(t *testing.T) {
	r := New(hub.New("replay-cap"), &sumSim{}, Options{ReplayMaxFrames: 3})
	for i := 0; i < 10; i++ {
		r.tickOnce()
	}
	log := r.Replay()
	if len(log.Frames) != 3 || !log.Truncated || log.Frames[2].Frame != 3 {
		t.Fatalf("frames=%d truncated=%v", len(log.Frames), log.Truncated)
	}
}

func TestRunnerRestoreSetsFrameAndRestartsReplay(t *testing.T) {
	s := &sumSim{}
	r := New(hub.New("restore"), s, Options{})
	r.Restore(500, []byte("42"))
	if r.Frame() != 500 || s.sum != 42 {
		t.Fatalf("frame=%d sum=%d", r.Frame(), s.sum)
	}
	r.tickOnce()
	if r.Frame() != 501 {
		t.Fatalf("frame=%d", r.Frame())
	}
	log := r.Replay()
	if string(log.Initial) != "42" || log.InitialFrame != 500 || len(log.Frames) != 1 || log.Frames[0].Frame != 501 {
		t.Fatalf("log=%+v", log)
	}
}

var errBadSnapshot = errors.New("bad snapshot")

// checkedSim is a Simulation that can refuse a snapshot, as game.Runtime can. It
// takes a number and refuses anything else.
type checkedSim struct{ sumSim }

func (s *checkedSim) RestoreSnapshot(b []byte) error {
	var n int
	if _, err := fmt.Sscan(string(b), &n); err != nil {
		return fmt.Errorf("%w: %q", errBadSnapshot, b)
	}
	s.sum = n
	return nil
}

// When the simulation refuses a snapshot, the runner must keep its frame and
// its replay log. Both would otherwise describe a state that the simulation
// never reached, and a replay from that log would start from the refused bytes.
func TestRunnerRestoreSnapshotKeepsStateWhenRefused(t *testing.T) {
	s := &checkedSim{sumSim{sum: 7}}
	r := New(hub.New("refused"), s, Options{})
	for i := 0; i < 3; i++ {
		r.ReceiveInput("p1", Input{Data: []byte{byte('a' + i)}})
		r.tickOnce()
	}
	frame, sum, log := r.Frame(), s.sum, r.Replay()

	if err := r.RestoreSnapshot(500, []byte("junk")); !errors.Is(err, errBadSnapshot) {
		t.Fatalf("RestoreSnapshot returned %v, want %v", err, errBadSnapshot)
	}
	r.Restore(600, []byte("junk")) // the void form drops the error but must not move anything either
	if r.Frame() != frame || s.sum != sum || !reflect.DeepEqual(r.Replay(), log) {
		t.Fatalf("a refused snapshot changed the runner\n frame %d, was %d\n sum %d, was %d\n log %+v\n was %+v", r.Frame(), frame, s.sum, sum, r.Replay(), log)
	}

	// A snapshot that the simulation takes sets the frame and restarts the log.
	if err := r.RestoreSnapshot(500, []byte("42")); err != nil {
		t.Fatal(err)
	}
	r.tickOnce()
	got := r.Replay()
	if r.Frame() != 501 || s.sum != 43 || string(got.Initial) != "42" || got.InitialFrame != 500 || len(got.Frames) != 1 || got.Frames[0].Frame != 501 {
		t.Fatalf("frame=%d sum=%d log=%+v", r.Frame(), s.sum, got)
	}
}

// A simulation without CheckedRestorer cannot refuse a snapshot, so
// RestoreSnapshot restores it as Restore does and reports no error.
func TestRunnerRestoreSnapshotWithoutCheckedRestorer(t *testing.T) {
	s := &sumSim{}
	r := New(hub.New("plain-restore"), s, Options{})
	if err := r.RestoreSnapshot(500, []byte("42")); err != nil {
		t.Fatal(err)
	}
	log := r.Replay()
	if r.Frame() != 500 || s.sum != 42 || string(log.Initial) != "42" || log.InitialFrame != 500 {
		t.Fatalf("frame=%d sum=%d log=%+v", r.Frame(), s.sum, log)
	}
}

// racyCheckedSim writes the field that Tick writes, with no lock of its own, so
// a restore that does not hold the runner's lock is a data race under -race.
type racyCheckedSim struct{ racySim }

func (s *racyCheckedSim) RestoreSnapshot([]byte) error { s.n = 0; return nil }

func TestRestoreSnapshotDoesNotRaceWithTick(t *testing.T) {
	r := New(hub.New("restore-race"), &racyCheckedSim{}, Options{TickRate: 1000})
	r.Start()
	defer r.Stop()
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		if err := r.RestoreSnapshot(1, []byte("0")); err != nil {
			t.Fatal(err)
		}
	}
}
