package vm

import (
	"encoding/json"
	"fmt"
	"math"

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

	// PatchCallback is called when shared signals trigger a re-render.
	// Set by the bridge to push patches to JS.
	PatchCallback func(patches []PatchOp)

	// HydrationMismatches records differences detected between the server-rendered
	// HTML and the client's initial evaluation. Non-empty means the server and
	// client produced different output — a potential bug in props or timing.
	HydrationMismatches []string
}

// CheckHydration compares the initial client-side tree against what the server
// would have rendered (represented as the DOM's current state). Returns
// mismatches if any. Call this after hydration to detect SSR/client divergence.
func (island *Island) CheckHydration(serverTree *ResolvedTree) []string {
	if serverTree == nil || island.prev == nil {
		return nil
	}
	var mismatches []string
	maxLen := minNodeCount(serverTree, island.prev)
	for i := 0; i < maxLen; i++ {
		mismatches = append(mismatches, compareHydrationNode(i, &serverTree.Nodes[i], &island.prev.Nodes[i])...)
	}

	if len(serverTree.Nodes) != len(island.prev.Nodes) {
		mismatches = append(mismatches, fmt.Sprintf("tree size: server=%d nodes, client=%d nodes",
			len(serverTree.Nodes), len(island.prev.Nodes)))
	}

	island.HydrationMismatches = mismatches
	return mismatches
}

// SetSharedSignal replaces an island-local signal with a shared one from the store.
func (island *Island) SetSharedSignal(name string, sig *signal.Signal[Value]) {
	island.vm.SetSignal(name, sig)
	// Re-evaluate the initial tree with the shared signal's current value
	island.prev = island.vm.EvalTree()
}

// EvalExpr evaluates an expression by ID in this island's VM.
// Used by the bridge to compute typed init values for shared signals.
func (island *Island) EvalExpr(id program.ExprID) Value {
	return island.vm.Eval(id)
}

// HasHandler reports whether the island exposes a named handler.
func (island *Island) HasHandler(name string) bool {
	if island == nil {
		return false
	}
	_, ok := island.handlers[name]
	return ok
}

// CurrentTree returns the most recently reconciled tree for inspection.
// Callers should treat the returned tree as read-only.
func (island *Island) CurrentTree() *ResolvedTree {
	if island == nil {
		return nil
	}
	return island.prev
}

// NewIsland creates a live island from a program and initial props JSON.
func NewIsland(prog *program.Program, propsJSON string) *Island {
	vm := NewVM(prog, parseProps(prog, propsJSON))
	initSignals(vm, prog)

	island := &Island{
		vm:       vm,
		program:  prog,
		handlers: handlerMap(prog),
	}

	island.prev = vm.EvalTree()
	return island
}

// ResolveInitialTree evaluates a program with its initial props and signal
// state, returning the tree the browser VM will see before any events fire.
func ResolveInitialTree(prog *program.Program, propsJSON string) *ResolvedTree {
	island := NewIsland(prog, propsJSON)
	if island == nil {
		return &ResolvedTree{}
	}
	return island.prev
}

// Dispatch executes a named handler and returns the resulting patch ops.
// All signal mutations within the handler body are batched into a single reconcile.
func (island *Island) Dispatch(handlerName string, eventDataJSON string) []PatchOp {
	handler, ok := island.handlers[handlerName]
	if !ok {
		return nil
	}

	island.vm.SetEventData(parseEventData(eventDataJSON))
	signal.Batch(func() {
		island.evalHandlerBody(handler)
	})
	island.vm.ClearEventData()
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

func minNodeCount(left, right *ResolvedTree) int {
	maxLen := len(left.Nodes)
	if len(right.Nodes) < maxLen {
		maxLen = len(right.Nodes)
	}
	return maxLen
}

func compareHydrationNode(idx int, serverNode, clientNode *ResolvedNode) []string {
	if serverNode == nil || clientNode == nil {
		return nil
	}
	var mismatches []string
	if serverNode.Tag != clientNode.Tag {
		mismatches = append(mismatches, fmt.Sprintf("node %d: server tag=%q, client tag=%q", idx, serverNode.Tag, clientNode.Tag))
	}
	if serverNode.Text != clientNode.Text {
		mismatches = append(mismatches, fmt.Sprintf("node %d: server text=%q, client text=%q", idx, serverNode.Text, clientNode.Text))
	}
	return mismatches
}

func parseProps(prog *program.Program, propsJSON string) map[string]Value {
	var rawProps map[string]json.RawMessage
	_ = json.Unmarshal([]byte(propsJSON), &rawProps)

	props := make(map[string]Value, len(rawProps)+len(prog.Props)+1)
	propsObject := make(map[string]any, len(rawProps))
	for name, raw := range rawProps {
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			props[name] = ZeroValue(program.TypeAny)
			continue
		}
		props[name] = parseAnyValue(decoded)
		propsObject[name] = decoded
	}
	props["props"] = parseAnyValue(propsObject)

	for _, def := range prog.Props {
		if raw, ok := rawProps[def.Name]; ok {
			props[def.Name] = parseJSONValue(raw, def.Type)
		} else if _, ok := props[def.Name]; !ok {
			props[def.Name] = ZeroValue(def.Type)
		}
	}
	return props
}

func initSignals(vm *VM, prog *program.Program) {
	for _, def := range prog.Signals {
		initVal := vm.Eval(def.Init)
		vm.SetSignal(def.Name, signal.New(initVal))
	}
}

func handlerMap(prog *program.Program) map[string]*program.Handler {
	handlers := make(map[string]*program.Handler, len(prog.Handlers))
	for i := range prog.Handlers {
		handlers[prog.Handlers[i].Name] = &prog.Handlers[i]
	}
	return handlers
}

func parseEventData(eventDataJSON string) map[string]string {
	if eventDataJSON == "" {
		return nil
	}
	var eventData map[string]string
	if err := json.Unmarshal([]byte(eventDataJSON), &eventData); err == nil {
		return eventData
	}
	return parseMixedEventData(eventDataJSON)
}

func parseMixedEventData(eventDataJSON string) map[string]string {
	var mixed map[string]any
	if err := json.Unmarshal([]byte(eventDataJSON), &mixed); err != nil {
		return nil
	}
	eventData := make(map[string]string, len(mixed))
	for key, value := range mixed {
		eventData[key] = fmt.Sprintf("%v", value)
	}
	return eventData
}

func (island *Island) evalHandlerBody(handler *program.Handler) {
	for _, exprID := range handler.Body {
		island.vm.Eval(exprID)
	}
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
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return ZeroValue(typ)
		}
		return parseAnyValue(value)
	}
}

func parseAnyValue(value any) Value {
	switch v := value.(type) {
	case nil:
		return ZeroValue(program.TypeAny)
	case string:
		return StringVal(v)
	case bool:
		return BoolVal(v)
	case float64:
		if math.Trunc(v) == v {
			return IntVal(int(v))
		}
		return FloatVal(v)
	case []any:
		items := make([]Value, len(v))
		for i := range v {
			items[i] = parseAnyValue(v[i])
		}
		return ArrayVal(items)
	case map[string]any:
		fields := make(map[string]Value, len(v))
		for key, field := range v {
			fields[key] = parseAnyValue(field)
		}
		return ObjectVal(fields)
	default:
		return StringVal(fmt.Sprintf("%v", v))
	}
}
