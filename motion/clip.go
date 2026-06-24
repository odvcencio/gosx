package motion

import "strconv"

// ClipChannel is one glTF animation channel targeting a node's transform property.
//
//   - Property: "translation" | "rotation" | "scale".
//   - Interp:   "LINEAR" | "STEP" | "CUBICSPLINE" (default/unknown → LINEAR).
//   - Times:    nKeys keyframe times (seconds).
//   - Values:   the flat glTF accessor data. For LINEAR/STEP this is nKeys*width;
//     for CUBICSPLINE it is nKeys*3*width laid out [inTangent, value, outTangent]
//     per key (width = 3 for translation/scale, 4 for rotation).
type ClipChannel struct {
	Node     int
	Property string
	Interp   string
	Times    []float64
	Values   []float64
}

// clipArity returns the motion arity and component width for a glTF transform
// property. translation/scale → vec3 (width 3); rotation → quat (width 4).
// Unknown properties return width 0 so the caller can skip the channel.
func clipArity(property string) (ValueArity, int) {
	switch property {
	case "translation", "scale":
		return ArityVec3, 3
	case "rotation":
		return ArityQuat, 4
	default:
		return 0, 0
	}
}

// clipInterp maps a glTF interpolation string to motion.Interp. "STEP" →
// InterpStep, "CUBICSPLINE" → InterpCubicSpline; everything else (including the
// empty string) → InterpLinear.
func clipInterp(s string) Interp {
	switch s {
	case "STEP":
		return InterpStep
	case "CUBICSPLINE":
		return InterpCubicSpline
	default:
		return InterpLinear
	}
}

// BuildClipTimeline builds an evaluable Timeline from a set of glTF animation
// channels. Each well-formed channel becomes one Track inside a Positioned child
// at absolute time 0.
//
// Mapping:
//   - translation / scale → ArityVec3 tracks; rotation → ArityQuat tracks (slerp).
//   - "LINEAR" → InterpLinear, "STEP" → InterpStep, "CUBICSPLINE" →
//     InterpCubicSpline with per-key InTangent/OutTangent populated from the
//     glTF value triplets; default/unknown → InterpLinear.
//   - Target.Ref = strconv.Itoa(Node) (matching the native consumer's
//     index-string keying convention).
//
// CUBICSPLINE accessor layout: for key i with component width w, the three
// triplet members live in the flat Values slice at:
//
//	inTangent  = Values[i*3*w + 0*w : i*3*w + 1*w]
//	value      = Values[i*3*w + 1*w : i*3*w + 2*w]
//	outTangent = Values[i*3*w + 2*w : i*3*w + 3*w]
//
// Malformed channels (unknown property, no keys, or a Values slice too short for
// the keyframe count and interpolation mode) are silently skipped — never a
// panic.
//
// PrepareTracks is run on the result with fresh interners, so every surviving
// Track gets a deterministic TargetID/PropID (first-seen order across channels)
// and the timeline is immediately evaluable.
//
// The returned duration is the larger of the maximum last-keyframe time across
// channels (clips with no usable keys yield 0).
func BuildClipTimeline(channels []ClipChannel) (*Timeline, float64) {
	children := make([]Positioned, 0, len(channels))
	var duration float64

	for _, ch := range channels {
		arity, w := clipArity(ch.Property)
		if w == 0 {
			continue // unknown property
		}
		nKeys := len(ch.Times)
		if nKeys == 0 {
			continue
		}

		interp := clipInterp(ch.Interp)

		// Guard: the flat Values slice must hold enough floats for this mode.
		need := nKeys * w
		if interp == InterpCubicSpline {
			need = nKeys * 3 * w
		}
		if len(ch.Values) < need {
			continue // malformed — skip rather than panic
		}

		keys := make([]Key, nKeys)
		if interp == InterpCubicSpline {
			for i := 0; i < nKeys; i++ {
				base := i * 3 * w
				inT := makeValue(arity, w, ch.Values[base:base+w])
				val := makeValue(arity, w, ch.Values[base+w:base+2*w])
				outT := makeValue(arity, w, ch.Values[base+2*w:base+3*w])
				keys[i] = Key{
					T:          ch.Times[i],
					Value:      val,
					InTangent:  &inT,
					OutTangent: &outT,
				}
			}
		} else {
			for i := 0; i < nKeys; i++ {
				offset := i * w
				keys[i] = Key{
					T:     ch.Times[i],
					Value: makeValue(arity, w, ch.Values[offset:offset+w]),
				}
			}
		}

		if last := ch.Times[nKeys-1]; last > duration {
			duration = last
		}

		track := Track{
			Target: Target{Kind: TargetSceneNode, Ref: strconv.Itoa(ch.Node)},
			Prop:   ch.Property,
			Keys:   keys,
			Interp: interp,
		}
		children = append(children, Positioned{
			At:    Position{Kind: PosAbs, Val: 0},
			Track: &track,
		})
	}

	tl := &Timeline{Children: children}

	// Deterministic TargetID/PropID assignment; refs are discarded (callers that
	// need the round-trip tables build their own interners).
	targets := NewInterner()
	props := NewInterner()
	PrepareTracks(tl, targets, props)

	return tl, duration
}

// makeValue copies the first w floats of src into a Value of the given arity.
// src must have length >= w (guaranteed by the BuildClipTimeline bounds check).
func makeValue(arity ValueArity, w int, src []float64) Value {
	var f [4]float64
	for j := 0; j < w; j++ {
		f[j] = src[j]
	}
	return Value{Arity: arity, F: f}
}
