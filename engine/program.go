package engine

import (
	"encoding/json"
	"fmt"

	islandprogram "github.com/odvcencio/gosx/island/program"
)

// Program describes a VM-driven engine scene graph.
// It mirrors the island program model closely enough that the runtime can
// share expression and signal infrastructure while targeting a non-DOM surface.
type Program struct {
	Name    string                    `json:"name"`
	Nodes   []Node                    `json:"nodes"`
	Exprs   []islandprogram.Expr      `json:"exprs,omitempty"`
	Signals []islandprogram.SignalDef `json:"signals,omitempty"`
}

// Node describes a single engine-scene node.
type Node struct {
	Kind     string                          `json:"kind"`
	Geometry string                          `json:"geometry,omitempty"`
	Material string                          `json:"material,omitempty"`
	Props    map[string]islandprogram.ExprID `json:"props,omitempty"`
	Children []int                           `json:"children,omitempty"`
	Static   bool                            `json:"static,omitempty"`
}

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

// DecodeProgramJSON deserializes an engine program from JSON.
func DecodeProgramJSON(data []byte) (*Program, error) {
	var p Program
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
