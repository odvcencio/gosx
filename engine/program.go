package engine

import (
	"encoding/json"
	"fmt"

	islandprogram "m31labs.dev/gosx/island/program"
)

// Program is the VM-driven engine scene graph.
//
// Aliased to the unified islandprogram.Program per Phase 1a (ADR 0001).
// Engine programs populate the EngineNodes slice and carry Surface=SurfaceScene3D
// after decode via DecodeProgramJSON.
type Program = islandprogram.Program

// Node is a single engine-scene node, aliased to islandprogram.EngineNode so
// existing engine.Node references continue to compile after the unification.
//
// TODO(phase-1c): when scene reconciler moves to its own package, re-target
// this alias to that home and drop the re-export from island/program.
type Node = islandprogram.EngineNode

// CommandKind identifies a renderer-facing scene command.
type CommandKind uint8

const (
	CommandCreateObject CommandKind = iota
	CommandRemoveObject
	CommandSetTransform
	CommandSetMaterial
	CommandSetLight
	CommandSetCamera
	CommandSetParticles
)

func (k CommandKind) String() string {
	switch k {
	case CommandCreateObject:
		return "CreateObject"
	case CommandRemoveObject:
		return "RemoveObject"
	case CommandSetTransform:
		return "SetTransform"
	case CommandSetMaterial:
		return "SetMaterial"
	case CommandSetLight:
		return "SetLight"
	case CommandSetCamera:
		return "SetCamera"
	case CommandSetParticles:
		return "SetParticles"
	default:
		return fmt.Sprintf("CommandKind(%d)", k)
	}
}

// Command is the serialized unit passed from the WASM-side reconciler to a
// renderer implementation.
type Command struct {
	Kind     CommandKind     `json:"kind"`
	ObjectID int             `json:"objectId"`
	Data     json.RawMessage `json:"data,omitempty"`
}

// EncodeProgramJSON serializes an engine program for development/runtime transport.
func EncodeProgramJSON(p *Program) ([]byte, error) {
	return json.Marshal(p)
}

// DecodeProgramJSON deserializes an engine program from JSON, injecting
// Surface=SurfaceScene3D into the in-memory model per ADR 0001.
func DecodeProgramJSON(data []byte) (*Program, error) {
	var p Program
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	p.Surface = islandprogram.SurfaceScene3D
	return &p, nil
}
