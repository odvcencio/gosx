package islandtest

import (
	"encoding/json"
	"fmt"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/island/program"
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

// Tree returns the current resolved tree. Treat it as read-only, and note that
// it is only valid until the SECOND Reconcile after this call.
//
// The island double-buffers its resolved trees: it keeps the tree it retired one
// generation ago and walks into that buffer instead of allocating a fresh one.
// That is what took a reconcile from about 1 KB and 3 allocations down to 8
// bytes and 1. The cost is a lifetime: the next Reconcile writes into the OTHER
// buffer, so the tree this returns survives it, and the one after that
// overwrites the tree in place.
//
// So a caller may read it, compare it, or render it. A caller must not hold it
// across two reconciles and expect the old contents, and must not mutate it —
// the island will read the same array as its previous generation.
//
// HTML() is safe because it uses the tree immediately and returns a string.
//
// This whole lifetime rule is a claim about another package. Two facts carry
// it: the island hands the retired buffer back to the walk, and it refuses to
// do so when the spare and the previous tree are the same array. Delete either
// one and the tree lives forever, which makes this paragraph wrong in the
// direction that costs least, or the tree dies one generation early, which
// makes it wrong in the direction that corrupts a test.
//
//	gosx:claim has client/vm/island.go `func \(island \*Island\) nextTreeBuffer\(\) \*ResolvedTree`
//	gosx:claim has client/vm/island.go `island.spare == island.prev`
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
