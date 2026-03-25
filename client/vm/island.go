package vm

import (
	"encoding/json"
	"fmt"

	"github.com/odvcencio/gosx/island/program"
	"github.com/odvcencio/gosx/signal"
)

// Island is a live instance of an island component with reactive state.
type Island struct {
	vm       *VM
	program  *program.Program
	id       string
	prev     *ResolvedTree // previous tree for reconciliation
	handlers map[string]*program.Handler
}

// NewIsland creates a live island from a program and initial props JSON.
func NewIsland(prog *program.Program, propsJSON string) *Island {
	// Parse props
	var rawProps map[string]json.RawMessage
	json.Unmarshal([]byte(propsJSON), &rawProps)

	// Convert to Value map
	props := make(map[string]Value)
	for _, def := range prog.Props {
		if raw, ok := rawProps[def.Name]; ok {
			props[def.Name] = parseJSONValue(raw, def.Type)
		} else {
			props[def.Name] = ZeroValue(def.Type)
		}
	}

	vm := NewVM(prog, props)

	// Initialize signals
	for _, def := range prog.Signals {
		initVal := vm.Eval(def.Init)
		sig := signal.New(initVal)
		vm.SetSignal(def.Name, sig)
	}

	// Build handler map
	handlers := make(map[string]*program.Handler)
	for i := range prog.Handlers {
		handlers[prog.Handlers[i].Name] = &prog.Handlers[i]
	}

	island := &Island{
		vm:       vm,
		program:  prog,
		handlers: handlers,
	}

	// Evaluate initial tree
	island.prev = vm.EvalTree()

	return island
}

// Dispatch executes a named handler and returns the resulting patch ops.
// All signal mutations within the handler body are batched into a single reconcile.
func (island *Island) Dispatch(handlerName string, eventDataJSON string) []PatchOp {
	handler, ok := island.handlers[handlerName]
	if !ok {
		return nil
	}

	// Batch all signal mutations
	signal.Batch(func() {
		for _, exprID := range handler.Body {
			island.vm.Eval(exprID)
		}
	})

	// Reconcile
	return island.Reconcile()
}

// Reconcile evaluates the current tree and diffs against the previous tree.
func (island *Island) Reconcile() []PatchOp {
	next := island.vm.EvalTree()
	ops := ReconcileTrees(island.prev, next, island.program.StaticMask)
	island.prev = next
	return ops
}

// Dispose cleans up the island's signals and effects.
func (island *Island) Dispose() {
	// Signal cleanup is handled by GC since we don't have persistent subscriptions
	// in this version. The bridge removes the island from its map.
	island.prev = nil
}

// ReconcileTrees diffs two resolved trees and returns patch ops.
// Stub — full implementation in Task 7 (reconcile.go).
func ReconcileTrees(prev, next *ResolvedTree, staticMask []bool) []PatchOp {
	if prev == nil || next == nil {
		return nil
	}
	var ops []PatchOp
	for i := range next.Nodes {
		if i >= len(prev.Nodes) {
			continue
		}
		if i < len(staticMask) && staticMask[i] {
			continue // skip static subtrees
		}
		// Simple text diff for now
		if prev.Nodes[i].Text != next.Nodes[i].Text {
			ops = append(ops, PatchOp{
				Kind: PatchSetText,
				Path: nodePath(i),
				Text: next.Nodes[i].Text,
			})
		}
	}
	return ops
}

func nodePath(idx int) string {
	// Simple path — will be replaced by proper tree-walking in Task 7
	return fmt.Sprintf("%d", idx)
}

// parseJSONValue converts a JSON value to a VM Value based on expected type.
func parseJSONValue(raw json.RawMessage, typ program.ExprType) Value {
	switch typ {
	case program.TypeString:
		var s string
		json.Unmarshal(raw, &s)
		return StringVal(s)
	case program.TypeInt:
		var n float64
		json.Unmarshal(raw, &n)
		return IntVal(int(n))
	case program.TypeFloat:
		var f float64
		json.Unmarshal(raw, &f)
		return FloatVal(f)
	case program.TypeBool:
		var b bool
		json.Unmarshal(raw, &b)
		return BoolVal(b)
	default:
		return ZeroValue(typ)
	}
}
