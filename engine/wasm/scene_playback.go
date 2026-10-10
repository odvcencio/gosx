package wasm

import (
	"fmt"
	"math"
)

// FramePlaybackOptions bounds presentation independently from simulation time.
// Duration and Offset are seconds. A zero Rate selects normal speed.
type FramePlaybackOptions struct{ Duration, Offset, Rate float64 }

// CameraTransitionOptions selects an optional temporary framing margin.
type CameraTransitionOptions struct{ FOVArc float64 }

func (o FramePlaybackOptions) validate() (FramePlaybackOptions, error) {
	for _, v := range []float64{o.Duration, o.Offset, o.Rate} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return o, fmt.Errorf("playback requires finite values")
		}
	}
	if o.Duration < 0 || o.Offset < 0 || o.Rate < 0 {
		return o, fmt.Errorf("playback requires nonnegative duration, offset and rate")
	}
	if o.Rate == 0 {
		o.Rate = 1
	}
	o.Offset = math.Min(o.Offset, o.Duration)
	return o, nil
}
