package scene

import (
	"errors"
	"time"
)

// Timed pose frames carry a simulation's state to a Scene3D mount at the
// simulation's own tick rate, for the page to interpolate. A game whose
// simulation runs in a Web Worker (engine.ThreadWorker) cannot write poses
// inside the renderer's frame, so it sends one pose frame per tick, stamped
// with the simulation time the poses hold. The page keeps the newest frames
// for the mount and, before every frame it draws, applies the poses
// interpolated at the newest time minus a delay of one tick (plus 2 ms for
// arrival jitter). Motion then stays smooth at the display rate while the
// simulation ticks at 30 Hz, and the main thread does no simulation work.
//
// Interpolation is linear for position and scale, takes the shortest arc for
// each rotation angle, and advances AnimationTime linearly while the clip
// stays the same (send ever-growing clip times with AnimationLoop set and let
// the renderer wrap them). When an instance changes clip, the newer frame's
// clip and time apply at once. Instances pair up by position within a batch;
// keep the batch's members and order the same from frame to frame.
//
// The page installs the mount's frame hook (docs/engine-frame-loop.md) for
// timed frames, so do not also drive the same mount with a
// game/host.SceneFrameSource. See docs/game-performance.md for the cost and
// the added latency.

// TimedPoseOptions tunes how the page interpolates timed pose frames. The
// zero value selects the defaults.
type TimedPoseOptions struct {
	// Delay is how far behind the newest frame the page draws. Zero selects
	// one step plus 2 ms. A shorter delay lowers latency and makes the page
	// hold the newest pose whenever a frame is late.
	Delay time.Duration
}

var errTimedPoseStep = errors.New("scene: timed pose frame needs a positive step")

func timedPoseMillis(at, step, delay time.Duration) (atMS, stepMS, delayMS float64, err error) {
	if step <= 0 {
		return 0, 0, 0, errTimedPoseStep
	}
	if at < 0 {
		return 0, 0, 0, errors.New("scene: timed pose frame time is negative")
	}
	if delay < 0 {
		delay = 0
	}
	return float64(at) / float64(time.Millisecond), float64(step) / float64(time.Millisecond), float64(delay) / float64(time.Millisecond), nil
}
