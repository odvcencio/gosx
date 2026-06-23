package motion

import "math"

// Eval samples the timeline at time t (seconds) and appends packed writes to out.
// Pure function of t — no mutable cross-call state. Zero heap allocation on the hot
// path (stack [4]float64 scratch; max value width is 4).
//
// When policy.ReducedMotion is true, each track immediately emits its rest/final
// state instead of animating (1.6d).
func Eval(tl *Timeline, t float64, policy Policy, out *WriteBuf) {
	evalTimeline(tl, t, 0, policy, out)
}

// evalTimeline recurses into a timeline with a base offset applied to t.
func evalTimeline(tl *Timeline, t, baseOffset float64, policy Policy, out *WriteBuf) {
	for i := range tl.Children {
		child := &tl.Children[i]

		// Resolve start time — 1.6a: PosAbs only.
		start := baseOffset
		if child.At.Kind == PosAbs {
			start = baseOffset + child.At.Val
		}

		if child.Track != nil {
			evalTrack(child.Track, t, start, policy, out)
		}

		if child.Sub != nil {
			// TODO(1.6): nested timeline positioning (PosRel, PosLabel, PosPrevRel)
			evalTimeline(child.Sub, t, start, policy, out)
		}
	}
}

// evalGenerator samples a procedural generator track at time t (localT already applied
// via start, but generators use global t directly as their time argument).
// Uses a stack [4]float64 scratch — zero alloc.
//
// When policy.ReducedMotion is true, the rest state is emitted immediately:
//   - GenSpin → identity quaternion {0,0,0,1}
//   - GenDrift → Base (no oscillation)
//   - GenSpring → Base.F[1] (the settled "to" value)
func evalGenerator(track *Track, t float64, policy Policy, out *WriteBuf) {
	gen := track.Gen
	var scratch [4]float64

	// Reduced-motion: emit the rest/settled state for each generator kind.
	if policy.ReducedMotion {
		switch gen.Kind {
		case GenSpin:
			// Identity quaternion — rotation stopped at rest.
			scratch[0] = 0
			scratch[1] = 0
			scratch[2] = 0
			scratch[3] = 1
			out.Push(track.TargetID, track.PropID, Value{Arity: ArityQuat, F: scratch})
		case GenDrift:
			// Base value — no oscillation.
			if gen.Base.Arity.Width() < 3 {
				return
			}
			for a := 0; a < 3; a++ {
				scratch[a] = gen.Base.F[a]
			}
			out.Push(track.TargetID, track.PropID, Value{Arity: ArityVec3, F: scratch})
		case GenSpring:
			// Settled "to" target: Base.F[1].
			if gen.Base.Arity.Width() < 2 {
				return
			}
			scratch[0] = gen.Base.F[1]
			out.Push(track.TargetID, track.PropID, Value{Arity: ArityScalar, F: scratch})
		default:
			// GenNone or unknown — emit nothing.
		}
		return
	}

	switch gen.Kind {
	case GenSpin:
		q := QuatFromEuler(gen.Spin[0]*t, gen.Spin[1]*t, gen.Spin[2]*t)
		scratch[0] = q.X
		scratch[1] = q.Y
		scratch[2] = q.Z
		scratch[3] = q.W
		out.Push(track.TargetID, track.PropID, Value{Arity: ArityQuat, F: scratch})

	case GenDrift:
		if gen.Base.Arity.Width() < 3 {
			return
		}
		for a := 0; a < 3; a++ {
			scratch[a] = gen.Base.F[a] + gen.Drift[a]*math.Sin(t*gen.DriftSpeed[a]+gen.DriftPhase[a])
		}
		out.Push(track.TargetID, track.PropID, Value{Arity: ArityVec3, F: scratch})

	case GenSpring:
		if gen.Base.Arity.Width() < 2 {
			return
		}
		v := gen.Spring.Value(gen.Base.F[0], gen.Base.F[1], t)
		scratch[0] = v
		out.Push(track.TargetID, track.PropID, Value{Arity: ArityScalar, F: scratch})

	default:
		// GenNone or unknown — emit nothing.
	}
}

// evalTrack samples a single track at global time t given its start offset.
func evalTrack(track *Track, t, start float64, policy Policy, out *WriteBuf) {
	// Generator tracks: dispatch to procedural evaluation (1.6c / 1.6d).
	if track.Gen != nil {
		evalGenerator(track, t, policy, out)
		return
	}

	// Skip tracks with no keys.
	if len(track.Keys) == 0 {
		return
	}

	// Reduced-motion: emit the last key's value immediately (end/rest state).
	if policy.ReducedMotion {
		last := track.Keys[len(track.Keys)-1]
		var scratch [4]float64
		w := last.Value.Arity.Width()
		StepInto(scratch[:w], last.Value)
		out.Push(track.TargetID, track.PropID, Value{Arity: last.Value.Arity, F: scratch})
		return
	}

	localT := t - start
	keys := track.Keys

	var scratch [4]float64

	switch {
	case localT <= keys[0].T:
		// Clamp to first key.
		w := keys[0].Value.Arity.Width()
		StepInto(scratch[:w], keys[0].Value)
		out.Push(track.TargetID, track.PropID, Value{Arity: keys[0].Value.Arity, F: scratch})

	case localT >= keys[len(keys)-1].T:
		// Clamp to last key.
		last := keys[len(keys)-1]
		w := last.Value.Arity.Width()
		StepInto(scratch[:w], last.Value)
		out.Push(track.TargetID, track.PropID, Value{Arity: last.Value.Arity, F: scratch})

	default:
		// Find surrounding pair by linear scan.
		i := 0
		for i < len(keys)-2 && keys[i+1].T <= localT {
			i++
		}
		ka := keys[i]
		kb := keys[i+1]
		alpha := (localT - ka.T) / (kb.T - ka.T)
		arity := ka.Value.Arity
		w := arity.Width()

		if track.Interp == InterpStep {
			StepInto(scratch[:w], ka.Value)
		} else {
			// InterpLinear (and unrecognised modes fall through to linear for 1.6a).
			// Per-key ease overrides track-level ease; EaseLinear (zero value) is identity.
			ease := track.Ease
			if ka.Ease != nil {
				ease = *ka.Ease
			}
			easedAlpha := ease.Apply(alpha)
			LerpValueInto(scratch[:w], ka.Value, kb.Value, easedAlpha)
		}
		out.Push(track.TargetID, track.PropID, Value{Arity: arity, F: scratch})
	}
}
