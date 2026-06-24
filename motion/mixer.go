package motion

import "math"

// PlayOptions configures a Play call. Zero values mean: no fade-in, no loop.
// Speed and Weight default to 1.0 when left at their zero value (see Play).
type PlayOptions struct {
	FadeIn float64 // seconds to ramp weight 0→Weight; <=0 starts at full weight
	Loop   bool    // wrap time at duration when true; clamp+remove otherwise
	Speed  float64 // time scale; 0 → treated as 1.0
	Weight float64 // target weight; 0 → treated as 1.0
}

// StopOptions configures a Stop call.
type StopOptions struct {
	FadeOut float64 // seconds to ramp weight →0 then remove; <=0 removes immediately
}

// clipDef is a registered clip: its timeline and duration.
type clipDef struct {
	tl       *Timeline
	duration float64
}

// activeEntry is one playing clip with its mutable playback state.
// Mirrors the JS mixer's active-entry fields {clip, time, weight, targetWeight,
// fadeIn, fadeOut, fadeTime, speed, loop, stopping}.
type activeEntry struct {
	name         string
	clip         *clipDef
	time         float64
	weight       float64
	targetWeight float64
	speed        float64
	loop         bool
	fadeIn       float64
	fadeOut      float64
	fadeTime     float64
	stopping     bool
}

// blendAccum is the per-key running accumulator used by the incremental
// weighted blend (mirrors the JS _mixerResults entry: value + totalWeight).
type blendAccum struct {
	arity       ValueArity
	value       [4]float64
	totalWeight float64
}

// Mixer plays and crossfades multiple animation clips, blending their per-frame
// writes per (targetID, propID). It mirrors the JS AnimationMixer
// (client/js/bootstrap-src/19a-scene-animation.js): clip time advance with
// loop/speed, fade-in/fade-out weight ramps, and weighted blending via the
// incremental sceneAnimBlendValue scheme (slerp for quaternions, lerp otherwise).
//
// TinyGo-clean: no reflect, no encoding/json. Scratch buffers are reused across
// Update calls; the only per-tick allocation is the small key→accumulator map
// (model animation drives far fewer objects than the per-frame spin path).
type Mixer struct {
	clips      map[string]*clipDef
	active     []*activeEntry
	scratch    *WriteBuf             // per-clip eval scratch, reused each tick
	results    map[int64]*blendAccum // (targetID,propID)→accumulator, live this tick
	freePool   []*blendAccum         // recycled accumulators (grows to peak key count)
	keyScratch []int64               // reused key slice for deterministic emit order
}

// NewMixer allocates an empty Mixer with reusable scratch buffers.
func NewMixer() *Mixer {
	return &Mixer{
		clips:   make(map[string]*clipDef),
		active:  nil,
		scratch: NewWriteBuf(64),
		results: make(map[int64]*blendAccum),
	}
}

// AddClip registers a clip timeline under a name, with its duration (seconds).
// Re-adding the same name replaces the definition.
func (m *Mixer) AddClip(name string, tl *Timeline, duration float64) {
	m.clips[name] = &clipDef{tl: tl, duration: duration}
}

// findActive returns the active entry for name, or nil.
func (m *Mixer) findActive(name string) *activeEntry {
	for _, e := range m.active {
		if e.name == name {
			return e
		}
	}
	return nil
}

// Play starts (or re-tunes) a clip. FadeIn>0 ramps its weight from 0→Weight over
// FadeIn seconds; otherwise the clip starts at full Weight. Speed and Weight
// default to 1.0 when zero. Mirrors the JS play().
func (m *Mixer) Play(name string, opts PlayOptions) {
	clip, ok := m.clips[name]
	if !ok {
		return // unknown clip: no-op (JS warns)
	}

	speed := opts.Speed
	if speed == 0 {
		speed = 1.0
	}
	weight := opts.Weight
	if weight == 0 {
		weight = 1.0
	}
	fadeIn := opts.FadeIn

	// Already playing: update mutable options and return (JS findActive branch).
	if e := m.findActive(name); e != nil {
		e.speed = speed
		e.loop = opts.Loop
		e.targetWeight = weight
		if !e.stopping {
			e.weight = weight
		}
		return
	}

	startWeight := weight
	if fadeIn > 0 {
		startWeight = 0
	}
	m.active = append(m.active, &activeEntry{
		name:         name,
		clip:         clip,
		time:         0,
		weight:       startWeight,
		targetWeight: weight,
		speed:        speed,
		loop:         opts.Loop,
		fadeIn:       fadeIn,
		fadeOut:      0,
		fadeTime:     0,
		stopping:     false,
	})
}

// Stop fades a clip out over FadeOut seconds then removes it, or removes it
// immediately when FadeOut<=0. Mirrors the JS stop().
func (m *Mixer) Stop(name string, opts StopOptions) {
	e := m.findActive(name)
	if e == nil {
		return
	}
	if opts.FadeOut > 0 {
		e.stopping = true
		e.fadeOut = opts.FadeOut
		e.fadeTime = 0
	} else {
		m.remove(name)
	}
}

// remove deletes the active entry for name (order-preserving for deterministic
// blend ordering).
func (m *Mixer) remove(name string) {
	for i, e := range m.active {
		if e.name == name {
			m.active = append(m.active[:i], m.active[i+1:]...)
			return
		}
	}
}

// IsPlaying reports whether a clip currently has an active entry.
func (m *Mixer) IsPlaying(name string) bool {
	return m.findActive(name) != nil
}

// Update advances every active clip by dt (time + fade weights), evaluates each
// at its current time, blends the results weighted per (targetID, propID), and
// writes one blended packed write per key into out. Mirrors the JS update() +
// sceneAnimBlendValue.
//
// Reduced motion is honored by forwarding policy to Eval, which collapses each
// track to its rest/final state.
//
// Allocation profile: scratch eval buffer and the results map are reused across
// calls. Each tick clears (does not reallocate) the map; a new *blendAccum is
// allocated only the first time a given key is seen and is then reused on
// subsequent ticks. After warmup, steady-state Update allocates nothing on the
// hot path for a fixed set of animated keys.
func (m *Mixer) Update(dt float64, policy Policy, out *WriteBuf) {
	out.Reset()

	// 1. Advance time and handle fading for every active entry (JS step 1).
	for _, e := range m.active {
		e.time += dt * e.speed

		// Looping: wrap time at duration.
		if e.loop && e.clip.duration > 0 && e.time >= e.clip.duration {
			e.time = math.Mod(e.time, e.clip.duration)
		}

		// Fade-in: ramp weight 0→targetWeight over fadeIn seconds.
		if e.fadeIn > 0 && !e.stopping && e.fadeTime < e.fadeIn {
			e.fadeTime += dt
			r := e.fadeTime / e.fadeIn
			if r > 1.0 {
				r = 1.0
			}
			e.weight = r * e.targetWeight
		}

		// Fade-out (stopping): ramp weight →0 over fadeOut seconds.
		if e.stopping && e.fadeOut > 0 {
			e.fadeTime += dt
			r := 1.0 - e.fadeTime/e.fadeOut
			if r < 0 {
				r = 0
			}
			e.weight = r * e.targetWeight
		}
	}

	// 2. Remove finished entries (JS step 2). Iterate backwards for safe removal.
	for i := len(m.active) - 1; i >= 0; i-- {
		e := m.active[i]
		if e.stopping && (e.fadeOut <= 0 || e.fadeTime >= e.fadeOut) {
			m.active = append(m.active[:i], m.active[i+1:]...)
			continue
		}
		if !e.loop && e.clip.duration > 0 && e.time >= e.clip.duration {
			m.active = append(m.active[:i], m.active[i+1:]...)
			continue
		}
	}

	// 3. Evaluate each active clip and blend per (targetID, propID) (JS step 3).
	m.clearResults()

	for _, e := range m.active {
		if e.weight <= 0 {
			continue
		}
		m.scratch.Reset()
		Eval(e.clip.tl, e.time, policy, m.scratch)
		m.blendInto(e.weight)
	}

	// 4. Emit blended transforms in deterministic key order (JS step 4).
	m.emit(out)
}

// clearResults drains the results map back into the free pool. After emit() the
// map is normally already empty; this guards the path where no clip contributed
// (or a prior tick was interrupted) so accumulators are never leaked.
func (m *Mixer) clearResults() {
	for k, a := range m.results {
		m.freePool = append(m.freePool, a)
		delete(m.results, k)
	}
}

// blendInto folds the scratch WriteBuf (one clip's evaluation) into the running
// per-key accumulators using the incremental sceneAnimBlendValue scheme:
//
//	t = w / (totalWeight + w)
//	value = blend(value, newValue, t)   // slerp for quat, lerp otherwise
//	totalWeight += w
//
// The first contributor to a key seeds the accumulator with that clip's value
// at the full incoming weight (matching the JS "if (!existing)" branch).
func (m *Mixer) blendInto(weight float64) {
	w := m.scratch.Writes()
	i := 0
	for i < len(w) {
		tid := int(w[i])
		pid := int(w[i+1])
		arity := ValueArity(w[i+2])
		width := arity.Width()
		vstart := i + 3

		key := blendKey(tid, pid)
		acc, ok := m.results[key]
		if !ok {
			// First contributor: seed value, totalWeight = incoming weight.
			acc = m.acquireAccum()
			acc.arity = arity
			for c := 0; c < width; c++ {
				acc.value[c] = w[vstart+c]
			}
			acc.totalWeight = weight
			m.results[key] = acc
		} else {
			// Incremental blend (sceneAnimBlendValue).
			t := weight / (acc.totalWeight + weight)
			if acc.arity == ArityQuat {
				qa := Quat{acc.value[0], acc.value[1], acc.value[2], acc.value[3]}
				qb := Quat{w[vstart], w[vstart+1], w[vstart+2], w[vstart+3]}
				q := Slerp(qa, qb, t)
				acc.value[0] = q.X
				acc.value[1] = q.Y
				acc.value[2] = q.Z
				acc.value[3] = q.W
			} else {
				for c := 0; c < width; c++ {
					nv := w[vstart+c]
					acc.value[c] = acc.value[c] + (nv-acc.value[c])*t
				}
			}
			acc.totalWeight += weight
		}
		i = vstart + width
	}
}

// acquireAccum returns a reusable *blendAccum from the free pool, or a freshly
// allocated one when the pool is empty. The pool grows monotonically to the peak
// number of simultaneously-blended keys, so steady-state Update is alloc-free.
func (m *Mixer) acquireAccum() *blendAccum {
	if n := len(m.freePool); n > 0 {
		a := m.freePool[n-1]
		m.freePool = m.freePool[:n-1]
		a.totalWeight = 0
		return a
	}
	return &blendAccum{}
}

// emit writes one blended packed write per accumulated key into out, in
// deterministic (targetID, propID) ascending order, and recycles accumulators.
func (m *Mixer) emit(out *WriteBuf) {
	// Deterministic order: collect keys, sort ascending. Model animation has a
	// small, bounded key set; the sort cost is negligible.
	keys := m.keyScratch[:0]
	for k := range m.results {
		keys = append(keys, k)
	}
	m.keyScratch = keys
	insertionSortInt64(keys)

	for _, k := range keys {
		acc := m.results[k]
		tid, pid := unblendKey(k)
		out.Push(tid, pid, Value{Arity: acc.arity, F: acc.value})
		// recycle into the free pool.
		m.freePool = append(m.freePool, acc)
		delete(m.results, k)
	}
}

// blendKey packs (targetID, propID) into a single int64 map key.
func blendKey(targetID, propID int) int64 {
	return (int64(int32(targetID)) << 32) | int64(uint32(int32(propID)))
}

// unblendKey unpacks a blendKey back into (targetID, propID).
func unblendKey(k int64) (int, int) {
	return int(int32(k >> 32)), int(int32(uint32(k)))
}

// insertionSortInt64 sorts a small int64 slice ascending in place (no sort import
// to keep the dependency surface minimal; key sets are tiny).
func insertionSortInt64(a []int64) {
	for i := 1; i < len(a); i++ {
		v := a[i]
		j := i - 1
		for j >= 0 && a[j] > v {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = v
	}
}
