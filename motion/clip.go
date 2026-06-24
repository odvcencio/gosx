package motion

import "strconv"

// Fixed glTF property → PropID mapping. These constants are the stable,
// cross-clip IDs used by BuildClipTimeline so every clip assigns the same
// PropID to the same property regardless of channel order.
const (
	propIDTranslation = 0
	propIDRotation    = 1
	propIDScale       = 2
)

// clipPropID returns the fixed PropID for a glTF transform property, or -1 for
// unknown properties.
func clipPropID(property string) int {
	switch property {
	case "translation":
		return propIDTranslation
	case "rotation":
		return propIDRotation
	case "scale":
		return propIDScale
	default:
		return -1
	}
}

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
//   - Target.Ref = strconv.Itoa(Node) for debuggability.
//
// ID assignment (cross-clip consistent):
//   - Track.TargetID = channel.Node (the glTF node index — globally unique across clips).
//   - Track.PropID   = fixed per-property constant: translation→0, rotation→1, scale→2.
//
// This guarantees (TargetID, PropID) is identical across ALL clips for the same
// (node, property), so motion.Mixer blends correctly when mixing clips that share
// animated nodes. PrepareTracks is NOT called here — the IDs are set directly and
// must not be overwritten by a per-clip interner.
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
		propID := clipPropID(ch.Property)
		if propID < 0 {
			continue // unknown property (should not happen if clipArity passes, but guard)
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
			Target:   Target{Kind: TargetSceneNode, Ref: strconv.Itoa(ch.Node)},
			Prop:     ch.Property,
			Keys:     keys,
			Interp:   interp,
			TargetID: ch.Node, // glTF node index: globally consistent across all clips
			PropID:   propID,  // fixed per-property constant: translation=0, rotation=1, scale=2
		}
		children = append(children, Positioned{
			At:    Position{Kind: PosAbs, Val: 0},
			Track: &track,
		})
	}

	tl := &Timeline{Children: children}
	// IDs are set directly above — do NOT call PrepareTracks here, which would
	// overwrite them with per-clip interner-assigned values.
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
