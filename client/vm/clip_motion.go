package vm

import (
	"math"
	"strconv"
	"strings"

	rootengine "m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/motion"
)

// Deterministic prop refs for the per-object clip timeline. The track Prop is
// also the interned PropID source, so evalClipTRS routes writes by matching the
// resolved PropID against the IDs assigned here at build time.
const (
	clipPropTranslation = "translation"
	clipPropRotation    = "rotation"
	clipPropScale       = "scale"
)

// objectMatchesTarget reports whether a channel TargetID resolves to this object
// via the native 3-way fallback used by render/bundle (animationStateForMesh):
//   - the object's string ID, then
//   - strconv(index), then
//   - strconv(index+1).
//
// Empty targetID never matches.
func objectMatchesTarget(targetID, objectID string, objectIndex int) bool {
	tid := strings.TrimSpace(targetID)
	if tid == "" {
		return false
	}
	if oid := strings.TrimSpace(objectID); oid != "" && tid == oid {
		return true
	}
	if tid == strconv.Itoa(objectIndex) {
		return true
	}
	if tid == strconv.Itoa(objectIndex+1) {
		return true
	}
	return false
}

// clipTRS is the evaluated transform for one object at one time.
type clipTRS struct {
	HasT, HasR, HasS bool
	T                [3]float64  // translation offset
	R                motion.Quat // rotation quaternion
	S                [3]float64  // scale (default 1,1,1 when HasS)
}

// clipChannelProperty mirrors render/bundle's normalizeNativeAnimationProperty
// so this converter recognizes exactly the property forms the native consumer
// honors. It collapses aliases to a canonical lowercase token.
func clipChannelProperty(property string) string {
	switch strings.ToLower(strings.TrimSpace(property)) {
	case "translation", "translate", "position":
		return "translation"
	case "rotation":
		return "rotation"
	case "rotationx", "rotatex":
		return "rotationx"
	case "rotationy", "rotatey":
		return "rotationy"
	case "rotationz", "rotatez":
		return "rotationz"
	case "scale":
		return "scale"
	case "scalex":
		return "scalex"
	case "scaley":
		return "scaley"
	case "scalez":
		return "scalez"
	default:
		return strings.ToLower(strings.TrimSpace(property))
	}
}

func clipInterp(s string) motion.Interp {
	if strings.EqualFold(strings.TrimSpace(s), "STEP") {
		return motion.InterpStep
	}
	return motion.InterpLinear
}

// buildObjectClipTimeline collects the animation channels (across all clips) that
// target this object and builds a motion.Timeline whose tracks are:
//   - translation/position -> ArityVec3 keyframe track, Prop clipPropTranslation
//   - scale                -> ArityVec3 keyframe track, Prop clipPropScale
//   - rotation (combined)  -> ArityQuat keyframe track, Prop clipPropRotation
//   - rotationX/Y/Z (Euler)-> reconstructed into ONE ArityQuat track whose
//     per-keyframe value is QuatFromEuler of the three axes sampled at that
//     keyframe time, Prop clipPropRotation.
//
// Per-axis Euler reconstruction assumes the rotationX/Y/Z channels for a target
// share a common Times array (the production case — see render/bundle's
// sharedRotationTimes). When axes differ, the union of all axis keyframe times
// is used and each axis is sampled (linearly, with end-clamp) at every union
// time, so the result is still a single coherent quaternion track. Missing axes
// contribute 0. Documented limitation: per-axis STEP semantics are approximated
// as linear across the union of times.
//
// The returned duration is the AUTHORED clip duration (the maximum anim.Duration
// across all matching clips), NOT synthesized from keyframe times. A duration of
// 0 means "clamp-and-hold" (no looping), matching the native render/bundle path
// (animation.go:70-77). Non-zero authored duration means the clip loops.
//
// Returns (tl, duration) or (nil, 0) if no channels target this object.
func buildObjectClipTimeline(anims []rootengine.RenderAnimation, objectID string, objectIndex int) (*motion.Timeline, float64) {
	ref := objectID
	if strings.TrimSpace(ref) == "" {
		ref = strconv.Itoa(objectIndex)
	}

	// Cheap pre-pass: check whether ANY channel in any clip targets this object.
	// When none match, return (nil,0) immediately — zero alloc, no map, no scan.
	// This is the common case for objects not targeted by any animation channel;
	// cost is a cheap match-scan (no alloc) when animations exist but none target
	// this object.
	anyMatch := false
	for _, anim := range anims {
		for _, ch := range anim.Channels {
			if objectMatchesTarget(ch.TargetID, objectID, objectIndex) {
				anyMatch = true
				break
			}
		}
		if anyMatch {
			break
		}
	}
	if !anyMatch {
		return nil, 0
	}

	var (
		// authoredDuration tracks the maximum authored clip duration across clips
		// that have at least one channel targeting this object. It stays 0 when all
		// matching clips omit a duration — meaning clamp-and-hold, not looping.
		authoredDuration float64
		children         []motion.Positioned
		// rotAxes is lazily allocated only when a matching per-axis rotation channel
		// is first encountered (avoids alloc for objects that use only T/S/quaternion).
		rotAxes    map[string]rootengine.RenderAnimationChannel
		hasRotAxes bool
	)

	for _, anim := range anims {
		hasMatchInClip := false
		for _, ch := range anim.Channels {
			if objectMatchesTarget(ch.TargetID, objectID, objectIndex) {
				hasMatchInClip = true
				break
			}
		}
		if !hasMatchInClip {
			continue
		}
		// Carry the AUTHORED duration from clips that target this object. A clip
		// with Duration==0 contributes 0 (hold-at-end, no loop), mirroring the
		// native sampleNativeAnimationClip path (animation.go:72).
		if anim.Duration > authoredDuration {
			authoredDuration = anim.Duration
		}
		for _, ch := range anim.Channels {
			if !objectMatchesTarget(ch.TargetID, objectID, objectIndex) {
				continue
			}
			if len(ch.Times) == 0 || len(ch.Values) == 0 {
				continue
			}
			switch clipChannelProperty(ch.Property) {
			case "translation":
				if track := vec3Track(ch, ref, clipPropTranslation, 3); track != nil {
					children = append(children, positioned(track))
				}
			case "scale":
				if track := scaleTrack(ch, ref); track != nil {
					children = append(children, positioned(track))
				}
			case "rotation":
				// Combined quaternion (width-4) form — Phase-1 / glTF style.
				if track := quatTrack(ch, ref); track != nil {
					children = append(children, positioned(track))
				}
			case "rotationx", "rotationy", "rotationz":
				// Per-axis Euler — accumulate and reconstruct after the loop.
				// Last channel per axis wins (matches eulerAt's per-axis read).
				// rotAxes is lazily allocated here (first matching axis encountered).
				if rotAxes == nil {
					rotAxes = make(map[string]rootengine.RenderAnimationChannel)
				}
				axis := clipChannelProperty(ch.Property)
				rotAxes[axis] = ch
				hasRotAxes = true
			default:
				// Unknown / unsupported (e.g. matrix) — skip; the renderer wiring
				// handles matrix separately if ever needed.
			}
		}
	}

	if hasRotAxes {
		if track := eulerAxesTrack(rotAxes, ref); track != nil {
			children = append(children, positioned(track))
		}
	}

	if len(children) == 0 {
		return nil, 0
	}

	tl := &motion.Timeline{Children: children}
	motion.PrepareTracks(tl, motion.NewInterner(), motion.NewInterner())
	return tl, authoredDuration
}

func positioned(track *motion.Track) motion.Positioned {
	return motion.Positioned{
		At:    motion.Position{Kind: motion.PosAbs, Val: 0},
		Track: track,
	}
}

func lastTime(times []float64) float64 {
	if len(times) == 0 {
		return 0
	}
	return times[len(times)-1]
}

// vec3Track builds an ArityVec3 keyframe track from a stride-3 channel.
func vec3Track(ch rootengine.RenderAnimationChannel, ref, prop string, stride int) *motion.Track {
	n := frameCount(ch, stride)
	if n == 0 {
		return nil
	}
	keys := make([]motion.Key, n)
	for i := 0; i < n; i++ {
		var f [4]float64
		base := i * stride
		f[0] = ch.Values[base]
		f[1] = ch.Values[base+1]
		f[2] = ch.Values[base+2]
		keys[i] = motion.Key{T: ch.Times[i], Value: motion.Value{Arity: motion.ArityVec3, F: f}}
	}
	return &motion.Track{
		Target: motion.Target{Kind: motion.TargetSceneNode, Ref: ref},
		Prop:   prop,
		Keys:   keys,
		Interp: clipInterp(ch.Interpolation),
	}
}

// scaleTrack handles the scale channel which may be stride-3 (vec3) or stride-1
// (uniform). Uniform scale is broadcast to all three components, mirroring the
// native applyNativeAnimationChannel scale handling.
func scaleTrack(ch rootengine.RenderAnimationChannel, ref string) *motion.Track {
	frames := len(ch.Times)
	if frames == 0 {
		return nil
	}
	stride := 1
	if len(ch.Values)/frames >= 3 {
		stride = 3
	}
	if stride == 3 {
		return vec3Track(ch, ref, clipPropScale, 3)
	}
	n := frameCount(ch, 1)
	if n == 0 {
		return nil
	}
	keys := make([]motion.Key, n)
	for i := 0; i < n; i++ {
		s := ch.Values[i]
		keys[i] = motion.Key{
			T:     ch.Times[i],
			Value: motion.Value{Arity: motion.ArityVec3, F: [4]float64{s, s, s}},
		}
	}
	return &motion.Track{
		Target: motion.Target{Kind: motion.TargetSceneNode, Ref: ref},
		Prop:   clipPropScale,
		Keys:   keys,
		Interp: clipInterp(ch.Interpolation),
	}
}

// quatTrack builds an ArityQuat keyframe track from a width-4 rotation channel.
func quatTrack(ch rootengine.RenderAnimationChannel, ref string) *motion.Track {
	n := frameCount(ch, 4)
	if n == 0 {
		return nil
	}
	keys := make([]motion.Key, n)
	for i := 0; i < n; i++ {
		base := i * 4
		f := [4]float64{
			ch.Values[base],
			ch.Values[base+1],
			ch.Values[base+2],
			ch.Values[base+3],
		}
		keys[i] = motion.Key{T: ch.Times[i], Value: motion.Value{Arity: motion.ArityQuat, F: f}}
	}
	return &motion.Track{
		Target: motion.Target{Kind: motion.TargetSceneNode, Ref: ref},
		Prop:   clipPropRotation,
		Keys:   keys,
		Interp: motion.InterpLinear, // quat lerp routes through Slerp in motion
	}
}

// eulerAxesTrack reconstructs a single ArityQuat track from per-axis Euler
// rotationX/Y/Z channels. Each output keyframe time is a value in the union of
// all axis Times; at that time each axis's Euler angle is sampled (linear, with
// end clamp) and QuatFromEuler composes the orientation.
func eulerAxesTrack(axes map[string]rootengine.RenderAnimationChannel, ref string) *motion.Track {
	chX, hasX := axes["rotationx"]
	chY, hasY := axes["rotationy"]
	chZ, hasZ := axes["rotationz"]
	if !hasX && !hasY && !hasZ {
		return nil
	}

	times := unionTimes(chX, hasX, chY, hasY, chZ, hasZ)
	if len(times) == 0 {
		return nil
	}

	keys := make([]motion.Key, len(times))
	for i, t := range times {
		rx := sampleAxis(chX, hasX, t)
		ry := sampleAxis(chY, hasY, t)
		rz := sampleAxis(chZ, hasZ, t)
		q := motion.QuatFromEuler(rx, ry, rz).Normalize()
		keys[i] = motion.Key{
			T:     t,
			Value: motion.Value{Arity: motion.ArityQuat, F: [4]float64{q.X, q.Y, q.Z, q.W}},
		}
	}
	return &motion.Track{
		Target: motion.Target{Kind: motion.TargetSceneNode, Ref: ref},
		Prop:   clipPropRotation,
		Keys:   keys,
		Interp: motion.InterpLinear,
	}
}

// unionTimes returns the sorted, de-duplicated union of the supplied axis Times.
// In the common case all axes share an identical Times array and this returns
// that array unchanged.
func unionTimes(chX rootengine.RenderAnimationChannel, hasX bool, chY rootengine.RenderAnimationChannel, hasY bool, chZ rootengine.RenderAnimationChannel, hasZ bool) []float64 {
	var all []float64
	if hasX {
		all = append(all, chX.Times...)
	}
	if hasY {
		all = append(all, chY.Times...)
	}
	if hasZ {
		all = append(all, chZ.Times...)
	}
	if len(all) == 0 {
		return nil
	}
	// Insertion sort + dedup (frame counts are tiny; avoids importing sort and
	// keeps the path allocation-light).
	for i := 1; i < len(all); i++ {
		v := all[i]
		j := i - 1
		for j >= 0 && all[j] > v {
			all[j+1] = all[j]
			j--
		}
		all[j+1] = v
	}
	out := all[:0]
	for i, v := range all {
		if i == 0 || v != all[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// sampleAxis linearly samples a single stride-1 Euler channel at time t with
// end-clamping. Returns 0 when the axis is absent or empty.
func sampleAxis(ch rootengine.RenderAnimationChannel, has bool, t float64) float64 {
	if !has {
		return 0
	}
	n := frameCount(ch, 1)
	if n == 0 {
		return 0
	}
	if t <= ch.Times[0] {
		return ch.Values[0]
	}
	last := n - 1
	if t >= ch.Times[last] {
		return ch.Values[last]
	}
	hi := 1
	for hi < n && ch.Times[hi] < t {
		hi++
	}
	lo := hi - 1
	if strings.EqualFold(strings.TrimSpace(ch.Interpolation), "STEP") {
		return ch.Values[lo]
	}
	t0, t1 := ch.Times[lo], ch.Times[hi]
	if t1 <= t0 {
		return ch.Values[lo]
	}
	alpha := (t - t0) / (t1 - t0)
	return ch.Values[lo] + (ch.Values[hi]-ch.Values[lo])*alpha
}

// frameCount returns the number of usable keyframes given a stride: the min of
// the time count and the value count divided by stride.
func frameCount(ch rootengine.RenderAnimationChannel, stride int) int {
	if stride <= 0 {
		return 0
	}
	n := len(ch.Times)
	if v := len(ch.Values) / stride; v < n {
		n = v
	}
	return n
}

// evalClipTRS evaluates the object's clip timeline at time t and returns the
// decoded TRS. It uses the provided reusable WriteBuf. A nil timeline yields a
// zero clipTRS (all Has* false) without panicking.
//
// Looping semantics (mirrors render/bundle animation.go:70-77):
//   - When the AUTHORED duration > 0, t is wrapped into [0,duration) via
//     math.Mod, enabling looping. A negative wrapped value is corrected to
//     stay in range (matching the native path's sampleTime < 0 correction).
//   - When duration == 0 (author set duration:0 → clamp-and-hold), t is passed
//     raw to motion.Eval. The keyframe evaluator already clamps at the last key,
//     so the clip holds its final value past the last keyframe — no wrap, no
//     loop. Negative t is clamped to 0 (matches native animation.go:74-76
//     behavior where sampleTime becomes 0 when duration==0 and t<0).
//
// Tracks are routed by their resolved PropID. Because buildObjectClipTimeline
// interns the canonical Prop strings (clipPropTranslation / clipPropRotation /
// clipPropScale) into a fresh PropID interner per timeline, each write's PropID
// maps deterministically back to its property here.
func evalClipTRS(tl *motion.Timeline, duration, t float64, buf *motion.WriteBuf) clipTRS {
	var out clipTRS
	if tl == nil || buf == nil {
		return out
	}

	var wrapped float64
	if duration > 0 {
		// Authored loop: wrap t into [0, duration).
		wrapped = math.Mod(t, duration)
		if wrapped < 0 {
			wrapped += duration
		}
	} else {
		// Clamp-and-hold: pass raw t; the keyframe path holds at the last key.
		// Match native behavior: clamp negative t to 0 (no backwards playback
		// when duration is unset).
		wrapped = t
		if wrapped < 0 {
			wrapped = 0
		}
	}

	// Resolve which PropID corresponds to each canonical property by scanning
	// the prepared tracks. The interner is deterministic but per-timeline, so we
	// read the assignments directly off the tracks rather than assuming fixed IDs.
	translationID, rotationID, scaleID := -1, -1, -1
	for i := range tl.Children {
		tr := tl.Children[i].Track
		if tr == nil {
			continue
		}
		switch tr.Prop {
		case clipPropTranslation:
			translationID = tr.PropID
		case clipPropRotation:
			rotationID = tr.PropID
		case clipPropScale:
			scaleID = tr.PropID
		}
	}

	buf.Reset()
	motion.Eval(tl, wrapped, motion.Policy{}, buf)

	writes := buf.Writes()
	for i := 0; i+3 <= len(writes); {
		propID := int(writes[i+1])
		arity := motion.ValueArity(writes[i+2])
		width := arity.Width()
		base := i + 3
		if base+width > len(writes) {
			break
		}
		switch propID {
		case translationID:
			if width >= 3 {
				out.T = [3]float64{writes[base], writes[base+1], writes[base+2]}
				out.HasT = true
			}
		case scaleID:
			if width >= 3 {
				out.S = [3]float64{writes[base], writes[base+1], writes[base+2]}
				out.HasS = true
			}
		case rotationID:
			if width >= 4 {
				out.R = motion.Quat{
					X: writes[base],
					Y: writes[base+1],
					Z: writes[base+2],
					W: writes[base+3],
				}
				out.HasR = true
			}
		}
		i = base + width
	}

	return out
}
