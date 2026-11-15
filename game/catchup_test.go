package game

import (
	"encoding/json"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// fakeWall is an injected wall clock. Fixed-step systems advance it to model
// how long a step takes, so budget decisions do not depend on the machine.
type fakeWall struct{ now time.Duration }

func (w *fakeWall) read() time.Duration { return w.now }

// stepCost returns a fixed-update system that records each tick and advances
// the wall clock by cost(tick).
func stepCost(wall *fakeWall, ticks *[]uint64, cost func(tick uint64) time.Duration) System {
	return Func("cost", PhaseFixedUpdate, func(ctx *Context) error {
		*ticks = append(*ticks, ctx.Tick)
		wall.now += cost(ctx.Tick)
		return nil
	})
}

func TestCatchUpDefaultsToDrop(t *testing.T) {
	if got := NewClock(LoopConfig{}).CatchUp(); got != CatchUpDrop {
		t.Fatalf("NewClock default policy = %v, want drop", got)
	}
	if got := New(Config{}).clock.CatchUp(); got != CatchUpDrop {
		t.Fatalf("Runtime default policy = %v, want drop", got)
	}
	if got := NewClock(LoopConfig{CatchUp: 9}).CatchUp(); got != CatchUpDrop {
		t.Fatalf("unknown policy = %v, want drop", got)
	}
	if CatchUpDrop.String() != "drop" || CatchUpLag.String() != "lag" || CatchUp(7).String() != "CatchUp(7)" {
		t.Fatalf("String: %q %q %q", CatchUpDrop, CatchUpLag, CatchUp(7))
	}
	// Drop keeps dropping what MaxSubsteps cannot run and never reports lag.
	c := NewClock(LoopConfig{FixedStep: 10 * time.Millisecond, MaxSubsteps: 2})
	f := c.Advance(55 * time.Millisecond)
	if f.FixedSteps != 2 || f.DroppedDuration != 35*time.Millisecond || f.Lag != 0 {
		t.Fatalf("drop frame = %+v, want 2 steps, 35ms dropped, no lag", f)
	}
}

// Under CatchUpLag the clock must run every due step eventually, whatever the
// frame deltas and step costs, and must never report dropped time. The ticks
// that fixed systems see stay contiguous.
func TestCatchUpLagNeverDropsAStep(t *testing.T) {
	const step = 10 * time.Millisecond
	rng := rand.New(rand.NewPCG(2026, 1108))
	wall := &fakeWall{}
	var ticks []uint64
	rt := New(Config{
		FixedStep:     step,
		MaxDelta:      time.Second,
		MaxSubsteps:   2,
		CatchUp:       CatchUpLag,
		CatchUpBudget: 25 * time.Millisecond,
		WallClock:     wall.read,
		Systems: []System{stepCost(wall, &ticks, func(uint64) time.Duration {
			return time.Duration(1+rng.IntN(12)) * time.Millisecond
		})},
	})
	var fed time.Duration
	maxLag := time.Duration(0)
	for i := 0; i < 2000; i++ {
		delta := time.Duration(rng.IntN(120)) * time.Millisecond
		fed += delta
		f, err := rt.Step(delta)
		if err != nil {
			t.Fatal(err)
		}
		if f.DroppedDuration != 0 {
			t.Fatalf("frame %d dropped %v under lag", f.Index, f.DroppedDuration)
		}
		if f.Lag%step != 0 || f.Lag != rt.clock.Lag() {
			t.Fatalf("frame %d lag %v, clock lag %v", f.Index, f.Lag, rt.clock.Lag())
		}
		// Every nanosecond fed is either run, owed as lag, or in the remainder.
		if got := f.Time + f.Lag + rt.clock.accumulator; got != fed {
			t.Fatalf("frame %d accounts for %v of %v fed", f.Index, got, fed)
		}
		if uint64(len(ticks)) != f.Steps {
			t.Fatalf("frame %d: systems saw %d steps, frame counts %d", f.Index, len(ticks), f.Steps)
		}
		maxLag = max(maxLag, f.Lag)
	}
	if maxLag == 0 {
		t.Fatal("the test never made the clock lag; raise the step costs")
	}
	// Idle frames drain the lag.
	for i := 0; rt.clock.Lag() > 0; i++ {
		if i > 10000 {
			t.Fatalf("lag %v never drained", rt.clock.Lag())
		}
		if _, err := rt.Step(0); err != nil {
			t.Fatal(err)
		}
	}
	if want := uint64(fed / step); rt.Frame().Steps != want {
		t.Fatalf("ran %d steps for %v fed, want %d", rt.Frame().Steps, fed, want)
	}
	for i, tick := range ticks {
		if tick != uint64(i+1) {
			t.Fatalf("tick %d = %d, want %d: ticks are not contiguous", i, tick, i+1)
		}
	}
}

// The budget stops the fixed steps once they have used it, checked before
// every step after the first.
func TestCatchUpLagRespectsBudget(t *testing.T) {
	const step = 10 * time.Millisecond
	wall := &fakeWall{}
	var ticks []uint64
	var late Frame
	rt := New(Config{
		FixedStep:     step,
		MaxDelta:      time.Second,
		CatchUp:       CatchUpLag,
		CatchUpBudget: 8 * time.Millisecond,
		WallClock:     wall.read,
		Systems: []System{
			stepCost(wall, &ticks, func(uint64) time.Duration { return 3 * time.Millisecond }),
			Func("late", PhaseLateUpdate, func(ctx *Context) error { late = ctx.Frame; return nil }),
		},
	})
	start := wall.now
	f, err := rt.Step(100 * time.Millisecond) // 10 steps due
	if err != nil {
		t.Fatal(err)
	}
	// Steps start at 0, 3 and 6 ms; at 9 ms the budget of 8 ms is spent.
	if f.FixedSteps != 3 || f.Steps != 3 || f.Time != 3*step || f.Lag != 7*step {
		t.Fatalf("frame = %+v, want 3 steps run and 7 owed", f)
	}
	if spent := wall.now - start; spent > 8*time.Millisecond+3*time.Millisecond {
		t.Fatalf("frame spent %v, more than the budget plus one step", spent)
	}
	if late != f || rt.Frame() != f {
		t.Fatalf("late update saw %+v, Frame() = %+v, Step returned %+v", late, rt.Frame(), f)
	}
	f, _ = rt.Step(0)
	if f.FixedSteps != 3 || f.Lag != 4*step {
		t.Fatalf("second frame = %+v, want 3 more steps and 4 owed", f)
	}
}

// A step that costs more than the whole budget still runs once per frame, so a
// slow simulation makes progress.
func TestCatchUpLagRunsOneStepPerFrameOverBudget(t *testing.T) {
	const step = 10 * time.Millisecond
	wall := &fakeWall{}
	var ticks []uint64
	rt := New(Config{
		FixedStep:     step,
		MaxDelta:      time.Second,
		CatchUp:       CatchUpLag,
		CatchUpBudget: 8 * time.Millisecond,
		WallClock:     wall.read,
		Systems:       []System{stepCost(wall, &ticks, func(uint64) time.Duration { return 20 * time.Millisecond })},
	})
	for i, wantLag := range []time.Duration{4 * step, 3 * step, 2 * step, step, 0} {
		delta := time.Duration(0)
		if i == 0 {
			delta = 5 * step
		}
		f, _ := rt.Step(delta)
		if f.FixedSteps != 1 || f.Lag != wantLag {
			t.Fatalf("frame %d = %+v, want 1 step and lag %v", i, f, wantLag)
		}
	}
	if f, _ := rt.Step(0); f.FixedSteps != 0 || f.Lag != 0 {
		t.Fatalf("drained frame = %+v, want no steps", f)
	}
}

// Without a budget, CatchUpLag runs every due step in one frame, past
// MaxSubsteps, and reads no wall clock.
func TestCatchUpLagUnboundedIgnoresMaxSubsteps(t *testing.T) {
	reads := 0
	rt := New(Config{
		FixedStep:     time.Millisecond,
		MaxDelta:      time.Second,
		MaxSubsteps:   2,
		CatchUp:       CatchUpLag,
		CatchUpBudget: -1,
		WallClock:     func() time.Duration { reads++; return 0 },
	})
	f, _ := rt.Step(40 * time.Millisecond)
	if f.FixedSteps != 40 || f.Lag != 0 || f.DroppedDuration != 0 {
		t.Fatalf("frame = %+v, want all 40 steps", f)
	}
	if reads != 0 {
		t.Fatalf("read the wall clock %d times without a budget", reads)
	}
}

// A paused clock keeps its carried steps and runs them after it resumes.
func TestCatchUpLagPauseKeepsLag(t *testing.T) {
	const step = 10 * time.Millisecond
	wall := &fakeWall{}
	var ticks []uint64
	rt := New(Config{
		FixedStep: step, MaxDelta: time.Second, CatchUp: CatchUpLag, CatchUpBudget: 5 * time.Millisecond, WallClock: wall.read,
		Systems: []System{stepCost(wall, &ticks, func(uint64) time.Duration { return 5 * time.Millisecond })},
	})
	f, _ := rt.Step(4 * step)
	if f.FixedSteps != 1 || f.Lag != 3*step {
		t.Fatalf("frame = %+v, want 1 step and 3 owed", f)
	}
	rt.SetPaused(true)
	f, _ = rt.Step(step)
	if f.FixedSteps != 0 || f.Lag != 3*step || f.Time != step {
		t.Fatalf("paused frame = %+v, want no steps and the lag kept", f)
	}
	rt.SetPaused(false)
	for i := 0; i < 3; i++ {
		rt.Step(0)
	}
	if got := rt.Frame(); got.Steps != 4 || got.Lag != 0 {
		t.Fatalf("after resume = %+v, want 4 steps and no lag", got)
	}
}

// Clock.AdvanceFunc applies the budget for hosts that use the clock without a
// Runtime; Clock.Advance plans every due step.
func TestClockAdvanceFuncBudget(t *testing.T) {
	const step = 10 * time.Millisecond
	wall := &fakeWall{}
	c := NewClock(LoopConfig{FixedStep: step, MaxDelta: time.Second, CatchUp: CatchUpLag, CatchUpBudget: 10 * time.Millisecond, WallClock: wall.read})
	var ticks []uint64
	run := func(tick uint64) { ticks = append(ticks, tick); wall.now += 4 * time.Millisecond }
	f := c.AdvanceFunc(60*time.Millisecond, run) // steps at 0, 4, 8; stop at 12
	if f.FixedSteps != 3 || f.Lag != 3*step || f.Time != 3*step {
		t.Fatalf("AdvanceFunc = %+v, want 3 steps and 3 owed", f)
	}
	f = c.AdvanceFunc(0, run)
	if f.FixedSteps != 3 || f.Lag != 0 {
		t.Fatalf("second AdvanceFunc = %+v, want the 3 owed steps", f)
	}
	for i, tick := range ticks {
		if tick != uint64(i+1) {
			t.Fatalf("ticks = %v, want 1..6", ticks)
		}
	}
	if f := c.Advance(25 * step); f.FixedSteps != 25 || f.Lag != 0 {
		t.Fatalf("Advance under lag = %+v, want all 25 due steps planned", f)
	}
	drop := NewClock(LoopConfig{FixedStep: step, MaxSubsteps: 2})
	calls := 0
	if f := drop.AdvanceFunc(45*time.Millisecond, func(uint64) { calls++ }); f.FixedSteps != 2 || calls != 2 || f.DroppedDuration != 25*time.Millisecond {
		t.Fatalf("drop AdvanceFunc = %+v after %d calls, want Advance semantics", f, calls)
	}
}

// A snapshot of a lagging runtime carries the owed steps, so the restored
// runtime runs the same ticks the original would.
func TestCatchUpLagSnapshotCarriesLag(t *testing.T) {
	const step = 10 * time.Millisecond
	newRT := func(wall *fakeWall, ticks *[]uint64) *Runtime {
		return New(Config{
			FixedStep: step, MaxDelta: time.Second, CatchUp: CatchUpLag, CatchUpBudget: 10 * time.Millisecond, WallClock: wall.read,
			Systems: []System{stepCost(wall, ticks, func(uint64) time.Duration { return 6 * time.Millisecond })},
		})
	}
	wallA, wallB := &fakeWall{}, &fakeWall{}
	var ticksA, ticksB []uint64
	a, b := newRT(wallA, &ticksA), newRT(wallB, &ticksB)
	a.Step(87 * time.Millisecond)
	if a.Frame().Lag != 6*step {
		t.Fatalf("lag = %v, want 6 steps", a.Frame().Lag)
	}
	snap := a.Snapshot()
	if !strings.Contains(string(snap), `"lagNanos":60000000`) {
		t.Fatalf("snapshot has no lag: %s", snap)
	}
	if err := b.RestoreSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	if b.Frame().Lag != 6*step || b.clock.accumulator != 7*time.Millisecond {
		t.Fatalf("restored lag %v remainder %v", b.Frame().Lag, b.clock.accumulator)
	}
	ticksA = ticksA[:0]
	for i := 0; i < 5; i++ {
		fa, _ := a.Step(13 * time.Millisecond)
		fb, _ := b.Step(13 * time.Millisecond)
		if fa != fb {
			t.Fatalf("frame %d: original %+v, restored %+v", i, fa, fb)
		}
	}
	if len(ticksA) != len(ticksB) || ticksA[0] != ticksB[0] || ticksA[len(ticksA)-1] != ticksB[len(ticksB)-1] {
		t.Fatalf("ticks differ: %v vs %v", ticksA, ticksB)
	}
	// A drop runtime that loads the snapshot runs the owed steps within its
	// MaxSubsteps instead of keeping them forever.
	drop := New(Config{FixedStep: step, MaxSubsteps: 2})
	if err := drop.RestoreSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	if f, _ := drop.Step(0); f.FixedSteps != 2 || f.Lag != 0 {
		t.Fatalf("drop runtime after a lag snapshot = %+v, want 2 steps and no lag", f)
	}
}

func TestCatchUpLagSnapshotRejectsBadLag(t *testing.T) {
	rt := New(Config{FixedStep: 10 * time.Millisecond, CatchUp: CatchUpLag})
	for _, lag := range []int64{-int64(10 * time.Millisecond), int64(15 * time.Millisecond)} {
		state := rt.runtimeState(true)
		state.LagNanos = lag
		data, _ := json.Marshal(state)
		if err := rt.RestoreSnapshot(data); err == nil || !strings.Contains(err.Error(), "lag") {
			t.Fatalf("lag %v: err = %v, want a lag error", time.Duration(lag), err)
		}
	}
}
