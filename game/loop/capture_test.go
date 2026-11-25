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

// When the host takes the clock, the share of a fixed step the page gathered
// on its own time is dropped, so the granted frames lie on whole steps on
// every page (Frame.Alpha 0 at a step a frame) however the page ran before;
// giving the time back and taking it again aligns again.
func TestCaptureClockStartsOnAStep(t *testing.T) {
	for _, before := range [][]float64{{0, 13}, {0, 16.6, 33.3, 47}, {0, 9}} {
		rt, fixedSteps := newCountingRuntime(t, 10*time.Millisecond)
		source := NewManualFrameSource()
		d := New(rt, source)
		host := &fakeCaptureHost{}
		d.capture = &captureClock{host: host}
		var frames []game.Frame
		d.Start(func(_ float64, f game.Frame) { frames = append(frames, f) })
		for _, ts := range before {
			source.Fire(ts) // the page on its own time
		}
		if last := frames[len(frames)-1]; last.Alpha == 0 {
			t.Fatalf("%v: the page's own time left no share of a step to drop", before)
		}
		ran := *fixedSteps
		host.stepMS, host.granted = 10, 3
		for i := 0; i < 3; i++ {
			source.Fire(100 + float64(i))
		}
		for _, f := range frames[len(frames)-3:] {
			if f.Alpha != 0 || f.FixedSteps != 1 {
				t.Fatalf("%v: a granted frame at alpha %v with %d steps, want 0 and 1", before, f.Alpha, f.FixedSteps)
			}
		}
		if *fixedSteps != ran+3 {
			t.Fatalf("%v: %d steps for 3 grants of one step", before, *fixedSteps-ran)
		}
		// The time back to the page, and the clock taken again.
		host.stepMS = 0
		source.Fire(200)
		source.Fire(204)
		host.stepMS, host.granted = 10, 4
		source.Fire(300)
		if f := frames[len(frames)-1]; f.Alpha != 0 || f.FixedSteps != 1 {
			t.Fatalf("%v: the clock taken again: alpha %v, %d steps", before, f.Alpha, f.FixedSteps)
		}
	}
}

// A page asks for the capture clock from its first frame only with the
// gosx-capture query parameter, with or without a value, among others.
func TestCaptureRequestedReadsTheQuery(t *testing.T) {
	for search, want := range map[string]bool{
		"":                                   false,
		"?":                                  false,
		"?gosx-capture":                      true,
		"?gosx-capture=1":                    true,
		"gosx-capture":                       true,
		"?a=1&gosx-capture&b=2":              true,
		"?gosx-padtest=8080.ab&gosx-capture": true,
		"?gosx-padtest=8080.ab":              false,
		"?gosx-captured=1":                   false,
		"?xgosx-capture":                     false,
		"?a=gosx-capture":                    false,
	} {
		if got := captureRequested(search); got != want {
			t.Errorf("captureRequested(%q) = %v, want %v", search, got, want)
		}
	}
}

// A page that did not ask for the clock runs on the frame source's time from
// its first frame, as in a player's window; when the host then writes a step,
// the clock takes over: it holds until a grant and steps one interval per
// grant, and a step of 0 hands the time back.
func TestCaptureClockRunsFreeUntilTheHostWritesAStep(t *testing.T) {
	rt, _ := newCountingRuntime(t, 10*time.Millisecond)
	source := NewManualFrameSource()
	d := New(rt, source)
	host := &fakeCaptureHost{} // set, step 0: what an unasked page reads before the host writes
	d.capture = &captureClock{host: host}
	var frames []game.Frame
	d.Start(func(_ float64, f game.Frame) { frames = append(frames, f) })

	source.Fire(0)
	source.Fire(16)
	if len(frames) != 2 || frames[1].RealDelta != 16*time.Millisecond {
		t.Fatalf("unasked page: %d frames, want 2 on the source's time", len(frames))
	}
	host.stepMS = 5 // the host takes the clock: held until a grant
	source.Fire(32)
	source.Fire(48)
	if len(frames) != 2 {
		t.Fatalf("a frame ran after the host set the clock without a grant: %d frames", len(frames))
	}
	host.granted = 1
	source.Fire(64)
	if len(frames) != 3 || frames[2].RealDelta != 5*time.Millisecond {
		t.Fatalf("granted frame: %d frames, last %v", len(frames), frames[len(frames)-1].RealDelta)
	}
	host.stepMS = 0 // and gives it back
	source.Fire(80)
	if len(frames) != 4 || frames[3].RealDelta != 16*time.Millisecond {
		t.Fatalf("free again: %d frames, last %v", len(frames), frames[len(frames)-1].RealDelta)
	}
}
