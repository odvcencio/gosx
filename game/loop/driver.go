package loop

import (
	"time"

	"m31labs.dev/gosx/game"
)

// FrameSource is the host clock a Driver rides. FrameSourceJS (built only
// under GOOS=js/GOARCH=wasm) wraps window.requestAnimationFrame and
// document.hidden; ManualFrameSource is a native, test-driven fake.
type FrameSource interface {
	// RequestFrame schedules cb to run exactly once, at the next animation
	// frame, with the host timestamp in milliseconds. It returns a handle
	// CancelFrame can use to cancel the request before it fires.
	RequestFrame(cb func(timestampMS float64)) int
	// CancelFrame cancels a pending RequestFrame call. Canceling a handle
	// that already fired, or was never issued, is a no-op.
	CancelFrame(handle int)
	// Hidden reports whether the host surface is currently hidden, for
	// example a backgrounded browser tab.
	Hidden() bool
}

// RenderFunc is called once per advanced frame, after the fixed-step
// accumulator inside game.Runtime.Step has run, with that frame's
// interpolation alpha for rendering between the last two simulated states.
type RenderFunc func(alpha float64, frame game.Frame)

// Driver drives a game.Runtime's Step from a FrameSource, replacing the
// js.FuncOf/requestAnimationFrame glue a game would otherwise have to write
// itself. The zero value is not usable; construct one with New.
type Driver struct {
	runtime *game.Runtime
	source  FrameSource
	render  RenderFunc

	// OnError, if set, is called with any non-nil error Step returns.
	OnError func(error)

	running  bool
	handle   int
	lastTS   float64
	haveLast bool
}

// New creates a Driver for runtime, riding source. It does not start
// scheduling frames — call Start.
func New(runtime *game.Runtime, source FrameSource) *Driver {
	return &Driver{runtime: runtime, source: source}
}

// Start begins scheduling frames and calls render, which may be nil, after
// every advanced frame. Calling Start while already running is a no-op.
func (d *Driver) Start(render RenderFunc) {
	if d == nil || d.runtime == nil || d.source == nil || d.running {
		return
	}
	d.render = render
	d.running = true
	d.haveLast = false
	d.scheduleNext()
}

// Stop cancels the pending frame request, if any. A stopped Driver can be
// restarted with Start.
func (d *Driver) Stop() {
	if d == nil || !d.running {
		return
	}
	d.running = false
	d.source.CancelFrame(d.handle)
	d.haveLast = false
}

// Running reports whether the driver is currently scheduling frames.
func (d *Driver) Running() bool {
	return d != nil && d.running
}

func (d *Driver) scheduleNext() {
	d.handle = d.source.RequestFrame(d.onFrame)
}

func (d *Driver) onFrame(timestampMS float64) {
	if d == nil || !d.running {
		return
	}
	if d.source.Hidden() {
		// Keep scheduling so the driver notices when the tab becomes visible
		// again, but advance neither game time nor the render callback while
		// hidden. haveLast resets so the first frame after hidden reports a
		// zero delta instead of the whole hidden duration as one jump.
		d.haveLast = false
	} else {
		deltaMS := 0.0
		if d.haveLast {
			deltaMS = timestampMS - d.lastTS
			if deltaMS < 0 {
				deltaMS = 0
			}
		}
		d.lastTS = timestampMS
		d.haveLast = true
		frame, err := d.runtime.Step(time.Duration(deltaMS * float64(time.Millisecond)))
		if err != nil && d.OnError != nil {
			d.OnError(err)
		}
		if d.render != nil {
			d.render(frame.Alpha, frame)
		}
	}
	if d.running {
		d.scheduleNext()
	}
}
