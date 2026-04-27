package game

import (
	"encoding/json"
	"math"
	"time"

	"github.com/odvcencio/gosx/hub"
	"github.com/odvcencio/gosx/physics"
	"github.com/odvcencio/gosx/sim"
)

// Phase is a runtime system phase.
type Phase string

const (
	PhaseUpdate      Phase = "update"
	PhaseFixedUpdate Phase = "fixed-update"
	PhaseLateUpdate  Phase = "late-update"
	PhaseRender      Phase = "render"
)

// Frame describes one advanced render frame and its fixed simulation work.
type Frame struct {
	Index           uint64        `json:"index"`
	Delta           time.Duration `json:"delta"`
	FixedStep       time.Duration `json:"fixedStep"`
	FixedSteps      int           `json:"fixedSteps"`
	Alpha           float64       `json:"alpha"`
	Time            time.Duration `json:"time"`
	DroppedDuration time.Duration `json:"droppedDuration,omitempty"`
}

// LoopConfig configures the deterministic frame clock.
type LoopConfig struct {
	FixedStep   time.Duration
	MaxDelta    time.Duration
	MaxSubsteps int
}

// Clock converts variable render deltas into bounded fixed simulation steps.
type Clock struct {
	fixedStep   time.Duration
	maxDelta    time.Duration
	maxSubsteps int
	accumulator time.Duration
	frame       uint64
	elapsed     time.Duration
}

// NewClock creates a deterministic fixed-step clock.
func NewClock(cfg LoopConfig) *Clock {
	fixed := cfg.FixedStep
	if fixed <= 0 {
		fixed = time.Second / 60
	}
	maxDelta := cfg.MaxDelta
	if maxDelta <= 0 {
		maxDelta = 250 * time.Millisecond
	}
	maxSubsteps := cfg.MaxSubsteps
	if maxSubsteps <= 0 {
		maxSubsteps = 5
	}
	return &Clock{
		fixedStep:   fixed,
		maxDelta:    maxDelta,
		maxSubsteps: maxSubsteps,
	}
}

// Advance advances the clock and returns the frame plan.
func (c *Clock) Advance(delta time.Duration) Frame {
	if c == nil {
		return Frame{}
	}
	if delta < 0 {
		delta = 0
	}
	if delta > c.maxDelta {
		delta = c.maxDelta
	}
	c.accumulator += delta
	steps := 0
	for c.accumulator >= c.fixedStep && steps < c.maxSubsteps {
		c.accumulator -= c.fixedStep
		c.elapsed += c.fixedStep
		steps++
	}
	var dropped time.Duration
	if steps == c.maxSubsteps && c.accumulator >= c.fixedStep {
		dropped = c.accumulator
		c.accumulator = 0
	}
	c.frame++
	alpha := 0.0
	if c.fixedStep > 0 {
		alpha = float64(c.accumulator) / float64(c.fixedStep)
		alpha = math.Max(0, math.Min(1, alpha))
	}
	return Frame{
		Index:           c.frame,
		Delta:           delta,
		FixedStep:       c.fixedStep,
		FixedSteps:      steps,
		Alpha:           alpha,
		Time:            c.elapsed,
		DroppedDuration: dropped,
	}
}

func (c *Clock) restore(frame uint64, elapsed time.Duration) {
	if c == nil {
		return
	}
	c.frame = frame
	c.elapsed = elapsed
	c.accumulator = 0
}

// System runs runtime work in a specific phase.
type System interface {
	Name() string
	Phase() Phase
	Update(*Context) error
}

// SystemFunc adapts a function into a System.
type SystemFunc struct {
	SystemName  string
	SystemPhase Phase
	Fn          func(*Context) error
}

// Func creates a system from fn.
func Func(name string, phase Phase, fn func(*Context) error) SystemFunc {
	if phase == "" {
		phase = PhaseUpdate
	}
	return SystemFunc{SystemName: name, SystemPhase: phase, Fn: fn}
}

func (s SystemFunc) Name() string {
	return s.SystemName
}

func (s SystemFunc) Phase() Phase {
	if s.SystemPhase == "" {
		return PhaseUpdate
	}
	return s.SystemPhase
}

func (s SystemFunc) Update(ctx *Context) error {
	if s.Fn == nil {
		return nil
	}
	return s.Fn(ctx)
}

// Event is a runtime event emitted by systems.
type Event struct {
	Type   string `json:"type"`
	Target string `json:"target,omitempty"`
	Data   any    `json:"data,omitempty"`
}

// Context is passed to systems for each phase.
type Context struct {
	Runtime       *Runtime
	World         *World
	Input         *Input
	Assets        *Assets
	Physics       *physics.World
	Frame         Frame
	Phase         Phase
	Delta         time.Duration
	FixedStep     int
	NetworkInputs map[string]sim.Input
}

// Emit records a runtime event for this frame.
func (ctx *Context) Emit(event Event) {
	if ctx == nil || ctx.Runtime == nil || event.Type == "" {
		return
	}
	ctx.Runtime.events = append(ctx.Runtime.events, event)
}

// Config describes a game/simulation runtime.
type Config struct {
	Name          string
	Profile       Profile
	World         *World
	Input         *Input
	Assets        *Assets
	Physics       *physics.World
	FixedStep     time.Duration
	MaxDelta      time.Duration
	MaxSubsteps   int
	Systems       []System
	Scene         SceneBuilder
	Snapshot      SnapshotCodec
	ManualPhysics bool
}

// Runtime coordinates input, systems, physics, assets, and scene output.
type Runtime struct {
	name          string
	profile       Profile
	world         *World
	input         *Input
	assets        *Assets
	physics       *physics.World
	clock         *Clock
	systems       []System
	sceneBuilder  SceneBuilder
	snapshot      SnapshotCodec
	manualPhysics bool

	frame         Frame
	events        []Event
	networkInputs map[string]sim.Input
	latestScene   latestScene
}

// New creates a runtime from cfg.
func New(cfg Config) *Runtime {
	profile := normalizeProfile(cfg.Profile)
	fixedStep := cfg.FixedStep
	if fixedStep <= 0 {
		fixedStep = profile.FixedStep
	}
	maxDelta := cfg.MaxDelta
	if maxDelta <= 0 {
		maxDelta = profile.MaxDelta
	}
	maxSubsteps := cfg.MaxSubsteps
	if maxSubsteps <= 0 {
		maxSubsteps = profile.MaxSubsteps
	}
	world := cfg.World
	if world == nil {
		world = NewWorld()
	}
	input := cfg.Input
	if input == nil {
		input = NewInput(profile.Bindings...)
	}
	assets := cfg.Assets
	if assets == nil {
		assets = NewAssets()
	}
	return &Runtime{
		name:          cfg.Name,
		profile:       profile,
		world:         world,
		input:         input,
		assets:        assets,
		physics:       cfg.Physics,
		clock:         NewClock(LoopConfig{FixedStep: fixedStep, MaxDelta: maxDelta, MaxSubsteps: maxSubsteps}),
		systems:       append([]System(nil), cfg.Systems...),
		sceneBuilder:  cfg.Scene,
		snapshot:      cfg.Snapshot,
		manualPhysics: cfg.ManualPhysics,
	}
}

// Step advances the runtime by delta.
func (r *Runtime) Step(delta time.Duration) (Frame, error) {
	if r == nil {
		return Frame{}, nil
	}
	frame := r.clock.Advance(delta)
	r.frame = frame
	r.events = nil
	ctx := &Context{
		Runtime:       r,
		World:         r.world,
		Input:         r.input,
		Assets:        r.assets,
		Physics:       r.physics,
		Frame:         frame,
		NetworkInputs: cloneSimInputs(r.networkInputs),
	}
	var firstErr error
	capture := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	capture(r.runPhase(ctx, PhaseUpdate, frame.Delta, 0))
	for step := 0; step < frame.FixedSteps; step++ {
		capture(r.runPhase(ctx, PhaseFixedUpdate, frame.FixedStep, step))
		if r.physics != nil && !r.manualPhysics {
			r.physics.StepFixed()
		}
	}
	capture(r.runPhase(ctx, PhaseLateUpdate, frame.Delta, 0))
	capture(r.runPhase(ctx, PhaseRender, frame.Delta, 0))
	r.rebuildScene(ctx)
	r.input.EndFrame()
	r.networkInputs = nil
	return frame, firstErr
}

func (r *Runtime) runPhase(ctx *Context, phase Phase, delta time.Duration, fixedStep int) error {
	ctx.Phase = phase
	ctx.Delta = delta
	ctx.FixedStep = fixedStep
	var firstErr error
	for _, system := range r.systems {
		if system == nil || system.Phase() != phase {
			continue
		}
		if err := system.Update(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Tick lets Runtime satisfy sim.Simulation. It applies collected network input
// and advances exactly one fixed simulation step.
func (r *Runtime) Tick(inputs map[string]sim.Input) {
	if r == nil {
		return
	}
	r.ApplyNetworkInputs(inputs)
	_, _ = r.Step(r.FixedStep())
}

// ApplyNetworkInputs stores inputs for systems and maps JSON InputEvent payloads
// into the frame input state when possible.
func (r *Runtime) ApplyNetworkInputs(inputs map[string]sim.Input) {
	if r == nil || len(inputs) == 0 {
		return
	}
	r.networkInputs = cloneSimInputs(inputs)
	for playerID, input := range inputs {
		if len(input.Data) == 0 {
			continue
		}
		r.applyNetworkInput(playerID, input.Data)
	}
}

func (r *Runtime) applyNetworkInput(playerID string, data []byte) {
	var events []InputEvent
	if err := json.Unmarshal(data, &events); err == nil {
		for _, event := range events {
			if event.PlayerID == "" {
				event.PlayerID = playerID
			}
			r.input.Apply(event)
		}
		return
	}
	var event InputEvent
	if err := json.Unmarshal(data, &event); err == nil {
		if event.PlayerID == "" {
			event.PlayerID = playerID
		}
		r.input.Apply(event)
	}
}

// FixedStep returns the runtime's deterministic simulation step.
func (r *Runtime) FixedStep() time.Duration {
	if r == nil || r.clock == nil {
		return time.Second / 60
	}
	return r.clock.fixedStep
}

// Frame returns the most recent frame.
func (r *Runtime) Frame() Frame {
	if r == nil {
		return Frame{}
	}
	return r.frame
}

// World returns the runtime entity/component world.
func (r *Runtime) World() *World {
	if r == nil {
		return nil
	}
	return r.world
}

// Input returns the runtime input mapper.
func (r *Runtime) Input() *Input {
	if r == nil {
		return nil
	}
	return r.input
}

// Assets returns the runtime asset registry.
func (r *Runtime) Assets() *Assets {
	if r == nil {
		return nil
	}
	return r.assets
}

// Physics returns the runtime physics world, if one is attached.
func (r *Runtime) Physics() *physics.World {
	if r == nil {
		return nil
	}
	return r.physics
}

// Events returns events emitted during the most recent Step.
func (r *Runtime) Events() []Event {
	if r == nil || len(r.events) == 0 {
		return nil
	}
	out := make([]Event, len(r.events))
	copy(out, r.events)
	return out
}

// NewRunner wires runtime into the existing server-authoritative sim runner.
func NewRunner(h *hub.Hub, runtime *Runtime, opts sim.Options) *sim.Runner {
	if runtime == nil {
		runtime = New(Config{})
	}
	if opts.TickRate <= 0 {
		opts.TickRate = int(math.Round(float64(time.Second) / float64(runtime.FixedStep())))
		if opts.TickRate <= 0 {
			opts.TickRate = 60
		}
	}
	return sim.New(h, runtime, opts)
}

// SnapshotCodec customizes runtime snapshot/state serialization.
type SnapshotCodec interface {
	Snapshot(*Runtime) []byte
	Restore(*Runtime, []byte)
	State(*Runtime) []byte
}

// SnapshotCodecFuncs adapts functions into a SnapshotCodec.
type SnapshotCodecFuncs struct {
	SnapshotFunc func(*Runtime) []byte
	RestoreFunc  func(*Runtime, []byte)
	StateFunc    func(*Runtime) []byte
}

func (f SnapshotCodecFuncs) Snapshot(r *Runtime) []byte {
	if f.SnapshotFunc == nil {
		return nil
	}
	return f.SnapshotFunc(r)
}

func (f SnapshotCodecFuncs) Restore(r *Runtime, data []byte) {
	if f.RestoreFunc != nil {
		f.RestoreFunc(r, data)
	}
}

func (f SnapshotCodecFuncs) State(r *Runtime) []byte {
	if f.StateFunc != nil {
		return f.StateFunc(r)
	}
	if f.SnapshotFunc != nil {
		return f.SnapshotFunc(r)
	}
	return nil
}

// RuntimeState is the default snapshot/state payload.
type RuntimeState struct {
	Frame       uint64              `json:"frame"`
	TimeSeconds float64             `json:"timeSeconds"`
	Physics     *physics.WorldState `json:"physics,omitempty"`
}

// Snapshot returns a restorable runtime checkpoint for sim.Runner.
func (r *Runtime) Snapshot() []byte {
	if r == nil {
		return nil
	}
	if r.snapshot != nil {
		return r.snapshot.Snapshot(r)
	}
	return r.defaultState()
}

// Restore applies a previously captured checkpoint.
func (r *Runtime) Restore(snapshot []byte) {
	if r == nil || len(snapshot) == 0 {
		return
	}
	if r.snapshot != nil {
		r.snapshot.Restore(r, snapshot)
		return
	}
	var state RuntimeState
	if err := json.Unmarshal(snapshot, &state); err != nil {
		return
	}
	if r.clock != nil {
		elapsed := time.Duration(state.TimeSeconds * float64(time.Second))
		r.clock.restore(state.Frame, elapsed)
		r.frame.Index = state.Frame
		r.frame.Time = elapsed
		r.frame.FixedStep = r.FixedStep()
	}
	if r.physics != nil && state.Physics != nil {
		r.physics.ApplyState(*state.Physics)
	}
}

// State returns the current authoritative state for sim.Runner broadcasts.
func (r *Runtime) State() []byte {
	if r == nil {
		return nil
	}
	if r.snapshot != nil {
		return r.snapshot.State(r)
	}
	return r.defaultState()
}

func (r *Runtime) defaultState() []byte {
	state := RuntimeState{
		Frame:       r.frame.Index,
		TimeSeconds: r.frame.Time.Seconds(),
	}
	if r.physics != nil {
		physicsState := r.physics.StateSnapshot()
		if len(physicsState.Bodies) > 0 {
			state.Physics = &physicsState
		}
	}
	data, err := json.Marshal(state)
	if err != nil {
		return nil
	}
	return data
}

func cloneSimInputs(inputs map[string]sim.Input) map[string]sim.Input {
	if len(inputs) == 0 {
		return nil
	}
	out := make(map[string]sim.Input, len(inputs))
	for key, input := range inputs {
		if len(input.Data) > 0 {
			input.Data = append([]byte(nil), input.Data...)
		}
		out[key] = input
	}
	return out
}
