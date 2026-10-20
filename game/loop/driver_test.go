package loop

import (
	"testing"
	"time"

	"m31labs.dev/gosx/game"
)

func newCountingRuntime(t *testing.T, fixedStep time.Duration) (*game.Runtime, *int) {
	t.Helper()
	count := 0
	system := game.Func("count", game.PhaseFixedUpdate, func(*game.Context) error {
		count++
		return nil
	})
	rt := game.New(game.Config{
		FixedStep:   fixedStep,
		MaxSubsteps: 10,
		MaxDelta:    time.Second,
		Systems:     []game.System{system},
	})
	return rt, &count
}

func TestDriverStepsRuntimeOnEachFiredFrame(t *testing.T) {
	rt, fixedSteps := newCountingRuntime(t, 10*time.Millisecond)
	source := NewManualFrameSource()
	d := New(rt, source)

	renders := 0
	d.Start(func(alpha float64, frame game.Frame) { renders++ })

	if !source.Pending() {
		t.Fatal("expected Start to request the first frame")
	}
	source.Fire(0)
	source.Fire(10) // 10ms delta -> exactly one fixed step
	source.Fire(25) // 15ms delta -> one more fixed step, 5ms carries over

	if renders != 3 {
		t.Fatalf("renders = %d, want 3", renders)
	}
	if *fixedSteps != 2 {
		t.Fatalf("fixed steps = %d, want 2", *fixedSteps)
	}
}

func TestDriverFirstFrameHasZeroDelta(t *testing.T) {
	rt, fixedSteps := newCountingRuntime(t, 10*time.Millisecond)
	source := NewManualFrameSource()
	d := New(rt, source)

	var firstFrame game.Frame
	first := true
	d.Start(func(alpha float64, frame game.Frame) {
		if first {
			firstFrame = frame
			first = false
		}
	})
	source.Fire(1000) // first callback: no prior timestamp, so delta must be 0.

	if firstFrame.Delta != 0 {
		t.Fatalf("first frame delta = %s, want 0", firstFrame.Delta)
	}
	if *fixedSteps != 0 {
		t.Fatalf("fixed steps after first frame = %d, want 0", *fixedSteps)
	}
}

func TestDriverSkipsStepAndRenderWhileHidden(t *testing.T) {
	rt, _ := newCountingRuntime(t, 10*time.Millisecond)
	source := NewManualFrameSource()
	d := New(rt, source)

	renders := 0
	var lastFrame game.Frame
	d.Start(func(alpha float64, frame game.Frame) {
		renders++
		lastFrame = frame
	})
	source.Fire(0)
	if renders != 1 {
		t.Fatalf("renders after first visible frame = %d, want 1", renders)
	}

	source.SetHidden(true)
	source.Fire(500) // large gap while hidden: must not turn into one huge Step.
	source.Fire(510)
	if renders != 1 {
		t.Fatalf("renders while hidden = %d, want 1 (unchanged)", renders)
	}
	if !source.Pending() {
		t.Fatal("expected driver to keep requesting frames while hidden")
	}

	source.SetHidden(false)
	source.Fire(520)
	if renders != 2 {
		t.Fatalf("renders after becoming visible = %d, want 2", renders)
	}
	if lastFrame.Delta != 0 {
		t.Fatalf("first visible-again frame delta = %s, want 0 (no catch-up jump)", lastFrame.Delta)
	}
}

func TestDriverStopCancelsPendingFrame(t *testing.T) {
	rt, _ := newCountingRuntime(t, 10*time.Millisecond)
	source := NewManualFrameSource()
	d := New(rt, source)
	d.Start(func(float64, game.Frame) {})
	if !source.Pending() {
		t.Fatal("expected a pending frame after Start")
	}
	d.Stop()
	if source.Pending() {
		t.Fatal("expected Stop to cancel the pending frame")
	}
	if source.Fire(1000) {
		t.Fatal("expected Fire to report no pending callback after Stop")
	}
}

func TestDriverStartTwiceIsANoOp(t *testing.T) {
	rt, _ := newCountingRuntime(t, 10*time.Millisecond)
	source := NewManualFrameSource()
	d := New(rt, source)
	calls := 0
	d.Start(func(float64, game.Frame) { calls++ })
	d.Start(func(float64, game.Frame) { calls += 100 }) // must not replace the first render func mid-flight in a way that breaks single-flight scheduling
	source.Fire(0)
	source.Fire(10)
	if calls >= 100 {
		t.Fatalf("second Start call should have been a no-op, calls=%d", calls)
	}
}

func TestDriverOnErrorReceivesStepError(t *testing.T) {
	boom := errFixture{}
	system := game.Func("boom", game.PhaseUpdate, func(*game.Context) error { return boom })
	rt := game.New(game.Config{FixedStep: 10 * time.Millisecond, Systems: []game.System{system}})
	source := NewManualFrameSource()
	d := New(rt, source)
	var gotErr error
	d.OnError = func(err error) { gotErr = err }
	d.Start(nil)
	source.Fire(0)
	source.Fire(10)
	if gotErr != boom {
		t.Fatalf("OnError = %v, want %v", gotErr, boom)
	}
}

type errFixture struct{}

func (errFixture) Error() string { return "boom" }
