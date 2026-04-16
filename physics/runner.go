package physics

import (
	"encoding/json"
	"math"
	"sort"
	"strings"

	"github.com/odvcencio/gosx/hub"
	"github.com/odvcencio/gosx/sim"
)

// BodyState is the serializable state for one rigid body in a physics world.
type BodyState struct {
	ID              string `json:"id,omitempty"`
	Index           int    `json:"index"`
	Position        Vec3   `json:"position"`
	Rotation        Quat   `json:"rotation"`
	Velocity        Vec3   `json:"velocity,omitempty"`
	AngularVelocity Vec3   `json:"angularVelocity,omitempty"`
}

// WorldState is the compact authoritative state broadcast by sim.Runner.
type WorldState struct {
	Bodies []BodyState `json:"bodies,omitempty"`
}

type inputVec3 struct {
	X   float64
	Y   float64
	Z   float64
	set bool
}

func (v *inputVec3) UnmarshalJSON(data []byte) error {
	var arr []float64
	if err := json.Unmarshal(data, &arr); err == nil {
		if len(arr) > 0 {
			v.X = arr[0]
		}
		if len(arr) > 1 {
			v.Y = arr[1]
		}
		if len(arr) > 2 {
			v.Z = arr[2]
		}
		v.set = true
		return nil
	}
	var obj map[string]float64
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	if value, ok := obj["x"]; ok {
		v.X = value
	} else if value, ok := obj["X"]; ok {
		v.X = value
	}
	if value, ok := obj["y"]; ok {
		v.Y = value
	} else if value, ok := obj["Y"]; ok {
		v.Y = value
	}
	if value, ok := obj["z"]; ok {
		v.Z = value
	} else if value, ok := obj["Z"]; ok {
		v.Z = value
	}
	v.set = true
	return nil
}

func (v *inputVec3) vec3() Vec3 {
	if v == nil {
		return Vec3{}
	}
	return Vec3{X: v.X, Y: v.Y, Z: v.Z}
}

type inputCommand struct {
	Type      string     `json:"type,omitempty"`
	ID        string     `json:"id,omitempty"`
	BodyID    string     `json:"bodyID,omitempty"`
	Index     int        `json:"index,omitempty"`
	BodyIndex int        `json:"bodyIndex,omitempty"`
	Impulse   *inputVec3 `json:"impulse,omitempty"`
	Force     *inputVec3 `json:"force,omitempty"`
	Torque    *inputVec3 `json:"torque,omitempty"`
	Point     *inputVec3 `json:"point,omitempty"`
}

// NewRunner wires a physics world into the existing server-authoritative
// simulation runner instead of starting a package-local ticker.
func NewRunner(h *hub.Hub, world *World, opts sim.Options) *sim.Runner {
	if world == nil {
		world = NewWorld(WorldConfig{})
	}
	if opts.TickRate <= 0 {
		opts.TickRate = tickRateForWorld(world)
	}
	return sim.New(h, world, opts)
}

func tickRateForWorld(world *World) int {
	if world == nil || world.fixedTimestep <= 0 {
		return 60
	}
	rate := int(math.Round(1 / world.fixedTimestep))
	if rate <= 0 {
		return 60
	}
	return rate
}

// Tick applies queued physics inputs, then advances the world by one fixed
// simulation step.
func (w *World) Tick(inputs map[string]sim.Input) {
	if w == nil {
		return
	}
	w.ApplyInputs(inputs)
	w.StepFixed()
}

// ApplyInputs decodes runner input payloads and applies primitive body
// commands. Payloads may be a single command object or an array of commands.
func (w *World) ApplyInputs(inputs map[string]sim.Input) {
	if w == nil || len(inputs) == 0 {
		return
	}
	players := make([]string, 0, len(inputs))
	for playerID := range inputs {
		players = append(players, playerID)
	}
	sort.Strings(players)
	for _, playerID := range players {
		input := inputs[playerID]
		if len(input.Data) == 0 {
			continue
		}
		w.applyInputData(input.Data)
	}
}

func (w *World) applyInputData(data []byte) {
	var commands []inputCommand
	if err := json.Unmarshal(data, &commands); err == nil {
		for _, command := range commands {
			w.applyInputCommand(command)
		}
		return
	}
	var command inputCommand
	if err := json.Unmarshal(data, &command); err == nil {
		w.applyInputCommand(command)
	}
}

func (w *World) applyInputCommand(command inputCommand) {
	body := w.bodyForInput(command)
	if body == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(command.Type)) {
	case "impulse", "physics:impulse":
		if command.Impulse == nil {
			return
		}
		point := body.Position
		if command.Point != nil {
			point = command.Point.vec3()
		}
		body.ApplyImpulse(command.Impulse.vec3(), point)
	case "force", "physics:force":
		if command.Force != nil {
			body.ApplyForce(command.Force.vec3())
		}
	case "torque", "physics:torque":
		if command.Torque != nil {
			body.ApplyTorque(command.Torque.vec3())
		}
	}
}

func (w *World) bodyForInput(command inputCommand) *RigidBody {
	id := strings.TrimSpace(command.BodyID)
	if id == "" {
		id = strings.TrimSpace(command.ID)
	}
	index := command.BodyIndex
	if index == 0 {
		index = command.Index
	}
	for _, body := range w.bodies {
		if body == nil {
			continue
		}
		if id != "" && body.ID == id {
			return body
		}
		if index > 0 && body.index == index {
			return body
		}
	}
	return nil
}

// Snapshot returns a restorable physics checkpoint for sim.Runner replay.
func (w *World) Snapshot() []byte {
	return w.State()
}

// Restore applies a previously captured Snapshot/State payload.
func (w *World) Restore(snapshot []byte) {
	if w == nil || len(snapshot) == 0 {
		return
	}
	var state WorldState
	if err := json.Unmarshal(snapshot, &state); err != nil {
		return
	}
	w.ApplyState(state)
}

// State returns the current authoritative state for sim.Runner broadcasts.
func (w *World) State() []byte {
	if w == nil {
		return nil
	}
	data, err := json.Marshal(w.StateSnapshot())
	if err != nil {
		return nil
	}
	return data
}

// StateSnapshot returns a typed copy of the world's dynamic body state.
func (w *World) StateSnapshot() WorldState {
	if w == nil || len(w.bodies) == 0 {
		return WorldState{}
	}
	state := WorldState{Bodies: make([]BodyState, 0, len(w.bodies))}
	for _, body := range w.bodies {
		if body == nil {
			continue
		}
		state.Bodies = append(state.Bodies, BodyState{
			ID:              body.ID,
			Index:           body.index,
			Position:        body.Position,
			Rotation:        body.Rotation,
			Velocity:        body.Velocity,
			AngularVelocity: body.AngularVelocity,
		})
	}
	return state
}

// ApplyState restores matching body transforms and velocities from a snapshot.
func (w *World) ApplyState(state WorldState) {
	if w == nil || len(state.Bodies) == 0 {
		return
	}
	byIndex := make(map[int]*RigidBody, len(w.bodies))
	byID := make(map[string]*RigidBody, len(w.bodies))
	for _, body := range w.bodies {
		if body == nil {
			continue
		}
		byIndex[body.index] = body
		if body.ID != "" {
			byID[body.ID] = body
		}
	}
	for _, item := range state.Bodies {
		body := byIndex[item.Index]
		if body == nil && item.ID != "" {
			body = byID[item.ID]
		}
		if body == nil {
			continue
		}
		body.Position = item.Position
		body.Rotation = item.Rotation.Normalize()
		body.Velocity = item.Velocity
		body.AngularVelocity = item.AngularVelocity
		body.Wake()
	}
}
