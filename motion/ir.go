package motion

// ValueArity identifies the semantic type and component width of a Value.
type ValueArity uint8

const (
	ArityScalar ValueArity = iota // 1 float
	ArityVec2                     // 2 floats
	ArityVec3                     // 3 floats
	ArityVec4                     // 4 floats
	ArityQuat                     // 4 floats (unit quaternion)
	ArityColor                    // 4 floats (r,g,b,a)
)

// Value is a typed flat-float value (plain-old-data; zero heap alloc).
// Only F[:Arity.Width()] is meaningful; unused trailing components are 0.
// Width: scalar=1, vec2=2, vec3=3, vec4/quat/color=4.
type Value struct {
	Arity ValueArity
	F     [4]float64
}

// Interp selects the keyframe interpolation mode for a Track.
type Interp uint8

const (
	InterpLinear      Interp = iota // component-wise linear lerp
	InterpStep                      // hold previous key value
	InterpCubicSpline               // cubic spline (tangent-based)
	InterpSlerp                     // spherical lerp (Quat arity)
)

// TargetKind identifies the kind of animatable target.
type TargetKind uint8

const (
	TargetSceneNode TargetKind = iota // a node in the GoSX scene graph
	TargetDOM                         // a DOM element (browser only)
	TargetCSSVar                      // a CSS custom property (browser only)
)

// Target identifies an animatable object by kind and reference string.
type Target struct {
	Kind TargetKind
	Ref  string
}

// Key is a single keyframe: a time, value snapshot, and optional per-key ease.
//
// InTangent / OutTangent carry the glTF CUBICSPLINE tangents (in-tangent a,
// out-tangent b — see the spec note on the evaluator). They are required ONLY
// for tracks with Interp == InterpCubicSpline; for every other interpolation
// mode they stay nil, so non-cubic keys carry zero pointer overhead. A
// cubicspline track whose keys lack tangents falls back to linear.
type Key struct {
	T          float64
	Value      Value
	Ease       *Ease  // nil → use track-level ease
	InTangent  *Value // cubicspline in-tangent (a); nil for non-cubic keys
	OutTangent *Value // cubicspline out-tangent (b); nil for non-cubic keys
}

// GeneratorKind selects the procedural generator algorithm.
type GeneratorKind uint8

const (
	GenNone   GeneratorKind = iota // no generator
	GenSpin                        // constant rotation about axes
	GenSpring                      // spring-driven toward Base
	GenDrift                       // sinusoidal drift
)

// Generator is a procedural animation source that continuously perturbs a base value.
type Generator struct {
	Kind       GeneratorKind
	Base       Value      // base value the generator perturbs
	Spin       [3]float64 // rad/sec per axis (GenSpin)
	Spring     Spring     // GenSpring
	Drift      [3]float64 // amplitude per axis (GenDrift)
	DriftSpeed [3]float64
	DriftPhase [3]float64
}

// Track is one animation channel: either a keyframe sequence or a procedural generator.
type Track struct {
	Target   Target
	Prop     string
	Keys     []Key      // keyframe track; OR
	Gen      *Generator // generator track (if non-nil, Keys ignored)
	Interp   Interp
	Ease     Ease // track-level default ease
	TargetID int  // resolved numeric target id (set by string interning, Task 1.12a)
	PropID   int  // resolved numeric prop id (set by string interning, Task 1.12a)
}

// PositionKind selects how a Positioned child's start time is resolved.
type PositionKind uint8

const (
	PosAbs     PositionKind = iota // absolute time offset
	PosRel                         // relative to previous sibling end
	PosLabel                       // named label anchor
	PosPrevRel                     // relative to previous sibling start
)

// Position is a resolved or symbolic start-time anchor.
type Position struct {
	Kind  PositionKind
	Val   float64
	Label string
}

// Positioned wraps a Track or sub-Timeline with its start Position.
// Exactly one of Track/Sub is set.
type Positioned struct {
	At    Position
	Track *Track
	Sub   *Timeline
}

// LoopMode controls repetition: 0 = no loop, -1 = infinite, n>0 = repeat n times.
type LoopMode int

// Timeline is the root composable animation container.
type Timeline struct {
	ID        string
	Children  []Positioned
	Loop      LoopMode
	Alternate bool
	Speed     float64 // 0 treated as 1
	Autoplay  bool
}
