package vm

import (
	"encoding/json"
	"fmt"
	"math"

	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// Compile-time assertion: *Island satisfies the Reconciler interface.
// If this stops compiling, Reconciler or Island has diverged — Phase 1c/1d
// must not break this conformance.
var _ Reconciler = (*Island)(nil)

// Island is a live instance of an island component with reactive state.
type Island struct {
	vm       *VM
	program  *program.Program
	prev     *ResolvedTree // previous tree for reconciliation
	handlers map[string]*program.Handler

	// reuse carries the per-program static analysis and the previous
	// walk's snapshot, so a reconcile can copy the subtrees that nothing
	// changed instead of evaluating them. nil disables the optimisation
	// and restores whole-tree evaluation. See reuse.go.
	reuse *islandReuse

	// spare holds the tree Reconcile retired one generation ago. The next
	// reconcile walks into it instead of allocating, which removes the
	// largest allocation on the event path. See nextTreeBuffer.
	spare *ResolvedTree

	// diff is the state the tree diff walks with: the path builder, the two
	// trees, the static mask and the reuse skip set. It is a pointer, and
	// the first Reconcile creates it, so an island that renders once and
	// never receives an event carries one word instead of the state. That
	// path is the server-side render, which builds one island per request.
	diff *diffWalk

	// PatchCallback is called when shared signals trigger a re-render.
	// Set by the bridge to push patches to JS.
	PatchCallback func(patches []PatchOp)

	// HydrationMismatches records differences detected between the server-rendered
	// HTML and the client's initial evaluation. Non-empty means the server and
	// client produced different output — a potential bug in props or timing.
	HydrationMismatches []string

	// The two flags sit last so they share one word of padding.
	//
	// reuseBuilt records that the analysis already ran, so a program that
	// cannot benefit is analysed once instead of on every reconcile.
	reuseBuilt bool
	// recycleOff disables the spare buffer and makes every reconcile
	// allocate a fresh tree, which is the behaviour before double
	// buffering. FuzzIslandRecycleMatchesFreshTree drives both paths and
	// compares them, so the reference path must stay reachable.
	recycleOff bool
	// skipOff makes the diff walk into the subtrees the reuse pass copied
	// verbatim, which is the behaviour before the skip. The same fuzz
	// target drives it as a third arm, because a wrong skip emits no patch
	// and no op assertion can see it. See nodeSpan.verbatim.
	skipOff bool
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
	island.prev = island.evalTree()
}

// ensureReuse runs the subtree-reuse analysis once, on the first reconcile.
//
// Building it in NewIsland would charge every server-side render for an
// optimisation only an interactive island can use. Building it here charges
// the first event instead, and every event after that reads the result.
func (island *Island) ensureReuse() {
	if island.reuseBuilt {
		return
	}
	island.reuseBuilt = true
	island.reuse = newIslandReuse(island.program)
}

// evalTree builds the next resolved tree. It hands the VM the subtree-reuse
// context first, so the walk copies the subtrees whose inputs did not change
// and evaluates the rest.
//
// The snapshot refresh runs before the walk. Read refresh's doc comment for
// why that order is the safe one.
func (island *Island) evalTree() *ResolvedTree {
	tree := &ResolvedTree{}
	island.evalTreeInto(tree)
	return tree
}

// evalTreeInto is evalTree against a caller-owned tree. Reconcile uses it to
// walk into the retired tree from the generation before last.
func (island *Island) evalTreeInto(tree *ResolvedTree) {
	reuse := island.reuse
	if reuse == nil {
		island.vm.EvalTreeInto(tree)
		return
	}
	dirty := reuse.refresh(island.vm)
	reuse.begin(island.vm, island.prev, dirty)
	defer reuse.end(island.vm)
	island.vm.EvalTreeInto(tree)
}

// nextTreeBuffer returns the tree the next walk writes into.
//
// The spare is the tree Reconcile retired one generation ago. Nothing inside
// the island reads it: prev is the newer tree, and the reuse context copies
// only out of prev. Handing it back to the walk therefore reuses a live-sized
// node array instead of allocating one.
//
// The identity check is the safety gate. Subtree reuse appends slices of
// prev.Nodes into the tree under construction, so writing into prev itself
// would read and overwrite the same array.
func (island *Island) nextTreeBuffer() *ResolvedTree {
	if island.recycleOff || island.spare == nil || island.spare == island.prev {
		return &ResolvedTree{}
	}
	spare := island.spare
	island.spare = nil
	return spare
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
//
// Treat the returned tree as read-only, and read it before the next
// Reconcile. Reconcile alternates between two trees, so the second Reconcile
// after this call overwrites the one returned here. Copy the parts you need
// if you must keep them longer.
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

	// The first tree has nothing to reuse, so the analysis stays unbuilt
	// here. An island that renders once and never receives an event — the
	// server-side path through ResolveInitialTree — then pays nothing for
	// subtree reuse at all. Reconcile builds the analysis on first use.
	island.prev = island.evalTree()
	return island
}

// SwapProgram hot-swaps the island's running program in place. It is the
// Island-level wrapper around VM.SwapProgram for `gosx dev`: the VM merges
// signal state by name (preserving live values where SignalDef.Name still
// matches; clean-reinit for renamed/removed signals), then the Island rebuilds
// its handler map against the new program and reconciles so the previous tree
// — and any patches pushed to JS — reflect the new bytecode.
//
// Reconcile diffs the freshly-evaluated tree against island.prev using the new
// program's StaticMask (Reconcile reads island.program), so callers that wired
// a PatchCallback receive the minimal patch set to bring the DOM current
// without a page reload. Returns the patch ops so the bridge can forward them.
func (island *Island) SwapProgram(prog *program.Program) []PatchOp {
	if island == nil || prog == nil {
		return nil
	}
	island.vm.SwapProgram(prog)
	island.program = prog
	island.handlers = handlerMap(prog)
	// The old analysis described the old bytecode, and the old spans point
	// into a tree the old node table produced. Both go away with the swap.
	// The rebuilt state starts unprimed, so the first walk after a swap
	// evaluates every node.
	island.reuse = nil
	island.reuseBuilt = false
	return island.Reconcile()
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
//
// The walk no longer evaluates every node. Route (b) of the old design note
// shipped: reuse.go computes, per program node, the set of signals and props
// its subtree reads, and the walk copies a subtree out of island.prev when
// none of those inputs changed. A counter increment now evaluates one
// expression node and copies the three elements around it.
//
// Two limits stay, both deliberate:
//
//   - A node inside a forEach body is never reused, because the VM rebinds
//     `_item` and the `as`/`index` names per iteration and the same program
//     node resolves to different content each pass. Making that case work
//     needs a per-iteration identity, not a per-node one.
//   - A subtree containing any impure opcode is never reused. isPureOp in
//     reuse.go holds the contract, and an unlisted opcode is impure, so the
//     safe direction is the default.
//
// Route (a) of the old note, skipping evaluation of Program.StaticMask
// subtrees, is now redundant. The mask means "this one node is static" — it
// is populated by populateStaticMask in ir/island.go straight from each
// source node's IsStatic flag, with no subtree rollup — so it could not have
// carried a whole subtree by itself. The read-set analysis proves the same
// property for a subtree and covers the dynamic-but-unchanged case too.
func (island *Island) Reconcile() []PatchOp {
	island.ensureReuse()
	next := island.nextTreeBuffer()
	island.evalTreeInto(next)
	if island.diff == nil {
		island.diff = &diffWalk{}
	}
	ops := reconcileTreesInto(island.diff, island.prev, next, island.program.StaticMask, island.verbatimSpans())
	// The tree that was current becomes the buffer the walk after next
	// writes into. It stays readable until then, which is what
	// CurrentTree's one-reconcile validity window states.
	island.spare, island.prev = island.prev, next
	return ops
}

// verbatimSpans returns the span table the walk that just ran filled, so the
// diff can stop at the subtrees that walk copied verbatim. It returns nil when
// the island has no reuse plan, and when skipOff switches the optimisation off
// for the fuzz target.
//
// The table describes the tree the walk just produced, against the tree the
// walk copied from. Reconcile diffs exactly that pair, so the table is valid
// only for the diff that follows the walk that filled it.
func (island *Island) verbatimSpans() []nodeSpan {
	if island.skipOff || island.reuse == nil {
		return nil
	}
	return island.reuse.ctx.spans
}

// Dispose cleans up the island's signals and effects.
func (island *Island) Dispose() {
	// Signal cleanup is handled by GC since we don't have persistent subscriptions
	// in this version. The bridge removes the island from its map.
	island.prev = nil
	island.spare = nil
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

// InitSignals is the exported wrapper around initSignals for tests and
// surface bootstraps that don't go through the full Island lifecycle.
// It evaluates each SignalDef's init expression against the VM and
// registers a fresh signal.Signal for it. Idempotency is the caller's
// responsibility; calling InitSignals twice for the same Program
// re-evaluates the init exprs and replaces the prior signals.
//
// Engine-surface handlers that mutate package-level struct/map/slice
// state via OpFieldSet / OpIndexSet (Slice Y.C) rely on this — without
// initialized signals, OpLocalGet falls through to a zero Value with
// nil Fields, and the in-place mutation silently no-ops.
func InitSignals(vm *VM, prog *program.Program) {
	initSignals(vm, prog)
}

func handlerMap(prog *program.Program) map[string]*program.Handler {
	handlers := make(map[string]*program.Handler, len(prog.Handlers))
	for i := range prog.Handlers {
		handlers[prog.Handlers[i].Name] = &prog.Handlers[i]
	}
	return handlers
}

func parseEventData(eventDataJSON string) map[string]string {
	// Fast path: the vast majority of handler dispatches come with
	// "{}" or "" as the event-data payload (counter increments,
	// plain button clicks, etc.). Skipping json.Unmarshal here
	// eliminates a reflect.MakeMapWithSize + decoder state setup
	// allocation on every dispatch.
	if eventDataJSON == "" || eventDataJSON == "{}" {
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
