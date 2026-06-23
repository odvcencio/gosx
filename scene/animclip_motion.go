package scene

import (
	"strconv"

	"m31labs.dev/gosx/motion"
)

// arityWidth returns the flat-slice chunk width for an AnimationChannel property.
// translation and scale use vec3 (width 3); rotation uses quat (width 4).
// Unknown properties return 0 (caller should skip).
func arityWidth(property string) (motion.ValueArity, int) {
	switch property {
	case "translation", "scale":
		return motion.ArityVec3, 3
	case "rotation":
		return motion.ArityQuat, 4
	default:
		return 0, 0
	}
}

// interpFromString maps the glTF/AnimationChannel interpolation string to
// motion.Interp. "STEP" maps to InterpStep; everything else (including
// "CUBICSPLINE") degrades to InterpLinear.
func interpFromString(s string) motion.Interp {
	if s == "STEP" {
		return motion.InterpStep
	}
	return motion.InterpLinear
}

// AnimationClipMotionTimeline converts an AnimationClip to an evaluable
// motion.Timeline. Each AnimationChannel becomes one motion.Track inside a
// Positioned child at absolute time 0.
//
// Target mapping: TargetNode (int node index) → stable ref = strconv.Itoa(TargetNode).
// This matches the native consumer's index-string keying convention.
//
// Flat Values layout:
//   - translation / scale → vec3 chunks of width 3 → ArityVec3
//   - rotation            → quat chunks of width 4  → ArityQuat
//
// Cubicspline interpolation degrades to InterpLinear (GoSX has no cubic tangent
// evaluator in the motion package yet).
//
// Malformed channels (Values slice too short for Times×width) are silently
// skipped rather than panicking.
//
// PrepareTracks is called on the resulting timeline with fresh interners so
// TargetID/PropID are populated and the timeline is wire-ready. The interner
// refs are discarded; pass the timeline to motion.EncodeProgram with the ref
// slices from fresh interners if you need the round-trip tables.
func AnimationClipMotionTimeline(clip AnimationClip) *motion.Timeline {
	children := make([]motion.Positioned, 0, len(clip.Channels))

	for _, ch := range clip.Channels {
		arity, w := arityWidth(ch.Property)
		if w == 0 {
			// Unknown property — skip.
			continue
		}
		nKeys := len(ch.Times)
		if nKeys == 0 {
			continue
		}
		// Guard: flat Values must contain exactly nKeys * w floats.
		if len(ch.Values) < nKeys*w {
			// Malformed — skip rather than panic.
			continue
		}

		interp := interpFromString(ch.Interpolation)
		ref := strconv.Itoa(ch.TargetNode)

		keys := make([]motion.Key, nKeys)
		for i := 0; i < nKeys; i++ {
			offset := i * w
			var f [4]float64
			for j := 0; j < w; j++ {
				f[j] = ch.Values[offset+j]
			}
			keys[i] = motion.Key{
				T:     ch.Times[i],
				Value: motion.Value{Arity: arity, F: f},
			}
		}

		track := motion.Track{
			Target: motion.Target{
				Kind: motion.TargetSceneNode,
				Ref:  ref,
			},
			Prop:   ch.Property,
			Keys:   keys,
			Interp: interp,
		}

		children = append(children, motion.Positioned{
			At:    motion.Position{Kind: motion.PosAbs, Val: 0},
			Track: &track,
		})
	}

	tl := &motion.Timeline{
		ID:       clip.Name,
		Children: children,
	}

	// Assign TargetID/PropID so the timeline is wire-ready.
	targets := motion.NewInterner()
	props := motion.NewInterner()
	motion.PrepareTracks(tl, targets, props)

	return tl
}
