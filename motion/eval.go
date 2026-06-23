package motion

// Eval samples the timeline at time t (seconds) and appends packed writes to out.
// Pure function of t — no mutable cross-call state. Zero heap allocation on the hot
// path (stack [4]float64 scratch; max value width is 4).
//
// 1.6a scope: handles PosAbs offsets and InterpLinear/InterpStep only.
// Generators (1.6c), per-key easing (1.6b), reduced-motion (1.6d), and
// relative/label positioning are deferred to later tasks.
func Eval(tl *Timeline, t float64, _ Policy, out *WriteBuf) {
	evalTimeline(tl, t, 0, out)
}

// evalTimeline recurses into a timeline with a base offset applied to t.
func evalTimeline(tl *Timeline, t, baseOffset float64, out *WriteBuf) {
	for i := range tl.Children {
		child := &tl.Children[i]

		// Resolve start time — 1.6a: PosAbs only.
		start := baseOffset
		if child.At.Kind == PosAbs {
			start = baseOffset + child.At.Val
		}

		if child.Track != nil {
			evalTrack(child.Track, t, start, out)
		}

		if child.Sub != nil {
			// TODO(1.6): nested timeline positioning (PosRel, PosLabel, PosPrevRel)
			evalTimeline(child.Sub, t, start, out)
		}
	}
}

// evalTrack samples a single track at global time t given its start offset.
func evalTrack(track *Track, t, start float64, out *WriteBuf) {
	// Skip generator tracks (1.6c) and tracks with no keys.
	if track.Gen != nil || len(track.Keys) == 0 {
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
		out.Push(track.TargetID, track.PropID, Value{Arity: keys[0].Value.Arity, F: scratch[:w]})

	case localT >= keys[len(keys)-1].T:
		// Clamp to last key.
		last := keys[len(keys)-1]
		w := last.Value.Arity.Width()
		StepInto(scratch[:w], last.Value)
		out.Push(track.TargetID, track.PropID, Value{Arity: last.Value.Arity, F: scratch[:w]})

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
		out.Push(track.TargetID, track.PropID, Value{Arity: arity, F: scratch[:w]})
	}
}
