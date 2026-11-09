package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
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
// DroppedDuration is game time the frame did not run as fixed steps and will
// never run: under CatchUpDrop, what was left after MaxSubsteps steps; under
// both policies, once game time has reached the end of a time.Duration, every
// step's worth that gathers (see Clock.Advance). A frame whose scaled time is
// longer than a Duration reports the excess here too.
//
// Lag is game time that was due as fixed steps but did not run this frame
// because the CatchUpLag wall-time budget ran out. The clock keeps it and runs
// it in later frames, so it is a whole number of FixedSteps. It is always zero
// under CatchUpDrop.
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
	Lag             time.Duration `json:"lag,omitempty"`
}

// CatchUp selects what the clock does with due fixed steps that one frame
// cannot run.
type CatchUp uint8

const (
	// CatchUpDrop runs at most MaxSubsteps fixed steps per frame and drops the
	// rest of the gathered game time, reported as Frame.DroppedDuration. A stall
	// then does not fast-forward the world, which suits a single-player game.
	// It is the default.
	CatchUpDrop CatchUp = iota
	// CatchUpLag never drops a due fixed step. Each frame runs due steps until
	// the wall-time budget (LoopConfig.CatchUpBudget) is spent, and always at
	// least one when one is due. The steps it did not run stay in the clock,
	// reported as Frame.Lag, and run first in the next frame. MaxSubsteps does
	// not apply. Netplay, replay and server-authoritative clients use it,
	// because a dropped step makes their tick count disagree with the other
	// side's.
	//
	// MaxDelta still clamps each frame's real delta before it becomes game
	// time; time cut by that clamp was never due. A client that must track wall
	// time through long stalls raises MaxDelta.
	CatchUpLag
)

// String returns "drop" or "lag".
func (p CatchUp) String() string {
	switch p {
	case CatchUpDrop:
		return "drop"
	case CatchUpLag:
		return "lag"
	}
	return "CatchUp(" + strconv.Itoa(int(p)) + ")"
}

// DefaultCatchUpBudget is the CatchUpLag wall-time budget when
// LoopConfig.CatchUpBudget is zero: half of a 60 Hz frame, which leaves the
// other half for rendering.
const DefaultCatchUpBudget = 8 * time.Millisecond

// LoopConfig configures the deterministic frame clock.
type LoopConfig struct {
	FixedStep   time.Duration
	MaxDelta    time.Duration
	MaxSubsteps int
	// CatchUp selects the catch-up policy. The zero value is CatchUpDrop.
	CatchUp CatchUp
	// CatchUpBudget bounds the wall time one frame spends running fixed steps
	// under CatchUpLag. Zero selects DefaultCatchUpBudget and a negative value
	// removes the bound. CatchUpDrop ignores it.
	CatchUpBudget time.Duration
	// WallClock reads a monotonic wall clock for CatchUpBudget. Nil uses the
	// process's monotonic clock. Tests inject a fake one so budget decisions are
	// deterministic. The clock reads it only under CatchUpLag with a budget.
	WallClock func() time.Duration
}

// processStart anchors the default monotonic wall clock.
var processStart = time.Now()

func monotonicNow() time.Duration { return time.Since(processStart) }

// Clock converts variable render deltas into bounded fixed simulation steps.
type Clock struct {
	fixedStep   time.Duration
	maxDelta    time.Duration
	maxSubsteps int
	catchUp     CatchUp
	budget      time.Duration // < 0 means no bound
	wallClock   func() time.Duration
	accumulator time.Duration
	lag         time.Duration // due steps carried under CatchUpLag, a multiple of fixedStep
	frame       uint64
	elapsed     time.Duration
	steps       uint64
	paused      bool
	scale       float64
	scaleFrac   float64 // sub-nanosecond remainder of scaled deltas
}

// SetPaused freezes or resumes game time. While paused, Advance still returns
// frames (the index advances) but runs no fixed steps and adds no time. Steps
// that CatchUpLag carries wait until the clock resumes.
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

// CatchUp returns the clock's catch-up policy.
func (c *Clock) CatchUp() CatchUp {
	if c == nil {
		return CatchUpDrop
	}
	return c.catchUp
}

// Lag returns the game time that CatchUpLag carries: due fixed steps that
// earlier frames did not run. It is zero under CatchUpDrop.
func (c *Clock) Lag() time.Duration {
	if c == nil {
		return 0
	}
	return c.lag
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
	budget := cfg.CatchUpBudget
	if budget == 0 {
		budget = DefaultCatchUpBudget
	}
	wallClock := cfg.WallClock
	if wallClock == nil {
		wallClock = monotonicNow
	}
	catchUp := cfg.CatchUp
	if catchUp != CatchUpLag {
		catchUp = CatchUpDrop
	}
	return &Clock{
		fixedStep:   fixed,
		maxDelta:    maxDelta,
		maxSubsteps: maxSubsteps,
		catchUp:     catchUp,
		budget:      budget,
		wallClock:   wallClock,
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

// Advance advances the clock and returns the frame plan. The caller runs
// Frame.FixedSteps fixed steps.
//
// Under CatchUpLag, Advance has no step work to time, so the budget does not
// apply: the frame plans every due step and Frame.Lag is zero. Use AdvanceFunc
// to run steps within the budget.
//
// Game time is a time.Duration, so it ends at about 292 years (math.MaxInt64
// nanoseconds), which is about 4.9 hours of play at a time scale of 525,600. The
// clock does not wrap past that. A frame whose scaled time is longer than a
// Duration counts as maxDuration, and the clock runs no fixed step that would
// take Frame.Time past maxDuration: from then on Time stays where it is and each
// frame reports the time it could not run in Frame.DroppedDuration, under
// either policy.
func (c *Clock) Advance(delta time.Duration) Frame {
	if c == nil {
		return Frame{}
	}
	return c.plan(delta)
}

// AdvanceFunc advances the clock by delta and runs the frame's fixed steps
// itself: it calls step once per fixed step, in order, with tick, the 1-based
// number of that step since the clock started. Under CatchUpLag with a budget,
// it reads the wall clock before every step after the first and stops once the
// steps have used the budget; the due steps it did not run stay in the clock
// and the returned Frame reports them as Lag. Under CatchUpDrop it is Advance
// followed by Frame.FixedSteps calls to step.
//
// The returned Frame counts only the steps that ran. A nil step runs nothing
// but still advances the clock as Advance does.
func (c *Clock) AdvanceFunc(delta time.Duration, step func(tick uint64)) Frame {
	if c == nil {
		return Frame{}
	}
	frame := c.plan(delta)
	if step == nil {
		return frame
	}
	first := frame.Steps - uint64(frame.FixedSteps) + 1
	budgeted := c.budgeted()
	var start time.Duration
	if budgeted && frame.FixedSteps > 1 {
		start = c.wallClock()
	}
	ran := 0
	for ; ran < frame.FixedSteps; ran++ {
		if ran > 0 && budgeted && c.wallClock()-start >= c.budget {
			break
		}
		step(first + uint64(ran))
	}
	return c.settle(frame, ran)
}

// budgeted reports whether the frame's fixed steps run within a wall-time
// budget.
func (c *Clock) budgeted() bool {
	return c.catchUp == CatchUpLag && c.budget > 0
}

// plan adds delta to the clock and moves it past every fixed step the frame may
// run: up to MaxSubsteps under CatchUpDrop, every due step under CatchUpLag.
// settle gives back the steps a budget stopped.
func (c *Clock) plan(delta time.Duration) Frame {
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
	limit := c.maxSubsteps
	if c.catchUp == CatchUpLag {
		limit = math.MaxInt
	}
	if !c.paused && c.lag > 0 {
		// Carried steps run first; a paused clock keeps them. Only CatchUpLag
		// carries steps, but a restored snapshot can hand them to either policy.
		c.accumulator, over = addDuration(c.accumulator, c.lag)
		dropped, _ = addDuration(dropped, over)
		c.lag = 0
	}
	c.accumulator, over = addDuration(c.accumulator, scaled)
	dropped, _ = addDuration(dropped, over)
	// The number of steps is the gathered time in whole steps, bounded by the
	// frame limit and by the steps left before game time reaches maxDuration.
	due := int64(c.accumulator / c.fixedStep)
	if room := int64((maxDuration - c.elapsed) / c.fixedStep); due > room {
		due = room
	}
	if due > int64(limit) {
		due = int64(limit)
	}
	steps := int(due)
	c.accumulator -= time.Duration(due) * c.fixedStep
	c.elapsed += time.Duration(due) * c.fixedStep
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
		Lag:             c.lag,
	}
}

// settle completes a planned frame in which only ran of its FixedSteps ran. The
// clock takes back the rest as lag, so a later frame runs them.
func (c *Clock) settle(frame Frame, ran int) Frame {
	if ran < 0 {
		ran = 0
	}
	if back := frame.FixedSteps - ran; back > 0 {
		owed := time.Duration(back) * c.fixedStep
		c.elapsed -= owed
		c.steps -= uint64(back)
		c.lag, _ = addDuration(c.lag, owed)
		frame.FixedSteps = ran
		frame.Steps = c.steps
		frame.Time = c.elapsed
	}
	frame.Lag = c.lag
	return frame
}

func (c *Clock) restore(frame uint64, elapsed time.Duration) {
	if c == nil {
		return
	}
	c.frame = frame
	c.elapsed = elapsed
	c.steps = uint64(elapsed / c.fixedStep)
	c.accumulator = 0
	c.lag = 0
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
//
// Frame is the frame being advanced. In PhaseUpdate and PhaseFixedUpdate it is
// the clock's plan for the frame. Under CatchUpDrop the plan is final. Under
// CatchUpLag the wall-time budget can stop the fixed steps early: the plan
// then counts steps that did not run, and PhaseLateUpdate and PhaseRender see
// the settled frame, with FixedSteps, Steps and Time counting the steps that
// ran and Lag the rest. Tick is exact in every phase.
//
// The runtime reuses one Context, its NetworkInputs map and the input bytes in
// it for every Step, so a steady-state Step allocates nothing. Do not keep the
// Context, the map or an Input's Data after the system returns; copy what you
// need.
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

// InvalidateScene marks the scene declaration as changed, so the runtime runs
// the SceneBuilder at the end of this Step. See Runtime.InvalidateScene.
func (ctx *Context) InvalidateScene() {
	if ctx != nil {
		ctx.Runtime.InvalidateScene()
	}
}

// Config describes a game/simulation runtime.
type Config struct {
	Name        string
	Profile     Profile
	World       *World
	Input       *Input
	Assets      *Assets
	Physics     *physics.World
	FixedStep   time.Duration
	MaxDelta    time.Duration
	MaxSubsteps int
	// CatchUp, CatchUpBudget and WallClock set the clock's catch-up policy; see
	// LoopConfig. The zero values keep CatchUpDrop.
	CatchUp       CatchUp
	CatchUpBudget time.Duration
	WallClock     func() time.Duration
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
	ctx           Context // reused by every Step
	events        []Event
	networkInputs map[string]sim.Input // this Step's inputs, or nil
	netInputs     map[string]sim.Input // reused storage behind networkInputs
	netBytes      []byte               // reused storage behind the Data of netInputs
	latestScene   latestScene
	sceneDirty    bool
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
		name:    cfg.Name,
		profile: profile,
		world:   world,
		input:   input,
		assets:  assets,
		physics: cfg.Physics,
		clock: NewClock(LoopConfig{
			FixedStep:     fixedStep,
			MaxDelta:      maxDelta,
			MaxSubsteps:   maxSubsteps,
			CatchUp:       cfg.CatchUp,
			CatchUpBudget: cfg.CatchUpBudget,
			WallClock:     cfg.WallClock,
		}),
		systems:          append([]System(nil), cfg.Systems...),
		sceneBuilder:     cfg.Scene,
		snapshot:         cfg.Snapshot,
		manualPhysics:    cfg.ManualPhysics,
		physicsFromScene: cfg.Physics == nil && !cfg.ManualPhysics,
		sceneDirty:       true,
	}
}

// Step advances the runtime by delta. It runs PhaseUpdate, then the frame's
// fixed steps (PhaseFixedUpdate and physics), then PhaseLateUpdate and
// PhaseRender. Under CatchUpLag the fixed steps run within the clock's
// wall-time budget, and the returned Frame reports the steps left over as Lag.
func (r *Runtime) Step(delta time.Duration) (Frame, error) {
	if r == nil {
		return Frame{}, nil
	}
	frame := r.clock.plan(delta)
	r.frame = frame
	clear(r.events) // drop references held by last frame's event data
	r.events = r.events[:0]
	ctx := &r.ctx
	*ctx = Context{
		Runtime:       r,
		World:         r.world,
		Input:         r.input,
		Assets:        r.assets,
		Physics:       r.physics,
		Frame:         frame,
		NetworkInputs: r.networkInputs,
	}
	var firstErr error
	capture := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	capture(r.runPhase(ctx, PhaseUpdate, frame.Delta, 0))
	budgeted := r.clock.budgeted()
	var start time.Duration
	if budgeted && frame.FixedSteps > 1 {
		start = r.clock.wallClock()
	}
	ran := 0
	for ; ran < frame.FixedSteps; ran++ {
		if ran > 0 && budgeted && r.clock.wallClock()-start >= r.clock.budget {
			break
		}
		capture(r.runPhase(ctx, PhaseFixedUpdate, frame.FixedStep, ran))
		if r.physics != nil && !r.manualPhysics {
			r.physics.StepFixed()
		}
	}
	if ran < frame.FixedSteps {
		frame = r.clock.settle(frame, ran)
		r.frame = frame
		ctx.Frame = frame
	}
	capture(r.runPhase(ctx, PhaseLateUpdate, frame.Delta, 0))
	capture(r.runPhase(ctx, PhaseRender, frame.Delta, 0))
	if r.sceneDirty {
		r.rebuildScene(ctx)
	}
	r.input.EndFrame()
	r.networkInputs = nil
	ctx.NetworkInputs = nil
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
	r.networkInputs = r.copyNetworkInputs(inputs)
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
	// Cadence. From an older snapshot or from State, which carry none of these,
	// Restore starts the clock remainder and every wrapper from zero, so the
	// restored runtime depends on the snapshot alone and not on what it did
	// before.
	FixedStepNanos   int64          `json:"fixedStepNanos,omitempty"`
	AccumulatorNanos int64          `json:"accumulatorNanos,omitempty"`
	ScaleFrac        float64        `json:"scaleFrac,omitempty"`
	Cadence          []CadenceState `json:"cadence,omitempty"`
	// LagNanos is the game time that CatchUpLag carried when the snapshot was
	// written: due fixed steps not yet run, a whole number of FixedStepNanos.
	// Only a version 2 snapshot of a lagging runtime writes it. Restore sets it
	// with the clock remainder, under the same fixed-step rule; a runtime
	// under CatchUpDrop runs the carried steps within its MaxSubsteps.
	LagNanos int64 `json:"lagNanos,omitempty"`
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
	state, err := decodeRuntimeState(snapshot)
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
func decodeRuntimeState(snapshot []byte) (RuntimeState, error) {
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
	if lag := time.Duration(state.LagNanos); lag < 0 || lag%step != 0 {
		return state, fmt.Errorf("game: snapshot lag %v is not a whole number of fixed steps (%v)", lag, step)
	}
	for _, c := range state.Cadence {
		if c.GatheredNanos < 0 {
			return state, fmt.Errorf("game: snapshot cadence %q has negative gathered time %v", c.System, time.Duration(c.GatheredNanos))
		}
		// An interval wrapper runs as soon as it has gathered its interval (every
		// tick if that is not positive), so between runs it holds less. A wrapper
		// that gathered more would hand its system a Delta that no run produces.
		if c.Kind == cadenceKindInterval && c.GatheredNanos >= max(c.Param, 1) {
			return state, fmt.Errorf("game: snapshot cadence %q has gathered time %v, but an interval of %v runs before that much gathers",
				c.System, time.Duration(c.GatheredNanos), time.Duration(c.Param))
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
			r.clock.lag = time.Duration(state.LagNanos)
		}
		r.frame.Lag = r.clock.lag
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
	// A version 1 snapshot or a State payload has no wrapper times. Every wrapper
	// then starts from zero, as the clock remainder does.
	var gathered []CadenceState
	if state.Version >= 2 {
		gathered = state.Cadence
	}
	r.restoreCadences(gathered)
	if r.physics != nil && state.Physics != nil {
		r.physics.ApplyState(*state.Physics)
	}
	// The restored state can change what the SceneBuilder returns.
	r.sceneDirty = true
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
			state.LagNanos = int64(r.clock.lag)
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

// copyNetworkInputs copies inputs into storage the runtime reuses from Step to
// Step, so the caller may reuse its map and buffers once
// ApplyNetworkInputs returns.
func (r *Runtime) copyNetworkInputs(inputs map[string]sim.Input) map[string]sim.Input {
	if r.netInputs == nil {
		r.netInputs = make(map[string]sim.Input, len(inputs))
	}
	clear(r.netInputs)
	size := 0
	for _, input := range inputs {
		size += len(input.Data)
	}
	if cap(r.netBytes) < size {
		r.netBytes = make([]byte, 0, size)
	}
	r.netBytes = r.netBytes[:0]
	for key, input := range inputs {
		if len(input.Data) > 0 {
			start := len(r.netBytes)
			r.netBytes = append(r.netBytes, input.Data...)
			input.Data = r.netBytes[start:len(r.netBytes):len(r.netBytes)]
		}
		r.netInputs[key] = input
	}
	return r.netInputs
}
