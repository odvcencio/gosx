// Package sim provides server-authoritative game simulation over gosx hubs.
// Games implement the Simulation interface and a Runner drives the simulation
// at a fixed tick rate, collecting player inputs and broadcasting state.
package sim

import (
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/odvcencio/gosx/hub"
)

// Input holds raw per-client input data for a single tick.
type Input struct {
	Data []byte
}

// Simulation is the interface a game must implement to be driven by a Runner.
type Simulation interface {
	// Tick advances the simulation one step with the collected inputs.
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

// Options configures a Runner.
type Options struct {
	// TickRate is the number of simulation ticks per second. Zero defaults to 60.
	TickRate int
	// StateEncoding selects the state/snapshot representation in hub payloads.
	// Leave empty for StateEncodingBytes compatibility.
	StateEncoding StateEncoding
}

// Runner drives a Simulation at a fixed tick rate over a hub.
type Runner struct {
	hub       *hub.Hub
	sim       Simulation
	tickRate  int
	mu        sync.Mutex
	inputs    map[string]Input
	running   atomic.Bool
	frame     atomic.Uint64
	snapshots *snapshotRing
	recorder  *replayRecorder
	encoding  StateEncoding
}

// New creates a Runner that will drive sim over h at the configured tick rate.
func New(h *hub.Hub, s Simulation, opts Options) *Runner {
	rate := opts.TickRate
	if rate <= 0 {
		rate = 60
	}
	return &Runner{
		hub:       h,
		sim:       s,
		tickRate:  rate,
		inputs:    make(map[string]Input),
		snapshots: newSnapshotRing(128),
		recorder:  newReplayRecorder(),
		encoding:  normalizeStateEncoding(opts.StateEncoding),
	}
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
		snap := r.sim.Snapshot()
		frame := r.frame.Load()
		ctx.Hub.Send(ctx.Client.ID, "sim:snapshot", r.snapshotPayload(frame, snap))
	})
}

// ReceiveInput stores an input for the given player, overwriting any prior
// input from that player in the current tick window.
func (r *Runner) ReceiveInput(playerID string, input Input) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inputs[playerID] = input
}

// DrainInputs returns all collected inputs and clears the buffer.
func (r *Runner) DrainInputs() map[string]Input {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.inputs
	r.inputs = make(map[string]Input)
	return out
}

// Replay returns the complete replay log of all recorded frames.
func (r *Runner) Replay() ReplayLog {
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
