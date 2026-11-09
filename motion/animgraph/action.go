package animgraph

// An action plays one clip over a layer's state machine without changing
// it: a hit reaction, an emote, or one side of a paired move. The state
// machine keeps running underneath, so when the action blends out the layer
// returns to whatever the machine is playing by then. The caller controls
// the action's playback rate every frame, which is how PairedMove lands a
// contact frame on a simulation-decided time.

// ActionOptions configures PlayAction.
type ActionOptions struct {
	// Layer receives the action. Zero is the base layer.
	Layer int
	// BlendIn and BlendOut are crossfade lengths in seconds.
	BlendIn, BlendOut float64
	// Rate scales playback. Zero means 1.
	Rate float64
	// Start is the starting clip time in seconds.
	Start float64
	// Loop repeats the clip until StopAction.
	Loop bool
}

// ActionInfo reports a layer's action.
type ActionInfo struct {
	Playing bool
	Clip    string
	// Time is the clip time in seconds; it does not wrap for looping clips.
	Time float64
	// Weight is the action's current share of the layer.
	Weight float64
}

type actionState struct {
	active       bool
	clip         int
	time, prev   float64
	rate         float64
	loop         bool
	fresh        bool
	elapsed      float64
	blendIn      float64
	blendOut     float64
	stopping     bool
	stopElapsed  float64
	stopDuration float64
	stopFrom     float64
	weight       float64
}

// PlayAction starts clip on a layer, replacing any action already there. It
// returns false for an unknown clip or layer.
func (in *Instance) PlayAction(clip string, opts ActionOptions) bool {
	return in.PlayActionID(in.prog.ClipID(clip), opts)
}

// PlayActionID is PlayAction with a clip index from Program.ClipID.
func (in *Instance) PlayActionID(clip int, opts ActionOptions) bool {
	if clip < 0 || clip >= len(in.prog.clips) || opts.Layer < 0 || opts.Layer >= len(in.actions) {
		return false
	}
	rate := opts.Rate
	if !finite(rate) || rate == 0 {
		rate = 1
	}
	if rate < 0 {
		rate = 0
	}
	start := opts.Start
	if !finite(start) || start < 0 {
		start = 0
	}
	a := &in.actions[opts.Layer]
	*a = actionState{
		active: true, clip: clip, time: start, prev: start, rate: rate, loop: opts.Loop, fresh: true,
		blendIn: nonNegative(opts.BlendIn), blendOut: nonNegative(opts.BlendOut),
	}
	if a.blendIn == 0 {
		a.weight = 1
	}
	return true
}

// StopAction blends a layer's action out over blendOut seconds (0 stops it
// at once).
func (in *Instance) StopAction(layer int, blendOut float64) {
	if layer < 0 || layer >= len(in.actions) || !in.actions[layer].active {
		return
	}
	a := &in.actions[layer]
	if !(blendOut > 0) {
		a.active, a.weight = false, 0
		return
	}
	a.stopping, a.stopElapsed, a.stopDuration, a.stopFrom = true, 0, blendOut, a.weight
}

// SetActionRate changes a playing action's playback rate (negative counts
// as zero).
func (in *Instance) SetActionRate(layer int, rate float64) {
	if layer >= 0 && layer < len(in.actions) && finite(rate) {
		if rate < 0 {
			rate = 0
		}
		in.actions[layer].rate = rate
	}
}

// Action reports a layer's action.
func (in *Instance) Action(layer int) ActionInfo {
	if layer < 0 || layer >= len(in.actions) || !in.actions[layer].active {
		return ActionInfo{}
	}
	a := &in.actions[layer]
	return ActionInfo{Playing: true, Clip: in.prog.clips[a.clip].name, Time: a.time, Weight: a.weight}
}

// advanceAction moves a layer's action clock, updates its weight, and fires
// the clip events it crossed.
func (in *Instance) advanceAction(li int, dt float64) {
	a := &in.actions[li]
	if !a.active {
		return
	}
	def := &in.prog.clips[a.clip]
	d := def.duration
	a.prev = a.time
	a.time += dt * a.rate
	a.elapsed += dt
	finished := false
	if !a.loop && a.time >= d-timeEpsilon {
		a.time = d
		finished = true
	}
	w := 1.0
	if a.blendIn > 0 && a.elapsed < a.blendIn {
		w = a.elapsed / a.blendIn
	}
	if !a.loop && a.blendOut > 0 && d-a.time < a.blendOut {
		w *= clamp01((d - a.time) / a.blendOut)
	}
	if a.stopping {
		a.stopElapsed += dt
		stop := a.stopFrom * (1 - a.stopElapsed/a.stopDuration)
		if stop <= 0 {
			finished = true
		}
		if stop < w {
			w = stop
		}
	}
	a.weight = clamp01(w)
	if a.weight > 0 && a.weight >= in.prog.eventMin {
		for i := range def.events {
			if crossed(a.prev/d, a.time/d, def.events[i].normalized, a.loop, a.fresh) {
				in.events = append(in.events, FiredEvent{Kind: Notify, Name: def.events[i].name, Layer: li, Clip: def.name, Weight: a.weight})
			}
		}
	}
	a.fresh = false
	if finished {
		a.active, a.weight = false, 0
	}
}

// mergeAction scales the machine's samples by (1 - action weight) and adds
// the action's clip sample.
func (in *Instance) mergeAction(li int) {
	a := &in.actions[li]
	ls := &in.layers[li]
	if !a.active || a.weight <= 0 {
		return
	}
	w := a.weight
	kept := 0
	for i := 0; i < ls.sampleCount; i++ {
		s := ls.samples[i]
		s.Weight *= 1 - w
		if s.Weight < minSampleWeight {
			continue
		}
		ls.samples[kept] = s
		kept++
	}
	ls.sampleCount = kept
	def := &in.prog.clips[a.clip]
	s := Sample{Clip: a.clip, Time: clipTime(a.time/def.duration, def.duration, a.loop), Weight: w, Loop: a.loop}
	if ls.sampleCount < MaxLayerSamples {
		ls.samples[ls.sampleCount] = s
		ls.sampleCount++
	} else {
		smallest := 0
		for i := 1; i < MaxLayerSamples; i++ {
			if ls.samples[i].Weight < ls.samples[smallest].Weight {
				smallest = i
			}
		}
		ls.samples[smallest] = s
	}
	ls.weight = clamp01(in.layerScale(li) * (ls.playing*(1-w) + w))
}

func nonNegative(v float64) float64 {
	if !finite(v) || v < 0 {
		return 0
	}
	return v
}
