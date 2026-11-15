// Package animgraph drives skeletal animation for Scene3D models from Go.
//
// A Graph declares parameters, clips, layered state machines, blend spaces,
// bone masks, and inverse-kinematics (IK) chains. Compile checks the
// declaration once and returns a Program. Each animated character gets its own
// Instance. Game code sets parameters (speed, grounded, aim), calls Step once
// per frame, reads the events that fired (footsteps, hit frames), and sends the
// result to the renderer with Dispatch.
//
// The split is deliberate. The state machine, blend weights, clip times, events
// and curves run here, in Go, so they are deterministic, testable without a
// browser, and usable on an authoritative server. The renderer owns the clip
// keyframes and the skinning, so it samples the clips at the times this package
// chooses, applies bone masks and additive layers, solves IK against the posed
// skeleton, and uploads joint matrices. docs/scene3d-animation.md explains the
// measurements behind that split.
package animgraph

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// ParamKind selects how a parameter stores and compares its value.
type ParamKind uint8

const (
	// Float holds any finite number.
	Float ParamKind = iota
	// Bool holds 0 (false) or 1 (true).
	Bool
	// Trigger is a Bool that a transition resets when it fires.
	Trigger
)

// Param declares one graph parameter and its value at Instance creation.
type Param struct {
	Name    string
	Kind    ParamKind
	Default float64
}

// Clip describes one glTF animation the graph can play.
//
// Name matches the glTF animation name. A name without "|" also matches a
// Blender export name of the form "Armature|Name". Duration is the clip length
// in seconds and must match the asset: the graph uses it for exit times,
// blend-space sync, and event timing. ReadClipInfo in the gltfclip subpackage
// reads names and durations from a .glb or .gltf file.
type Clip struct {
	Name     string
	Duration float64
	// Loop is the default for states that play this clip.
	Loop bool
	// Events fire when playback crosses their time.
	Events []Event
	// Curves are named float tracks keyed in clip seconds, blended by clip
	// weight. Use them for IK weights (foot planted = 1) and gameplay values.
	Curves []Curve
}

// Event marks a named moment in a clip, in seconds from the clip start.
type Event struct {
	Name string
	Time float64
}

// Curve is a piecewise-linear float track. Keys must be sorted by T (seconds).
type Curve struct {
	Name string
	Keys []CurveKey
}

// CurveKey is one curve point: value V at clip time T in seconds.
type CurveKey struct {
	T, V float64
}

// EventMode selects which playing clips fire events.
type EventMode uint8

const (
	// EventsDominant fires events only from the highest-weighted state of each
	// layer, and within it the highest-weighted clip. Synced blend spaces
	// therefore fire one footstep per step, not one per blended clip.
	EventsDominant EventMode = iota
	// EventsAll fires events from every clip whose weight is at least
	// Graph.EventMinWeight.
	EventsAll
)

// Graph is the full animation declaration for one kind of character.
type Graph struct {
	Params []Param
	Clips  []Clip
	// Layers evaluate in order. Layer 0 is the base pose; later layers
	// override or add to it inside their masks.
	Layers []Layer
	Masks  []Mask
	IK     []TwoBoneIK
	LookAt []LookAt
	// Pelvis lets foot placement lower the hips so a leg can reach ground below
	// the animated foot. Zero value: no pelvis adjustment.
	Pelvis Pelvis
	// Rig selects how the renderer maps node names to canonical humanoid bones.
	Rig Rig
	// EventMode and EventMinWeight filter which clips fire events.
	EventMode      EventMode
	EventMinWeight float64
}

// Layer is one state machine whose pose combines with the layers below it.
type Layer struct {
	Name string
	// Mask names one of Graph.Masks. Empty means the whole skeleton.
	Mask string
	// Additive adds this layer's motion as an offset from a reference pose.
	// The reference is each clip's own first frame unless RefClip is set.
	Additive bool
	RefClip  string
	RefTime  float64
	// WeightParam names a Float parameter (clamped to [0, 1]) that scales the
	// layer. Instance.SetLayerWeight also scales it.
	WeightParam string
	// SyncLayer names an earlier layer whose playing state sets this layer's
	// normalized time, so a style clip (a waddle sway, a bounce) stays in step
	// with the locomotion cycle under it.
	SyncLayer string
	Machine   StateMachine
}

// StateMachine is a set of states with transitions between them.
type StateMachine struct {
	// Entry names the starting state. Empty means the first state.
	Entry  string
	States []State
	// Any holds transitions that may leave every state.
	Any []Transition
}

// LoopMode overrides a clip's loop default for one state.
type LoopMode uint8

const (
	LoopDefault LoopMode = iota
	LoopOn
	LoopOff
)

// State plays one motion: a clip, a 1D blend space, or a 2D blend space. A
// state with no motion contributes nothing; in an upper layer it fades the
// layer out.
type State struct {
	Name    string
	Clip    string
	Blend1D *BlendSpace1D
	Blend2D *BlendSpace2D
	// Speed multiplies playback rate. Zero means 1.
	Speed float64
	// SpeedParam names a Float parameter that further multiplies the rate.
	SpeedParam  string
	Loop        LoopMode
	Transitions []Transition
}

// BlendSpace1D blends clips placed along one parameter axis. Clips play in
// sync by normalized time, so a walk and a run with different durations keep
// their feet in phase.
type BlendSpace1D struct {
	Param  string
	Points []BlendPoint1D
	// ScaleSpeed raises the playback rate when the parameter exceeds the last
	// point (or falls below the first, for a positive first point), in
	// proportion. Use it when points sit at their clip's ground speed so feet
	// do not slide.
	ScaleSpeed bool
}

// BlendPoint1D places a clip at parameter value At.
type BlendPoint1D struct {
	Clip string
	At   float64
}

// BlendSpace2D blends clips placed on a plane, for example strafe velocity (X)
// and forward velocity (Y). Weights use gradient band interpolation, which
// works for any point layout and gives exact weights at each point.
type BlendSpace2D struct {
	ParamX, ParamY string
	Points         []BlendPoint2D
}

// BlendPoint2D places a clip at (X, Y).
type BlendPoint2D struct {
	Clip string
	X, Y float64
}

// Op compares a parameter against Condition.Value.
type Op uint8

const (
	Greater Op = iota
	Less
	GreaterOrEqual
	LessOrEqual
	Equal
	NotEqual
	IsTrue
	IsFalse
	// Triggered holds when a Trigger parameter is set; the transition resets it.
	Triggered
)

// Condition is one test on a parameter. A transition fires when all of its
// conditions hold.
type Condition struct {
	Param string
	Op    Op
	Value float64
}

// When builds a comparison condition.
func When(param string, op Op, value float64) Condition {
	return Condition{Param: param, Op: op, Value: value}
}

// WhenTrue holds while a Bool parameter is true.
func WhenTrue(param string) Condition { return Condition{Param: param, Op: IsTrue} }

// WhenFalse holds while a Bool parameter is false.
func WhenFalse(param string) Condition { return Condition{Param: param, Op: IsFalse} }

// WhenTriggered holds while a Trigger parameter is set.
func WhenTriggered(param string) Condition { return Condition{Param: param, Op: Triggered} }

// Transition moves a state machine to the state named To.
type Transition struct {
	To   string
	When []Condition
	// ExitTime gates the transition on the source state's normalized time.
	// Zero means no gate. For a looping state and ExitTime <= 1, the gate
	// opens at that fraction of every loop; otherwise it opens once the total
	// normalized time reaches ExitTime.
	ExitTime float64
	// Duration is the crossfade length in seconds. Zero switches at once.
	Duration float64
	// Offset is the destination's starting normalized time.
	Offset float64
	// CanSelf lets an any-state transition restart the state it targets.
	CanSelf bool
	// Interruptible lets the destination's own transitions fire before this
	// crossfade ends. Any-state transitions can always interrupt.
	Interruptible bool
}

// Mask selects the bones a layer affects. Bone names are canonical humanoid
// names (see HumanoidBones) or raw glTF node names.
type Mask struct {
	Name string
	// Include adds each named bone and its descendants with weight 1.
	Include []string
	// Exclude removes each named bone and its descendants.
	Exclude []string
	// Weights sets the weight of single bones (not descendants) after Include
	// and Exclude, for example a gradual spine blend.
	Weights map[string]float64
}

// TwoBoneIK declares one analytic two-bone chain: Root (upper leg or upper
// arm), Mid (knee or elbow), and End (foot or hand).
type TwoBoneIK struct {
	Name           string
	Root, Mid, End string
	// Foot marks a leg chain for ground placement. Instance.SetGround supplies
	// the ground under the foot, and the solver keeps the animated clearance.
	Foot bool
	// AlignToGround rotates a foot to the ground normal (Foot chains only).
	AlignToGround bool
	// WeightCurve names a clip curve that scales the chain weight each frame.
	WeightCurve string
}

// LookAt rotates a bone chain so the last bone's forward axis points at a
// target: head look-at, or upper-body aim with Pivot set to a hand.
type LookAt struct {
	Name string
	// Bones run from the chain root to the tip, for example spine, chest,
	// neck, head.
	Bones []string
	// Weights splits the rotation across Bones; empty means equal parts. The
	// last bone always finishes the remaining rotation.
	Weights []float64
	// Axis is the forward direction in model space at the bind pose. Zero
	// means +Z, the glTF forward.
	Axis [3]float64
	// Pivot names the bone whose axis aims; empty means the last bone.
	Pivot string
	// MaxAngle limits the rotation away from the animated pose, in radians.
	// Zero means 1.2 radians.
	MaxAngle float64
}

// Pelvis configures hip adjustment during foot placement.
type Pelvis struct {
	// Bone names the hips; empty means the canonical "hips" bone.
	Bone string
	// MaxDrop limits how far the hips may move down, in meters. Zero
	// disables pelvis adjustment.
	MaxDrop float64
}

// Rig tells the renderer how to name and source bones.
type Rig struct {
	// Humanoid selects the naming scheme: "auto" (default), "mixamo",
	// "rigify", "unreal", "meshy", "biped", or "none" (raw node names only).
	Humanoid string
	// Bones maps canonical bone names to node names, overriding detection.
	Bones map[string]string
	// Sources lists extra glTF files whose clips retarget onto this model by
	// canonical bone name.
	Sources []ClipSource
	// KeepProportions drops translation keys on every bone except the root
	// bones from the model's own clips, so clips exported from a rig with
	// other bone lengths (a shared animation set on a mascot with a long neck)
	// keep this rig's proportions. Retargeted Sources always do this.
	KeepProportions bool
	// BoneScale multiplies the scale of single bones (canonical or node
	// names) after animation, for stylized proportions such as an oversized
	// head. The scale carries to the bone's children.
	BoneScale map[string]float64
}

// ClipSource is a glTF file whose animations play on this model.
type ClipSource struct {
	Src      string
	Humanoid string
	// Prefix is prepended to each clip name to avoid collisions.
	Prefix string
}

// Limits on the wire format and the per-layer crossfade stack.
const (
	MaxLayers        = 8
	MaxLayerSamples  = 16
	MaxIK            = 8
	MaxLookAt        = 4
	maxActiveStates  = 4
	minSampleWeight  = 1e-4
	defaultMaxLookAt = 1.2
)

// Program is a compiled, immutable Graph. It is safe to share between
// goroutines; each Instance is not.
type Program struct {
	params     []Param
	paramIndex map[string]int
	clips      []clipDef
	clipIndex  map[string]int
	curveNames []string
	curveIndex map[string]int
	layers     []layerDef
	ik         []ikDef
	look       []lookDef
	eventMode  EventMode
	eventMin   float64
	maxPoints  int
	rigJSON    []byte
	rigHash    uint32
}

type clipDef struct {
	name     string
	duration float64
	loop     bool
	events   []eventDef
	curves   []curveDef
}

type eventDef struct {
	name       string
	normalized float64
}

type curveDef struct {
	curve int
	keys  []CurveKey
}

type layerDef struct {
	name        string
	weightParam int
	additive    bool
	syncLayer   int
	machine     machineDef
}

type machineDef struct {
	entry  int
	states []stateDef
	any    []transDef
}

type motionKind uint8

const (
	motionNone motionKind = iota
	motionClip
	motion1D
	motion2D
)

type stateDef struct {
	name       string
	kind       motionKind
	clips      []int
	param      int
	paramY     int
	at         []float64
	px, py     []float64
	scaleSpeed bool
	speed      float64
	speedParam int
	loop       bool
	trans      []transDef
}

type transDef struct {
	to            int
	conds         []condDef
	exitTime      float64
	duration      float64
	offset        float64
	canSelf       bool
	interruptible bool
}

type condDef struct {
	param int
	op    Op
	value float64
}

type ikDef struct {
	name        string
	weightCurve int
}

type lookDef struct {
	name string
}

// MustCompile is Compile that panics on error, for package-level graphs.
func (g Graph) MustCompile() *Program {
	p, err := g.Compile()
	if err != nil {
		panic(err)
	}
	return p
}

// Compile validates the graph and resolves every name to an index.
func (g Graph) Compile() (*Program, error) {
	p := &Program{
		paramIndex: make(map[string]int, len(g.Params)),
		clipIndex:  make(map[string]int, len(g.Clips)),
		curveIndex: map[string]int{},
		eventMode:  g.EventMode,
		eventMin:   g.EventMinWeight,
	}
	if len(g.Layers) == 0 {
		return nil, errors.New("animgraph: graph has no layers")
	}
	if len(g.Layers) > MaxLayers {
		return nil, fmt.Errorf("animgraph: graph has %d layers, limit %d", len(g.Layers), MaxLayers)
	}
	if len(g.IK) > MaxIK {
		return nil, fmt.Errorf("animgraph: graph has %d IK chains, limit %d", len(g.IK), MaxIK)
	}
	if len(g.LookAt) > MaxLookAt {
		return nil, fmt.Errorf("animgraph: graph has %d look-at chains, limit %d", len(g.LookAt), MaxLookAt)
	}
	if !finite(g.EventMinWeight) || g.EventMinWeight < 0 || g.EventMinWeight > 1 {
		return nil, errors.New("animgraph: EventMinWeight must be in [0, 1]")
	}
	for _, param := range g.Params {
		if param.Name == "" {
			return nil, errors.New("animgraph: parameter with empty name")
		}
		if _, dup := p.paramIndex[param.Name]; dup {
			return nil, fmt.Errorf("animgraph: duplicate parameter %q", param.Name)
		}
		if param.Kind > Trigger || !finite(param.Default) {
			return nil, fmt.Errorf("animgraph: parameter %q has an invalid kind or default", param.Name)
		}
		p.paramIndex[param.Name] = len(p.params)
		p.params = append(p.params, param)
	}
	for _, clip := range g.Clips {
		def, err := p.compileClip(clip)
		if err != nil {
			return nil, err
		}
		p.clipIndex[clip.Name] = len(p.clips)
		p.clips = append(p.clips, def)
	}
	masks := map[string]bool{}
	for _, mask := range g.Masks {
		if mask.Name == "" || masks[mask.Name] {
			return nil, fmt.Errorf("animgraph: mask name %q is empty or duplicate", mask.Name)
		}
		masks[mask.Name] = true
	}
	for i, layer := range g.Layers {
		if layer.Mask != "" && !masks[layer.Mask] {
			return nil, fmt.Errorf("animgraph: layer %d uses unknown mask %q", i, layer.Mask)
		}
		if layer.RefClip != "" {
			if _, ok := p.clipIndex[layer.RefClip]; !ok {
				return nil, fmt.Errorf("animgraph: layer %d reference clip %q is not declared", i, layer.RefClip)
			}
		}
		if !finite(layer.RefTime) || layer.RefTime < 0 {
			return nil, fmt.Errorf("animgraph: layer %d reference time must be finite and nonnegative", i)
		}
		def := layerDef{name: layer.Name, weightParam: -1, additive: layer.Additive, syncLayer: -1}
		if layer.SyncLayer != "" {
			for j := 0; j < i; j++ {
				if g.Layers[j].Name == layer.SyncLayer {
					def.syncLayer = j
				}
			}
			if def.syncLayer < 0 {
				return nil, fmt.Errorf("animgraph: layer %d syncs to %q, which is not an earlier layer", i, layer.SyncLayer)
			}
		}
		if layer.WeightParam != "" {
			index, err := p.param(layer.WeightParam, Float)
			if err != nil {
				return nil, fmt.Errorf("animgraph: layer %d: %w", i, err)
			}
			def.weightParam = index
		}
		machine, err := p.compileMachine(layer.Machine)
		if err != nil {
			return nil, fmt.Errorf("animgraph: layer %d (%s): %w", i, layer.Name, err)
		}
		def.machine = machine
		p.layers = append(p.layers, def)
	}
	names := map[string]bool{}
	for _, chain := range g.IK {
		if chain.Name == "" || names[chain.Name] || chain.Root == "" || chain.Mid == "" || chain.End == "" {
			return nil, fmt.Errorf("animgraph: IK chain %q needs a unique name and Root, Mid, End bones", chain.Name)
		}
		names[chain.Name] = true
		def := ikDef{name: chain.Name, weightCurve: -1}
		if chain.WeightCurve != "" {
			index, ok := p.curveIndex[chain.WeightCurve]
			if !ok {
				return nil, fmt.Errorf("animgraph: IK chain %q weight curve %q is not on any clip", chain.Name, chain.WeightCurve)
			}
			def.weightCurve = index
		}
		p.ik = append(p.ik, def)
	}
	names = map[string]bool{}
	for _, chain := range g.LookAt {
		if chain.Name == "" || names[chain.Name] || len(chain.Bones) == 0 || len(chain.Bones) > 8 {
			return nil, fmt.Errorf("animgraph: look-at chain %q needs a unique name and 1 to 8 bones", chain.Name)
		}
		if len(chain.Weights) != 0 && len(chain.Weights) != len(chain.Bones) {
			return nil, fmt.Errorf("animgraph: look-at chain %q has %d weights for %d bones", chain.Name, len(chain.Weights), len(chain.Bones))
		}
		for _, w := range chain.Weights {
			if !finite(w) || w < 0 {
				return nil, fmt.Errorf("animgraph: look-at chain %q has a negative or non-finite weight", chain.Name)
			}
		}
		if !finite(chain.MaxAngle) || chain.MaxAngle < 0 || !finite3(chain.Axis) {
			return nil, fmt.Errorf("animgraph: look-at chain %q has an invalid axis or angle", chain.Name)
		}
		names[chain.Name] = true
		p.look = append(p.look, lookDef{name: chain.Name})
	}
	if !finite(g.Pelvis.MaxDrop) || g.Pelvis.MaxDrop < 0 {
		return nil, errors.New("animgraph: Pelvis.MaxDrop must be finite and nonnegative")
	}
	for name, scale := range g.Rig.BoneScale {
		if name == "" || !finite(scale) || scale <= 0 || scale > 10 {
			return nil, fmt.Errorf("animgraph: bone scale %q must be in (0, 10]", name)
		}
	}
	switch g.Rig.Humanoid {
	case "", "auto", "mixamo", "rigify", "unreal", "meshy", "biped", "none":
	default:
		return nil, fmt.Errorf("animgraph: unknown humanoid scheme %q", g.Rig.Humanoid)
	}
	p.rigJSON = rigJSON(g)
	p.rigHash = rigHash(p.rigJSON)
	return p, nil
}

func (p *Program) compileClip(clip Clip) (clipDef, error) {
	if clip.Name == "" {
		return clipDef{}, errors.New("animgraph: clip with empty name")
	}
	if _, dup := p.clipIndex[clip.Name]; dup {
		return clipDef{}, fmt.Errorf("animgraph: duplicate clip %q", clip.Name)
	}
	if !finite(clip.Duration) || clip.Duration <= 0 {
		return clipDef{}, fmt.Errorf("animgraph: clip %q needs a positive Duration", clip.Name)
	}
	def := clipDef{name: clip.Name, duration: clip.Duration, loop: clip.Loop}
	for _, event := range clip.Events {
		if event.Name == "" || !finite(event.Time) || event.Time < 0 || event.Time > clip.Duration {
			return clipDef{}, fmt.Errorf("animgraph: clip %q event %q time must be in [0, Duration]", clip.Name, event.Name)
		}
		def.events = append(def.events, eventDef{name: event.Name, normalized: event.Time / clip.Duration})
	}
	sort.SliceStable(def.events, func(i, j int) bool { return def.events[i].normalized < def.events[j].normalized })
	for _, curve := range clip.Curves {
		if curve.Name == "" || len(curve.Keys) == 0 {
			return clipDef{}, fmt.Errorf("animgraph: clip %q has a curve without a name or keys", clip.Name)
		}
		for i, key := range curve.Keys {
			if !finite(key.T) || !finite(key.V) || (i > 0 && key.T < curve.Keys[i-1].T) {
				return clipDef{}, fmt.Errorf("animgraph: clip %q curve %q keys must be finite and sorted", clip.Name, curve.Name)
			}
		}
		index, ok := p.curveIndex[curve.Name]
		if !ok {
			index = len(p.curveNames)
			p.curveIndex[curve.Name] = index
			p.curveNames = append(p.curveNames, curve.Name)
		}
		keys := append([]CurveKey(nil), curve.Keys...)
		def.curves = append(def.curves, curveDef{curve: index, keys: keys})
	}
	return def, nil
}

func (p *Program) param(name string, kinds ...ParamKind) (int, error) {
	index, ok := p.paramIndex[name]
	if !ok {
		return -1, fmt.Errorf("unknown parameter %q", name)
	}
	if len(kinds) == 0 {
		return index, nil
	}
	for _, kind := range kinds {
		if p.params[index].Kind == kind {
			return index, nil
		}
	}
	return -1, fmt.Errorf("parameter %q has the wrong kind", name)
}

func (p *Program) clip(name string) (int, error) {
	index, ok := p.clipIndex[name]
	if !ok {
		return -1, fmt.Errorf("unknown clip %q", name)
	}
	return index, nil
}

func (p *Program) compileMachine(m StateMachine) (machineDef, error) {
	if len(m.States) == 0 {
		return machineDef{}, errors.New("state machine has no states")
	}
	stateIndex := make(map[string]int, len(m.States))
	for i, state := range m.States {
		if state.Name == "" {
			return machineDef{}, errors.New("state with empty name")
		}
		if _, dup := stateIndex[state.Name]; dup {
			return machineDef{}, fmt.Errorf("duplicate state %q", state.Name)
		}
		stateIndex[state.Name] = i
	}
	def := machineDef{}
	if m.Entry != "" {
		entry, ok := stateIndex[m.Entry]
		if !ok {
			return machineDef{}, fmt.Errorf("unknown entry state %q", m.Entry)
		}
		def.entry = entry
	}
	for _, state := range m.States {
		sd, err := p.compileState(state)
		if err != nil {
			return machineDef{}, fmt.Errorf("state %q: %w", state.Name, err)
		}
		for _, t := range state.Transitions {
			td, err := p.compileTransition(t, stateIndex)
			if err != nil {
				return machineDef{}, fmt.Errorf("state %q: %w", state.Name, err)
			}
			sd.trans = append(sd.trans, td)
		}
		def.states = append(def.states, sd)
	}
	for _, t := range m.Any {
		td, err := p.compileTransition(t, stateIndex)
		if err != nil {
			return machineDef{}, fmt.Errorf("any-state: %w", err)
		}
		def.any = append(def.any, td)
	}
	return def, nil
}

func (p *Program) compileState(s State) (stateDef, error) {
	def := stateDef{name: s.Name, speed: s.Speed, speedParam: -1, param: -1, paramY: -1}
	if !finite(s.Speed) || s.Speed < 0 {
		return def, errors.New("Speed must be finite and nonnegative")
	}
	if def.speed == 0 {
		def.speed = 1
	}
	if s.SpeedParam != "" {
		index, err := p.param(s.SpeedParam, Float)
		if err != nil {
			return def, err
		}
		def.speedParam = index
	}
	motions := 0
	if s.Clip != "" {
		motions++
	}
	if s.Blend1D != nil {
		motions++
	}
	if s.Blend2D != nil {
		motions++
	}
	if motions > 1 {
		return def, errors.New("set at most one of Clip, Blend1D, Blend2D")
	}
	switch {
	case s.Clip != "":
		index, err := p.clip(s.Clip)
		if err != nil {
			return def, err
		}
		def.kind = motionClip
		def.clips = []int{index}
	case s.Blend1D != nil:
		b := s.Blend1D
		if len(b.Points) == 0 {
			return def, errors.New("Blend1D has no points")
		}
		index, err := p.param(b.Param, Float)
		if err != nil {
			return def, err
		}
		points := append([]BlendPoint1D(nil), b.Points...)
		sort.SliceStable(points, func(i, j int) bool { return points[i].At < points[j].At })
		for i, point := range points {
			if !finite(point.At) || (i > 0 && point.At == points[i-1].At) {
				return def, errors.New("Blend1D points must be finite and distinct")
			}
			clip, err := p.clip(point.Clip)
			if err != nil {
				return def, err
			}
			def.clips = append(def.clips, clip)
			def.at = append(def.at, point.At)
		}
		def.kind = motion1D
		def.param = index
		def.scaleSpeed = b.ScaleSpeed
	case s.Blend2D != nil:
		b := s.Blend2D
		if len(b.Points) == 0 {
			return def, errors.New("Blend2D has no points")
		}
		x, err := p.param(b.ParamX, Float)
		if err != nil {
			return def, err
		}
		y, err := p.param(b.ParamY, Float)
		if err != nil {
			return def, err
		}
		for i, point := range b.Points {
			if !finite(point.X) || !finite(point.Y) {
				return def, errors.New("Blend2D points must be finite")
			}
			for j := 0; j < i; j++ {
				if b.Points[j].X == point.X && b.Points[j].Y == point.Y {
					return def, errors.New("Blend2D points must be distinct")
				}
			}
			clip, err := p.clip(point.Clip)
			if err != nil {
				return def, err
			}
			def.clips = append(def.clips, clip)
			def.px = append(def.px, point.X)
			def.py = append(def.py, point.Y)
		}
		def.kind = motion2D
		def.param, def.paramY = x, y
	}
	if len(def.clips) > MaxLayerSamples {
		return def, fmt.Errorf("blend space has %d points, limit %d", len(def.clips), MaxLayerSamples)
	}
	if len(def.clips) > p.maxPoints {
		p.maxPoints = len(def.clips)
	}
	switch s.Loop {
	case LoopOn:
		def.loop = true
	case LoopOff:
		def.loop = false
	case LoopDefault:
		def.loop = len(def.clips) > 0 && p.clips[def.clips[0]].loop
	default:
		return def, errors.New("unknown loop mode")
	}
	return def, nil
}

func (p *Program) compileTransition(t Transition, states map[string]int) (transDef, error) {
	to, ok := states[t.To]
	if !ok {
		return transDef{}, fmt.Errorf("transition to unknown state %q", t.To)
	}
	if !finite(t.ExitTime) || t.ExitTime < 0 || !finite(t.Duration) || t.Duration < 0 || !finite(t.Offset) || t.Offset < 0 {
		return transDef{}, fmt.Errorf("transition to %q needs finite nonnegative ExitTime, Duration and Offset", t.To)
	}
	def := transDef{to: to, exitTime: t.ExitTime, duration: t.Duration, offset: t.Offset, canSelf: t.CanSelf, interruptible: t.Interruptible}
	for _, c := range t.When {
		var index int
		var err error
		switch c.Op {
		case Greater, Less, GreaterOrEqual, LessOrEqual, Equal, NotEqual:
			index, err = p.param(c.Param)
		case IsTrue, IsFalse:
			index, err = p.param(c.Param, Bool, Trigger)
		case Triggered:
			index, err = p.param(c.Param, Trigger)
		default:
			err = fmt.Errorf("unknown condition op %d", c.Op)
		}
		if err != nil {
			return transDef{}, fmt.Errorf("transition to %q: %w", t.To, err)
		}
		if !finite(c.Value) {
			return transDef{}, fmt.Errorf("transition to %q compares against a non-finite value", t.To)
		}
		def.conds = append(def.conds, condDef{param: index, op: c.Op, value: c.Value})
	}
	return def, nil
}

// ParamID returns the index of a parameter for the allocation-free setters,
// or -1 when the name is unknown.
func (p *Program) ParamID(name string) int {
	if index, ok := p.paramIndex[name]; ok {
		return index
	}
	return -1
}

// LayerID returns the index of a layer by name, or -1.
func (p *Program) LayerID(name string) int {
	for i, layer := range p.layers {
		if layer.name == name {
			return i
		}
	}
	return -1
}

// ClipID returns the index of a declared clip, or -1.
func (p *Program) ClipID(name string) int {
	if index, ok := p.clipIndex[name]; ok {
		return index
	}
	return -1
}

// IKID returns the index of a two-bone IK chain, or -1.
func (p *Program) IKID(name string) int {
	for i, chain := range p.ik {
		if chain.name == name {
			return i
		}
	}
	return -1
}

// LookAtID returns the index of a look-at chain, or -1.
func (p *Program) LookAtID(name string) int {
	for i, chain := range p.look {
		if chain.name == name {
			return i
		}
	}
	return -1
}

// RigJSON returns the static rig description the renderer needs: masks, layer
// modes, IK and look-at bones, and the naming scheme. Dispatch sends it once
// per page and refers to it by RigHash afterwards.
func (p *Program) RigJSON() []byte { return append([]byte(nil), p.rigJSON...) }

// RigHash identifies RigJSON on the wire. It is never zero.
func (p *Program) RigHash() uint32 { return p.rigHash }

// ClipDuration returns a declared clip's duration in seconds, or 0.
func (p *Program) ClipDuration(name string) float64 {
	if index, ok := p.clipIndex[name]; ok {
		return p.clips[index].duration
	}
	return 0
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func finite3(v [3]float64) bool { return finite(v[0]) && finite(v[1]) && finite(v[2]) }
