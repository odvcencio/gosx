//go:build js && wasm

package wasm

import (
	"context"
	"fmt"
	"math"
	"syscall/js"
	"time"

	"m31labs.dev/gosx/game/host"
	"m31labs.dev/gosx/scene"
)

// sceneFrameWaiter owns one visibility subscription for a finite playback.
// Hidden documents retain no queued RAF; cancellation prevents late writes.
type sceneFrameWaiter struct {
	source         *host.FrameSourceJS
	changed        chan struct{}
	stopVisibility HandleFunc
}

func newSceneFrameWaiter() *sceneFrameWaiter {
	w := &sceneFrameWaiter{source: host.NewFrameSourceJS(), changed: make(chan struct{}, 1)}
	notify := func() {
		select {
		case w.changed <- struct{}{}:
		default:
		}
	}
	if root := js.Global().Get("__gosx"); root.Truthy() {
		if bridge := root.Get("worker"); bridge.Truthy() && bridge.Get("onVisibility").Type() == js.TypeFunction {
			w.stopVisibility = (Port{value: bridge}).OnVisibility(func(bool) { notify() })
			return w
		}
	}
	doc := js.Global().Get("document")
	if doc.Truthy() {
		listener := js.FuncOf(func(js.Value, []js.Value) any { notify(); return nil })
		doc.Call("addEventListener", "visibilitychange", listener)
		w.stopVisibility = func() {
			doc.Call("removeEventListener", "visibilitychange", listener)
			listener.Release()
		}
	}
	return w
}
func (w *sceneFrameWaiter) dispose() {
	if w.stopVisibility != nil {
		w.stopVisibility()
		w.stopVisibility = nil
	}
}
func (w *sceneFrameWaiter) next(ctx context.Context) (float64, bool, error) {
	if ctx == nil {
		return 0, false, fmt.Errorf("frame requires a context")
	}
	paused := false
	for {
		if err := ctx.Err(); err != nil {
			return 0, paused, err
		}
		if w.source.Hidden() {
			paused = true
			select {
			case <-w.changed:
				continue
			case <-ctx.Done():
				return 0, paused, ctx.Err()
			}
		}
		frame := make(chan float64, 1)
		id := w.source.RequestFrame(func(at float64) { frame <- at })
		select {
		case at := <-frame:
			return at, paused, nil
		case <-w.changed:
			w.source.CancelFrame(id)
			paused = true
		case <-ctx.Done():
			w.source.CancelFrame(id)
			return 0, paused, ctx.Err()
		}
	}
}

// NextFrame waits for a visible presentation opportunity without owning a
// permanent animation loop. It must be called from a goroutine.
func NextFrame(ctx context.Context) error {
	w := newSceneFrameWaiter()
	defer w.dispose()
	_, _, err := w.next(ctx)
	return err
}

// PlayFrames runs a finite procedural presentation and returns only after its
// final command transaction and subsequent visible frame. The sampler receives
// an exact final=true endpoint, even when Offset skips the complete animation.
// Explicitly skipped playbacks settle immediately when hidden; active playback
// pauses hidden time. Cancellation prevents all later samples and writes.
func (s Scene3D) PlayFrames(ctx context.Context, options FramePlaybackOptions, sample func(float64, bool) []scene.Command) error {
	if sample == nil {
		return fmt.Errorf("playback requires a sampler")
	}
	return playFrames(ctx, options, func(at float64, final bool) error { return s.Dispatch(ctx, sample(at, final)) })
}
func playFrames(ctx context.Context, options FramePlaybackOptions, apply func(float64, bool) error) error {
	return playFramesOnClock(ctx, options, apply, func() float64 {
		return js.Global().Get("performance").Call("now").Float()
	})
}

func playFramesOnClock(ctx context.Context, options FramePlaybackOptions, apply func(float64, bool) error, now func() float64) error {
	o, err := options.validate()
	if err != nil {
		return err
	}
	if ctx == nil || apply == nil {
		return fmt.Errorf("playback requires context and sampler")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	write := apply
	apply = func(at float64, final bool) error {
		if err := write(at, final); err != nil {
			return err
		}
		if o.Applied != nil {
			o.Applied(at, final)
		}
		return nil
	}
	waiter := newSceneFrameWaiter()
	defer waiter.dispose()
	elapsed := o.Offset
	// Skipping has no frames left to present. Commit its endpoint even when a
	// hidden document cannot deliver RAF, so callers can finish catching up.
	if elapsed >= o.Duration && waiter.source.Hidden() {
		return apply(o.Duration, true)
	}
	_, _, err = waiter.next(ctx)
	if err != nil {
		return err
	}
	// RAF timestamps describe the frame, rather than callback delivery. Earlier
	// callbacks can spend seconds rendering before this playback receives that
	// same timestamp. Only time after our first delivery belongs to this player.
	previous := now()
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		final := elapsed >= o.Duration
		if err = apply(elapsed, final); err != nil {
			return err
		}
		if final {
			_, _, err := waiter.next(ctx)
			return err
		}
		_, paused, err := waiter.next(ctx)
		if err != nil {
			return err
		}
		at := now()
		if !paused {
			elapsed = math.Min(o.Duration, elapsed+math.Max(0, at-previous)/1000*o.Rate)
		}
		previous = at
	}
}

// TransitionCamera moves from the effective mounted pose, so replacing an
// in-flight transition can start without snapping to either authored endpoint.
// Callers cancel the previous transition before starting its replacement.
func (s Scene3D) TransitionCamera(ctx context.Context, target scene.IRCamera, options CameraTransitionOptions) error {
	from, err := s.Camera()
	if err != nil {
		return err
	}
	if scene.SameCameraPose(from, target, .0001) {
		return nil
	}
	duration := math.Max(0, target.TransitionMS) / 1000
	if from.Kind != target.Kind {
		duration = 0
	}
	move := scene.CameraTransition{From: from, To: target, Duration: time.Duration(duration * float64(time.Second)), FOVArc: options.FOVArc}
	return playFrames(ctx, FramePlaybackOptions{Duration: duration}, func(at float64, _ bool) error {
		camera, _ := move.Sample(time.Duration(at * float64(time.Second)))
		return s.SetCamera(ctx, camera)
	})
}
