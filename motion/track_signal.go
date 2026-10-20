package motion

import (
	"fmt"
	"math"
	"strings"
)

// SampleTrack drives a typed Track with an absolute input, such as Playhead
// seconds or ScrollProgress. Key times use the input's units. Values hold before
// the first and after the last key; easing belongs to the outgoing key, with the
// track's Ease as its default. InterpStep holds until the next key.
//
// This reuses Track, Key, Ease and ExpandStagger from the native motion IR. It
// does not start another clock. Vectors and colors interpolate component-wise;
// unit quaternions use shortest-arc slerp. InterpCubicSpline uses the native
// duration-scaled Hermite tangents, ignoring easing on complete spline segments.
// Segments missing either tangent fall back to eased interpolation. Generators
// are rejected. Reduced motion holds the last value, matching the native evaluator.
// Target and resolved IDs are ignored;
// bind the returned signal with the usual Program binding methods.
func (p *Program) SampleTrack(id string, input SignalRef, track Track) (SignalRef, error) {
	if p == nil {
		return "", fmt.Errorf("motion: nil program")
	}
	if track.Gen != nil || track.Interp > InterpSlerp {
		return "", fmt.Errorf("motion: sampled track requires linear, step, cubic spline, or quaternion slerp keyframes")
	}
	if len(track.Keys) < 2 || len(track.Keys) > 4096 {
		return "", fmt.Errorf("motion: sampled track needs 2..4096 keys")
	}
	frames := make([]MotionKeyframe, len(track.Keys))
	arity := track.Keys[0].Value.Arity
	if arity > ArityColor || track.Interp == InterpSlerp && arity != ArityQuat {
		return "", fmt.Errorf("motion: invalid track arity or non-quaternion slerp")
	}
	for i, key := range track.Keys {
		if key.Value.Arity != arity {
			return "", fmt.Errorf("motion: track key %d must have the same arity as the first key", i)
		}
		ease := track.Ease
		if key.Ease != nil {
			ease = *key.Ease
		}
		if track.Interp == InterpStep {
			ease = Ease{Kind: EaseSteps, Args: []float64{1}}
		}
		frames[i] = MotionKeyframe{At: key.T, Value: wireTrackValue(key.Value), Ease: wireEase(ease)}
		if track.Interp == InterpCubicSpline {
			for j, tangent := range []*Value{key.InTangent, key.OutTangent} {
				if tangent == nil {
					continue
				}
				if tangent.Arity != arity {
					return "", fmt.Errorf("motion: track key %d tangent must match the value arity", i)
				}
				if j == 0 {
					frames[i].InTangent = wireTrackValue(*tangent)
				} else {
					frames[i].OutTangent = wireTrackValue(*tangent)
				}
			}
		}
	}
	spec := SignalSpec{ID: strings.TrimSpace(id), Kind: SignalTrack, Input: input, Frames: frames, Arity: arity, Interp: track.Interp}
	if spec.ID == "" {
		return "", fmt.Errorf("motion: track signal id is required")
	}
	if err := validateTrackSignal(spec); err != nil {
		return "", err
	}
	return p.add(spec), nil
}

func wireTrackValue(value Value) any {
	if value.Arity == ArityScalar {
		return value.F[0]
	}
	return append([]float64(nil), value.F[:value.Arity.Width()]...)
}

func validateTrackSignal(signal SignalSpec) error {
	if strings.TrimSpace(string(signal.Input)) == "" || len(signal.Frames) < 2 || len(signal.Frames) > 4096 {
		return fmt.Errorf("motion: track %q needs an input and 2..4096 keys", signal.ID)
	}
	if signal.Arity > ArityColor || signal.Interp > InterpSlerp || signal.Interp == InterpSlerp && signal.Arity != ArityQuat {
		return fmt.Errorf("motion: track %q has invalid value arity or interpolation", signal.ID)
	}
	previous := math.Inf(-1)
	for i, frame := range signal.Frames {
		if !validTrackValue(frame.Value, signal.Arity, true) || !finiteTrackNumber(frame.At) || !(frame.At > previous) {
			return fmt.Errorf("motion: track %q key %d must have a finite scalar or correctly sized vector value, unit quaternion, and strictly increasing time", signal.ID, i)
		}
		for _, tangent := range []any{frame.InTangent, frame.OutTangent} {
			if tangent != nil && (signal.Interp != InterpCubicSpline || !validTrackValue(tangent, signal.Arity, false)) {
				return fmt.Errorf("motion: track %q key %d needs finite tangents matching the spline value arity", signal.ID, i)
			}
		}
		previous = frame.At
		if err := validateTrackEase(frame.Ease); err != nil {
			return fmt.Errorf("motion: track %q key %d: %w", signal.ID, i, err)
		}
	}
	return nil
}

func validTrackValue(value any, arity ValueArity, unitQuaternion bool) bool {
	if arity == ArityScalar {
		number, ok := value.(float64)
		return ok && finiteTrackNumber(number)
	}
	var values []float64
	switch v := value.(type) {
	case []float64:
		values = v
	case []any:
		if len(v) != arity.Width() {
			return false
		}
		values = make([]float64, len(v))
		for i, component := range v {
			var ok bool
			values[i], ok = component.(float64)
			if !ok {
				return false
			}
		}
	default:
		return false
	}
	if len(values) != arity.Width() {
		return false
	}
	var norm float64
	for _, component := range values {
		if !finiteTrackNumber(component) {
			return false
		}
		norm += component * component
	}
	return !unitQuaternion || arity != ArityQuat || math.Abs(norm-1) <= 1e-6
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
