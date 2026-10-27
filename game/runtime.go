package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/physics"
	"m31labs.dev/gosx/sim"
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
//
// Delta is game time: the clamped real delta times the time scale, and zero
// while paused. RealDelta is the clamped delta before scaling. At scale 1 and
// not paused the two are equal. Steps is the total number of fixed steps run
// since the clock started.
//
// DroppedDuration is game time the frame did not run as fixed steps: what was
// left after MaxSubsteps steps, and, once game time has reached the end of a
// time.Duration, every step's worth that gathers (see Clock.Advance). A frame
// whose scaled time is longer than a Duration reports the excess here too.
type Frame struct {
	Index           uint64        `json:"index"`
	Delta           time.Duration `json:"delta"`
	RealDelta       time.Duration `json:"realDelta,omitempty"`
	TimeScale       float64       `json:"timeScale,omitempty"`
	Paused          bool          `json:"paused,omitempty"`
	Steps           uint64        `json:"steps,omitempty"`
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
	steps       uint64
	paused      bool
	scale       float64
	scaleFrac   float64 // sub-nanosecond remainder of scaled deltas
}

// SetPaused freezes or resumes game time. While paused, Advance still returns
// frames (the index advances) but runs no fixed steps and adds no time.
func (c *Clock) SetPaused(paused bool) {
	if c != nil {
		c.paused = paused
	}
}

// Paused reports whether the clock is paused.
func (c *Clock) Paused() bool { return c != nil && c.paused }

// SetTimeScale sets how much game time passes per unit of real time: 2 is
// double speed, 0.5 half speed, 0 stops time. Negative, infinite and NaN
// values are ignored.
func (c *Clock) SetTimeScale(scale float64) {
	if c == nil || math.IsNaN(scale) || math.IsInf(scale, 0) || scale < 0 {
		return
	}
	c.scale = scale
}

// TimeScale returns the current time scale (1 by default).
func (c *Clock) TimeScale() float64 {
	if c == nil {
		return 1
	}
	return c.scale
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
		scale:       1,
	}
}

// maxDuration is the largest game time a time.Duration holds: about 292 years.
const maxDuration = time.Duration(math.MaxInt64)

// twoPow63 is the smallest float64 that is not a Duration. float64(maxDuration)
// rounds up to it.
const twoPow63 = float64(1 << 63)

// addDuration returns a+b for a, b >= 0. When the sum would pass maxDuration it
// returns maxDuration and the part of b that did not fit.
func addDuration(a, b time.Duration) (sum, over time.Duration) {
	if room := maxDuration - a; b > room {
		return maxDuration, b - room
	}
	return a + b, 0
}

// floorDuration converts nanoseconds to a Duration, rounding toward zero. A value
// beyond maxDuration gives maxDuration, and a negative value or NaN gives 0.
func floorDuration(ns float64) time.Duration {
	switch {
	case ns >= twoPow63:
		return maxDuration
	case ns > 0:
		return time.Duration(ns)
	}
	return 0
}

// Advance advances the clock and returns the frame plan.
//
// Game time is a time.Duration, so it ends at about 292 years (math.MaxInt64
// nanoseconds), which is about 4.9 hours of play at a time scale of 525,600. The
// clock does not wrap past that. A frame whose scaled time is longer than a
// Duration counts as maxDuration, and the clock runs no fixed step that would
// take Frame.Time past maxDuration: from then on Time stays where it is and each
// frame reports the time it could not run in Frame.DroppedDuration.
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
	realDelta := delta
	var scaled, dropped time.Duration
	if !c.paused {
		exact := float64(delta)*c.scale + c.scaleFrac
		switch {
		case exact < twoPow63:
			whole := math.Floor(exact)
			c.scaleFrac = exact - whole
			scaled = time.Duration(whole)
		case exact >= twoPow63: // more than a Duration, or infinite
			scaled, dropped = maxDuration, floorDuration(exact-twoPow63)
			c.scaleFrac = 0
		default: // NaN
			c.scaleFrac = 0
		}
	}
	var over time.Duration
	c.accumulator, over = addDuration(c.accumulator, scaled)
	dropped, _ = addDuration(dropped, over)
	steps := 0
	for c.accumulator >= c.fixedStep && steps < c.maxSubsteps && c.elapsed <= maxDuration-c.fixedStep {
		c.accumulator -= c.fixedStep
		c.elapsed += c.fixedStep
		steps++
	}
	c.steps += uint64(steps)
	if c.accumulator >= c.fixedStep {
		// The frame used its MaxSubsteps, or game time has reached the end of a
		// Duration: drop what is left, which the next frame could not run either.
		dropped, _ = addDuration(dropped, c.accumulator)
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
		Delta:           scaled,
		RealDelta:       realDelta,
		TimeScale:       c.scale,
		Paused:          c.paused,
		Steps:           c.steps,
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
	c.steps = uint64(elapsed / c.fixedStep)
	c.accumulator = 0
	c.scaleFrac = 0
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
	Runtime   *Runtime
	World     *World
	Input     *Input
	Assets    *Assets
	Physics   *physics.World
	Frame     Frame
	Phase     Phase
	Delta     time.Duration
	FixedStep int
	// Tick numbers the work for cadence: in PhaseFixedUpdate it is the
	// 1-based count of fixed steps since the clock started; in other phases it
	// is the frame index. See Every and EveryInterval.
	Tick          uint64
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
	name             string
	profile          Profile
	world            *World
	input            *Input
	assets           *Assets
	physics          *physics.World
	clock            *Clock
	systems          []System
	sceneBuilder     SceneBuilder
	snapshot         SnapshotCodec
	manualPhysics    bool
	physicsFromScene bool

	frame         Frame
	events        []Event
	networkInputs map[string]sim.Input
	latestScene   latestScene
}

// Runtime implements sim.CheckedRestorer, so Runner.RestoreSnapshot sees why it
// refuses a snapshot.
var _ sim.CheckedRestorer = (*Runtime)(nil)

// New creates a runtime from cfg.
func New(cfg Config) *Runtime {
	profile := normalizeProfile(cfg.Profile)
	fixedStep := cfg.FixedStep
	if fixedStep <= 0 {
		fixedStep = profile.FixedStep
	}
	maxDelta := cfg.MaxDelta
	maxSubsteps := cfg.MaxSubsteps
	if maxSubsteps <= 0 {
		maxSubsteps = profile.MaxSubsteps
	}
	if maxDelta <= 0 {
		maxDelta = profile.MaxDelta
		// A coarse fixed step (say one game hour) must not be clamped away
		// by a default meant for 60 Hz.
		if maxDelta < fixedStep {
			maxDelta = fixedStep * time.Duration(maxSubsteps)
		}
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
		name:             cfg.Name,
		profile:          profile,
		world:            world,
		input:            input,
		assets:           assets,
		physics:          cfg.Physics,
		clock:            NewClock(LoopConfig{FixedStep: fixedStep, MaxDelta: maxDelta, MaxSubsteps: maxSubsteps}),
		systems:          append([]System(nil), cfg.Systems...),
		sceneBuilder:     cfg.Scene,
		snapshot:         cfg.Snapshot,
		manualPhysics:    cfg.ManualPhysics,
		physicsFromScene: cfg.Physics == nil && !cfg.ManualPhysics,
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
	if phase == PhaseFixedUpdate {
		ctx.Tick = ctx.Frame.Steps - uint64(ctx.Frame.FixedSteps) + uint64(fixedStep) + 1
	} else {
		ctx.Tick = ctx.Frame.Index
	}
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
	// Ascending player order keeps event order and last-writer fields
	// (pointer position) identical across runs and replays.
	sim.EachInput(inputs, func(playerID string, input sim.Input) {
		if len(input.Data) == 0 {
			return
		}
		r.applyNetworkInput(playerID, input.Data)
	})
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

// SetPaused pauses or resumes game time. See Clock.SetPaused.
func (r *Runtime) SetPaused(paused bool) {
	if r != nil {
		r.clock.SetPaused(paused)
	}
}

// Paused reports whether game time is paused.
func (r *Runtime) Paused() bool { return r != nil && r.clock.Paused() }

// SetTimeScale sets the game-time multiplier. See Clock.SetTimeScale. For a
// large factor, raise FixedStep so each frame needs only a few steps (one game
// hour per step reaches a game year per real minute at scale 525600); also
// raise MaxSubsteps if a frame can need more than the default 5.
func (r *Runtime) SetTimeScale(scale float64) {
	if r != nil {
		r.clock.SetTimeScale(scale)
	}
}

// TimeScale returns the game-time multiplier.
func (r *Runtime) TimeScale() float64 {
	if r == nil {
		return 1
	}
	return r.clock.TimeScale()
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

// SnapshotCodec customizes runtime snapshot/state serialization. A codec whose
// restore can fail should also implement SnapshotRestorer.
type SnapshotCodec interface {
	Snapshot(*Runtime) []byte
	Restore(*Runtime, []byte)
	State(*Runtime) []byte
}

// SnapshotRestorer is the optional part of a SnapshotCodec that reports a
// snapshot it cannot apply. Runtime.RestoreSnapshot calls it in place of
// Restore and returns its error.
type SnapshotRestorer interface {
	RestoreSnapshot(*Runtime, []byte) error
}

// SnapshotCodecFuncs adapts functions into a SnapshotCodec. RestoreSnapshotFunc
// is the restore that can fail. When both it and RestoreFunc are set, every
// restore runs RestoreSnapshotFunc and RestoreFunc is ignored; Restore drops the
// error.
type SnapshotCodecFuncs struct {
	SnapshotFunc        func(*Runtime) []byte
	RestoreFunc         func(*Runtime, []byte)
	StateFunc           func(*Runtime) []byte
	RestoreSnapshotFunc func(*Runtime, []byte) error
}

func (f SnapshotCodecFuncs) Snapshot(r *Runtime) []byte {
	if f.SnapshotFunc == nil {
		return nil
	}
	return f.SnapshotFunc(r)
}

func (f SnapshotCodecFuncs) Restore(r *Runtime, data []byte) {
	_ = f.RestoreSnapshot(r, data)
}

// RestoreSnapshot implements SnapshotRestorer. It reports the error of
// RestoreSnapshotFunc, and runs RestoreFunc, which cannot fail, when that is
// nil.
func (f SnapshotCodecFuncs) RestoreSnapshot(r *Runtime, data []byte) error {
	switch {
	case f.RestoreSnapshotFunc != nil:
		return f.RestoreSnapshotFunc(r, data)
	case f.RestoreFunc != nil:
		f.RestoreFunc(r, data)
	}
	return nil
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

// RuntimeStateVersion is the snapshot format Runtime.Snapshot writes. Version 1
// is the format before the field existed: the clock, time control and physics.
// Version 2 adds the fixed step that wrote the snapshot (FixedStepNanos), the
// clock remainder (AccumulatorNanos and ScaleFrac) and the gathered time of
// Every and EveryInterval wrappers (Cadence).
const RuntimeStateVersion = 2

// RuntimeState is the default snapshot/state payload.
type RuntimeState struct {
	// Version is the snapshot format. Snapshots written before it existed omit it
	// and count as version 1. Restore refuses a version above RuntimeStateVersion.
	// State carries no version 2 fields and so no version.
	Version int    `json:"version,omitempty"`
	Frame   uint64 `json:"frame"`
	// TimeSeconds is a human-readable copy of the time. It loses nanoseconds
	// after about 104 game days; Restore prefers TimeNanos when present.
	TimeSeconds float64 `json:"timeSeconds"`
	// TimeNanos is the exact elapsed time. Snapshots written before this field
	// existed omit it, and Restore falls back to TimeSeconds.
	TimeNanos *int64 `json:"timeNanos,omitempty"`
	// TimeScale and Paused save the time control settings. Older snapshots
	// omit them and Restore then leaves the current settings alone.
	TimeScale *float64            `json:"timeScale,omitempty"`
	Paused    *bool               `json:"paused,omitempty"`
	Physics   *physics.WorldState `json:"physics,omitempty"`
	// FixedStepNanos is the fixed step of the runtime that wrote the snapshot.
	// AccumulatorNanos and ScaleFrac are the clock remainder: game time gathered
	// toward the next fixed step, always less than one FixedStepNanos, and the
	// sub-nanosecond part of scaled deltas, in [0, 1). Cadence is the time each
	// Every and EveryInterval wrapper has gathered. Only a version 2 snapshot
	// writes them. Restore sets all of them exactly in a runtime with the same
	// fixed step. A runtime with another step starts the clock remainder from
	// zero, because it was gathered in steps of another size, and still sets
	// Cadence. From an older snapshot or from State, Restore starts the clock
	// remainder from zero and leaves the wrappers alone.
	FixedStepNanos   int64          `json:"fixedStepNanos,omitempty"`
	AccumulatorNanos int64          `json:"accumulatorNanos,omitempty"`
	ScaleFrac        float64        `json:"scaleFrac,omitempty"`
	Cadence          []CadenceState `json:"cadence,omitempty"`
	// World holds registered world content. Only a SaveCodec snapshot writes
	// it; the default snapshot leaves it out.
	World *WorldSave `json:"world,omitempty"`
}

// Snapshot returns a restorable runtime checkpoint for sim.Runner. Unlike State
// it holds the clock remainder and the Every and EveryInterval accumulators,
// which a restored runtime needs to run as if it had never stopped.
func (r *Runtime) Snapshot() []byte {
	if r == nil {
		return nil
	}
	if r.snapshot != nil {
		return r.snapshot.Snapshot(r)
	}
	return r.defaultState(true)
}

// Restore applies a previously captured checkpoint. It is the sim.Simulation
// method, which has no result, so it drops a snapshot that is empty, does not
// decode or does not fit this runtime, and says nothing. Call RestoreSnapshot to
// learn why. To load a save into a sim.Runner, call Runner.RestoreSnapshot, which
// returns the same error and keeps the runner's frame and replay log when the
// runtime refuses.
func (r *Runtime) Restore(snapshot []byte) {
	_ = r.RestoreSnapshot(snapshot)
}

// RestoreSnapshot applies a checkpoint like Restore and returns the reason when
// it cannot: the snapshot is empty, is not valid JSON, is newer than
// RuntimeStateVersion, or holds a time, clock remainder or gathered time that is
// out of range. The default codec and SaveCodec leave the runtime unchanged after
// an error. A custom SnapshotCodec reports errors only if it implements
// SnapshotRestorer. A version 2 snapshot written with another fixed step still
// loads; its clock remainder starts from zero.
//
// Do not call it while a sim.Runner drives the runtime: it races with the tick.
// Use Runner.RestoreSnapshot, which takes the runner's lock.
func (r *Runtime) RestoreSnapshot(snapshot []byte) error {
	if r == nil {
		return errors.New("game: nil Runtime")
	}
	if len(snapshot) == 0 {
		return errors.New("game: empty snapshot")
	}
	if r.snapshot != nil {
		if codec, ok := r.snapshot.(SnapshotRestorer); ok {
			return codec.RestoreSnapshot(r, snapshot)
		}
		r.snapshot.Restore(r, snapshot)
		return nil
	}
	state, err := r.decodeRuntimeState(snapshot)
	if err != nil {
		return err
	}
	r.applyRuntimeState(state)
	return nil
}

// decodeRuntimeState parses and checks a snapshot without touching the runtime,
// so a restore applies all of it or none. It accepts every state that the clock
// and the Every and EveryInterval wrappers of a runtime can reach, so a runtime
// can always load the snapshot it wrote.
func (r *Runtime) decodeRuntimeState(snapshot []byte) (RuntimeState, error) {
	var state RuntimeState
	if err := json.Unmarshal(snapshot, &state); err != nil {
		return state, fmt.Errorf("game: decode snapshot: %w", err)
	}
	if state.Version < 0 || state.Version > RuntimeStateVersion {
		return state, fmt.Errorf("game: snapshot version %d is not supported (newest is %d)", state.Version, RuntimeStateVersion)
	}
	if _, err := state.elapsed(); err != nil {
		return state, err
	}
	if state.Version < 2 {
		return state, nil
	}
	// The remainder is checked against the step that wrote it, not against this
	// runtime's step, which may have changed since.
	step := time.Duration(state.FixedStepNanos)
	if step <= 0 {
		return state, fmt.Errorf("game: snapshot fixed step %v is not positive", step)
	}
	if acc := time.Duration(state.AccumulatorNanos); acc < 0 || acc >= step {
		return state, fmt.Errorf("game: snapshot clock accumulator %v is not within one fixed step (%v)", acc, step)
	}
	if state.ScaleFrac < 0 || state.ScaleFrac >= 1 {
		return state, fmt.Errorf("game: snapshot scale fraction %v is not in [0, 1)", state.ScaleFrac)
	}
	for _, c := range state.Cadence {
		if c.GatheredNanos < 0 {
			return state, fmt.Errorf("game: snapshot cadence %q has negative gathered time %v", c.System, time.Duration(c.GatheredNanos))
		}
	}
	return state, nil
}

// elapsed returns the game time of the snapshot: TimeNanos, or TimeSeconds in a
// snapshot that predates it. It refuses a time below zero or beyond what a
// time.Duration holds.
func (s RuntimeState) elapsed() (time.Duration, error) {
	if s.TimeNanos != nil {
		if *s.TimeNanos < 0 {
			return 0, fmt.Errorf("game: snapshot time %v is negative", time.Duration(*s.TimeNanos))
		}
		return time.Duration(*s.TimeNanos), nil
	}
	if nanos := s.TimeSeconds * float64(time.Second); nanos >= 0 && nanos < 1<<63 {
		return time.Duration(nanos), nil
	}
	return 0, fmt.Errorf("game: snapshot timeSeconds %v is outside the range of a duration", s.TimeSeconds)
}

// applyRuntimeState restores the clock, time control settings, cadence and
// physics. It cannot fail: decodeRuntimeState has checked state.
func (r *Runtime) applyRuntimeState(state RuntimeState) {
	if r.clock != nil {
		elapsed, _ := state.elapsed() // decodeRuntimeState has checked it
		r.clock.restore(state.Frame, elapsed)
		// The remainder fits the step that wrote it. With another step it starts
		// from zero, as it does for a version 1 snapshot.
		if state.Version >= 2 && time.Duration(state.FixedStepNanos) == r.clock.fixedStep {
			r.clock.accumulator = time.Duration(state.AccumulatorNanos)
			r.clock.scaleFrac = state.ScaleFrac
		}
		r.frame.Index = state.Frame
		r.frame.Time = elapsed
		r.frame.FixedStep = r.FixedStep()
		r.frame.Steps = r.clock.steps
		if state.TimeScale != nil {
			r.clock.SetTimeScale(*state.TimeScale)
		}
		if state.Paused != nil {
			r.clock.SetPaused(*state.Paused)
		}
	}
	if state.Version >= 2 {
		r.restoreCadences(state.Cadence)
	}
	if r.physics != nil && state.Physics != nil {
		r.physics.ApplyState(*state.Physics)
	}
}

// State returns the current authoritative state for sim.Runner broadcasts. It
// leaves out what only a restore needs, so a broadcast stays small; see
// Snapshot.
func (r *Runtime) State() []byte {
	if r == nil {
		return nil
	}
	if r.snapshot != nil {
		return r.snapshot.State(r)
	}
	return r.defaultState(false)
}

func (r *Runtime) defaultState(restorable bool) []byte {
	data, err := json.Marshal(r.runtimeState(restorable))
	if err != nil {
		return nil
	}
	return data
}

// runtimeState captures the clock, time control settings and physics. A
// restorable state adds the version, the fixed step, the clock remainder and the
// cadence accumulators.
func (r *Runtime) runtimeState(restorable bool) RuntimeState {
	nanos := int64(r.frame.Time)
	state := RuntimeState{
		Frame:       r.frame.Index,
		TimeSeconds: r.frame.Time.Seconds(),
		TimeNanos:   &nanos,
	}
	if r.clock != nil {
		scale, paused := r.clock.TimeScale(), r.clock.Paused()
		state.TimeScale, state.Paused = &scale, &paused
	}
	if restorable {
		state.Version = RuntimeStateVersion
		state.FixedStepNanos = int64(r.FixedStep())
		if r.clock != nil {
			state.AccumulatorNanos = int64(r.clock.accumulator)
			state.ScaleFrac = r.clock.scaleFrac
		}
		state.Cadence = r.cadenceStates()
	}
	if r.physics != nil {
		physicsState := r.physics.StateSnapshot()
		if len(physicsState.Bodies) > 0 {
			state.Physics = &physicsState
		}
	}
	return state
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
