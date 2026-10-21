// Package sim provides server-authoritative game simulation over gosx hubs.
// Games implement the Simulation interface and a Runner drives the simulation
// at a fixed tick rate, collecting player inputs and broadcasting state.
package sim

import (
	"encoding/json"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"m31labs.dev/gosx/hub"
)

// Input holds raw per-client input data for a single tick.
type Input struct {
	Data []byte
}

// Simulation is the interface a game must implement to be driven by a Runner.
type Simulation interface {
	// Tick advances the simulation one step with the collected inputs. The
	// inputs map has random iteration order: use SortedPlayerIDs or EachInput
	// when order can affect state. The Runner never calls Tick, Snapshot,
	// Restore or State concurrently with one another.
	Tick(inputs map[string]Input)
	// Snapshot returns an opaque checkpoint for rollback or replay.
	Snapshot() []byte
	// Restore resets the simulation to a previous snapshot.
	Restore(snapshot []byte)
	// State returns the current authoritative state for broadcast.
	State() []byte
}

// StateEncoding controls how Runner places Simulation state bytes into hub
// payloads.
type StateEncoding string

const (
	// StateEncodingBytes preserves the historical behavior: []byte state fields
	// are JSON-encoded by encoding/json, which produces base64 strings.
	StateEncodingBytes StateEncoding = "bytes"
	// StateEncodingJSON embeds valid JSON state bytes directly as JSON values.
	// Invalid JSON falls back to StateEncodingBytes behavior.
	StateEncodingJSON StateEncoding = "json"
)

// RingDisabled turns off the rollback snapshot ring when set as Options.RingSize.
const RingDisabled = -1

// Options configures a Runner.
type Options struct {
	// TickRate is the number of simulation ticks per second. Zero defaults to 60.
	TickRate int
	// StateEncoding selects the state/snapshot representation in hub payloads.
	// Leave empty for StateEncodingBytes compatibility.
	StateEncoding StateEncoding
	// DisableReplay skips the unbounded input log for long-running simulations.
	// Replay returns an empty log when recording is disabled.
	DisableReplay bool
	// Seed is stored in the ReplayLog so a replay can rebuild the simulation's
	// RNG. The runner does not use it otherwise.
	Seed uint64
	// ReplayMaxFrames bounds the replay log. Once the log holds this many
	// frames, recording stops and ReplayLog.Truncated is set; the kept frames
	// still replay exactly from ReplayLog.Initial. Zero means unbounded.
	ReplayMaxFrames int
	// Broadcaster replaces the per-tick "sim:tick" JSON broadcast. Nil keeps
	// the historical behavior (and StateEncoding applies). See
	// NewBinarySnapshotBroadcaster for a keyframe plus delta adapter.
	Broadcaster Broadcaster
	// RingSize is the number of rollback snapshots kept. Zero keeps the
	// default of 128; RingDisabled (-1) keeps none and skips the per-tick
	// Simulation.Snapshot call.
	RingSize int
	// SnapshotEvery stores a ring snapshot every N frames. Zero or one means
	// every frame.
	SnapshotEvery int
	// CatchUpBudget turns on catch-up. When the loop wakes late and ticker
	// periods were dropped, the runner runs the missed ticks, but spends at
	// most this much wall time per wake (the first tick always runs). Ticks
	// still owed after the budget stay owed and show up as TickEvent.Behind.
	// The backlog is capped at one second of ticks; older debt is dropped.
	// Zero keeps the historical behavior: dropped periods only count as lag.
	// The same budget bounds the extra ticks that SetSpeed above 1 produces.
	CatchUpBudget time.Duration
	// BroadcastEvery broadcasts state at most once per this much wall time
	// instead of every tick. State is not built on ticks that skip the
	// broadcast. Zero broadcasts every tick.
	BroadcastEvery time.Duration
	// InputQueue keeps every input a player sends instead of overwriting
	// the previous one. The runner hands each player at most one queued input
	// per tick, oldest first, so a burst between slow ticks is not lost.
	InputQueue bool
	// InputQueueDepth caps queued inputs per player when InputQueue is set; the
	// oldest input is dropped on overflow. Zero means 64.
	InputQueueDepth int
	// Observer receives aggregate timing after the existing whole tick finishes.
	// A nil observer skips timing reads and event construction.
	Observer Observer
	// Clock supplies ticker deadlines and elapsed time; nil uses a real clock.
	Clock Clock
}

// Runner drives a Simulation at a fixed tick rate over a hub.
type Runner struct {
	hub        *hub.Hub
	sim        Simulation
	tickRate   int
	mu         sync.Mutex
	simMu      sync.Mutex // serializes every call into sim
	runMu      sync.Mutex
	stop, done chan struct{}
	inputs     map[string]Input
	queued     map[string][]queuedInput
	queueOn    bool
	queueDepth int
	period     time.Duration
	catchUp    time.Duration
	bcastEvery time.Duration
	paused     atomic.Bool
	speedBits  atomic.Uint64
	// Tick-goroutine state, touched only by wake and tickOnce.
	owed        float64
	lastWake    time.Duration
	haveWake    bool
	haveBcast   bool
	behind      int
	lastBcastAt time.Duration
	running     atomic.Bool
	frame       atomic.Uint64
	snapshots   *snapshotRing // nil when disabled
	snapEvery   uint64
	bcast       Broadcaster
	recorder    *replayRecorder
	encoding    StateEncoding
	observer    Observer
	clock       Clock
}

// New creates a Runner that will drive sim over h at the configured tick rate.
func New(h *hub.Hub, s Simulation, opts Options) *Runner {
	rate := opts.TickRate
	if rate <= 0 {
		rate = 60
	}
	var recorder *replayRecorder
	if !opts.DisableReplay {
		recorder = newReplayRecorder()
		recorder.seed, recorder.max = opts.Seed, opts.ReplayMaxFrames
	}
	var ring *snapshotRing
	switch {
	case opts.RingSize == 0:
		ring = newSnapshotRing(128)
	case opts.RingSize > 0:
		ring = newSnapshotRing(opts.RingSize)
	}
	c := opts.Clock
	if c == nil {
		c = newClock()
	}
	depth := opts.InputQueueDepth
	if depth <= 0 {
		depth = 64
	}
	r := &Runner{
		queued:     make(map[string][]queuedInput),
		queueOn:    opts.InputQueue,
		queueDepth: depth,
		catchUp:    opts.CatchUpBudget,
		bcastEvery: opts.BroadcastEvery,
		period:     time.Second / time.Duration(rate),
		hub:        h,
		sim:        s,
		tickRate:   rate,
		inputs:     make(map[string]Input),
		snapshots:  ring,
		snapEvery:  uint64(max(opts.SnapshotEvery, 1)),
		bcast:      opts.Broadcaster,
		recorder:   recorder,
		encoding:   normalizeStateEncoding(opts.StateEncoding),
		observer:   opts.Observer,
		clock:      c,
	}
	r.speedBits.Store(math.Float64bits(1))
	return r
}

// TickRate returns the configured ticks per second.
func (r *Runner) TickRate() int {
	return r.tickRate
}

// RegisterHandlers wires hub event handlers for input collection and spectator joins.
func (r *Runner) RegisterHandlers() {
	r.hub.On("input", func(ctx *hub.Context) {
		r.ReceiveInput(ctx.Client.ID, Input{Data: ctx.Data})
	})

	r.hub.On("join", func(ctx *hub.Context) {
		frame, snap := r.snapshotNow()
		ctx.Hub.Send(ctx.Client.ID, "sim:snapshot", r.snapshotPayload(frame, snap))
	})
}

// Restore resets the simulation to snapshot and sets the runner frame counter
// to frame, so the next tick is frame+1. Use it to load a save. The replay log
// restarts from this snapshot. Call it from the owner while the runner is
// stopped or between ticks; it is safe against a running tick but the tick
// loop keeps going.
func (r *Runner) Restore(frame uint64, snapshot []byte) {
	r.simMu.Lock()
	defer r.simMu.Unlock()
	r.sim.Restore(snapshot)
	r.frame.Store(frame)
	if r.recorder != nil {
		r.recorder.begin(frame, snapshot)
	}
}

// SnapshotAt returns the ring snapshot stored for frame, if it is still held.
// It reports false when the ring is disabled or the frame was skipped by
// Options.SnapshotEvery.
func (r *Runner) SnapshotAt(frame uint64) ([]byte, bool) {
	if r.snapshots == nil {
		return nil, false
	}
	r.simMu.Lock()
	defer r.simMu.Unlock()
	return r.snapshots.Get(frame)
}

// snapshotNow returns the current frame and a snapshot that belong together.
func (r *Runner) snapshotNow() (uint64, []byte) {
	r.simMu.Lock()
	defer r.simMu.Unlock()
	return r.frame.Load(), r.sim.Snapshot()
}

// ReceiveInput stores an input for the given player. By default it overwrites
// any prior input from that player in the current tick window. With
// Options.InputQueue it queues the input for the next free tick instead.
func (r *Runner) ReceiveInput(playerID string, input Input) {
	r.ReceiveInputAt(playerID, 0, input)
}

// ReceiveInputAt is ReceiveInput with a target frame stamp. With
// Options.InputQueue the input is held until frame becomes the next frame to
// run; a stamp that is already past (late) is delivered on the next tick, and a
// zero stamp means "next tick". Inputs of one player are delivered in arrival
// order, so a future stamp also holds back the inputs queued behind it. Without
// Options.InputQueue the stamp is ignored.
func (r *Runner) ReceiveInputAt(playerID string, frame uint64, input Input) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.queueOn {
		r.inputs[playerID] = cloneInput(input)
		return
	}
	q := append(r.queued[playerID], queuedInput{frame: frame, input: cloneInput(input)})
	if len(q) > r.queueDepth {
		q = append(q[:0], q[len(q)-r.queueDepth:]...)
	}
	r.queued[playerID] = q
}

type queuedInput struct {
	frame uint64
	input Input
}

// DrainInputs returns the inputs for the next tick and clears them. With
// Options.InputQueue it removes at most one due input per player.
func (r *Runner) DrainInputs() map[string]Input {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.queueOn {
		next := r.frame.Load() + 1
		out := make(map[string]Input)
		for id, q := range r.queued {
			if len(q) == 0 || q[0].frame > next {
				continue
			}
			out[id] = q[0].input
			if len(q) == 1 {
				delete(r.queued, id)
			} else {
				r.queued[id] = q[1:]
			}
		}
		return out
	}
	out := cloneInputs(r.inputs)
	r.inputs = make(map[string]Input)
	return out
}

// Pause stops ticking. The loop keeps running, so Resume continues at once;
// time spent paused is not caught up. Inputs received while paused are kept.
func (r *Runner) Pause() { r.paused.Store(true) }

// Resume continues ticking after Pause.
func (r *Runner) Resume() { r.paused.Store(false) }

// Paused reports whether the runner is paused.
func (r *Runner) Paused() bool { return r.paused.Load() }

// SetSpeed sets the simulation speed as a multiple of TickRate: 2 runs two
// ticks per tick period, 0.5 runs one every other period, 0 stops ticking.
// Negative, infinite and NaN values are ignored. Above 1 the loop runs several
// ticks per wake, bounded by Options.CatchUpBudget. Speed applies from the next
// wake.
func (r *Runner) SetSpeed(speed float64) {
	if math.IsNaN(speed) || math.IsInf(speed, 0) || speed < 0 {
		return
	}
	r.speedBits.Store(math.Float64bits(speed))
}

// Speed returns the current speed multiplier.
func (r *Runner) Speed() float64 { return math.Float64frombits(r.speedBits.Load()) }

// Replay returns the complete replay log of all recorded frames.
// It returns an empty log if Options.DisableReplay is set.
func (r *Runner) Replay() ReplayLog {
	if r.recorder == nil {
		return ReplayLog{}
	}
	return r.recorder.Finish()
}

func normalizeStateEncoding(encoding StateEncoding) StateEncoding {
	switch encoding {
	case StateEncodingJSON:
		return StateEncodingJSON
	default:
		return StateEncodingBytes
	}
}

func (r *Runner) tickPayload(frame uint64, state []byte) map[string]any {
	return map[string]any{
		"frame": frame,
		"state": encodeStateData(r.encoding, state),
	}
}

func (r *Runner) snapshotPayload(frame uint64, snapshot []byte) map[string]any {
	return map[string]any{
		"frame":    frame,
		"snapshot": encodeStateData(r.encoding, snapshot),
	}
}

func encodeStateData(encoding StateEncoding, data []byte) any {
	if encoding == StateEncodingJSON && json.Valid(data) {
		return json.RawMessage(data)
	}
	if data == nil {
		return []byte(nil)
	}
	return data
}

func cloneInput(input Input) Input {
	if input.Data == nil {
		return Input{}
	}
	data := make([]byte, len(input.Data))
	copy(data, input.Data)
	return Input{Data: data}
}

func cloneInputs(inputs map[string]Input) map[string]Input {
	if len(inputs) == 0 {
		return map[string]Input{}
	}
	out := make(map[string]Input, len(inputs))
	for playerID, input := range inputs {
		out[playerID] = cloneInput(input)
	}
	return out
}
