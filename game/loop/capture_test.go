package loop

import (
	"testing"
	"time"

	"m31labs.dev/gosx/game"
)

type fakeCaptureHost struct {
	unset   bool
	stepMS  float64
	granted uint64
	done    []uint64
}

func (h *fakeCaptureHost) read() (bool, float64, uint64) { return !h.unset, h.stepMS, h.granted }
func (h *fakeCaptureHost) announce(done uint64)          { h.done = append(h.done, done) }

// Under the capture clock the game steps one fixed interval per granted frame,
// whatever the frame source's timestamps, holds while no grant waits, and
// announces each stepped frame on the callback after it.
func TestCaptureClockStepsOneIntervalPerGrant(t *testing.T) {
	rt, fixedSteps := newCountingRuntime(t, 10*time.Millisecond)
	source := NewManualFrameSource()
	d := New(rt, source)
	host := &fakeCaptureHost{unset: true}
	d.capture = &captureClock{host: host}
	var frames []game.Frame
	d.Start(func(_ float64, f game.Frame) { frames = append(frames, f) })

	source.Fire(0) // the host has not set the clock: held
	if len(frames) != 0 {
		t.Fatal("a frame ran before the host set the clock")
	}
	host.unset, host.stepMS = false, 5
	source.Fire(10)
	source.Fire(500) // nothing granted: held
	if len(frames) != 0 || *fixedSteps != 0 {
		t.Fatalf("held frames rendered %d, fixed steps %d", len(frames), *fixedSteps)
	}
	host.granted = 3
	for _, ts := range []float64{900, 901, 5000, 5001} {
		source.Fire(ts)
	}
	if len(frames) != 3 {
		t.Fatalf("rendered %d frames for 3 grants", len(frames))
	}
	for i, f := range frames {
		if f.RealDelta != 5*time.Millisecond {
			t.Fatalf("frame %d real delta %v, want 5ms", i, f.RealDelta)
		}
	}
	if *fixedSteps != 1 {
		t.Fatalf("fixed steps %d after 15ms at 10ms", *fixedSteps)
	}
	if want := []uint64{1, 2, 3}; len(host.done) != 3 || host.done[0] != 1 || host.done[2] != 3 {
		t.Fatalf("announced %v, want %v", host.done, want)
	}
	// Turning the clock off returns the game to the source's time.
	host.stepMS = 0
	source.Fire(5017)
	if len(frames) != 4 || frames[3].RealDelta != 16*time.Millisecond {
		t.Fatalf("free-running frame: %d frames, last %v", len(frames), frames[len(frames)-1].RealDelta)
	}
}
