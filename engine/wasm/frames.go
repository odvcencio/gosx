package wasm

import "errors"

// Frame describes one animation frame delivered to an EachFrame loop.
type Frame struct {
	// Time is the browser's timestamp for the frame, in milliseconds. In an
	// engine worker it comes from the worker's frame clock.
	Time float64
	// Delta is the milliseconds since the previous frame this loop received. It is
	// zero on the first frame and on the first frame after the page was hidden,
	// so a paused page never feeds a long delta into a simulation or a camera.
	Delta float64
}

// StopFrames is returned from an EachFrame step to end the loop without an error.
var StopFrames = errors.New("stop frames")
