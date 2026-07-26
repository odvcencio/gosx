package assetpipe

import (
	"math"

	"m31labs.dev/gosx/assetpipe/gltfedit"
)

// MaxKeyframeLookahead bounds how many keyframes one decimation step may drop
// at once. The check that a drop is safe costs one comparison per dropped
// frame, so an unbounded window would cost the square of the keyframe count on
// a straight line. A window of 256 keeps the pass linear in practice.
const MaxKeyframeLookahead = 256

// reduceAnimations drops keyframes that linear interpolation reproduces.
//
// The pass only touches a LINEAR sampler. A STEP sampler holds a value until
// the next key, so no interior key is redundant. A CUBICSPLINE sampler stores
// tangents beside the value, and dropping a key there changes the curve.
func reduceAnimations(document *gltfedit.Document, opts OptimizeOptions, summary *optimizeSummary) {
	tolerance := opts.AnimationTolerance
	if tolerance == 0 {
		tolerance = DefaultAnimationTolerance
	}
	changed := false
	for animationIndex := range document.Animations {
		animation := &document.Animations[animationIndex]
		for samplerIndex := range animation.Samplers {
			sampler := &animation.Samplers[samplerIndex]
			if sampler.Interpolation != "" && sampler.Interpolation != "LINEAR" {
				continue
			}
			times, components, err := document.ReadAccessor(sampler.Input)
			if err != nil || components != 1 || len(times) < 3 {
				continue
			}
			values, valueComponents, err := document.ReadAccessor(sampler.Output)
			if err != nil || valueComponents <= 0 {
				continue
			}
			if len(values) != len(times)*valueComponents {
				continue
			}
			outputInfo := document.AccessorInfo(sampler.Output)
			if outputInfo.ComponentType != gltfedit.ComponentFloat {
				// A normalized integer output already costs little, and its own
				// rounding hides the tolerance the pass would use.
				continue
			}
			keep, maxError := decimateKeyframes(times, values, valueComponents, tolerance)
			summary.animationKeyframes += len(times)
			if len(keep) >= len(times) {
				continue
			}
			summary.animationDropped += len(times) - len(keep)
			summary.animationMaxError = math.Max(summary.animationMaxError, maxError)

			newTimes := make([]float64, 0, len(keep))
			newValues := make([]float64, 0, len(keep)*valueComponents)
			for _, frame := range keep {
				newTimes = append(newTimes, times[frame])
				newValues = append(newValues, values[frame*valueComponents:(frame+1)*valueComponents]...)
			}
			inputIndex, err := document.AddAccessor(newTimes, "SCALAR", gltfedit.ComponentFloat, false, 0)
			if err != nil {
				continue
			}
			outputIndex, err := document.AddAccessor(newValues, outputInfo.Type, gltfedit.ComponentFloat, false, 0)
			if err != nil {
				continue
			}
			sampler.Input = inputIndex
			sampler.Output = outputIndex
			changed = true
		}
	}
	if changed {
		document.MarkAnimationsChanged()
		document.CompactAccessors()
	}
}

// decimateKeyframes returns the keyframes to keep and the largest error the
// dropped keyframes leave behind, as a fraction of the value range.
//
// The pass keeps an anchor keyframe and extends the candidate window while
// linear interpolation between the anchor and the frame after the window
// reproduces every frame inside the window.
func decimateKeyframes(times, values []float64, components int, tolerance float64) ([]int, float64) {
	frames := len(times)
	span := valueSpan(values, components)
	limit := tolerance * span
	if span == 0 {
		limit = 0
	}
	keep := make([]int, 0, frames)
	keep = append(keep, 0)
	anchor := 0
	worst := 0.0
	next := 1
	for next < frames-1 {
		window := next
		accepted := -1
		acceptedError := 0.0
		for window < frames-1 && window-anchor <= MaxKeyframeLookahead {
			deviation, ok := windowFits(times, values, components, anchor, window+1, limit)
			if !ok {
				break
			}
			accepted = window
			acceptedError = deviation
			window++
		}
		if accepted < 0 {
			keep = append(keep, next)
			anchor = next
			next++
			continue
		}
		worst = math.Max(worst, acceptedError)
		next = accepted + 1
	}
	keep = append(keep, frames-1)
	if span > 0 {
		return keep, worst / span
	}
	return keep, 0
}

// windowFits reports whether dropping every frame between low and high keeps
// the error under limit.
func windowFits(times, values []float64, components, low, high int, limit float64) (float64, bool) {
	timeSpan := times[high] - times[low]
	worst := 0.0
	for frame := low + 1; frame < high; frame++ {
		alpha := 0.0
		if timeSpan > 0 {
			alpha = (times[frame] - times[low]) / timeSpan
		}
		for component := 0; component < components; component++ {
			a := values[low*components+component]
			b := values[high*components+component]
			predicted := a + (b-a)*alpha
			deviation := math.Abs(predicted - values[frame*components+component])
			if deviation > limit {
				return 0, false
			}
			worst = math.Max(worst, deviation)
		}
	}
	return worst, true
}

// valueSpan returns the largest range any single component covers.
func valueSpan(values []float64, components int) float64 {
	if components <= 0 || len(values) == 0 {
		return 0
	}
	span := 0.0
	for component := 0; component < components; component++ {
		low := math.Inf(1)
		high := math.Inf(-1)
		for index := component; index < len(values); index += components {
			low = math.Min(low, values[index])
			high = math.Max(high, values[index])
		}
		if high > low {
			span = math.Max(span, high-low)
		}
	}
	return span
}
