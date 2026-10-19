package motion

import (
	"fmt"
	"math"
	"strings"
)

// SampleTrack drives a scalar Track with an absolute input, such as Playhead
// seconds or ScrollProgress. Key times use the input's units. Values hold before
// the first and after the last key; easing belongs to the outgoing key, with the
// track's Ease as its default. InterpStep holds until the next key.
//
// This reuses Track, Key, Ease and ExpandStagger from the native motion IR. It
// does not start another clock. Generators, vectors, quaternions and tangent
// splines are not scalar signals and are rejected. Reduced motion holds the
// last value, matching the native evaluator. Target and resolved IDs are ignored;
// bind the returned signal with the usual Program binding methods.
func (p *Program) SampleTrack(id string, input SignalRef, track Track) (SignalRef, error) {
	if p == nil {
		return "", fmt.Errorf("motion: nil program")
	}
	if track.Gen != nil || track.Interp != InterpLinear && track.Interp != InterpStep {
		return "", fmt.Errorf("motion: scalar track requires linear or step keyframes")
	}
	if len(track.Keys) < 2 || len(track.Keys) > 4096 {
		return "", fmt.Errorf("motion: scalar track needs 2..4096 keys")
	}
	frames := make([]MotionKeyframe, len(track.Keys))
	for i, key := range track.Keys {
		if key.Value.Arity != ArityScalar {
			return "", fmt.Errorf("motion: track key %d must be scalar", i)
		}
		ease := track.Ease
		if key.Ease != nil {
			ease = *key.Ease
		}
		if track.Interp == InterpStep {
			ease = Ease{Kind: EaseSteps, Args: []float64{1}}
		}
		frames[i] = MotionKeyframe{At: key.T, Value: key.Value.F[0], Ease: wireEase(ease)}
	}
	spec := SignalSpec{ID: strings.TrimSpace(id), Kind: SignalTrack, Input: input, Frames: frames}
	if spec.ID == "" {
		return "", fmt.Errorf("motion: track signal id is required")
	}
	if err := validateTrackSignal(spec); err != nil {
		return "", err
	}
	return p.add(spec), nil
}

func validateTrackSignal(signal SignalSpec) error {
	if strings.TrimSpace(string(signal.Input)) == "" || len(signal.Frames) < 2 || len(signal.Frames) > 4096 {
		return fmt.Errorf("motion: track %q needs an input and 2..4096 keys", signal.ID)
	}
	previous := math.Inf(-1)
	for i, frame := range signal.Frames {
		value, ok := frame.Value.(float64)
		if !ok || !finiteTrackNumber(value) || !finiteTrackNumber(frame.At) || !(frame.At > previous) {
			return fmt.Errorf("motion: track %q key %d must have a finite scalar value and strictly increasing time", signal.ID, i)
		}
		previous = frame.At
		if err := validateTrackEase(frame.Ease); err != nil {
			return fmt.Errorf("motion: track %q key %d: %w", signal.ID, i, err)
		}
	}
	return nil
}

func finiteTrackNumber(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func validateTrackEase(ease *MotionEase) error {
	if ease == nil {
		return nil
	}
	if ease.Kind > EaseInOutBack || len(ease.Args) > 4 {
		return fmt.Errorf("invalid easing kind or argument count")
	}
	for _, arg := range ease.Args {
		if !finiteTrackNumber(arg) {
			return fmt.Errorf("easing arguments must be finite")
		}
	}
	if ease.Kind >= EaseInPow && ease.Kind <= EaseInOutPow && len(ease.Args) > 0 && ease.Args[0] <= 0 {
		return fmt.Errorf("easing exponent must be positive")
	}
	if ease.Kind == EaseSteps && len(ease.Args) > 0 && (ease.Args[0] < 1 || math.Trunc(ease.Args[0]) != ease.Args[0]) {
		return fmt.Errorf("easing steps must be a positive integer")
	}
	if ease.Kind == EaseCubicBezier && (len(ease.Args) != 4 || ease.Args[0] < 0 || ease.Args[0] > 1 || ease.Args[2] < 0 || ease.Args[2] > 1) {
		return fmt.Errorf("bezier needs four arguments with x in [0,1]")
	}
	return nil
}
