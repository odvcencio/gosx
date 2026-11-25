package replay_test

import (
	"testing"

	"m31labs.dev/gosx/game/replay"
)

func TestTapeZeroIsClosedAndEmpty(t *testing.T) {
	var tape replay.Tape
	if tape.Active() || tape.Playing() || tape.Index() != -1 || tape.Count() != 0 {
		t.Fatalf("zero tape: active %v playing %v index %d count %d", tape.Active(), tape.Playing(), tape.Index(), tape.Count())
	}
	tape.Open()
	if tape.Active() {
		t.Fatal("an empty tape must not open")
	}
	tape.Seek(3)
	tape.Toggle()
	tape.Tick()
	if tape.Jump(1) || tape.Active() || tape.Index() != -1 {
		t.Fatal("a closed tape must ignore the transport")
	}
}

func TestTapePlaysAtItsSpeedAndHoldsOnTheLastFrame(t *testing.T) {
	var tape replay.Tape
	tape.Load(5, nil)
	tape.Open()
	if !tape.Playing() || tape.Index() != 0 {
		t.Fatalf("Open: playing %v index %d, want playing from 0", tape.Playing(), tape.Index())
	}
	// Zero speed is half speed: two ticks a frame.
	var seen []int
	for i := 0; i < 10; i++ {
		tape.Tick()
		seen = append(seen, tape.Index())
	}
	want := []int{0, 1, 1, 2, 2, 3, 3, 4, 4, 4}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("half speed frames %v, want %v", seen, want)
		}
	}
	if tape.Playing() {
		t.Fatal("the tape must hold on the last frame")
	}
	// Playing from the last frame starts over.
	tape.Toggle()
	if !tape.Playing() || tape.Index() != 0 {
		t.Fatalf("Toggle at the end: playing %v index %d, want playing from 0", tape.Playing(), tape.Index())
	}
	tape.Speed = 2
	tape.Tick()
	if tape.Index() != 2 {
		t.Fatalf("speed 2: index %d after one tick, want 2", tape.Index())
	}
	tape.Toggle()
	tape.Tick()
	if tape.Playing() || tape.Index() != 2 {
		t.Fatalf("held tape moved: playing %v index %d", tape.Playing(), tape.Index())
	}
}

func TestTapeSeekClampsAndHolds(t *testing.T) {
	var tape replay.Tape
	tape.Load(4, nil)
	tape.Open()
	for _, c := range []struct{ seek, want int }{{2, 2}, {-5, 0}, {99, 3}} {
		tape.Seek(c.seek)
		if tape.Index() != c.want || tape.Playing() {
			t.Fatalf("Seek(%d): index %d playing %v, want %d held", c.seek, tape.Index(), tape.Playing(), c.want)
		}
	}
	tape.Close()
	if tape.Active() || tape.Index() != -1 {
		t.Fatal("Close must go back to the live game")
	}
}

func TestTapeJumpsBetweenMarks(t *testing.T) {
	var tape replay.Tape
	marks := []replay.TapeMark{{Index: 0, Label: "Snap"}, {Index: 4, Label: "Tackle"}, {Index: 9, Label: "Whistle"}}
	tape.Load(10, marks)
	tape.Open()
	tape.Seek(2)
	steps := []struct {
		dir  int
		ok   bool
		want int
	}{{1, true, 4}, {1, true, 9}, {1, false, 9}, {-1, true, 4}, {-1, true, 0}, {-1, false, 0}}
	for _, s := range steps {
		ok := tape.Jump(s.dir)
		if ok != s.ok || tape.Index() != s.want || tape.Playing() {
			t.Fatalf("Jump(%d): ok %v index %d playing %v, want ok %v index %d held", s.dir, ok, tape.Index(), tape.Playing(), s.ok, s.want)
		}
	}
	if got := tape.Marks(); len(got) != 3 || got[1].Label != "Tackle" {
		t.Fatalf("Marks() = %v", got)
	}
}

func TestTapeLoadAndClearKeepTheSpeed(t *testing.T) {
	tape := replay.Tape{Speed: 1}
	tape.Load(3, nil)
	tape.Open()
	tape.Load(6, nil)
	if tape.Active() || tape.Count() != 6 || tape.Speed != 1 {
		t.Fatalf("Load: active %v count %d speed %v, want closed, 6, 1", tape.Active(), tape.Count(), tape.Speed)
	}
	tape.Clear()
	if tape.Count() != 0 || tape.Speed != 1 {
		t.Fatalf("Clear: count %d speed %v, want 0 and 1", tape.Count(), tape.Speed)
	}
}

// A game shows the tape's frame by seeking its Player to the stretch's first
// tick plus Index; every frame it shows is the live state at that tick.
func TestTapeDrivesAPlayerThroughTheRecordedStretch(t *testing.T) {
	rec, hashes := play(t, 200, replay.Config{Interval: 32})
	g := newGame()
	p, err := replay.NewPlayer(g, rec.Recording())
	if err != nil {
		t.Fatal(err)
	}
	const from, to = 40, 170
	tape := replay.Tape{Speed: 0.75}
	tape.Load(to-from+1, []replay.TapeMark{{Index: 60, Label: "Mark"}})
	tape.Open()
	shown := 0
	for i := 0; i < 400 && tape.Playing(); i++ {
		tape.Tick()
		tick := uint64(from + tape.Index())
		if err := p.Seek(tick); err != nil {
			t.Fatalf("Seek(%d): %v", tick, err)
		}
		if got := replay.Hash(g); got != hashes[tick] {
			t.Fatalf("frame %d (tick %d): replay hash %x, live %x", tape.Index(), tick, got, hashes[tick])
		}
		shown++
	}
	if tape.Index() != to-from || p.Tick() != to {
		t.Fatalf("the tape ended on frame %d (tick %d), want the last frame %d (tick %d)", tape.Index(), p.Tick(), to-from, to)
	}
	if shown < (to-from)*4/3-2 {
		t.Fatalf("0.75 speed showed %d ticks, want about %d", shown, (to-from)*4/3)
	}
}
