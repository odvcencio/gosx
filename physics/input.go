package physics

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
)

// Input holds one client's raw input payload for a single step.
//
// The type is deliberately local to this package. A transport package converts
// its own input type into this one, so physics never imports a network or hub
// package and stays extractable on its own.
type Input struct {
	Data []byte
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

// TickRate returns the whole ticks per second that match the fixed timestep.
// A transport layer reads it to drive the world at the rate the world expects.
func (w *World) TickRate() int {
	if w == nil || w.fixedTimestep <= 0 {
		return 60
	}
	rate := int(math.Round(1 / w.fixedTimestep))
	if rate <= 0 {
		return 60
	}
	return rate
}

// Tick applies queued physics inputs, then advances the world by one fixed
// simulation step.
func (w *World) Tick(inputs map[string]Input) {
	if w == nil {
		return
	}
	w.ApplyInputs(inputs)
	w.StepFixed()
}

// ApplyInputs decodes input payloads and applies primitive body commands. A
// payload may be a single command object or an array of commands.
//
// Player identifiers are sorted before the payloads run, because Go randomises
// map iteration and an unsorted order would make two replays of the same frame
// produce different float results.
func (w *World) ApplyInputs(inputs map[string]Input) {
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
