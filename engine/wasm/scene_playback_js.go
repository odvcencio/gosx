//go:build js && wasm

package wasm

import (
	"context"
	"fmt"
	"math"
	"syscall/js"
	"time"

	"m31labs.dev/gosx/game/loop"
	"m31labs.dev/gosx/scene"
)

// sceneFrameWaiter owns one visibility subscription for a finite playback.
// Hidden documents retain no queued RAF; cancellation prevents late writes.
type sceneFrameWaiter struct {
	source   *loop.FrameSourceJS
	doc      js.Value
	changed  chan struct{}
	listener js.Func
}

func newSceneFrameWaiter() *sceneFrameWaiter {
	w := &sceneFrameWaiter{source: loop.NewFrameSourceJS(), doc: js.Global().Get("document"), changed: make(chan struct{}, 1)}
	w.listener = js.FuncOf(func(js.Value, []js.Value) any {
		select {
		case w.changed <- struct{}{}:
		default:
		}
		return nil
	})
	w.doc.Call("addEventListener", "visibilitychange", w.listener)
	return w
}
func (w *sceneFrameWaiter) dispose() {
	w.doc.Call("removeEventListener", "visibilitychange", w.listener)
	w.listener.Release()
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
// Hidden time is paused. Cancellation prevents all later samples and writes.
func (s Scene3D) PlayFrames(ctx context.Context, options FramePlaybackOptions, sample func(float64, bool) []scene.Command) error {
	if sample == nil {
		return fmt.Errorf("playback requires a sampler")
	}
	return playFrames(ctx, options, func(at float64, final bool) error { return s.Dispatch(ctx, sample(at, final)) })
}
func playFrames(ctx context.Context, options FramePlaybackOptions, apply func(float64, bool) error) error {
	o, err := options.validate()
	if err != nil {
		return err
	}
	if ctx == nil || apply == nil {
		return fmt.Errorf("playback requires context and sampler")
	}
	waiter := newSceneFrameWaiter()
	defer waiter.dispose()
	elapsed := o.Offset
	previous, _, err := waiter.next(ctx)
	if err != nil {
		return err
	}
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
		at, paused, err := waiter.next(ctx)
		if err != nil {
			return err
		}
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
