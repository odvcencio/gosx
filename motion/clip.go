package motion

import (
	"math"
	"strconv"
)

// Fixed glTF property → PropID mapping. These constants are the stable,
// cross-clip IDs used by BuildClipTimeline so every clip assigns the same
// PropID to the same property regardless of channel order.
const (
	propIDTranslation = 0
	propIDRotation    = 1
	propIDScale       = 2

	// morphIDBase is the fixed base for morph-weight PropIDs: weight j of a
	// node gets PropID morphIDBase+j (TargetID stays the glTF node index).
	morphIDBase = 1000
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

// ClipChannel is one glTF animation channel targeting a node's transform or
// morph-weight property.
//
//   - Property: "translation" | "rotation" | "scale" | "weights".
//   - Interp:   "LINEAR" | "STEP" | "CUBICSPLINE" (default/unknown → LINEAR).
//   - Times:    nKeys keyframe times (seconds).
//   - Values:   the flat glTF accessor data. For LINEAR/STEP this is nKeys*width;
//     for CUBICSPLINE it is nKeys*3*width laid out [inTangent, value, outTangent]
//     per key (width = 3 for translation/scale, 4 for rotation, WeightCount
//     for "weights").
//   - WeightCount: for Property "weights", the number of morph weights (one
//     ArityScalar track is emitted per weight); ignored for TRS channels.
type ClipChannel struct {
	Node        int
	Property    string
	Interp      string
	Times       []float64
	Values      []float64
	WeightCount int
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
// channels. Each well-formed channel contributes one or more Tracks inside
// Positioned children at absolute time 0: a TRS channel becomes exactly one
// Track, and a "weights" channel becomes WeightCount scalar Tracks — one
// ArityScalar track per morph weight.
//
// Mapping:
//   - translation / scale → ArityVec3 tracks; rotation → ArityQuat tracks (slerp).
//   - "weights" → one ArityScalar track per morph weight; see the "weights"
//     layout and ID rules below.
//   - "LINEAR" → InterpLinear, "STEP" → InterpStep, "CUBICSPLINE" →
//     InterpCubicSpline with per-key InTangent/OutTangent populated from the
//     glTF value triplets; default/unknown → InterpLinear.
//   - Target.Ref = strconv.Itoa(Node) for debuggability.
//
// ID assignment (cross-clip consistent):
//   - Track.TargetID = channel.Node (the glTF node index — globally unique across clips).
//   - Track.PropID   = fixed per-property constant: translation→0, rotation→1, scale→2.
//   - "weights":     PropID = morphIDBase+j (morphIDBase = 1000) for weight j of
//     the channel's node, with TargetID = channel.Node, so weight j of a node
//     blends under the same (TargetID, PropID) key in every clip.
//
// "weights" channel layout: Values is the flat glTF morph WEIGHTS accessor —
// one WeightCount-wide vector [w0 … wWeightCount-1] per key, in key order.
// Weight j's track reads component j of every per-key vector:
//
//	LINEAR/STEP: len(Values) must be exactly nKeys*WeightCount;
//	  key i, weight j = Values[i*WeightCount + j]
//	CUBICSPLINE: len(Values) must be exactly nKeys*3*WeightCount; each key
//	  holds the [inTangent, value, outTangent] triplet of WeightCount-wide
//	  vectors, the middle vector being the animated value:
//	  key i, weight j value      = Values[i*3*WeightCount + WeightCount + j]
//	  key i, weight j inTangent  = Values[i*3*WeightCount + 0*WeightCount + j]
//	  key i, weight j outTangent = Values[i*3*WeightCount + 2*WeightCount + j]
//
// WeightCount is authoritative and never inferred: a missing, zero, or negative
// WeightCount invalidates the channel (weights are not guessed as vec4), as do
// a Values length that is not a whole multiple of the key count (truncated or
// extra data) and a per-key width that does not equal WeightCount (times 3 for
// CUBICSPLINE). A rejected "weights" channel contributes no tracks; its sibling
// channels are still built. Node indexes and weight counts whose packed
// (TargetID, PropID) would not fit a signed int32 are rejected as well.
//
// This guarantees (TargetID, PropID) is identical across ALL clips for the same
// (node, property) — including (node, weight j) pairs — so motion.Mixer blends
// correctly when mixing clips that share animated nodes or morph targets.
// PrepareTracks is NOT called here — the IDs are set directly and must not be
// overwritten by a per-clip interner.
//
// CUBICSPLINE accessor layout: for key i with component width w, the three
// triplet members live in the flat Values slice at:
//
//	inTangent  = Values[i*3*w + 0*w : i*3*w + 1*w]
//	value      = Values[i*3*w + 1*w : i*3*w + 2*w]
//	outTangent = Values[i*3*w + 2*w : i*3*w + 3*w]
//
// (w = 3 for translation/scale, 4 for rotation, and WeightCount for "weights".)
//
// Malformed channels (unknown property, no keys, or a Values slice too short for
// the keyframe count and interpolation mode; for "weights", any malformed
// WeightCount/layout above) are silently skipped — never a panic.
//
// The returned duration is the larger of the maximum last-keyframe time across
// channels (clips with no usable keys yield 0).
func BuildClipTimeline(channels []ClipChannel) (*Timeline, float64) {
	children := make([]Positioned, 0, len(channels))
	var duration float64

	for _, ch := range channels {
		if ch.Property == "weights" {
			weightChildren, wDur, ok := weightTracks(ch)
			if ok {
				children = append(children, weightChildren...)
				if wDur > duration {
					duration = wDur
				}
			}
			continue
		}
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

// weightTracks builds one ArityScalar track per morph weight for a "weights"
// channel. Malformed channels (non-positive WeightCount, no keys, Values not a
// whole multiple of the key count, a per-key width that does not match
// WeightCount for the interpolation mode, truncated or extra data, or IDs that
// do not fit a signed int32) are rejected with ok=false — never a panic.
func weightTracks(ch ClipChannel) ([]Positioned, float64, bool) {
	wc, nKeys := ch.WeightCount, len(ch.Times)
	if wc <= 0 || nKeys == 0 {
		return nil, 0, false
	}
	if ch.Node < 0 || ch.Node > int(math.MaxInt32) ||
		wc-1 > int(math.MaxInt32)-morphIDBase {
		return nil, 0, false // IDs must pack into a signed int32 blend key
	}
	interp := clipInterp(ch.Interp)
	factor := 1
	if interp == InterpCubicSpline {
		factor = 3 // [inTangent, value, outTangent] vectors per key
	}
	if len(ch.Values)%nKeys != 0 {
		return nil, 0, false // truncated per-key data
	}
	perKey := len(ch.Values) / nKeys
	if perKey%factor != 0 || perKey/factor != wc {
		return nil, 0, false // width mismatch (covers truncated and extra data)
	}

	tracks := make([]Positioned, 0, wc)
	for j := 0; j < wc; j++ {
		keys := make([]Key, nKeys)
		for i := 0; i < nKeys; i++ {
			vBase := i * wc
			if interp == InterpCubicSpline {
				vBase = i*3*wc + wc // value is the middle vector of the triplet
			}
			k := Key{T: ch.Times[i], Value: ScalarV(ch.Values[vBase+j])}
			if interp == InterpCubicSpline {
				base := i * 3 * wc
				inT := ScalarV(ch.Values[base+j])
				outT := ScalarV(ch.Values[base+2*wc+j])
				k.InTangent = &inT
				k.OutTangent = &outT
			}
			keys[i] = k
		}
		track := Track{
			Target:   Target{Kind: TargetSceneNode, Ref: strconv.Itoa(ch.Node)},
			Prop:     "weights",
			Keys:     keys,
			Interp:   interp,
			TargetID: ch.Node,
			PropID:   morphIDBase + j,
		}
		tracks = append(tracks, Positioned{
			At:    Position{Kind: PosAbs, Val: 0},
			Track: &track,
		})
	}
	return tracks, ch.Times[nKeys-1], true
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
