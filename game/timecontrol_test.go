package game

import (
	"testing"
	"time"
)

func TestClockPauseFreezesGameTimeButCountsFrames(t *testing.T) {
	c := NewClock(LoopConfig{FixedStep: 10 * time.Millisecond})
	c.Advance(20 * time.Millisecond)
	c.SetPaused(true)
	f := c.Advance(50 * time.Millisecond)
	if f.FixedSteps != 0 || f.Delta != 0 || f.RealDelta != 50*time.Millisecond || f.Time != 20*time.Millisecond || !f.Paused || f.Index != 2 {
		t.Fatalf("paused frame %+v", f)
	}
	c.SetPaused(false)
	if f := c.Advance(10 * time.Millisecond); f.FixedSteps != 1 || f.Time != 30*time.Millisecond || f.Paused {
		t.Fatalf("resumed frame %+v", f)
	}
}

func TestClockTimeScale(t *testing.T) {
	c := NewClock(LoopConfig{FixedStep: 10 * time.Millisecond})
	c.SetTimeScale(2.5)
	f := c.Advance(8 * time.Millisecond) // 20 ms of game time
	if f.FixedSteps != 2 || f.Delta != 20*time.Millisecond || f.TimeScale != 2.5 {
		t.Fatalf("scaled frame %+v", f)
	}
	c.SetTimeScale(-3) // ignored
	if c.TimeScale() != 2.5 {
		t.Fatal("negative scale accepted")
	}
	c.SetTimeScale(0)
	if f := c.Advance(time.Second / 4); f.FixedSteps != 0 {
		t.Fatalf("scale 0 stepped: %+v", f)
	}
}

func TestClockScaleKeepsFractionalNanoseconds(t *testing.T) {
	c := NewClock(LoopConfig{FixedStep: time.Second})
	c.SetTimeScale(0.3)
	var total time.Duration
	for i := 0; i < 10000; i++ {
		total += c.Advance(time.Millisecond).Delta
	}
	// 10000 x 1 ms x 0.3 = 3 s exactly; truncation per frame must not lose it.
	if total < 3*time.Second-time.Nanosecond || total > 3*time.Second+time.Nanosecond {
		t.Fatalf("total %v", total)
	}
}

// One game year per real minute with one-hour steps: no extra substeps beyond
// a few per frame.
func TestCoarseStepsReachYearPerMinuteRate(t *testing.T) {
	rt := New(Config{FixedStep: time.Hour})
	rt.SetTimeScale(525600)
	maxSteps := 0
	for i := 0; i < 60; i++ { // one real second at 60 fps
		f, err := rt.Step(time.Second / 60)
		if err != nil {
			t.Fatal(err)
		}
		if f.FixedSteps > maxSteps {
			maxSteps = f.FixedSteps
		}
	}
	got := rt.Frame().Time
	if got < 145*time.Hour || got > 146*time.Hour || maxSteps > 3 {
		t.Fatalf("game time %v after 1 real second, max steps/frame %d", got, maxSteps)
	}
}

func TestCoarseFixedStepIsNotClampedByDefaultMaxDelta(t *testing.T) {
	rt := New(Config{FixedStep: time.Hour})
	rt.Tick(nil)
	if rt.Frame().Time != time.Hour {
		t.Fatalf("time %v", rt.Frame().Time)
	}
}

func TestEverySkipsFixedStepsAndAccumulatesDelta(t *testing.T) {
	var runs int
	var deltas []time.Duration
	var ticks []uint64
	sys := Every(3, Func("slow", PhaseFixedUpdate, func(ctx *Context) error {
		runs++
		deltas = append(deltas, ctx.Delta)
		ticks = append(ticks, ctx.Tick)
		return nil
	}))
	rt := New(Config{FixedStep: time.Hour, Systems: []System{sys}})
	for i := 0; i < 9; i++ {
		rt.Tick(nil)
	}
	if runs != 3 || deltas[0] != 3*time.Hour || ticks[0] != 3 || ticks[2] != 9 {
		t.Fatalf("runs=%d deltas=%v ticks=%v", runs, deltas, ticks)
	}
	if sys.Name() != "slow" || sys.Phase() != PhaseFixedUpdate {
		t.Fatal("wrapper must keep name and phase")
	}
}

func TestEveryAcrossFramesWithManySteps(t *testing.T) {
	var runs int
	rt := New(Config{FixedStep: 10 * time.Millisecond, Systems: []System{
		Every(2, Func("s", PhaseFixedUpdate, func(*Context) error { runs++; return nil })),
	}})
	rt.Step(40 * time.Millisecond) // steps 1-4
	rt.Step(30 * time.Millisecond) // steps 5-7
	if runs != 3 {                 // steps 2, 4, 6
		t.Fatalf("runs=%d", runs)
	}
}

func TestEveryIntervalRunsOnGameTimeNotFrames(t *testing.T) {
	var deltas []time.Duration
	rt := New(Config{FixedStep: time.Minute, Systems: []System{
		EveryInterval(24*time.Hour, Func("daily", PhaseFixedUpdate, func(ctx *Context) error {
			deltas = append(deltas, ctx.Delta)
			return nil
		})),
	}})
	rt.SetTimeScale(1)
	for i := 0; i < 24*60*2; i++ {
		rt.Tick(nil)
	}
	if len(deltas) != 2 || deltas[0] != 24*time.Hour {
		t.Fatalf("deltas %v", deltas)
	}
}

func TestSnapshotKeepsPauseAndScale(t *testing.T) {
	rt := New(Config{})
	rt.SetTimeScale(4)
	rt.SetPaused(true)
	other := New(Config{})
	other.Restore(rt.Snapshot())
	if other.TimeScale() != 4 || !other.Paused() {
		t.Fatalf("scale=%v paused=%v", other.TimeScale(), other.Paused())
	}
	old := New(Config{})
	old.SetTimeScale(3)
	old.Restore([]byte(`{"frame":2,"timeSeconds":1}`)) // legacy snapshot: leave settings alone
	if old.TimeScale() != 3 {
		t.Fatal("legacy restore reset time scale")
	}
}
