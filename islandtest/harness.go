package islandtest

import (
	"encoding/json"
	"fmt"

	"github.com/odvcencio/gosx/client/vm"
	"github.com/odvcencio/gosx/island"
	"github.com/odvcencio/gosx/island/program"
)

// Harness runs an island program entirely inside go test.
// It provides a small developer-facing surface for rendering HTML,
// dispatching handlers, and asserting on the resulting DOM output.
type Harness struct {
	program *program.Program
	island  *vm.Island
}

// New creates a test harness for a compiled island program.
func New(prog *program.Program, props any) (*Harness, error) {
	if prog == nil {
		return nil, fmt.Errorf("program is nil")
	}

	propsJSON := []byte("{}")
	if props != nil {
		var err error
		propsJSON, err = island.SerializeProps(props)
		if err != nil {
			return nil, err
		}
	}

	return &Harness{
		program: prog,
		island:  vm.NewIsland(prog, string(propsJSON)),
	}, nil
}

// HTML returns the current rendered HTML for the island.
func (h *Harness) HTML() string {
	if h == nil || h.island == nil {
		return ""
	}
	return island.RenderResolvedHTML(h.program, h.island.CurrentTree())
}

// Tree returns the current resolved tree. Callers should treat it as read-only.
func (h *Harness) Tree() *vm.ResolvedTree {
	if h == nil || h.island == nil {
		return nil
	}
	return h.island.CurrentTree()
}

// Dispatch runs a named handler with optional event payload.
func (h *Harness) Dispatch(handler string, event any) ([]vm.PatchOp, error) {
	if h == nil || h.island == nil {
		return nil, fmt.Errorf("harness is nil")
	}
	if !h.island.HasHandler(handler) {
		return nil, fmt.Errorf("handler %q not found", handler)
	}

	payload := []byte("{}")
	if event != nil {
		var err error
		payload, err = json.Marshal(event)
		if err != nil {
			return nil, fmt.Errorf("marshal event payload: %w", err)
		}
	}

	return h.island.Dispatch(handler, string(payload)), nil
}

// Click dispatches a click-style event to a named handler.
func (h *Harness) Click(handler string) ([]vm.PatchOp, error) {
	return h.Dispatch(handler, map[string]any{"type": "click"})
}

// Input dispatches an input event with a value payload to a named handler.
func (h *Harness) Input(handler, value string) ([]vm.PatchOp, error) {
	return h.Dispatch(handler, map[string]any{
		"type":  "input",
		"value": value,
	})
}
