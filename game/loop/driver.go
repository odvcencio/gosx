package loop

import (
	"sync"
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

	// OnError, if set before Start, receives any non-nil error Step returns.
	OnError func(error)

	mu       sync.Mutex
	frameMu  sync.Mutex // serialize Step/render across a stop and restart
	running  bool
	handle   int
	lastTS   float64
	haveLast bool
	started  bool
	epoch    uint64
	callback func(float64)
	observer Observer
	now      func() time.Time
}

// New creates a Driver for runtime, riding source. It does not start
// scheduling frames — call Start.
func New(runtime *game.Runtime, source FrameSource) *Driver {
	return &Driver{runtime: runtime, source: source, now: time.Now}
}

// Start begins scheduling frames and calls render, which may be nil, after
// every advanced frame. Calling Start while already running is a no-op.
func (d *Driver) Start(render RenderFunc) {
	if d == nil || d.runtime == nil || d.source == nil {
		return
	}
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return
	}
	d.render = render
	d.running, d.started = true, true
	d.haveLast = false
	d.epoch++
	epoch := d.epoch
	d.callback = func(ts float64) { d.onFrame(epoch, ts) }
	d.mu.Unlock()
	d.scheduleNext(epoch)
}

// Stop cancels the pending frame request, if any. A stopped Driver can be
// restarted with Start.
func (d *Driver) Stop() {
	if d == nil {
		return
	}
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return
	}
	d.running = false
	handle := d.handle
	d.epoch++
	d.haveLast = false
	d.mu.Unlock()
	d.source.CancelFrame(handle)
}

// Running reports whether the driver is currently scheduling frames.
func (d *Driver) Running() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.running
}

func (d *Driver) scheduleNext(epoch uint64) {
	d.mu.Lock()
	if !d.running || d.epoch != epoch {
		d.mu.Unlock()
		return
	}
	callback := d.callback
	d.mu.Unlock()
	handle := d.source.RequestFrame(callback)
	d.mu.Lock()
	keep := d.running && d.epoch == epoch
	if keep {
		d.handle = handle
	}
	d.mu.Unlock()
	if !keep {
		d.source.CancelFrame(handle)
	}
}

func (d *Driver) onFrame(epoch uint64, timestampMS float64) {
	observer, event := d.advanceFrame(epoch, timestampMS)
	if observer != nil {
		observer.ObserveFrame(event)
	}
	d.scheduleNext(epoch)
}

func (d *Driver) advanceFrame(epoch uint64, timestampMS float64) (Observer, FrameEvent) {
	d.frameMu.Lock()
	defer d.frameMu.Unlock()
	hidden := d.source.Hidden()
	d.mu.Lock()
	if !d.running || d.epoch != epoch {
		d.mu.Unlock()
		return nil, FrameEvent{}
	}
	if hidden {
		// Keep scheduling so the driver notices when the tab becomes visible
		// again, but advance neither game time nor the render callback while
		// hidden. haveLast resets so the first frame after hidden reports a
		// zero delta instead of the whole hidden duration as one jump.
		d.haveLast = false
		d.mu.Unlock()
		return nil, FrameEvent{}
	}
	deltaMS := 0.0
	if d.haveLast {
		deltaMS = timestampMS - d.lastTS
		if deltaMS < 0 {
			deltaMS = 0
		}
	}
	d.lastTS, d.haveLast = timestampMS, true
	observer, render, onError := d.observer, d.render, d.OnError
	d.mu.Unlock()
	interval := time.Duration(deltaMS * float64(time.Millisecond))
	var start time.Time
	if observer != nil {
		start = d.now()
	}
	frame, err := d.runtime.Step(interval)
	if err != nil && onError != nil {
		onError(err)
	}
	if render != nil {
		render(frame.Alpha, frame)
	}
	if observer == nil {
		return nil, FrameEvent{}
	}
	return observer, FrameEvent{Duration: d.now().Sub(start), Interval: interval}
}
