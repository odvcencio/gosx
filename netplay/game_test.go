package netplay

import (
	"math/rand/v2"
	"testing"
)

func TestRepeatLastReturnsLastInput(t *testing.T) {
	var p Predictor[pad] = RepeatLast[pad]
	if got := p(1, padAttack|padLeft, 99); got != padAttack|padLeft {
		t.Fatalf("RepeatLast = %v", got)
	}
}

func runGame(g *fightGame, in [][2]pad) {
	buf := make([]pad, 2)
	for _, f := range in {
		buf[0], buf[1] = f[0], f[1]
		g.Step(buf)
	}
}

func TestHeldAttackFiresOnePressEdge(t *testing.T) {
	g := newFightGame()
	g.x = [2]int32{100, 110}
	var evs []Event
	buf := make([]pad, 2)
	for i := 0; i < 40; i++ {
		buf[0], buf[1] = padAttack, 0 // held for 40 frames
		g.Step(buf)
		evs = g.Events(evs)
	}
	// One press gives two hit windows (moves 1 and 2), never a second attack.
	hits := 0
	for _, e := range evs {
		if e.Kind == evHit && e.Source>>16 == 0 {
			hits++
		}
	}
	if hits != 2 {
		t.Fatalf("held attack produced %d hits, want 2 (one press)", hits)
	}
}

func TestSameFrameTradeEmitsBothEvents(t *testing.T) {
	g := newFightGame()
	g.x = [2]int32{100, 110}
	buf := make([]pad, 2)
	var evs []Event
	for i := 0; i < 4; i++ {
		buf[0], buf[1] = padAttack, padAttack
		g.Step(buf)
		evs = g.Events(evs[:0])
	}
	if len(evs) != 2 {
		t.Fatalf("trade frame emitted %d events, want 2: %+v", len(evs), evs)
	}
	if evs[0].Frame != evs[1].Frame || evs[0].Source == evs[1].Source {
		t.Fatalf("trade events not same-frame/distinct: %+v", evs)
	}
	if g.hp[0] != 90 || g.hp[1] != 90 {
		t.Fatalf("trade hp = %v", g.hp)
	}
}

func TestBlockEmitsBlockEvent(t *testing.T) {
	g := newFightGame()
	g.x = [2]int32{100, 110}
	buf := make([]pad, 2)
	var evs []Event
	for i := 0; i < 4; i++ {
		buf[0], buf[1] = padAttack, padBlock
		g.Step(buf)
		evs = g.Events(evs[:0])
	}
	if len(evs) != 1 || evs[0].Kind != evBlock || g.hp[1] != 99 {
		t.Fatalf("block: evs=%+v hp=%v", evs, g.hp)
	}
}

func randomInputs(seed uint64, n int) [][2]pad {
	r := rand.New(rand.NewPCG(seed, 1))
	out := make([][2]pad, n)
	var cur [2]pad
	for i := range out {
		for p := 0; p < 2; p++ {
			if r.IntN(6) == 0 {
				cur[p] = pad(r.IntN(16))
			}
		}
		out[i] = cur
	}
	return out
}

func TestSaveLoadRoundTrip(t *testing.T) {
	in := randomInputs(7, 300)
	a := newFightGame()
	runGame(a, in[:100])
	snap := a.Save(nil)
	runGame(a, in[100:])
	want := a.Save(nil)

	b := newFightGame()
	if err := b.Load(snap); err != nil {
		t.Fatal(err)
	}
	runGame(b, in[100:])
	if got := b.Save(nil); string(got) != string(want) {
		t.Fatal("state after Load+replay differs")
	}
	if err := b.Load(snap[:3]); err == nil {
		t.Fatal("Load accepted a short blob")
	}
}

func TestWrongInputIsVisibleInState(t *testing.T) {
	in := randomInputs(9, 120)
	a := newFightGame()
	runGame(a, in)
	bad := append([][2]pad(nil), in...)
	bad[50][1] ^= padRight // one wrong remote input, frame 50
	b := newFightGame()
	runGame(b, bad)
	if string(a.Save(nil)) == string(b.Save(nil)) {
		t.Fatal("a wrong input at frame 50 left no trace at frame 120")
	}
}

func TestGameStepSaveLoadEventsDoNotAllocate(t *testing.T) {
	g := newFightGame()
	buf := make([]pad, 2)
	state := make([]byte, 0, 256)
	evs := make([]Event, 0, 16)
	n := 0
	allocs := testing.AllocsPerRun(200, func() {
		buf[0], buf[1] = pad(n&15), pad((n>>2)&15)
		n++
		g.Step(buf)
		state = g.Save(state[:0])
		_ = g.Load(state)
		evs = g.Events(evs[:0])
	})
	if allocs != 0 {
		t.Fatalf("allocs per frame = %v", allocs)
	}
}
