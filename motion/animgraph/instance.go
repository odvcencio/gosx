package animgraph

import "math"

// EventKind tells a FiredEvent apart: a clip notify, or a state change.
type EventKind uint8

const (
	// Notify is a clip Event crossed during Step.
	Notify EventKind = iota
	// Enter fires when a transition starts toward a state.
	Enter
	// Exit fires when a state stops contributing to its layer.
	Exit
)

// FiredEvent is one event raised by Step.
type FiredEvent struct {
	Kind EventKind
	// Name is the clip event name for Notify, and the state name for Enter
	// and Exit.
	Name  string
	Layer int
	State string
	// Clip is the clip that crossed the event time (Notify only).
	Clip string
	// Weight is the clip's blend weight in its layer when the event fired, so
	// game code can scale a footstep's volume during a crossfade.
	Weight float64
}

// Sample is one clip the renderer samples for a layer: Clip indexes the
// program's clips in declaration order, Time is in seconds.
type Sample struct {
	Clip   int
	Time   float64
	Weight float64
	Loop   bool
}

// StateInfo reports the current (newest) state of a layer.
type StateInfo struct {
	Name string
	// Normalized is the total normalized time: 2.5 means halfway through the
	// third loop.
	Normalized float64
	// InTransition is true while a crossfade into this state runs.
	InTransition bool
	// Progress is the crossfade progress in [0, 1]; 1 when not fading.
	Progress float64
}

// IKInput is the per-frame input for one two-bone chain. Positions and
// normals are in world space.
type IKInput struct {
	Weight    float64
	Target    [3]float64
	HasTarget bool
	// BoneTarget, when set, makes the chain reach a bone of another
	// character (TargetBone indexes HumanoidBones) plus TargetOffset in that
	// bone's frame. The renderer resolves it from the other character's pose
	// in the same frame, so both must go in one Dispatch.
	BoneTarget   *Instance
	TargetBone   int
	TargetOffset [3]float64
	Pole         [3]float64
	HasPole      bool
	GroundHeight float64
	GroundNormal [3]float64
	HasGround    bool
}

// LookInput is the per-frame input for one look-at chain.
type LookInput struct {
	Weight float64
	Target [3]float64
}

type stateInst struct {
	state      int
	phase      float64
	prev       float64
	from       float64
	weight     float64
	speedScale float64
	fresh      bool
	w          []float64
}

type layerState struct {
	stack         [maxActiveStates]stateInst
	n             int
	fading        bool
	fadeElapsed   float64
	fadeDuration  float64
	interruptible bool
	samples       [MaxLayerSamples]Sample
	sampleCount   int
	weight        float64
	playing       float64
	runtimeWeight float64
}

// Instance is one character's animation state. It is not safe for
// concurrent use. Step does not allocate once the event buffer has grown to
// its peak size.
type Instance struct {
	prog      *Program
	id        string
	params    []float64
	layers    []layerState
	events    []FiredEvent
	curves    []float64
	curveAcc  []float64
	curveHas  []bool
	ik        []IKInput
	look      []LookInput
	dt        float64
	steps     uint64
	feedback  Feedback
	rigOnPage bool
	actions   []actionState
	lod       lodState
	// sinceEncode is the time stepped since this instance was last encoded:
	// the frame dt the renderer uses for smoothing, also across frames that
	// LOD skips.
	sinceEncode float64
}

// New creates an Instance for the Scene3D model with the given ID. The model
// must be declared with scene.Model.ClipSampled set.
func (p *Program) New(modelID string) *Instance {
	in := &Instance{
		prog:     p,
		id:       modelID,
		params:   make([]float64, len(p.params)),
		layers:   make([]layerState, len(p.layers)),
		events:   make([]FiredEvent, 0, 16),
		curves:   make([]float64, len(p.curveNames)),
		curveAcc: make([]float64, len(p.curveNames)),
		curveHas: make([]bool, len(p.curveNames)),
		ik:       make([]IKInput, len(p.ik)),
		look:     make([]LookInput, len(p.look)),
		actions:  make([]actionState, len(p.layers)),
	}
	in.lod = newLODState(len(p.layers))
	points := p.maxPoints
	if points < 1 {
		points = 1
	}
	for li := range in.layers {
		backing := make([]float64, maxActiveStates*points)
		in.layers[li].runtimeWeight = 1
		for k := range in.layers[li].stack {
			in.layers[li].stack[k].w = backing[k*points : (k+1)*points : (k+1)*points]
		}
	}
	for i := range in.ik {
		in.ik[i].GroundNormal = [3]float64{0, 1, 0}
	}
	in.Reset()
	return in
}

// ID returns the model ID this instance poses.
func (in *Instance) ID() string { return in.id }

// Program returns the compiled graph this instance runs.
func (in *Instance) Program() *Program { return in.prog }

// Reset restores parameter defaults, enters each layer's entry state at time
// zero, and clears events. IK inputs and layer weights are kept.
func (in *Instance) Reset() {
	for i, param := range in.prog.params {
		in.params[i] = param.Default
		if param.Kind != Float && param.Default != 0 {
			in.params[i] = 1
		}
	}
	for li := range in.layers {
		ls := &in.layers[li]
		ls.n = 0
		ls.fading = false
		ls.sampleCount = 0
		in.actions[li] = actionState{}
		in.push(li, in.prog.layers[li].machine.entry, 0)
		ls.stack[0].weight = 1
		in.buildSamples(li)
	}
	in.events = in.events[:0]
	in.updateCurves()
}

// SetFloat sets a parameter by name. Unknown names are ignored.
func (in *Instance) SetFloat(name string, v float64) { in.SetFloatID(in.prog.ParamID(name), v) }

// SetBool sets a Bool or Trigger parameter by name.
func (in *Instance) SetBool(name string, v bool) { in.SetBoolID(in.prog.ParamID(name), v) }

// SetTrigger sets a Trigger parameter. The first transition that tests it
// resets it.
func (in *Instance) SetTrigger(name string) { in.SetBoolID(in.prog.ParamID(name), true) }

// ResetTrigger clears a Trigger parameter that no transition consumed.
func (in *Instance) ResetTrigger(name string) { in.SetBoolID(in.prog.ParamID(name), false) }

// SetFloatID sets a parameter by ParamID. Non-finite values are ignored.
func (in *Instance) SetFloatID(id int, v float64) {
	if id < 0 || id >= len(in.params) || !finite(v) {
		return
	}
	if in.prog.params[id].Kind != Float {
		if v != 0 {
			v = 1
		}
	}
	in.params[id] = v
}

// SetBoolID sets a Bool or Trigger parameter by ParamID.
func (in *Instance) SetBoolID(id int, v bool) {
	if id < 0 || id >= len(in.params) {
		return
	}
	if v {
		in.params[id] = 1
	} else {
		in.params[id] = 0
	}
}

// Float returns a parameter value by name (Bool and Trigger read 0 or 1).
func (in *Instance) Float(name string) float64 {
	if id := in.prog.ParamID(name); id >= 0 {
		return in.params[id]
	}
	return 0
}

// SetLayerWeight scales a layer at run time, clamped to [0, 1].
func (in *Instance) SetLayerWeight(layer int, w float64) {
	if layer < 0 || layer >= len(in.layers) || !finite(w) {
		return
	}
	in.layers[layer].runtimeWeight = clamp01(w)
}

// LayerWeight returns the effective weight the renderer applies to a layer
// after the last Step: run-time weight, weight parameter, and the share of
// states that play a motion.
func (in *Instance) LayerWeight(layer int) float64 {
	if layer < 0 || layer >= len(in.layers) {
		return 0
	}
	return in.layers[layer].weight
}

// Step advances every layer by dt seconds: crossfades, clip times, events,
// then at most one transition per layer. Negative or non-finite dt counts as
// zero.
func (in *Instance) Step(dt float64) {
	if !finite(dt) || dt < 0 {
		dt = 0
	}
	in.dt = dt
	in.sinceEncode += dt
	in.steps++
	in.events = in.events[:0]
	for li := range in.layers {
		in.stepLayer(li, dt)
	}
	in.updateCurves()
	in.advanceLOD(dt)
}

// Events returns the events the last Step raised. The slice is reused by the
// next Step.
func (in *Instance) Events() []FiredEvent { return in.events }

// State reports the newest state of a layer.
func (in *Instance) State(layer int) StateInfo {
	if layer < 0 || layer >= len(in.layers) {
		return StateInfo{}
	}
	ls := &in.layers[layer]
	top := &ls.stack[ls.n-1]
	info := StateInfo{
		Name:       in.prog.layers[layer].machine.states[top.state].name,
		Normalized: top.phase,
		Progress:   1,
	}
	if ls.fading {
		info.InTransition = true
		info.Progress = clamp01(ls.fadeElapsed / ls.fadeDuration)
	}
	return info
}

// Curve returns a blended curve value after the last Step, or 0.
func (in *Instance) Curve(name string) float64 {
	if index, ok := in.prog.curveIndex[name]; ok {
		return in.curves[index]
	}
	return 0
}

// Samples returns the clips the renderer will sample for a layer. The slice
// is reused by the next Step.
func (in *Instance) Samples(layer int) []Sample {
	if layer < 0 || layer >= len(in.layers) {
		return nil
	}
	ls := &in.layers[layer]
	return ls.samples[:ls.sampleCount]
}

// SetIKWeight sets a chain's weight in [0, 1]; zero disables the chain.
func (in *Instance) SetIKWeight(chain int, w float64) {
	if chain >= 0 && chain < len(in.ik) && finite(w) {
		in.ik[chain].Weight = clamp01(w)
	}
}

// SetIKTarget moves a chain's end bone toward a world position.
func (in *Instance) SetIKTarget(chain int, pos [3]float64) {
	if chain >= 0 && chain < len(in.ik) && finite3(pos) {
		in.ik[chain].Target, in.ik[chain].HasTarget = pos, true
	}
}

// ClearIKTarget stops target reaching for a chain, for a position or a
// bone target.
func (in *Instance) ClearIKTarget(chain int) {
	if chain >= 0 && chain < len(in.ik) {
		in.ik[chain].HasTarget = false
		in.ik[chain].BoneTarget = nil
	}
}

// SetIKTargetBone makes a chain reach a canonical bone of another character
// (for example a hand to the other's shoulder), offset in that bone's frame.
// Send both instances in the same Dispatch. It reports false for an unknown
// bone or chain.
func (in *Instance) SetIKTargetBone(chain int, other *Instance, bone string, offset [3]float64) bool {
	index := HumanoidBoneID(bone)
	if chain < 0 || chain >= len(in.ik) || other == nil || other == in || index < 0 || !finite3(offset) {
		return false
	}
	ik := &in.ik[chain]
	ik.BoneTarget, ik.TargetBone, ik.TargetOffset = other, index, offset
	ik.HasTarget = false
	return true
}

// HumanoidBoneID returns a canonical bone's index in HumanoidBones, or -1.
func HumanoidBoneID(name string) int {
	for i, bone := range HumanoidBones {
		if bone == name {
			return i
		}
	}
	return -1
}

// SetIKPole sets the world position the knee or elbow bends toward. Without
// a pole the chain bends toward its animated mid bone.
func (in *Instance) SetIKPole(chain int, pos [3]float64) {
	if chain >= 0 && chain < len(in.ik) && finite3(pos) {
		in.ik[chain].Pole, in.ik[chain].HasPole = pos, true
	}
}

// ClearIKPole returns a chain to its animated bend direction.
func (in *Instance) ClearIKPole(chain int) {
	if chain >= 0 && chain < len(in.ik) {
		in.ik[chain].HasPole = false
	}
}

// SetGround gives a foot chain the ground under its foot: a world height and
// a normal, typically from a downward raycast at Feedback().Animated(chain).
func (in *Instance) SetGround(chain int, height float64, normal [3]float64) {
	if chain < 0 || chain >= len(in.ik) || !finite(height) || !finite3(normal) {
		return
	}
	length := math.Sqrt(normal[0]*normal[0] + normal[1]*normal[1] + normal[2]*normal[2])
	if length < 1e-9 {
		normal, length = [3]float64{0, 1, 0}, 1
	}
	in.ik[chain].GroundHeight = height
	in.ik[chain].GroundNormal = [3]float64{normal[0] / length, normal[1] / length, normal[2] / length}
	in.ik[chain].HasGround = true
}

// ClearGround stops ground placement for a foot chain.
func (in *Instance) ClearGround(chain int) {
	if chain >= 0 && chain < len(in.ik) {
		in.ik[chain].HasGround = false
	}
}

// IK returns a chain's current input.
func (in *Instance) IK(chain int) IKInput {
	if chain >= 0 && chain < len(in.ik) {
		return in.ik[chain]
	}
	return IKInput{}
}

// IKWeight returns the weight sent for a chain: SetIKWeight times its weight
// curve, if it has one, times the level-of-detail IK fade.
func (in *Instance) IKWeight(chain int) float64 {
	if chain < 0 || chain >= len(in.ik) {
		return 0
	}
	w := in.ik[chain].Weight * in.lod.ik
	if curve := in.prog.ik[chain].weightCurve; curve >= 0 {
		w *= clamp01(in.curves[curve])
	}
	return w
}

// SetLookAt aims a look-at chain at a world position with weight in [0, 1].
func (in *Instance) SetLookAt(chain int, target [3]float64, weight float64) {
	if chain < 0 || chain >= len(in.look) || !finite3(target) || !finite(weight) {
		return
	}
	in.look[chain] = LookInput{Weight: clamp01(weight), Target: target}
}

func (in *Instance) stepLayer(li int, dt float64) {
	ls := &in.layers[li]
	machine := &in.prog.layers[li].machine
	if ls.fading {
		ls.fadeElapsed += dt
		if ls.fadeElapsed >= ls.fadeDuration-timeEpsilon {
			in.finishFade(li)
		}
	}
	for k := 0; k < ls.n; k++ {
		si := &ls.stack[k]
		sd := &machine.states[si.state]
		in.motionWeights(sd, si)
		si.prev = si.phase
		if d := in.effectiveDuration(sd, si); d > 0 {
			si.phase += dt * in.stateSpeed(sd, si) / d
		}
	}
	if leader := in.prog.layers[li].syncLayer; leader >= 0 {
		in.syncToLayer(li, leader)
	}
	in.updateStackWeights(ls)
	in.fireEvents(li)
	for k := 0; k < ls.n; k++ {
		ls.stack[k].fresh = false
	}
	if t := in.pickTransition(li); t != nil {
		in.startTransition(li, t)
		in.updateStackWeights(ls)
	}
	in.buildSamples(li)
	in.advanceAction(li, dt)
	in.mergeAction(li)
}

// syncToLayer gives this layer's playing states the normalized time of the
// leader layer's dominant state, so follower clips stay in phase with it.
func (in *Instance) syncToLayer(li, leader int) {
	ll := &in.layers[leader]
	lm := &in.prog.layers[leader].machine
	best := -1
	for k := 0; k < ll.n; k++ {
		if lm.states[ll.stack[k].state].kind == motionNone {
			continue
		}
		if best < 0 || ll.stack[k].weight >= ll.stack[best].weight {
			best = k
		}
	}
	if best < 0 {
		return
	}
	lead := &ll.stack[best]
	ls := &in.layers[li]
	for k := 0; k < ls.n; k++ {
		si := &ls.stack[k]
		if in.prog.layers[li].machine.states[si.state].kind == motionNone {
			continue
		}
		si.prev, si.phase = lead.prev, lead.phase
	}
}

// layerScale is a layer's run-time weight times its weight parameter.
func (in *Instance) layerScale(li int) float64 {
	w := in.layers[li].runtimeWeight
	if param := in.prog.layers[li].weightParam; param >= 0 {
		w *= clamp01(in.params[param])
	}
	return w
}

func (in *Instance) push(li, state int, offset float64) *stateInst {
	ls := &in.layers[li]
	si := &ls.stack[ls.n]
	ls.n++
	si.state = state
	si.phase, si.prev = offset, offset
	si.from, si.weight = 0, 0
	si.speedScale = 1
	si.fresh = true
	for i := range si.w {
		si.w[i] = 0
	}
	in.motionWeights(&in.prog.layers[li].machine.states[state], si)
	return si
}

// remove drops stack entry k, keeping order. Entries are swapped, never
// copied, so each keeps its own weight buffer.
func (ls *layerState) remove(k int) {
	for i := k; i < ls.n-1; i++ {
		ls.stack[i], ls.stack[i+1] = ls.stack[i+1], ls.stack[i]
	}
	ls.n--
}

func (in *Instance) finishFade(li int) {
	ls := &in.layers[li]
	for ls.n > 1 {
		in.emitState(li, Exit, &ls.stack[0])
		ls.remove(0)
	}
	ls.fading = false
	ls.fadeElapsed, ls.fadeDuration = 0, 0
}

func (in *Instance) updateStackWeights(ls *layerState) {
	if ls.n == 0 {
		return
	}
	if !ls.fading {
		ls.stack[ls.n-1].weight = 1
		return
	}
	a := clamp01(ls.fadeElapsed / ls.fadeDuration)
	ls.stack[ls.n-1].weight = a
	for k := 0; k < ls.n-1; k++ {
		ls.stack[k].weight = ls.stack[k].from * (1 - a)
	}
}

func (in *Instance) motionWeights(sd *stateDef, si *stateInst) {
	si.speedScale = 1
	switch sd.kind {
	case motionClip:
		si.w[0] = 1
	case motion1D:
		v := in.params[sd.param]
		n := len(sd.at)
		for i := 0; i < n; i++ {
			si.w[i] = 0
		}
		switch {
		case v <= sd.at[0]:
			si.w[0] = 1
		case v >= sd.at[n-1]:
			si.w[n-1] = 1
			if sd.scaleSpeed && sd.at[n-1] > 0 {
				si.speedScale = v / sd.at[n-1]
			}
		default:
			for i := 0; i < n-1; i++ {
				if v < sd.at[i+1] {
					t := (v - sd.at[i]) / (sd.at[i+1] - sd.at[i])
					si.w[i], si.w[i+1] = 1-t, t
					break
				}
			}
		}
	case motion2D:
		gradientBand(sd.px, sd.py, in.params[sd.param], in.params[sd.paramY], si.w[:len(sd.px)])
	}
}

// gradientBand writes gradient band interpolation weights (Johansen 2009,
// the cartesian form): for each point i, h_i = min over j != i of
// 1 - ((p - p_i) . (p_j - p_i)) / |p_j - p_i|^2, clamped at zero, then
// normalized. Weights are exact at every point and change smoothly between
// them.
func gradientBand(px, py []float64, x, y float64, out []float64) {
	n := len(px)
	if n == 1 {
		out[0] = 1
		return
	}
	sum := 0.0
	for i := 0; i < n; i++ {
		h := math.Inf(1)
		ax, ay := x-px[i], y-py[i]
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			bx, by := px[j]-px[i], py[j]-py[i]
			v := 1 - (ax*bx+ay*by)/(bx*bx+by*by)
			if v < h {
				h = v
			}
		}
		if h < 0 {
			h = 0
		}
		out[i] = h
		sum += h
	}
	if sum <= 0 {
		best, bestDist := 0, math.Inf(1)
		for i := 0; i < n; i++ {
			out[i] = 0
			if d := (x-px[i])*(x-px[i]) + (y-py[i])*(y-py[i]); d < bestDist {
				best, bestDist = i, d
			}
		}
		out[best] = 1
		return
	}
	for i := 0; i < n; i++ {
		out[i] /= sum
	}
}

// effectiveDuration is the weight-averaged clip duration: the length of one
// normalized cycle of the state. A state without motion counts one second so
// exit times still read as seconds.
func (in *Instance) effectiveDuration(sd *stateDef, si *stateInst) float64 {
	if sd.kind == motionNone {
		return 1
	}
	d := 0.0
	for j, clip := range sd.clips {
		d += si.w[j] * in.prog.clips[clip].duration
	}
	return d
}

func (in *Instance) stateSpeed(sd *stateDef, si *stateInst) float64 {
	speed := sd.speed * si.speedScale
	if sd.speedParam >= 0 {
		speed *= in.params[sd.speedParam]
	}
	if !(speed > 0) {
		return 0
	}
	return speed
}

func (in *Instance) pickTransition(li int) *transDef {
	ls := &in.layers[li]
	machine := &in.prog.layers[li].machine
	top := &ls.stack[ls.n-1]
	sd := &machine.states[top.state]
	for i := range machine.any {
		t := &machine.any[i]
		if t.to == top.state && !t.canSelf {
			continue
		}
		if in.transitionReady(t, top, sd) {
			return t
		}
	}
	if ls.fading && !ls.interruptible {
		return nil
	}
	for i := range sd.trans {
		if t := &sd.trans[i]; in.transitionReady(t, top, sd) {
			return t
		}
	}
	return nil
}

func (in *Instance) transitionReady(t *transDef, si *stateInst, sd *stateDef) bool {
	for i := range t.conds {
		if !in.condHolds(&t.conds[i]) {
			return false
		}
	}
	return t.exitTime <= 0 || exitReached(si.prev, si.phase, t.exitTime, sd.loop)
}

// timeEpsilon absorbs float drift from summing frame times, so a boundary
// that a run of 1/60 s steps reaches exactly is crossed on that step.
const timeEpsilon = 1e-9

func exitReached(prev, phase, exit float64, loop bool) bool {
	if loop && exit <= 1 {
		frac := phase - math.Floor(phase+timeEpsilon)
		return frac >= exit-timeEpsilon || math.Floor(prev-exit+timeEpsilon) < math.Floor(phase-exit+timeEpsilon)
	}
	return phase >= exit-timeEpsilon
}

func (in *Instance) condHolds(c *condDef) bool {
	v := in.params[c.param]
	switch c.op {
	case Greater:
		return v > c.value
	case Less:
		return v < c.value
	case GreaterOrEqual:
		return v >= c.value
	case LessOrEqual:
		return v <= c.value
	case Equal:
		return v == c.value
	case NotEqual:
		return v != c.value
	case IsTrue, Triggered:
		return v != 0
	case IsFalse:
		return v == 0
	}
	return false
}

func (in *Instance) startTransition(li int, t *transDef) {
	for i := range t.conds {
		c := &t.conds[i]
		if in.prog.params[c.param].Kind == Trigger && (c.op == Triggered || c.op == IsTrue) {
			in.params[c.param] = 0
		}
	}
	ls := &in.layers[li]
	if t.duration <= 0 {
		for k := 0; k < ls.n; k++ {
			in.emitState(li, Exit, &ls.stack[k])
		}
		ls.n = 0
		ls.fading = false
		ls.fadeElapsed, ls.fadeDuration = 0, 0
	} else {
		for k := 0; k < ls.n; k++ {
			ls.stack[k].from = ls.stack[k].weight
		}
		if ls.n == maxActiveStates {
			smallest := 0
			for k := 1; k < ls.n; k++ {
				if ls.stack[k].from < ls.stack[smallest].from {
					smallest = k
				}
			}
			in.emitState(li, Exit, &ls.stack[smallest])
			ls.remove(smallest)
		}
		ls.fading = true
		ls.fadeElapsed = 0
		ls.fadeDuration = t.duration
		ls.interruptible = t.interruptible
	}
	si := in.push(li, t.to, t.offset)
	in.emitState(li, Enter, si)
}

func (in *Instance) emitState(li int, kind EventKind, si *stateInst) {
	name := in.prog.layers[li].machine.states[si.state].name
	in.events = append(in.events, FiredEvent{Kind: kind, Name: name, Layer: li, State: name, Weight: si.weight})
}

func (in *Instance) fireEvents(li int) {
	ls := &in.layers[li]
	machine := &in.prog.layers[li].machine
	if in.prog.eventMode == EventsAll {
		for k := 0; k < ls.n; k++ {
			si := &ls.stack[k]
			sd := &machine.states[si.state]
			for j, clip := range sd.clips {
				w := si.weight * si.w[j]
				if w > 0 && w >= in.prog.eventMin {
					in.clipEvents(li, si, sd, clip, w)
				}
			}
		}
		return
	}
	best := -1
	for k := 0; k < ls.n; k++ {
		si := &ls.stack[k]
		if machine.states[si.state].kind == motionNone || si.weight <= 0 {
			continue
		}
		if best < 0 || si.weight >= ls.stack[best].weight {
			best = k
		}
	}
	if best < 0 {
		return
	}
	si := &ls.stack[best]
	sd := &machine.states[si.state]
	clipSlot := 0
	for j := range sd.clips {
		if si.w[j] > si.w[clipSlot] {
			clipSlot = j
		}
	}
	if w := si.weight * si.w[clipSlot]; w > 0 && w >= in.prog.eventMin {
		in.clipEvents(li, si, sd, sd.clips[clipSlot], w)
	}
}

func (in *Instance) clipEvents(li int, si *stateInst, sd *stateDef, clip int, weight float64) {
	def := &in.prog.clips[clip]
	for i := range def.events {
		e := def.events[i].normalized
		if crossed(si.prev, si.phase, e, sd.loop, si.fresh) {
			in.events = append(in.events, FiredEvent{
				Kind: Notify, Name: def.events[i].name, Layer: li, State: sd.name, Clip: def.name, Weight: weight,
			})
		}
	}
}

// crossed reports whether normalized playback moved over event time e
// between prev and phase. The interval is (prev, phase], or [prev, phase] on
// the first step of a state so an event at time zero fires.
func crossed(prev, phase, e float64, loop, inclusive bool) bool {
	if phase < prev {
		return false
	}
	phase += timeEpsilon
	prev += timeEpsilon
	if !loop {
		if inclusive {
			return prev-2*timeEpsilon <= e && e <= phase
		}
		return prev < e && e <= phase
	}
	if inclusive {
		return math.Floor(phase-e) >= math.Ceil(prev-2*timeEpsilon-e)
	}
	return math.Floor(phase-e) > math.Floor(prev-e)
}

func (in *Instance) buildSamples(li int) {
	ls := &in.layers[li]
	machine := &in.prog.layers[li].machine
	ls.sampleCount = 0
	playing := 0.0
	for k := 0; k < ls.n; k++ {
		si := &ls.stack[k]
		sd := &machine.states[si.state]
		if sd.kind == motionNone || si.weight <= 0 {
			continue
		}
		playing += si.weight
		for j, clip := range sd.clips {
			w := si.weight * si.w[j]
			if w < minSampleWeight {
				continue
			}
			s := Sample{Clip: clip, Time: clipTime(si.phase, in.prog.clips[clip].duration, sd.loop), Weight: w, Loop: sd.loop}
			if ls.sampleCount < MaxLayerSamples {
				ls.samples[ls.sampleCount] = s
				ls.sampleCount++
				continue
			}
			smallest := 0
			for i := 1; i < MaxLayerSamples; i++ {
				if ls.samples[i].Weight < ls.samples[smallest].Weight {
					smallest = i
				}
			}
			if w > ls.samples[smallest].Weight {
				ls.samples[smallest] = s
			}
		}
	}
	ls.playing = playing
	ls.weight = clamp01(in.layerScale(li) * playing)
}

func clipTime(phase, duration float64, loop bool) float64 {
	if loop {
		return (phase - math.Floor(phase)) * duration
	}
	return clamp01(phase) * duration
}

func (in *Instance) updateCurves() {
	if len(in.curves) == 0 {
		return
	}
	for c := range in.curves {
		in.curves[c] = 0
	}
	for li := range in.layers {
		ls := &in.layers[li]
		if ls.weight <= 0 || ls.sampleCount == 0 {
			continue
		}
		total := 0.0
		for c := range in.curveAcc {
			in.curveAcc[c], in.curveHas[c] = 0, false
		}
		for i := 0; i < ls.sampleCount; i++ {
			s := &ls.samples[i]
			total += s.Weight
			for _, cd := range in.prog.clips[s.Clip].curves {
				in.curveAcc[cd.curve] += s.Weight * evalCurve(cd.keys, s.Time)
				in.curveHas[cd.curve] = true
			}
		}
		additive := in.prog.layers[li].additive
		for c := range in.curves {
			if !in.curveHas[c] || total <= 0 {
				continue
			}
			v := in.curveAcc[c] / total
			if additive {
				in.curves[c] += v * ls.weight
			} else {
				in.curves[c] += (v - in.curves[c]) * ls.weight
			}
		}
	}
}

func evalCurve(keys []CurveKey, t float64) float64 {
	if t <= keys[0].T {
		return keys[0].V
	}
	last := len(keys) - 1
	if t >= keys[last].T {
		return keys[last].V
	}
	lo, hi := 0, last
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if keys[mid].T <= t {
			lo = mid
		} else {
			hi = mid
		}
	}
	span := keys[hi].T - keys[lo].T
	if span <= 0 {
		return keys[hi].V
	}
	return keys[lo].V + (keys[hi].V-keys[lo].V)*(t-keys[lo].T)/span
}

func clamp01(v float64) float64 {
	if !(v > 0) {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
