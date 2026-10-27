package game

import (
	"bytes"
	"fmt"
	"math"
	"testing"
	"time"
)

// maxGameTime is the largest game time a Duration holds: about 292 years.
const maxGameTime = time.Duration(math.MaxInt64)

// requireSoundFrame fails when a frame or the clock that produced it holds a
// value the clock must never hold. Each of these wraps around to a negative
// number when game time passes the end of a Duration, and a snapshot of such a
// clock cannot be loaded.
func requireSoundFrame(t *testing.T, c *Clock, f Frame) {
	t.Helper()
	switch {
	case f.Delta < 0, f.RealDelta < 0, f.Time < 0, f.DroppedDuration < 0:
		t.Fatalf("frame %d holds a negative time: %+v", f.Index, f)
	case f.FixedSteps < 0, f.FixedSteps > c.maxSubsteps:
		t.Fatalf("frame %d ran %d fixed steps, want 0 to %d", f.Index, f.FixedSteps, c.maxSubsteps)
	case !(f.Alpha >= 0 && f.Alpha <= 1):
		t.Fatalf("frame %d has alpha %v", f.Index, f.Alpha)
	case c.accumulator < 0, c.accumulator >= c.fixedStep:
		t.Fatalf("frame %d left an accumulator of %v with a step of %v", f.Index, c.accumulator, c.fixedStep)
	case !(c.scaleFrac >= 0 && c.scaleFrac < 1):
		t.Fatalf("frame %d left a scale fraction of %v", f.Index, c.scaleFrac)
	case c.elapsed < 0:
		t.Fatalf("frame %d left elapsed time %v", f.Index, c.elapsed)
	}
}

// A frame whose scaled time is beyond a Duration used to wrap to a negative
// number on amd64. The clock must count it as the largest Duration, run the fixed
// steps the frame allows and report the rest as dropped.
func TestClockSaturatesScaledTime(t *testing.T) {
	cases := []struct {
		name    string
		scale   float64
		delta   time.Duration
		dropped time.Duration // what the frame cannot run, with the time that exceeded a Duration
	}{
		// 5 h at 525,600 is 9.46e18 ns; a Duration holds 9.22e18. Together with the
		// longest Duration that the clock holds, the frame drops more than a Duration.
		{"README scale, a 5 hour frame", 525600, 5 * time.Hour, maxGameTime},
		{"scale beyond a float64 product", math.MaxFloat64, 5 * time.Hour, maxGameTime},
		{"scale of 1e300", 1e300, 250 * time.Millisecond, maxGameTime},
		// float64(MaxInt64) rounds up to 2^63, which no Duration holds, but nothing
		// exceeds it: the frame holds the longest Duration, and runs 5 h of it.
		{"the largest frame at scale 1", 1, maxGameTime, maxGameTime - 5*time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clock := NewClock(LoopConfig{FixedStep: time.Hour, MaxDelta: maxGameTime})
			clock.SetTimeScale(c.scale)
			f := clock.Advance(c.delta)
			requireSoundFrame(t, clock, f)
			if f.Delta != maxGameTime {
				t.Errorf("Delta %v, want the largest Duration %v", f.Delta, maxGameTime)
			}
			if f.FixedSteps != 5 || f.Time != 5*time.Hour {
				t.Errorf("ran %d steps to %v, want the frame limit of 5 steps to 5h", f.FixedSteps, f.Time)
			}
			if f.DroppedDuration != c.dropped {
				t.Errorf("DroppedDuration %v, want %v", f.DroppedDuration, c.dropped)
			}
			if clock.scaleFrac != 0 {
				t.Errorf("scale fraction %v after a saturated frame, want 0", clock.scaleFrac)
			}
			// The next frame is an ordinary one.
			clock.SetTimeScale(1)
			next := clock.Advance(time.Hour)
			requireSoundFrame(t, clock, next)
			if next.FixedSteps != 1 || next.Time != 6*time.Hour || next.DroppedDuration != 0 {
				t.Errorf("the frame after: %+v, want one step to 6h and nothing dropped", next)
			}
		})
	}
}

// The conversions at the edge of a Duration. Converting a float64 that is out of
// range is implementation-defined in Go (amd64 gives the most negative
// Duration, js/wasm saturates), so the clock must never do it.
func TestDurationHelpersStopAtTheEdges(t *testing.T) {
	for _, c := range []struct {
		ns   float64
		want time.Duration
	}{
		{0, 0}, {0.9, 0}, {2.9, 2}, {-1, 0}, {math.Inf(-1), 0}, {math.NaN(), 0},
		{1 << 62, 1 << 62},
		{twoPow63 - 1024, 1<<63 - 1024}, // the largest float64 below 2^63
		{twoPow63, maxGameTime}, {1e300, maxGameTime}, {math.Inf(1), maxGameTime},
	} {
		if got := floorDuration(c.ns); got != c.want {
			t.Errorf("floorDuration(%v) = %d, want %d", c.ns, got, c.want)
		}
	}
	for _, c := range []struct{ a, b, sum, over time.Duration }{
		{0, 0, 0, 0},
		{5, 7, 12, 0},
		{maxGameTime, 0, maxGameTime, 0},
		{maxGameTime - 5, 5, maxGameTime, 0},
		{maxGameTime - 5, 7, maxGameTime, 2},
		{maxGameTime, 1, maxGameTime, 1},
		{0, maxGameTime, maxGameTime, 0},
		{1, maxGameTime, maxGameTime, 1},
		{maxGameTime, maxGameTime, maxGameTime, maxGameTime},
	} {
		if sum, over := addDuration(c.a, c.b); sum != c.sum || over != c.over {
			t.Errorf("addDuration(%d, %d) = %d, %d; want %d, %d", c.a, c.b, sum, over, c.sum, c.over)
		}
	}
	for _, c := range []struct{ held, d, want time.Duration }{
		{0, 5, 5}, {5, 0, 5}, {maxGameTime - 5, 5, maxGameTime}, {maxGameTime - 5, 6, maxGameTime},
		{maxGameTime, maxGameTime, maxGameTime}, {10, -4, 6},
	} {
		if got := gather(c.held, c.d); got != c.want {
			t.Errorf("gather(%d, %d) = %d, want %d", c.held, c.d, got, c.want)
		}
	}
}

// A NaN scale fraction cannot come from a snapshot, because JSON has no NaN, but
// one in the clock would poison every later frame. The clock drops it and runs on.
func TestClockRecoversFromANaNScaleFraction(t *testing.T) {
	clock := NewClock(LoopConfig{FixedStep: 10 * time.Millisecond})
	clock.scaleFrac = math.NaN()
	f := clock.Advance(20 * time.Millisecond)
	requireSoundFrame(t, clock, f)
	if f.Delta != 0 || f.FixedSteps != 0 {
		t.Errorf("the frame with a NaN fraction: %+v, want no time", f)
	}
	f = clock.Advance(20 * time.Millisecond)
	requireSoundFrame(t, clock, f)
	if f.Delta != 20*time.Millisecond || f.FixedSteps != 2 || f.Time != 20*time.Millisecond {
		t.Errorf("the frame after: %+v, want 20ms and two steps", f)
	}
}

// The scaled time of a frame can fit in a Duration and still overflow it once
// added to the part of a step the clock holds.
func TestClockSaturatesTheAccumulator(t *testing.T) {
	clock := NewClock(LoopConfig{FixedStep: time.Hour, MaxDelta: maxGameTime})
	clock.accumulator = 30 * time.Minute
	f := clock.Advance(maxGameTime - 10*time.Minute) // fits alone; 20 min over with the accumulator
	requireSoundFrame(t, clock, f)
	if f.FixedSteps != 5 || f.Time != 5*time.Hour {
		t.Errorf("ran %d steps to %v, want 5 steps to 5h", f.FixedSteps, f.Time)
	}
	if f.DroppedDuration < maxGameTime-5*time.Hour {
		t.Errorf("DroppedDuration %v, want at least %v", f.DroppedDuration, maxGameTime-5*time.Hour)
	}
}

// Game time ends at about 292 years. The clock must stop there, not wrap to a
// negative time: it runs no further steps, keeps its time and reports the time
// it cannot run as dropped.
func TestClockStopsAtTheEndOfGameTime(t *testing.T) {
	const step = 50 * 365 * 24 * time.Hour // five steps (250 years) fit; a sixth would pass 292
	clock := NewClock(LoopConfig{FixedStep: step, MaxDelta: 5 * step})

	f := clock.Advance(5 * step)
	requireSoundFrame(t, clock, f)
	if f.FixedSteps != 5 || f.Time != 5*step || f.DroppedDuration != 0 {
		t.Fatalf("the first frame: %+v, want five steps to %v and nothing dropped", f, 5*step)
	}
	for i := 0; i < 3; i++ {
		f = clock.Advance(step)
		requireSoundFrame(t, clock, f)
		if f.FixedSteps != 0 || f.Time != 5*step || f.Steps != 5 {
			t.Fatalf("frame %d at the end of game time: %+v, want no step and time held at %v", f.Index, f, 5*step)
		}
		if f.Delta != step || f.DroppedDuration != step {
			t.Fatalf("frame %d at the end of game time: Delta %v dropped %v, want %v for both", f.Index, f.Delta, f.DroppedDuration, step)
		}
	}

	// A runtime at the end of game time must write a snapshot that loads.
	rt := New(Config{FixedStep: step})
	play(t, rt, 1, 5*step)
	play(t, rt, 2, step)
	if got := rt.Frame().Time; got != 5*step {
		t.Fatalf("runtime time %v, want %v", got, 5*step)
	}
	if err := New(Config{FixedStep: step}).RestoreSnapshot(rt.Snapshot()); err != nil {
		t.Fatalf("the snapshot of a runtime at the end of game time was refused: %v", err)
	}
}

// The README's year-per-minute setup scales a long real frame (a laptop that
// slept overnight) past what a Duration holds. The snapshot of that runtime must
// load, and the runtime it loads into must run as the original does.
func TestSnapshotLoadsAfterALongFrameAtReadmeScale(t *testing.T) {
	var straight, restored cadenceLog
	build := func(log *cadenceLog) *Runtime {
		noop := func(*Context) error { return nil }
		rt := New(Config{FixedStep: time.Hour, Systems: []System{
			Func("weather", PhaseFixedUpdate, noop),
			Every(24, Func("economy", PhaseFixedUpdate, recordInto(&log.every))),
			EveryInterval(30*24*time.Hour, Func("census", PhaseFixedUpdate, recordInto(&log.interval))),
		}})
		rt.SetTimeScale(525600)
		return rt
	}
	rt := build(&straight)
	play(t, rt, 3, 16*time.Millisecond)
	long := play(t, rt, 1, 5*time.Hour)[0] // one long real frame, clamped at MaxDelta
	if long.Delta < 0 || long.Time < 0 || long.FixedSteps != 5 {
		t.Fatalf("the long frame: %+v, want five steps and no negative time", long)
	}
	for _, f := range play(t, rt, 3, 16*time.Millisecond) {
		if f.FixedSteps == 0 {
			t.Fatalf("the clock is stuck after the long frame: %+v", f)
		}
	}

	snapshot := rt.Snapshot()
	other := build(&restored)
	if err := other.RestoreSnapshot(snapshot); err != nil {
		t.Fatalf("the runtime's own snapshot was refused: %v", err)
	}
	straight = cadenceLog{}
	want := play(t, rt, 600, 16*time.Millisecond) // about 58 game days
	got := play(t, other, 600, 16*time.Millisecond)
	requireSameFrames(t, want, got)
	requireSameRuns(t, straight, restored)
}

// The code before the clock saturated wrote this snapshot from a clock stuck by
// a long frame at the README scale: the stuck part, a negative accumulator, was
// not in the snapshot. It must still load, and the clock must run.
func TestSnapshotOfAStuckClockFromBeforeStillLoads(t *testing.T) {
	const stuck = `{"frame":7,"timeSeconds":25200,"timeNanos":25200000000000,"timeScale":525600,"paused":false}`
	rt := New(Config{FixedStep: time.Hour})
	if err := rt.RestoreSnapshot([]byte(stuck)); err != nil {
		t.Fatal(err)
	}
	f := play(t, rt, 1, 16*time.Millisecond)[0] // 2.3 game hours
	if f.Index != 8 || f.FixedSteps != 2 || f.Time != 9*time.Hour {
		t.Fatalf("the frame after the load: %+v, want frame 8 with two steps to 9h", f)
	}
}

// A scale that overflows the product of a frame and the scale used to leave a
// NaN scale fraction, which JSON cannot hold, so Snapshot returned nothing.
func TestSnapshotExistsAtAnEnormousTimeScale(t *testing.T) {
	rt := New(Config{})
	rt.SetTimeScale(1e300)
	play(t, rt, 2, 250*time.Millisecond)
	snapshot := rt.Snapshot()
	if len(snapshot) == 0 {
		t.Fatal("Snapshot returned nothing")
	}
	if err := New(Config{}).RestoreSnapshot(snapshot); err != nil {
		t.Fatalf("the snapshot was refused: %v", err)
	}
}

// A wrapper adds each frame to the time it has gathered. Past the end of a
// Duration that must stop at the largest Duration, not wrap to a negative
// number, which Restore refuses.
func TestCadenceGatheredTimeStopsAtTheEndOfDuration(t *testing.T) {
	const year = 365 * 24 * time.Hour
	var log cadenceLog
	build := func() *Runtime {
		return New(Config{FixedStep: year, Systems: []System{
			Every(1000, Func("slow", PhaseUpdate, recordInto(&log.every))),
		}})
	}
	rt := build()
	for i := 0; i < 300; i++ { // 300 years of frames; a Duration holds 292
		rt.Tick(nil)
	}
	states := rt.cadenceStates()
	if len(states) != 1 || states[0].GatheredNanos != math.MaxInt64 {
		t.Fatalf("gathered %+v, want the largest Duration", states)
	}

	snapshot := rt.Snapshot()
	other := build()
	if err := other.RestoreSnapshot(snapshot); err != nil {
		t.Fatalf("the snapshot of a runtime with a saturated wrapper was refused: %v", err)
	}
	if again := other.Snapshot(); !bytes.Equal(again, snapshot) {
		t.Fatalf("snapshot, restore, snapshot\n before %s\n after  %s", snapshot, again)
	}
	other.Tick(nil)
	if states := other.cadenceStates(); states[0].GatheredNanos != math.MaxInt64 {
		t.Fatalf("one more frame left %v gathered, want the largest Duration", time.Duration(states[0].GatheredNanos))
	}
}

// Whatever the clock does with an extreme step, scale or frame, the snapshot of
// the runtime must load into a new runtime, write the same bytes again and then
// run exactly as the original does. This sweeps the values in which a Duration
// wraps: coarse steps, huge scales and long frames, in both orders.
func TestSnapshotsRestoreAtExtremeClocks(t *testing.T) {
	steps := []time.Duration{time.Nanosecond, 10 * time.Millisecond, time.Hour, 50 * 365 * 24 * time.Hour}
	scales := []float64{0, 0.3, 1, 525600, 1e12, 1e300, math.MaxFloat64}
	deltas := []time.Duration{0, 1, 16 * time.Millisecond, 250 * time.Millisecond, time.Hour, 5 * time.Hour, 1000 * time.Hour, maxGameTime}
	codecs := []struct {
		name string
		make func() SnapshotCodec
	}{
		{"default", func() SnapshotCodec { return nil }},
		{"savecodec", func() SnapshotCodec { return NewSaveCodec().SnapshotCodec() }},
	}
	reversed := make([]time.Duration, len(deltas))
	for i, d := range deltas {
		reversed[len(deltas)-1-i] = d
	}
	noop := func(*Context) error { return nil }
	for _, c := range codecs {
		for _, step := range steps {
			for _, scale := range scales {
				build := func() *Runtime {
					rt := New(Config{FixedStep: step, Snapshot: c.make(), Systems: []System{
						Every(3, Func("frames", PhaseUpdate, noop)),
						EveryInterval(5*step/2, Func("gather", PhaseFixedUpdate, noop)),
						Every(1000, Func("slow", PhaseLateUpdate, noop)),
					}})
					rt.SetTimeScale(scale)
					return rt
				}
				for order, frames := range [][]time.Duration{deltas, reversed} {
					rt := build()
					for i, delta := range frames {
						where := fmt.Sprintf("%s, step %v, scale %v, order %d, frame %d of %v", c.name, step, scale, order, i+1, delta)
						f, err := rt.Step(delta)
						if err != nil {
							t.Fatal(err)
						}
						requireSoundFrame(t, rt.clock, f)
						snapshot := rt.Snapshot()
						if len(snapshot) == 0 {
							t.Fatalf("%s: Snapshot returned nothing", where)
						}
						other := build()
						if err := other.RestoreSnapshot(snapshot); err != nil {
							t.Fatalf("%s: the runtime's own snapshot was refused: %v\n%s", where, err, snapshot)
						}
						if again := other.Snapshot(); !bytes.Equal(again, snapshot) {
							t.Fatalf("%s: snapshot, restore, snapshot\n before %s\n after  %s", where, snapshot, again)
						}
						next := time.Duration(i+1) * 7 * time.Millisecond
						want, err := rt.Step(next)
						if err != nil {
							t.Fatal(err)
						}
						got, err := other.Step(next)
						if err != nil {
							t.Fatal(err)
						}
						if want != got {
							t.Fatalf("%s: the frame after the restore differs\n original %+v\n restored %+v", where, want, got)
						}
					}
				}
			}
		}
	}
}
