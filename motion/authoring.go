package motion

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var motionCSSUnits = map[string]struct{}{
	"": {}, "px": {}, "%": {}, "em": {}, "rem": {}, "vh": {}, "vw": {},
	"vmin": {}, "vmax": {}, "deg": {}, "rad": {}, "turn": {}, "ms": {}, "s": {},
}

var motionStyleProperties = map[string]struct{}{
	"opacity": {}, "color": {}, "background": {}, "backgroundColor": {}, "background-color": {},
	"transform": {}, "transform.x": {}, "transform.y": {}, "transform.z": {}, "transform.scale": {}, "transform.rotate": {}, "transform.rotation": {},
	"width": {}, "height": {}, "minWidth": {}, "maxWidth": {}, "minHeight": {}, "maxHeight": {},
	"top": {}, "right": {}, "bottom": {}, "left": {}, "margin": {}, "marginTop": {}, "marginRight": {}, "marginBottom": {}, "marginLeft": {},
	"padding": {}, "paddingTop": {}, "paddingRight": {}, "paddingBottom": {}, "paddingLeft": {},
	"borderRadius": {}, "filter": {}, "clipPath": {}, "letterSpacing": {}, "wordSpacing": {},
}

var motionSceneProperties = map[string]struct{}{
	"x": {}, "y": {}, "z": {}, "position.x": {}, "position.y": {}, "position.z": {},
	"rotation.x": {}, "rotation.y": {}, "rotation.z": {}, "rotationX": {}, "rotationY": {}, "rotationZ": {},
	"scale.x": {}, "scale.y": {}, "scale.z": {}, "opacity": {},
}

var motionCameraProperties = map[string]struct{}{
	"x": {}, "y": {}, "z": {}, "position.x": {}, "position.y": {}, "position.z": {},
	"rotation.x": {}, "rotation.y": {}, "rotation.z": {}, "rotationX": {}, "rotationY": {}, "rotationZ": {},
	"fov": {}, "near": {}, "far": {}, "zoom": {},
}

// SignalRef names a motion value in a Program.
type SignalRef string

// ReducedMotionPolicy selects how one animation behaves when the user asks
// for less motion. The empty policy is ReducedMotionSkip.
type ReducedMotionPolicy string

const (
	ReducedMotionSkip   ReducedMotionPolicy = "skip"
	ReducedMotionFade   ReducedMotionPolicy = "fade"
	ReducedMotionStatic ReducedMotionPolicy = "static"
)

// SignalKind describes a value or input source in a Program.
type SignalKind string

const (
	SignalTime       SignalKind = "time"
	SignalVisibility SignalKind = "visibility"
	SignalScroll     SignalKind = "scroll"
	SignalPointer    SignalKind = "pointer"
	SignalHover      SignalKind = "hover"
	SignalSpring     SignalKind = "spring"
	SignalTween      SignalKind = "tween"
	SignalKeyframes  SignalKind = "keyframes"
	SignalMap        SignalKind = "map"
	SignalClamp      SignalKind = "clamp"
	SignalMix        SignalKind = "mix"
	SignalVelocity   SignalKind = "velocity"
	SignalCurve      SignalKind = "curve"
)

// MotionAxis names the axis for scroll and pointer sources.
type MotionAxis string

const (
	AxisX MotionAxis = "x"
	AxisY MotionAxis = "y"
)

// MotionSource describes a browser input read by the runtime.
type MotionSource struct {
	Kind     SignalKind `json:"kind"`
	Selector string     `json:"selector,omitempty"`
	Axis     MotionAxis `json:"axis,omitempty"`
}

// MotionEase is the wire form of an easing curve.
type MotionEase struct {
	Kind EaseKind  `json:"kind"`
	Args []float64 `json:"args,omitempty"`
}

// MotionKeyframe is a scalar or vector sample in a keyframe signal.
type MotionKeyframe struct {
	At    float64     `json:"at"`
	Value any         `json:"value"`
	Ease  *MotionEase `json:"ease,omitempty"`
}

// SignalSpec describes one signal in a serializable motion Program.
type SignalSpec struct {
	ID            string              `json:"id"`
	Kind          SignalKind          `json:"kind"`
	From          float64             `json:"from"`
	To            float64             `json:"to"`
	Input         SignalRef           `json:"input,omitempty"`
	A             SignalRef           `json:"a,omitempty"`
	B             SignalRef           `json:"b,omitempty"`
	Weight        SignalRef           `json:"weight,omitempty"`
	Source        *MotionSource       `json:"source,omitempty"`
	Duration      float64             `json:"duration,omitempty"`
	Delay         float64             `json:"delay,omitempty"`
	Mass          float64             `json:"mass,omitempty"`
	Stiffness     float64             `json:"stiffness,omitempty"`
	Damping       float64             `json:"damping,omitempty"`
	Velocity      float64             `json:"velocity,omitempty"`
	Min           float64             `json:"min,omitempty"`
	Max           float64             `json:"max,omitempty"`
	Ease          *MotionEase         `json:"ease,omitempty"`
	Frames        []MotionKeyframe    `json:"frames,omitempty"`
	Smooth        bool                `json:"smooth,omitempty"`
	ReducedMotion ReducedMotionPolicy `json:"reducedMotion,omitempty"`
}

// BindingTarget selects an HTML or Scene3D property.
type BindingTarget string

const (
	BindingStyle           BindingTarget = "style"
	BindingCSSVariable     BindingTarget = "cssVar"
	BindingSceneNode       BindingTarget = "sceneNode"
	BindingMaterialUniform BindingTarget = "materialUniform"
	BindingCamera          BindingTarget = "camera"
)

// Binding connects one signal to an HTML style, CSS variable, or scene value.
type Binding struct {
	Signal   SignalRef     `json:"signal"`
	Target   BindingTarget `json:"target"`
	Selector string        `json:"selector"`
	Node     string        `json:"node,omitempty"`
	Property string        `json:"property"`
	Unit     string        `json:"unit,omitempty"`
}

// PinBinding keeps a Scene3D node aligned with a DOM element's cached rect.
type PinBinding struct {
	Scene   string `json:"scene"`
	Node    string `json:"node"`
	Element string `json:"element"`
}

// Program is a serializable graph of motion signals and their bindings.
// The JavaScript runtime evaluates it alongside the Scene3D renderer.
type Program struct {
	Version  int          `json:"version"`
	ID       string       `json:"id"`
	Signals  []SignalSpec `json:"signals"`
	Bindings []Binding    `json:"bindings,omitempty"`
	Pins     []PinBinding `json:"pins,omitempty"`
	// CSSCompiled lists the indexes of Bindings that CompileCSS turned into
	// CSS. The browser runtime skips them when it supports scroll timelines.
	CSSCompiled []int `json:"cssCompiled,omitempty"`
}

// SpringOptions configures a spring signal. Input, when set, retargets the
// spring as its source changes while retaining the current velocity.
type SpringOptions struct {
	To            float64
	Input         SignalRef
	Physics       Spring
	ReducedMotion ReducedMotionPolicy
}

// NewProgram starts a browser motion program with stable JSON versioning.
func NewProgram(id string) *Program {
	return &Program{Version: 1, ID: strings.TrimSpace(id)}
}

// Time adds a continuously updated seconds signal.
func (p *Program) Time(id string) SignalRef {
	return p.add(SignalSpec{ID: id, Kind: SignalTime})
}

// Visibility adds an intersection-ratio signal for a DOM element.
func (p *Program) Visibility(id, selector string) SignalRef {
	return p.source(id, MotionSource{Kind: SignalVisibility, Selector: selector})
}

// ScrollProgress adds a page or container scroll-progress signal. An empty
// selector uses document scroll; a selector measures that element's scroll.
func (p *Program) ScrollProgress(id, selector string, axis MotionAxis) SignalRef {
	return p.source(id, MotionSource{Kind: SignalScroll, Selector: selector, Axis: normalizeMotionAxis(axis)})
}

// Pointer adds a normalized pointer-position signal for a DOM element.
func (p *Program) Pointer(id, selector string, axis MotionAxis) SignalRef {
	return p.source(id, MotionSource{Kind: SignalPointer, Selector: selector, Axis: normalizeMotionAxis(axis)})
}

// Hover adds a 0/1 pointer and focus signal for a DOM element.
func (p *Program) Hover(id, selector string) SignalRef {
	return p.source(id, MotionSource{Kind: SignalHover, Selector: selector})
}

// Spring adds an interruptible spring value. Setting Input makes changes to
// that signal retarget this spring without resetting its velocity.
func (p *Program) Spring(id string, from float64, options SpringOptions) SignalRef {
	physics := options.Physics.defaults()
	return p.add(SignalSpec{
		ID: id, Kind: SignalSpring, From: from, To: options.To, Input: options.Input,
		Mass: physics.Mass, Stiffness: physics.Stiffness, Damping: physics.Damping,
		Velocity: physics.Velocity, ReducedMotion: normalizeReducedMotionPolicy(options.ReducedMotion),
	})
}

// Tween adds a timed scalar interpolation. Duration is expressed as time.Duration.
func (p *Program) Tween(id string, from, to float64, duration time.Duration, ease Ease, policy ReducedMotionPolicy) SignalRef {
	seconds := duration.Seconds()
	if seconds < 0 {
		seconds = 0
	}
	return p.add(SignalSpec{
		ID: id, Kind: SignalTween, From: from, To: to, Duration: seconds,
		Ease: wireEase(ease), ReducedMotion: normalizeReducedMotionPolicy(policy),
	})
}

// Keyframes adds an ordered scalar or vector timeline signal.
func (p *Program) Keyframes(id string, frames []MotionKeyframe, duration time.Duration, policy ReducedMotionPolicy) SignalRef {
	seconds := duration.Seconds()
	if seconds < 0 {
		seconds = 0
	}
	return p.add(SignalSpec{
		ID: id, Kind: SignalKeyframes, Duration: seconds,
		Frames: append([]MotionKeyframe(nil), frames...), ReducedMotion: normalizeReducedMotionPolicy(policy),
	})
}

// Map remaps an input interval to an output interval without clamping.
func (p *Program) Map(id string, input SignalRef, inMin, inMax, outMin, outMax float64) SignalRef {
	return p.add(SignalSpec{ID: id, Kind: SignalMap, Input: input, From: inMin, To: inMax, Min: outMin, Max: outMax})
}

// Clamp limits an input signal to [min, max].
func (p *Program) Clamp(id string, input SignalRef, min, max float64) SignalRef {
	return p.add(SignalSpec{ID: id, Kind: SignalClamp, Input: input, Min: min, Max: max})
}

// Mix interpolates from a to b using weight, which is clamped to [0, 1].
func (p *Program) Mix(id string, a, b, weight SignalRef) SignalRef {
	return p.add(SignalSpec{ID: id, Kind: SignalMix, A: a, B: b, Weight: weight})
}

// Velocity derives units per second from an input signal.
func (p *Program) Velocity(id string, input SignalRef) SignalRef {
	return p.add(SignalSpec{ID: id, Kind: SignalVelocity, Input: input})
}

// BindStyle binds a signal to an element style property such as opacity or
// transform.y. Numeric values use unit when the property needs one.
func (p *Program) BindStyle(signal SignalRef, selector, property, unit string) {
	p.bind(Binding{Signal: signal, Target: BindingStyle, Selector: selector, Property: property, Unit: unit})
}

// BindCSSVariable writes a signal as a CSS custom property.
func (p *Program) BindCSSVariable(signal SignalRef, selector, name, unit string) {
	p.bind(Binding{Signal: signal, Target: BindingCSSVariable, Selector: selector, Property: name, Unit: unit})
}

// BindSceneNode binds a scalar signal to a node property such as position.y,
// rotation.z, scale.x, or opacity.
func (p *Program) BindSceneNode(signal SignalRef, sceneSelector, node, property string) {
	p.bind(Binding{Signal: signal, Target: BindingSceneNode, Selector: sceneSelector, Node: node, Property: property})
}

// BindMaterialUniform binds a scalar signal to a mesh's named material uniform.
func (p *Program) BindMaterialUniform(signal SignalRef, sceneSelector, mesh, uniform string) {
	p.bind(Binding{Signal: signal, Target: BindingMaterialUniform, Selector: sceneSelector, Node: mesh, Property: uniform})
}

// BindCamera binds a scalar signal to a camera property such as position.z,
// rotation.y, or fov.
func (p *Program) BindCamera(signal SignalRef, sceneSelector, property string) {
	p.bind(Binding{Signal: signal, Target: BindingCamera, Selector: sceneSelector, Property: property})
}

// PinTo aligns a scene node's center with the center of an element's rect.
func (p *Program) PinTo(sceneSelector, node, elementSelector string) {
	p.Pins = append(p.Pins, PinBinding{Scene: strings.TrimSpace(sceneSelector), Node: strings.TrimSpace(node), Element: strings.TrimSpace(elementSelector)})
}

// Marshal validates and serializes this program for a server-rendered data
// attribute. The browser runtime ignores malformed or unsupported programs.
func (p *Program) Marshal() ([]byte, error) {
	if p == nil {
		return nil, errors.New("motion: nil program")
	}
	version := p.Version
	if version == 0 {
		version = 1
	}
	if version != 1 {
		return nil, fmt.Errorf("motion: unsupported program version %d", version)
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, errors.New("motion: program id is required")
	}
	ids := make(map[string]struct{}, len(p.Signals))
	for _, signal := range p.Signals {
		id := strings.TrimSpace(signal.ID)
		if id == "" {
			return nil, errors.New("motion: signal id is required")
		}
		if _, exists := ids[id]; exists {
			return nil, fmt.Errorf("motion: duplicate signal %q", id)
		}
		ids[id] = struct{}{}
	}
	for _, signal := range p.Signals {
		if signal.Kind == SignalCurve {
			if err := validateCurveSignal(signal); err != nil {
				return nil, err
			}
		}
	}
	for _, signal := range p.Signals {
		for _, ref := range []SignalRef{signal.Input, signal.A, signal.B, signal.Weight} {
			if ref == "" {
				continue
			}
			if _, exists := ids[string(ref)]; !exists {
				return nil, fmt.Errorf("motion: signal %q references unknown signal %q", signal.ID, ref)
			}
		}
	}
	for _, binding := range p.Bindings {
		if _, exists := ids[string(binding.Signal)]; !exists {
			return nil, fmt.Errorf("motion: binding references unknown signal %q", binding.Signal)
		}
		if strings.TrimSpace(binding.Selector) == "" || strings.TrimSpace(binding.Property) == "" {
			return nil, errors.New("motion: binding selector and property are required")
		}
		if err := validateBinding(binding); err != nil {
			return nil, err
		}
	}
	for _, index := range p.CSSCompiled {
		if index < 0 || index >= len(p.Bindings) {
			return nil, fmt.Errorf("motion: cssCompiled index %d is outside the %d bindings", index, len(p.Bindings))
		}
	}
	for _, pin := range p.Pins {
		if strings.TrimSpace(pin.Scene) == "" || strings.TrimSpace(pin.Node) == "" || strings.TrimSpace(pin.Element) == "" {
			return nil, errors.New("motion: PinTo requires a scene, node, and element selector")
		}
	}
	copy := *p
	copy.Version = version
	return json.Marshal(&copy)
}

func validateBinding(binding Binding) error {
	property := strings.TrimSpace(binding.Property)
	unit := strings.TrimSpace(binding.Unit)
	if property != binding.Property || unit != binding.Unit {
		return errors.New("motion: binding property and unit must not contain surrounding whitespace")
	}
	if _, ok := motionCSSUnits[unit]; !ok {
		return fmt.Errorf("motion: unsupported binding unit %q", binding.Unit)
	}
	switch binding.Target {
	case BindingStyle:
		if _, ok := motionStyleProperties[property]; !ok {
			return fmt.Errorf("motion: unsupported style binding property %q", binding.Property)
		}
	case BindingCSSVariable:
		if !validCSSVariable(property) {
			return fmt.Errorf("motion: invalid CSS variable binding property %q", binding.Property)
		}
	case BindingSceneNode:
		if _, ok := motionSceneProperties[property]; !ok || strings.TrimSpace(binding.Node) == "" || unit != "" {
			return fmt.Errorf("motion: unsupported scene-node binding property %q", binding.Property)
		}
	case BindingMaterialUniform:
		if !validUniformName(property) || strings.TrimSpace(binding.Node) == "" || unit != "" {
			return fmt.Errorf("motion: invalid material-uniform binding property %q", binding.Property)
		}
	case BindingCamera:
		if _, ok := motionCameraProperties[property]; !ok || unit != "" {
			return fmt.Errorf("motion: unsupported camera binding property %q", binding.Property)
		}
	default:
		return fmt.Errorf("motion: unsupported binding target %q", binding.Target)
	}
	return nil
}

func validCSSVariable(property string) bool {
	if !strings.HasPrefix(property, "--") || len(property) < 3 || len(property) > 128 {
		return false
	}
	for i, r := range property[2:] {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && ((r >= '0' && r <= '9') || r == '-')) {
			continue
		}
		return false
	}
	return true
}

func validUniformName(property string) bool {
	if property == "" || len(property) > 64 {
		return false
	}
	for i, r := range property {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func (p *Program) source(id string, source MotionSource) SignalRef {
	return p.add(SignalSpec{ID: id, Kind: source.Kind, Source: &source})
}

func (p *Program) add(signal SignalSpec) SignalRef {
	if p == nil {
		return ""
	}
	signal.ID = strings.TrimSpace(signal.ID)
	p.Signals = append(p.Signals, signal)
	return SignalRef(signal.ID)
}

func (p *Program) bind(binding Binding) {
	if p != nil {
		p.Bindings = append(p.Bindings, binding)
	}
}

func normalizeMotionAxis(axis MotionAxis) MotionAxis {
	if axis == AxisX {
		return AxisX
	}
	return AxisY
}

func normalizeReducedMotionPolicy(policy ReducedMotionPolicy) ReducedMotionPolicy {
	switch policy {
	case ReducedMotionFade, ReducedMotionStatic:
		return policy
	default:
		return ReducedMotionSkip
	}
}

func wireEase(ease Ease) *MotionEase {
	args := append([]float64(nil), ease.Args...)
	return &MotionEase{Kind: ease.Kind, Args: args}
}
