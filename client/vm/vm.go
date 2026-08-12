package vm

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"

	"m31labs.dev/gosx/internal/htmlattr"
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// VM evaluates island expressions against props and signal state.
type VM struct {
	program        *program.Program
	props          map[string]Value
	signals        map[string]*signal.Signal[Value]
	computeds      map[string]*signal.Computed[Value]
	exprs          []program.Expr
	lits           []litSlot                   // numeric literals decoded once, indexed by ExprID
	eventData      map[string]Value            // current event data (set during handler dispatch)
	frame          *frame                      // locals table for the current handler evaluation (X.A)
	forCap         int                         // per-loop AND per-dispatch total iteration cap (X.C, loops.go); 0 → default
	funcs          map[string]*program.FuncDef // user-function registry (Y.D)
	callDepth      int                         // current OpIndirectCall recursion depth (Y.D)
	evalDepth      int                         // current Eval recursion depth, guards the general (non-call) recursion path
	loopSteps      int                         // total OpFor / OpForRange iterations charged in the current top-level dispatch
	loopBudgetHit  bool                        // sticky once loopSteps exceeds its budget, so every nested loop level stops
	signalGen      uint32                      // bumped whenever the signals table changes identity or membership
	hosts          map[string]HostReceiver     // per-VM host-receiver bindings for OpHostCall (Y.E)
	hostsMu        sync.RWMutex                // guards hosts: BindHost may run on a teardown goroutine concurrently with LookupHost / dispatch
	diagnostics    []Diagnostic
	diagnosticSink DiagnosticSink

	// reuse carries the subtree-reuse context for the current EvalTree
	// call. The Island sets it before the walk and clears it after, so a
	// direct EvalTree caller sees the original full-evaluation behaviour.
	// nil means "evaluate every node". See reuse.go.
	reuse *treeReuse
}

type iterationContext struct {
	values  map[string]Value
	present map[string]bool
}

type forEachScope struct {
	itemName  string
	indexName string
	keyName   string
}

// SetEventData sets the current event context for OpEventGet evaluation.
func (vm *VM) SetEventData(data map[string]Value) {
	vm.eventData = data
}

// ClearEventData clears the event context after handler dispatch.
func (vm *VM) ClearEventData() {
	vm.eventData = nil
}

// NewVM creates a VM for an island program with the given props.
func NewVM(prog *program.Program, props map[string]Value) *VM {
	if props == nil {
		props = make(map[string]Value)
	}
	vm := &VM{
		program: prog,
		props:   props,
		signals: make(map[string]*signal.Signal[Value]),
	}
	if prog == nil {
		vm.program = &program.Program{}
		vm.recordDiagnostic(Diagnostic{
			Severity: DiagnosticError,
			Code:     "nil_program",
			Message:  "island VM created with a nil program",
		})
		return vm
	}
	vm.exprs = prog.Exprs
	vm.lits = buildLiteralTable(prog.Exprs)
	// Slice Y.D: build a fast funcDef lookup so OpIndirectCall is one
	// map probe. Programs without user functions (Funcs nil/empty)
	// pay nothing — the map stays nil and the dispatcher records the
	// missing-callee diagnostic.
	if len(prog.Funcs) > 0 {
		vm.funcs = make(map[string]*program.FuncDef, len(prog.Funcs))
		for i := range prog.Funcs {
			vm.funcs[prog.Funcs[i].Name] = &prog.Funcs[i]
		}
	}
	return vm
}

// SetSignal registers a signal by name.
func (vm *VM) SetSignal(name string, sig *signal.Signal[Value]) {
	vm.signals[name] = sig
	vm.signalGen++
	// A shared-signal install replaces the dependency object, not just its
	// value. Rebuild derived nodes so their subscriptions move from the
	// retired local signal to the shared instance.
	if len(vm.computeds) > 0 {
		vm.rebuildComputeds()
	}
}

// stopComputeds releases every dependency subscription retained by the
// current program's derived values. It is shared by hot-swap and disposal so
// neither path can leave an old program observing live signals.
func (vm *VM) stopComputeds() {
	for _, computed := range vm.computeds {
		computed.Stop()
	}
	vm.computeds = nil
}

// rebuildComputeds recreates the program's declared derived values in source
// order. signal.Derive records the mutable or earlier-computed values read by
// Eval, giving the VM dependency invalidation and Batch semantics without a
// second reactive implementation.
func (vm *VM) rebuildComputeds() {
	vm.stopComputeds()
	if vm.program == nil || len(vm.program.Computeds) == 0 {
		return
	}
	vm.computeds = make(map[string]*signal.Computed[Value], len(vm.program.Computeds))
	for _, def := range vm.program.Computeds {
		expr := def.Expr
		vm.computeds[def.Name] = signal.Derive(func() Value {
			return vm.Eval(expr)
		})
	}
}

// SwapProgram replaces the VM's running program in place, preserving signal
// state by name. This is the VM-level seam for program hot-swap (`gosx dev`):
// the compiled bytecode changes but the live reactive state is carried across
// where the signal names still match.
//
// Teardown: computed definitions retain dependency subscriptions, so the old
// set is stopped before bytecode replacement and rebuilt against the new
// program and merged signal table below.
//
// Signal merge-by-name (the reason signals are name-keyed at vm.signals):
// for each SignalDef in the new program, if a signal with the same Name is
// already live, its current Value is KEPT and the new init expr is ignored.
// Signal names that are new init fresh from their init expr (mirrors
// initSignals in island.go). Signals present in the old program but absent
// from the new one are dropped — a renamed or removed signal gets a clean
// slate rather than leaking a stale value under a name nothing references.
//
// SwapProgram does NOT reconcile the DOM; reconciliation belongs to the
// Island, which owns the previous tree and the patch callback. Island.
// SwapProgram wraps this method and performs the reconcile.
func (vm *VM) SwapProgram(p *program.Program) {
	if p == nil {
		vm.recordDiagnostic(Diagnostic{
			Severity: DiagnosticError,
			Code:     "nil_program",
			Message:  "SwapProgram called with a nil program",
		})
		return
	}
	vm.stopComputeds()

	// Install the new bytecode. Point at the new exprs and rebuild the
	// funcDef lookup exactly as NewVM does, so OpIndirectCall resolves
	// against the new program's helpers (and a program that drops all of
	// its funcs releases the old map).
	vm.program = p
	vm.exprs = p.Exprs
	vm.lits = buildLiteralTable(p.Exprs)
	vm.funcs = nil
	if len(p.Funcs) > 0 {
		vm.funcs = make(map[string]*program.FuncDef, len(p.Funcs))
		for i := range p.Funcs {
			vm.funcs[p.Funcs[i].Name] = &p.Funcs[i]
		}
	}

	// Re-run signal init, merging by name. Build the next signal set from
	// the new program's SignalDefs so that removed/renamed signals are not
	// carried over; for each retained name, keep the live signal instance
	// (and thus its current value), and for each new name, evaluate the
	// init expr against the freshly-installed exprs.
	oldSignals := vm.signals
	merged := make(map[string]*signal.Signal[Value], len(p.Signals))
	// Expose the in-progress table while evaluating fresh initializers. This
	// preserves source-order references to earlier signal declarations without
	// making removed names from the old program visible.
	vm.signals = merged
	for _, def := range p.Signals {
		if existing, ok := oldSignals[def.Name]; ok {
			merged[def.Name] = existing
			continue
		}
		merged[def.Name] = signal.New(vm.Eval(def.Init))
	}
	vm.signalGen++
	vm.rebuildComputeds()
}

// SetProp installs a value under name in the VM's prop map. Use this
// when an out-of-band caller (engine-surface event dispatcher, host
// bridge, …) needs to feed values to a handler that reads them via
// OpPropGet without going through the constructor's initial map. The
// write creates no signal subscription and emits no diagnostic. Declared
// computeds are rebuilt because signal.Derive cannot observe ordinary prop-map
// writes; callers must still restore prior values via GetProp + SetProp when
// they care about scoping.
func (vm *VM) SetProp(name string, value Value) {
	if vm.props == nil {
		vm.props = make(map[string]Value)
	}
	vm.props[name] = value
	if len(vm.computeds) > 0 {
		vm.rebuildComputeds()
	}
}

// GetProp returns the value under name plus a presence flag. Mirrors
// SetProp so callers can snapshot prior state, mutate during a
// dispatch, and restore afterwards.
func (vm *VM) GetProp(name string) (Value, bool) {
	v, ok := vm.props[name]
	return v, ok
}

// DeleteProp removes name from the prop map. Used by callers
// restoring previously-absent slots after a temporary SetProp.
func (vm *VM) DeleteProp(name string) {
	delete(vm.props, name)
	if len(vm.computeds) > 0 {
		vm.rebuildComputeds()
	}
}

// PropMutation describes one slot in a bulk prop update. Delete removes the
// slot; otherwise Value replaces it. ApplyPropMutations is the preferred seam
// for event scopes that stage several related props because ordinary props
// are not reactive dependencies and the computed graph must be rebuilt after
// the complete state transition, not once per field.
type PropMutation struct {
	Name   string
	Value  Value
	Delete bool
}

// ApplyPropMutations changes every requested prop, then rebuilds declared
// computeds exactly once. Empty mutation sets are a no-op.
func (vm *VM) ApplyPropMutations(mutations []PropMutation) {
	if len(mutations) == 0 {
		return
	}
	if vm.props == nil {
		vm.props = make(map[string]Value)
	}
	for _, mutation := range mutations {
		if mutation.Delete {
			delete(vm.props, mutation.Name)
			continue
		}
		vm.props[mutation.Name] = mutation.Value
	}
	if len(vm.computeds) > 0 {
		vm.rebuildComputeds()
	}
}

// maxEvalDepth bounds Eval's own recursion. Program.MaxCallDepth
// (Y.D) only guards OpIndirectCall and closure dispatch. A
// self-referential or pathologically deep expression graph instead
// reaches Eval through ordinary operand recursion, with no call
// boundary at all. Examples include evalBinary, OpCond, and OpSeq.
// This path needs its own, independent cap.
//
// The cap is generous relative to the deepest legitimate
// call/expression nesting seen in the test suite. It still sits far
// below the depth that would exhaust the goroutine stack.
const maxEvalDepth = 10000

// Eval evaluates an expression by ID and returns its value. The VM keeps its
// panic-free contract: malformed programs produce zero values and structured
// diagnostics instead of panics.
func (vm *VM) Eval(id program.ExprID) Value {
	if int(id) >= len(vm.exprs) {
		vm.recordDiagnostic(Diagnostic{
			Severity: DiagnosticError,
			Code:     "expr_out_of_range",
			Message:  fmt.Sprintf("expression %d is outside the expression table length %d", id, len(vm.exprs)),
			ExprID:   diagnosticExprID(id),
		})
		return ZeroValue(program.TypeAny)
	}
	// evalDepth == 0 marks the start of a fresh top-level dispatch: a
	// call from EvalWithFrame, EvalTree, or a direct Eval by a host.
	// Reset the loop step budget here. This bounds the total OpFor /
	// OpForRange work for THIS dispatch, not for every dispatch the
	// VM ever runs.
	if vm.evalDepth == 0 {
		vm.loopSteps = 0
		vm.loopBudgetHit = false
	}
	if vm.evalDepth >= maxEvalDepth {
		vm.recordExprDiagnostic(
			"eval_depth_exceeded",
			fmt.Sprintf("Eval recursion exceeded depth cap %d; aborting to avoid a stack overflow", maxEvalDepth),
			vm.exprs[id].Op,
			vm.exprs[id].Value,
		)
		return ZeroValue(program.TypeAny)
	}
	// Numeric literals answer straight from the table built at program load.
	// A loop body re-evaluates the same literal once per iteration, so this
	// removes up to one strconv parse per iteration. Literals recurse into
	// nothing, so they need no depth charge.
	if int(id) < len(vm.lits) {
		if slot := vm.lits[id]; slot.kind != litNone {
			return vm.decodedLiteral(id, slot)
		}
	}
	vm.evalDepth++
	v := vm.evalExpr(&vm.exprs[id])
	vm.evalDepth--
	return v
}

// evalExpr routes one expression to its handler.
//
// Dispatch is a single switch on the opcode. It replaced a chain of nineteen
// "try this opcode family, else try the next" calls, which charged a late
// opcode up to eighteen extra calls and switches before it reached its own
// handler. The Expr arrives by pointer: the struct is 56 bytes, and the old
// chain copied it once per stage.
func (vm *VM) evalExpr(e *program.Expr) Value {
	switch e.Op {
	// --- literals ---
	case program.OpLitString:
		return StringVal(e.Value)
	case program.OpLitInt:
		return vm.parseIntLiteral(e)
	case program.OpLitFloat:
		return vm.parseFloatLiteral(e)
	case program.OpLitBool:
		return BoolVal(e.Value == "true")

	// --- props, signals, event data ---
	case program.OpPropGet:
		return vm.propValue(e.Value, e.Type)
	case program.OpSignalGet:
		return vm.signalValue(e.Value, e.Type)
	case program.OpSignalSet, program.OpSignalUpdate:
		return vm.updateSignal(e)
	case program.OpEventGet:
		return vm.eventValue(e.Value, e.Type)

	// --- two-operand arithmetic, comparison, boolean and string joins ---
	case program.OpAdd, program.OpSub, program.OpMul, program.OpDiv,
		program.OpMod, program.OpEq, program.OpNeq, program.OpLt,
		program.OpGt, program.OpLte, program.OpGte, program.OpAnd,
		program.OpOr, program.OpConcat:
		return vm.evalBinaryOp(e)

	// --- one-operand negation, conversion and string case/trim ---
	case program.OpNeg, program.OpNot, program.OpToUpper, program.OpToLower,
		program.OpTrim, program.OpToString, program.OpToInt, program.OpToFloat:
		return vm.evalUnaryOp(e)

	// --- formatting and control flow ---
	case program.OpFormat:
		return vm.formatValue(e)
	case program.OpCond:
		return vm.conditionalValue(e)
	case program.OpCall:
		return vm.callValue(e)

	// --- collections ---
	case program.OpIndex:
		return vm.indexValue(e)
	case program.OpLen:
		return vm.lenValue(e)
	case program.OpRange:
		return ZeroValue(program.TypeAny)
	case program.OpMap:
		return vm.mapValue(e)
	case program.OpFilter:
		return vm.filterValue(e)
	case program.OpFind:
		return vm.findValue(e)
	case program.OpSlice:
		return vm.sliceValue(e)
	case program.OpAppend:
		return vm.appendValue(e)
	case program.OpContains:
		return vm.containsValue(e)

	// --- string methods with extra operands ---
	case program.OpSplit:
		return vm.splitValue(e)
	case program.OpJoin:
		return vm.joinValue(e)
	case program.OpReplace:
		return vm.replaceValue(e)
	case program.OpSubstring:
		return vm.substringValue(e)
	case program.OpStartsWith:
		return vm.startsWithValue(e)
	case program.OpEndsWith:
		return vm.endsWithValue(e)
	case program.OpToRunes:
		return vm.toRunesValue(e)

	// --- statement sequencing and locals (Slice X.A) ---
	case program.OpSeq:
		return vm.seqValue(e)
	case program.OpAssign:
		return vm.assignValue(e)
	case program.OpLocalDecl:
		return vm.localDeclValue(e)
	case program.OpLocalGet:
		return vm.localGetValue(e)
	case program.OpLocalSet:
		return vm.localSetValue(e)

	// --- imperative iteration and control exits (Slice X.C) ---
	case program.OpFor:
		return vm.forValue(e)
	case program.OpForRange:
		return vm.forRangeValue(e)
	case program.OpReturn:
		return vm.returnValue(e)
	case program.OpBreak:
		return Value{}.WithControl(ControlBreak)
	case program.OpContinue:
		return Value{}.WithControl(ControlContinue)

	// --- composites, lookups, in-place writes, calls (Slices Y.A–Y.G) ---
	case program.OpComposite:
		return vm.compositeValue(e)
	case program.OpMapLookup:
		return vm.mapLookupValue(e)
	case program.OpFieldSet:
		return vm.fieldSetValue(e)
	case program.OpIndexSet:
		return vm.indexSetValue(e)
	case program.OpIndirectCall:
		return vm.indirectCallValue(e)
	case program.OpMake:
		return vm.makeValue(e)
	case program.OpHostCall:
		return vm.hostCallValue(e)
	case program.OpClosure:
		return vm.closureValue(e)
	}

	vm.recordExprDiagnostic(
		"unknown_opcode",
		fmt.Sprintf("unknown island VM opcode %d", e.Op),
		e.Op,
		e.Value,
	)
	return ZeroValue(program.TypeAny)
}

// evalCompositeExpr dispatches the Slice Y.A composite-literal opcode.
// OpComposite materializes a struct, slice, or map value from its
// operand pairs. The kind tag in Value selects the materialization
// strategy:
//
//   - "struct:<TypeName>" — ObjectVal whose Fields map is keyed by the
//     string-literal first-operand of each pair.
//   - "slice"             — ArrayVal whose Items list is the
//     second-operand of each pair in pair order (the index operand is
//     informational; the lowerer emits 0..len-1 to keep the encoding
//     uniform with the struct/map cases).
//   - "map"               — ObjectVal keyed by each pair's first operand
//     evaluated and stringified.
//
// Unknown kind tags record an "invalid_composite" diagnostic and fall
// back to the zero Any value so the VM's panic-free contract holds.
func (vm *VM) compositeValue(e *program.Expr) Value {
	if len(e.Operands)%2 != 0 {
		vm.recordExprDiagnostic(
			"invalid_composite",
			fmt.Sprintf("OpComposite %q requires an even operand count (key/value pairs), got %d", e.Value, len(e.Operands)),
			e.Op,
			e.Value,
		)
		return ZeroValue(program.TypeAny)
	}
	switch {
	case e.Value == "slice":
		return vm.compositeSlice(e)
	case e.Value == "map":
		return vm.compositeMap(e)
	case len(e.Value) >= 7 && e.Value[:7] == "struct:":
		return vm.compositeStruct(e)
	default:
		vm.recordExprDiagnostic(
			"invalid_composite",
			fmt.Sprintf("OpComposite has unknown kind tag %q (want struct:<Name>, slice, or map)", e.Value),
			e.Op,
			e.Value,
		)
		return ZeroValue(program.TypeAny)
	}
}

// compositeStruct materializes a struct Value from interleaved
// (keyExpr, valueExpr) operand pairs. Keys must evaluate to strings —
// the lowerer always emits OpLitString for them, so this is a near-
// noop string read at runtime.
func (vm *VM) compositeStruct(e *program.Expr) Value {
	fields := make(map[string]Value, len(e.Operands)/2)
	for i := 0; i < len(e.Operands); i += 2 {
		key := vm.Eval(e.Operands[i]).String()
		fields[key] = vm.Eval(e.Operands[i+1])
	}
	return ObjectVal(fields)
}

// compositeSlice materializes an array Value from the value half of
// each (indexExpr, valueExpr) operand pair. The index operand is
// evaluated for side effects but its result is discarded — items
// land in the slice in pair order.
func (vm *VM) compositeSlice(e *program.Expr) Value {
	items := make([]Value, 0, len(e.Operands)/2)
	for i := 0; i < len(e.Operands); i += 2 {
		// Evaluate the index expr for any side effects (typically a literal).
		vm.Eval(e.Operands[i])
		items = append(items, vm.Eval(e.Operands[i+1]))
	}
	return ArrayVal(items)
}

// compositeMap materializes a map Value whose Fields keys are each
// pair's evaluated key (stringified through Value.String). Duplicate
// keys are last-wins, matching Go's map literal evaluation order.
func (vm *VM) compositeMap(e *program.Expr) Value {
	fields := make(map[string]Value, len(e.Operands)/2)
	for i := 0; i < len(e.Operands); i += 2 {
		key := vm.Eval(e.Operands[i]).String()
		fields[key] = vm.Eval(e.Operands[i+1])
	}
	return ObjectVal(fields)
}

// mapLookupValue evaluates the Slice Y.B two-value map lookup
// opcode. OpMapLookup mirrors Go's comma-ok form (`v, ok := m[k]`) by
// returning an ObjectVal with "value" and "ok" fields so the lowerer
// can extract each binding via two OpIndex reads against the result.
//
// Per Y.A's deferred decision point (Tuple vs Object carrier), the
// ObjectVal route was chosen: it reuses Value.Fields machinery without
// touching equality, String, JSON, or any of the formatters that would
// have to learn a new Kind. Y.B exit report documents the trade.
//
// The lookup honors map presence semantics:
//   - key present  → {"value": <stored>, "ok": true}
//   - key absent   → {"value": <zero Any>, "ok": false}
//   - non-map LHS  → {"value": <zero Any>, "ok": false} + diagnostic
func (vm *VM) mapLookupValue(e *program.Expr) Value {
	if !vm.requireOperands(e, 2) {
		return ObjectVal(map[string]Value{
			"value": ZeroValue(program.TypeAny),
			"ok":    BoolVal(false),
		})
	}
	coll := vm.Eval(e.Operands[0])
	key := vm.Eval(e.Operands[1]).String()
	if !coll.isMap() {
		// Non-map collection — diagnose and yield the zero/false pair so
		// downstream OpIndex reads still resolve to safe defaults.
		vm.recordExprDiagnostic(
			"map_lookup_non_map",
			fmt.Sprintf("OpMapLookup target has no Fields map (Value type %d)", coll.Type),
			e.Op,
			e.Value,
		)
		return ObjectVal(map[string]Value{
			"value": ZeroValue(program.TypeAny),
			"ok":    BoolVal(false),
		})
	}
	if got, ok := coll.dict()[key]; ok {
		return ObjectVal(map[string]Value{
			"value": got,
			"ok":    BoolVal(true),
		})
	}
	return ObjectVal(map[string]Value{
		"value": ZeroValue(program.TypeAny),
		"ok":    BoolVal(false),
	})
}

// returnValue evaluates Operands[0] (or yields zero when absent) and
// marks the result with ControlReturn so OpSeq and EvalWithFrame can
// unwind to the handler boundary.
func (vm *VM) returnValue(e *program.Expr) Value {
	var payload Value
	if len(e.Operands) > 0 {
		payload = vm.Eval(e.Operands[0])
	}
	return payload.WithControl(ControlReturn)
}

// seqValue evaluates each operand in order and returns the last one's
// value. An empty OpSeq is harmless — it produces the zero Value of
// TypeAny so handler bodies can be no-ops without a missing-operand
// diagnostic.
//
// If any operand returns a Control signal (return / break / continue
// from X.C), evaluation stops and the signal propagates up; the
// enclosing loop or EvalWithFrame is responsible for catching it.
func (vm *VM) seqValue(e *program.Expr) Value {
	if len(e.Operands) == 0 {
		return ZeroValue(program.TypeAny)
	}
	var last Value
	for _, op := range e.Operands {
		last = vm.Eval(op)
		if last.Control() != ControlNone {
			return last
		}
	}
	return last
}

// assignValue writes the value expression to the target named in Value.
// Targets resolve in this order:
//  1. registered signal — same effect as OpSignalSet.
//  2. local declared in the current frame — same effect as OpLocalSet.
//  3. local declared on-demand in the current frame (treats OpAssign as
//     `:=` when the lowerer hasn't emitted a prior OpLocalDecl).
//  4. with no frame and no signal — diagnostic and zero return.
//
// Returns the assigned value so OpSeq sequences can chain assignments.
func (vm *VM) assignValue(e *program.Expr) Value {
	if !vm.requireOperands(e, 1) {
		return ZeroValue(program.TypeAny)
	}
	value := vm.Eval(e.Operands[0])
	if _, ok := vm.signals[e.Value]; ok {
		vm.signals[e.Value].Set(value)
		return value
	}
	if vm.frame == nil {
		vm.recordExprDiagnostic(
			"missing_frame",
			fmt.Sprintf("OpAssign target %q resolves to neither a signal nor a local (no frame active)", e.Value),
			e.Op,
			e.Value,
		)
		return ZeroValue(program.TypeAny)
	}
	vm.frame.set(e.Value, value)
	return value
}

// localDeclValue reserves a slot in the current frame. Re-declarations
// are no-ops so the lowerer can emit OpLocalDecl idempotently. Returns
// the zero Value of TypeAny.
func (vm *VM) localDeclValue(e *program.Expr) Value {
	if vm.frame == nil {
		vm.recordExprDiagnostic(
			"missing_frame",
			fmt.Sprintf("OpLocalDecl %q evaluated without an active frame", e.Value),
			e.Op,
			e.Value,
		)
		return ZeroValue(program.TypeAny)
	}
	vm.frame.declare(e.Value)
	return ZeroValue(program.TypeAny)
}

// localGetValue returns the value of a declared local. If no frame is
// active or the name isn't a local, it falls back to signals then to
// props. This three-tier lookup lets the X.C lowerer emit OpLocalGet
// for every bare identifier without knowing in advance whether the
// name refers to a function local, a package var (signal), or a
// handler parameter (prop) — runtime resolution is one map lookup
// per tier and is correct for all three cases.
//
// Only when none of the tiers contain the name does the VM record a
// missing_local diagnostic and return the zero value.
func (vm *VM) localGetValue(e *program.Expr) Value {
	if v, ok := vm.frame.get(e.Value); ok {
		return v
	}
	if sig, ok := vm.signals[e.Value]; ok {
		return sig.Get()
	}
	if computed, ok := vm.computeds[e.Value]; ok {
		return computed.Get()
	}
	if v, ok := vm.props[e.Value]; ok {
		return v
	}
	vm.recordExprDiagnostic(
		"missing_local",
		fmt.Sprintf("identifier %q is not declared as a local, signal, or prop", e.Value),
		e.Op,
		e.Value,
	)
	return ZeroValue(program.TypeAny)
}

// localSetValue writes Operands[0] to the local named in Value. Unlike
// OpAssign, OpLocalSet never falls through to signals; the lowerer
// emits it only when the target is known to be a local.
func (vm *VM) localSetValue(e *program.Expr) Value {
	if !vm.requireOperands(e, 1) {
		return ZeroValue(program.TypeAny)
	}
	if vm.frame == nil {
		vm.recordExprDiagnostic(
			"missing_frame",
			fmt.Sprintf("OpLocalSet %q evaluated without an active frame", e.Value),
			e.Op,
			e.Value,
		)
		return ZeroValue(program.TypeAny)
	}
	value := vm.Eval(e.Operands[0])
	vm.frame.set(e.Value, value)
	return value
}

// EvalWithFrame evaluates the given expression with a fresh locals
// frame, restoring any previous frame after the evaluation completes.
// This is the entry point for handler-body evaluation (Slice X.A): a
// handler that uses OpLocalDecl / OpAssign must be evaluated through
// this method so the locals table is set up.
//
// A ControlReturn signal from inside the frame is consumed here — the
// caller observes the wrapped value with Control reset to None, so
// return semantics terminate at the handler boundary rather than
// leaking out into surrounding evaluation.
func (vm *VM) EvalWithFrame(id program.ExprID) Value {
	prev := vm.frame
	vm.frame = newFrame()
	defer func() { vm.frame = prev }()
	v := vm.Eval(id)
	if v.Control() == ControlReturn {
		return v.WithControl(ControlNone)
	}
	return v
}

// litKind tags a slot in the pre-decoded numeric literal table.
type litKind uint8

const (
	litNone  litKind = iota // the expression is not a numeric literal
	litInt                  // OpLitInt whose text parsed
	litFloat                // OpLitFloat whose text parsed
	litBad                  // numeric literal whose text does not parse
)

// litSlot caches the decoded form of one OpLitInt or OpLitFloat expression.
type litSlot struct {
	num  float64
	kind litKind
}

// buildLiteralTable decodes every numeric literal in the expression table once,
// at program load.
//
// A tree-walking VM re-evaluates the same literal expression on every visit, so
// a loop bounded at 1<<20 paid up to a million strconv calls for one literal.
// The table is indexed by ExprID and stops after the last numeric literal, so a
// program with none allocates nothing.
func buildLiteralTable(exprs []program.Expr) []litSlot {
	last := -1
	for i := range exprs {
		switch exprs[i].Op {
		case program.OpLitInt, program.OpLitFloat:
			last = i
		}
	}
	if last < 0 {
		return nil
	}
	table := make([]litSlot, last+1)
	for i := 0; i <= last; i++ {
		switch exprs[i].Op {
		case program.OpLitInt:
			n, err := strconv.ParseInt(exprs[i].Value, 10, 64)
			if err != nil {
				table[i] = litSlot{kind: litBad}
				continue
			}
			// Match IntVal(int(n)) exactly, including its narrowing.
			table[i] = litSlot{num: float64(int(n)), kind: litInt}
		case program.OpLitFloat:
			f, err := strconv.ParseFloat(exprs[i].Value, 64)
			if err != nil {
				table[i] = litSlot{kind: litBad}
				continue
			}
			table[i] = litSlot{num: f, kind: litFloat}
		}
	}
	return table
}

// decodedLiteral turns a pre-decoded slot into a Value. A slot that failed to
// parse falls back to the parsing path, which records the diagnostic naming the
// offending text.
func (vm *VM) decodedLiteral(id program.ExprID, slot litSlot) Value {
	switch slot.kind {
	case litInt:
		return Value{Type: program.TypeInt, num: slot.num}
	case litFloat:
		return Value{Type: program.TypeFloat, num: slot.num}
	}
	e := &vm.exprs[id]
	if e.Op == program.OpLitFloat {
		return vm.parseFloatLiteral(e)
	}
	return vm.parseIntLiteral(e)
}

// parseIntLiteral decodes an OpLitInt from its text. Eval normally answers from
// the pre-decoded table; this path serves synthetic expressions that carry no
// ExprID and literals whose text is invalid.
func (vm *VM) parseIntLiteral(e *program.Expr) Value {
	n, err := strconv.ParseInt(e.Value, 10, 64)
	if err != nil {
		vm.recordExprDiagnostic("invalid_int_literal", fmt.Sprintf("invalid integer literal %q: %v", e.Value, err), e.Op, e.Value)
		return ZeroValue(program.TypeInt)
	}
	return IntVal(int(n))
}

// parseFloatLiteral decodes an OpLitFloat from its text. See parseIntLiteral.
func (vm *VM) parseFloatLiteral(e *program.Expr) Value {
	f, err := strconv.ParseFloat(e.Value, 64)
	if err != nil {
		vm.recordExprDiagnostic("invalid_float_literal", fmt.Sprintf("invalid float literal %q: %v", e.Value, err), e.Op, e.Value)
		return ZeroValue(program.TypeFloat)
	}
	return FloatVal(f)
}

// evalBinaryOp evaluates both operands once, then applies the opcode.
//
// The previous shape passed a method value (Value.Add and friends) into a
// generic helper, which forced an indirect call and an extra Value copy on
// every arithmetic step.
func (vm *VM) evalBinaryOp(e *program.Expr) Value {
	if !vm.requireOperands(e, 2) {
		return ZeroValue(program.TypeAny)
	}
	left := vm.Eval(e.Operands[0])
	right := vm.Eval(e.Operands[1])
	switch e.Op {
	case program.OpAdd:
		return left.Add(right)
	case program.OpSub:
		return left.Sub(right)
	case program.OpMul:
		return left.Mul(right)
	case program.OpDiv:
		return left.Div(right)
	case program.OpMod:
		return left.Mod(right)
	case program.OpEq:
		return left.Eq(right)
	case program.OpNeq:
		return left.Neq(right)
	case program.OpLt:
		return left.Lt(right)
	case program.OpGt:
		return left.Gt(right)
	case program.OpLte:
		return left.Lte(right)
	case program.OpGte:
		return left.Gte(right)
	case program.OpAnd:
		return left.And(right)
	case program.OpOr:
		return left.Or(right)
	default: // program.OpConcat
		return left.Concat(right)
	}
}

// evalUnaryOp evaluates the single operand once, then applies the opcode. A
// missing operand yields the zero value the opcode's result type expects.
func (vm *VM) evalUnaryOp(e *program.Expr) Value {
	if !vm.requireOperands(e, 1) {
		return unaryFallback(e.Op)
	}
	operand := vm.Eval(e.Operands[0])
	switch e.Op {
	case program.OpNeg:
		return operand.Neg()
	case program.OpNot:
		return operand.Not()
	case program.OpToUpper:
		return operand.ToUpper()
	case program.OpToLower:
		return operand.ToLower()
	case program.OpTrim:
		return operand.TrimVal()
	case program.OpToString:
		return operand.ToStringVal()
	case program.OpToInt:
		return operand.ToIntVal()
	default: // program.OpToFloat
		return operand.ToFloatVal()
	}
}

// unaryFallback returns the value a one-operand opcode yields when its operand
// is missing.
func unaryFallback(op program.OpCode) Value {
	switch op {
	case program.OpNeg:
		return ZeroValue(program.TypeInt)
	case program.OpNot:
		return ZeroValue(program.TypeBool)
	case program.OpToInt:
		return IntVal(0)
	case program.OpToFloat:
		return FloatVal(0)
	default: // OpToUpper, OpToLower, OpTrim, OpToString
		return StringVal("")
	}
}

// callValue evaluates OpCall.
//
// Slice X.B: OpCall first tries the stdlib intrinsic registry (math.Sin,
// strings.Split, ...). Unknown callee names fall back to the zero Value so
// legacy programs that never registered an intrinsic keep evaluating
// identically.
//
// sort.Slice takes a dedicated path because its comparator operand is a body
// expression that must be re-evaluated for each comparison, not pre-evaluated
// once.
func (vm *VM) callValue(e *program.Expr) Value {
	if e.Value == "sort.Slice" {
		return vm.sortSliceValue(e)
	}
	if v, ok := vm.callIntrinsic(e); ok {
		return v
	}
	return ZeroValue(program.TypeAny)
}

// toRunesValue evaluates OpToRunes.
//
// Slice Y.E.3: `[]rune(s)` / `[]byte(s)` — convert a string into an ArrayVal
// whose Items are one-rune StringVals. Reading len() returns the rune count;
// slicing returns a rune subsequence; OpToString concatenates back to a string
// via the ToStringVal join path.
func (vm *VM) toRunesValue(e *program.Expr) Value {
	if !vm.requireOperands(e, 1) {
		return ArrayVal(nil)
	}
	src := vm.Eval(e.Operands[0]).Text()
	items := make([]Value, 0, len(src))
	for _, r := range src {
		items = append(items, StringVal(string(r)))
	}
	return ArrayVal(items)
}

func (vm *VM) propValue(name string, typ program.ExprType) Value {
	if v, ok := vm.props[name]; ok {
		return v
	}
	return ZeroValue(typ)
}

func (vm *VM) signalValue(name string, typ program.ExprType) Value {
	if sig, ok := vm.signals[name]; ok {
		return sig.Get()
	}
	if computed, ok := vm.computeds[name]; ok {
		return computed.Get()
	}
	return ZeroValue(typ)
}

func (vm *VM) updateSignal(e *program.Expr) Value {
	if !vm.requireOperands(e, 1) {
		return ZeroValue(program.TypeAny)
	}
	if sig, ok := vm.signals[e.Value]; ok {
		sig.Set(vm.Eval(e.Operands[0]))
	} else {
		vm.recordExprDiagnostic("missing_signal", fmt.Sprintf("signal %q is not registered", e.Value), e.Op, e.Value)
	}
	return ZeroValue(program.TypeAny)
}

func (vm *VM) eventValue(name string, typ program.ExprType) Value {
	if vm.eventData != nil {
		if v, ok := vm.eventData[name]; ok {
			// JSON has only one number type. parseAnyValue keeps integral
			// numbers as IntVal to preserve ordinary payload values, but known
			// event fields have a static VM type. Promote or narrow those numeric
			// values here so arithmetic follows the lowered field contract (for
			// example clientX=13 divided by 2 must be 6.5, not integer 6).
			switch typ {
			case program.TypeFloat:
				return v.ToFloatVal()
			case program.TypeInt:
				return v.ToIntVal()
			case program.TypeString:
				return v.ToStringVal()
			default:
				return v
			}
		}
	}
	return ZeroValue(typ)
}

func (vm *VM) formatValue(e *program.Expr) Value {
	result := e.Value
	for _, op := range e.Operands {
		result += vm.Eval(op).String()
	}
	return StringVal(result)
}

func (vm *VM) conditionalValue(e *program.Expr) Value {
	if !vm.requireOperands(e, 3) {
		return ZeroValue(program.TypeAny)
	}
	if vm.Eval(e.Operands[0]).Truth() {
		return vm.Eval(e.Operands[1])
	}
	return vm.Eval(e.Operands[2])
}

func (vm *VM) indexValue(e *program.Expr) Value {
	if vm.requireOperands(e, 2) {
		return vm.Eval(e.Operands[0]).IndexVal(vm.Eval(e.Operands[1]))
	}
	return ZeroValue(program.TypeAny)
}

func (vm *VM) lenValue(e *program.Expr) Value {
	if vm.requireOperands(e, 1) {
		return IntVal(vm.Eval(e.Operands[0]).Len())
	}
	return IntVal(0)
}

func (vm *VM) mapValue(e *program.Expr) Value {
	if !vm.requireOperands(e, 2) {
		return ArrayVal(nil)
	}
	coll := vm.Eval(e.Operands[0])
	return ArrayVal(vm.mapItems(coll.list(), e.Operands[1]))
}

func (vm *VM) filterValue(e *program.Expr) Value {
	if !vm.requireOperands(e, 2) {
		return ArrayVal(nil)
	}
	coll := vm.Eval(e.Operands[0])
	return ArrayVal(vm.filterItems(coll.list(), e.Operands[1]))
}

func (vm *VM) findValue(e *program.Expr) Value {
	if !vm.requireOperands(e, 2) {
		return ZeroValue(program.TypeAny)
	}
	coll := vm.Eval(e.Operands[0])
	if found, ok := vm.findItem(coll.list(), e.Operands[1]); ok {
		return found
	}
	return ZeroValue(program.TypeAny)
}

// safeBoundInt converts a slice/substring bound's float64 field to
// an int. It never lets NaN or +/-Inf produce a huge or negative
// int.
//
// Go's float-to-int conversion is implementation-defined once the
// value is outside the target range. In practice, NaN and Inf both
// land on math.MinInt64 on amd64. So a literal like 9e999 (which
// parses as +Inf) would otherwise reach SliceVal / SubstringVal as
// MinInt64.
//
// SliceVal and SubstringVal both clamp into [0, n] on their own now.
// Clamping the cast here too keeps any future caller of this
// conversion safe by construction, not just by convention.
func safeBoundInt(f float64) int {
	if math.IsNaN(f) {
		return 0
	}
	if f > math.MaxInt32 {
		return math.MaxInt32
	}
	if f < math.MinInt32 {
		return math.MinInt32
	}
	return int(f)
}

func (vm *VM) sliceValue(e *program.Expr) Value {
	if vm.requireOperands(e, 3) {
		coll := vm.Eval(e.Operands[0])
		start := safeBoundInt(vm.Eval(e.Operands[1]).num)
		end := safeBoundInt(vm.Eval(e.Operands[2]).num)
		// Slice Y.E.3: OpSlice now dispatches on the runtime collection
		// kind so the lowerer's *ast.SliceExpr handler can emit a single
		// opcode without knowing whether the source operand is a slice
		// or a string. String operands route through SubstringVal;
		// rune-array operands (produced by Y.E's `[]rune(s)` cast)
		// route through the existing SliceVal path because they carry
		// Items, not Str.
		if !coll.isList() && coll.text() != "" {
			return coll.SubstringVal(start, end)
		}
		return coll.SliceVal(start, end)
	}
	return ArrayVal(nil)
}

func (vm *VM) appendValue(e *program.Expr) Value {
	if vm.requireOperands(e, 2) {
		return vm.Eval(e.Operands[0]).AppendVal(vm.Eval(e.Operands[1]))
	}
	return ArrayVal(nil)
}

func (vm *VM) containsValue(e *program.Expr) Value {
	if vm.requireOperands(e, 2) {
		return vm.Eval(e.Operands[0]).ContainsVal(vm.Eval(e.Operands[1]))
	}
	return BoolVal(false)
}

func (vm *VM) splitValue(e *program.Expr) Value {
	if !vm.requireOperands(e, 1) {
		return ArrayVal(nil)
	}
	return vm.Eval(e.Operands[0]).SplitVal(vm.separatorValue(e))
}

func (vm *VM) joinValue(e *program.Expr) Value {
	if !vm.requireOperands(e, 1) {
		return StringVal("")
	}
	return vm.Eval(e.Operands[0]).JoinVal(vm.separatorValue(e))
}

func (vm *VM) separatorValue(e *program.Expr) string {
	sep := e.Value
	if len(e.Operands) >= 2 {
		sep = vm.Eval(e.Operands[1]).String()
	}
	return sep
}

func (vm *VM) replaceValue(e *program.Expr) Value {
	if vm.requireOperands(e, 3) {
		return vm.Eval(e.Operands[0]).ReplaceVal(vm.Eval(e.Operands[1]).Text(), vm.Eval(e.Operands[2]).Text())
	}
	return StringVal("")
}

func (vm *VM) substringValue(e *program.Expr) Value {
	if vm.requireOperands(e, 3) {
		coll := vm.Eval(e.Operands[0])
		start := safeBoundInt(vm.Eval(e.Operands[1]).num)
		end := safeBoundInt(vm.Eval(e.Operands[2]).num)
		return coll.SubstringVal(start, end)
	}
	return StringVal("")
}

func (vm *VM) startsWithValue(e *program.Expr) Value {
	if vm.requireOperands(e, 2) {
		return vm.Eval(e.Operands[0]).StartsWithVal(vm.Eval(e.Operands[1]))
	}
	return BoolVal(false)
}

func (vm *VM) endsWithValue(e *program.Expr) Value {
	if vm.requireOperands(e, 2) {
		return vm.Eval(e.Operands[0]).EndsWithVal(vm.Eval(e.Operands[1]))
	}
	return BoolVal(false)
}

func (vm *VM) mapItems(items []Value, exprID program.ExprID) []Value {
	result := make([]Value, len(items))
	restore := vm.captureProps([]string{"_item", "_index"})
	defer vm.restoreProps(restore)
	for i, item := range items {
		vm.props["_item"] = item
		vm.props["_index"] = IntVal(i)
		result[i] = vm.Eval(exprID)
	}
	return result
}

func (vm *VM) filterItems(items []Value, exprID program.ExprID) []Value {
	result := make([]Value, 0, len(items))
	restore := vm.captureProps([]string{"_item", "_index"})
	defer vm.restoreProps(restore)
	for i, item := range items {
		vm.props["_item"] = item
		vm.props["_index"] = IntVal(i)
		if vm.Eval(exprID).Truth() {
			result = append(result, item)
		}
	}
	return result
}

func (vm *VM) findItem(items []Value, exprID program.ExprID) (Value, bool) {
	restore := vm.captureProps([]string{"_item", "_index"})
	defer vm.restoreProps(restore)
	for i, item := range items {
		vm.props["_item"] = item
		vm.props["_index"] = IntVal(i)
		if vm.Eval(exprID).Truth() {
			return item, true
		}
	}
	return Value{}, false
}

// EvalTree walks the island's node tree, evaluating all dynamic expressions,
// and returns a resolved node tree for reconciliation.
//
// Pre-sizes tree.Nodes to len(program.Nodes) because in the common case
// each program node resolves 1:1 to a resolved node. forEach / fragment
// expansion can push the count higher — the append grow path handles
// that — but pre-sizing eliminates 3-4 doublings for a small counter or
// form-sized island.
func (vm *VM) EvalTree() *ResolvedTree {
	tree := &ResolvedTree{
		Nodes: make([]ResolvedNode, 0, len(vm.program.Nodes)),
	}
	vm.appendNodeRefs(tree, nil, vm.program.Root)
	return tree
}

// EvalTreeInto is EvalTree against a caller-owned tree. It exists so a live
// island can alternate between two trees instead of allocating one per event.
// The tree is emptied first, and its node array is kept when it is already
// large enough.
//
// The caller must not pass the tree the reuse context reads from. Subtree
// reuse copies nodes out of the previous tree into this one, so the two must
// be distinct arrays. Island.nextTreeBuffer holds that invariant.
func (vm *VM) EvalTreeInto(tree *ResolvedTree) {
	if tree == nil {
		return
	}
	tree.reset(len(vm.program.Nodes))
	vm.appendNodeRefs(tree, nil, vm.program.Root)
	tree.releaseTail()
}

func (vm *VM) resolveNode(node program.Node) ResolvedNode {
	return vm.resolveNodeWithSource(-1, node)
}

func (vm *VM) resolveNodeWithSource(source int, node program.Node) ResolvedNode {
	rn := ResolvedNode{
		Source:    source,
		HasSource: source >= 0,
		Tag:       node.Tag,
	}

	switch node.Kind {
	case program.NodeText:
		rn.Text = node.Text
	case program.NodeExpr:
		rn.Text = vm.Eval(node.Expr).String()
	case program.NodeElement:
		vm.resolveElementNode(&rn, source, node)
	}

	return rn
}

// appendNodeRefs walks the program node at nodeID and appends the resolved
// indices into the caller-provided out slice. Uses the append-return-value
// pattern so callers can grow a shared buffer without paying the per-node
// []int{idx} allocation the previous implementation incurred.
func (vm *VM) appendNodeRefs(tree *ResolvedTree, out []int, nodeID program.NodeID) []int {
	if int(nodeID) >= len(vm.program.Nodes) {
		vm.recordDiagnostic(Diagnostic{
			Severity: DiagnosticError,
			Code:     "node_out_of_range",
			Message:  fmt.Sprintf("node %d is outside the node table length %d", nodeID, len(vm.program.Nodes)),
			NodeID:   diagnosticNodeID(nodeID),
		})
		return out
	}
	node := vm.program.Nodes[nodeID]
	switch node.Kind {
	case program.NodeFragment:
		for _, child := range node.Children {
			out = vm.appendNodeRefs(tree, out, child)
		}
		return out
	case program.NodeForEach:
		return vm.appendForEach(tree, out, int(nodeID), node)
	case program.NodeConditional:
		return vm.appendConditional(tree, out, int(nodeID), node)
	default:
		if idx, ok := vm.reuseResolvedNode(tree, int(nodeID)); ok {
			return append(out, idx)
		}
		idx := vm.appendResolvedNode(tree, int(nodeID), node)
		return append(out, idx)
	}
}

func (vm *VM) appendResolvedNode(tree *ResolvedTree, source int, node program.Node) int {
	idx := len(tree.Nodes)
	tree.Nodes = append(tree.Nodes, ResolvedNode{
		Source:    source,
		HasSource: true,
		Tag:       node.Tag,
	})

	switch node.Kind {
	case program.NodeText:
		tree.Nodes[idx].Text = node.Text
	case program.NodeExpr:
		tree.Nodes[idx].Text = vm.Eval(node.Expr).String()
	case program.NodeElement:
		vm.resolveElementNode(&tree.Nodes[idx], source, node)
		// resolveChildren recurses back into appendNodeRefs for every
		// descendant, and each one can append to tree.Nodes. An append
		// past capacity moves the backing array, so tree.Nodes[idx] must
		// be re-read after that call returns. Binding the result first
		// avoids storing through a stale pre-growth address under TinyGo.
		children := vm.resolveChildren(tree, node.Children)
		tree.Nodes[idx].Children = children
	}

	// The subtree is complete and contiguous now, so record where it
	// landed. The next walk copies it from here when nothing it reads has
	// changed. recordReuseSpan ignores nodes the plan cannot reuse.
	// A freshly evaluated subtree is not a copy, so it is never verbatim.
	vm.recordReuseSpan(source, idx, len(tree.Nodes), false)

	return idx
}

func (vm *VM) resolveChildren(tree *ResolvedTree, children []program.NodeID) []int {
	if len(children) == 0 {
		return nil
	}
	// Pre-size to len(children); fragments / forEach may expand but
	// 1:1 child resolution is by far the most common case. Any growth
	// beyond the initial capacity is absorbed by a single append regrow.
	resolved := make([]int, 0, len(children))
	for _, childID := range children {
		resolved = vm.appendNodeRefs(tree, resolved, childID)
	}
	return vm.mergeAdjacentText(tree, resolved)
}

// mergeAdjacentText collapses runs of consecutive resolved text nodes into a
// single text node, mirroring how a browser parses the server's contiguous
// HTML string: `text` + `{expr}` siblings, or the two whitespace runs left
// around an empty conditional, all become ONE DOM text node. Without this the
// client VDOM has more child nodes than the hydrated DOM, so reconcile patch
// paths drift (the bug behind conditional/list reactivity and the "count is 34"
// SetText append). Dropped members are left orphaned in tree.Nodes.
//
// Static identity matters: a static text node ("count is ") merged with a
// dynamic expr ("{n.Get()}") must be reconciled on every change, so the merged
// node adopts a DYNAMIC source whenever any absorbed member is dynamic —
// otherwise staticMask would skip it and the text would never update.
func (vm *VM) mergeAdjacentText(tree *ResolvedTree, indices []int) []int {
	if len(indices) < 2 {
		return indices
	}
	merged := make([]int, 0, len(indices))
	for _, idx := range indices {
		if len(merged) > 0 {
			prev := &tree.Nodes[merged[len(merged)-1]]
			cur := &tree.Nodes[idx]
			if isResolvedTextNode(prev) && isResolvedTextNode(cur) {
				prev.Text += cur.Text
				if vm.isDynamicSource(cur) && !vm.isDynamicSource(prev) {
					prev.Source = cur.Source
					prev.HasSource = cur.HasSource
				}
				continue
			}
		}
		merged = append(merged, idx)
	}
	return merged
}

// isDynamicSource reports whether a resolved node would be reconciled (not
// skipped by staticMask). A node with no source, or whose program source is not
// flagged static, is dynamic.
func (vm *VM) isDynamicSource(n *ResolvedNode) bool {
	if !n.HasSource || n.Source < 0 {
		return true
	}
	sm := vm.program.StaticMask
	return !(n.Source < len(sm) && sm[n.Source])
}

// isResolvedTextNode reports whether a resolved node is a text node (no tag, no
// children) — i.e. the resolution of a NodeText or NodeExpr. Element nodes carry
// a Tag; fragments/conditionals/forEach expand inline and never produce a node.
func isResolvedTextNode(n *ResolvedNode) bool {
	return n.Tag == "" && len(n.Children) == 0
}

func (vm *VM) resolveElementNode(rn *ResolvedNode, source int, node program.Node) {
	resolved, domAttrs, events, key, explicitKey := vm.resolveElementAttrs(node.Attrs)
	rn.Attrs = resolved
	rn.DOMAttrs = domAttrs
	rn.Key = key
	rn.Events = events
	if explicitKey {
		return
	}
	if autoKey, ok := vm.autoKey(source, node.Tag); ok {
		rn.Key = autoKey
	}
}

// resolveElementAttrs walks a program node's attribute list and returns:
//
//   - resolved: the non-event attrs (class/id/data-*/etc)
//   - domAttrs: everything the browser-side reconciler needs (static
//     attrs PLUS the synthesized data-gosx-on-* / data-gosx-handler
//     entries for each event)
//   - events: the list of (eventName, handler) pairs for renderResolvedAttrs
//   - key / explicitKey: the element's key attribute if any
//
// domAttrs and resolved share a single backing array — resolved is the
// prefix `domAttrs[:staticCount]` covering just the static attrs, and
// domAttrs extends past that with the synthesized event marker entries.
// This means elements with BOTH static attrs and events allocate a
// single []ResolvedAttr slice instead of the two the earlier fused
// implementation used. Elements with only statics (resolved == domAttrs)
// and elements with no attrs at all (both nil) stay the same.
func (vm *VM) resolveElementAttrs(attrs []program.Attr) (resolved, domAttrs []ResolvedAttr, events []ResolvedEvent, key string, explicitKey bool) {
	// Two-pass: first count what we need so we can allocate exactly once.
	// Counting is cheap (it's the same loop body minus the writes) and
	// lets us avoid the lazy-init branches the earlier version used.
	staticCount := 0
	eventCount := 0
	clickCount := 0
	for _, attr := range attrs {
		switch attr.Kind {
		case program.AttrStatic, program.AttrExpr:
			if attr.Name != "key" {
				staticCount++
			}
		case program.AttrBool:
			staticCount++
		case program.AttrEvent:
			eventCount++
			if eventAttrType(attr.Name) == "click" {
				clickCount++
			}
		}
	}

	totalDOMAttrs := staticCount + eventCount + clickCount
	if totalDOMAttrs > 0 {
		domAttrs = make([]ResolvedAttr, 0, totalDOMAttrs)
	}
	if eventCount > 0 {
		events = make([]ResolvedEvent, 0, eventCount)
	}

	for _, attr := range attrs {
		switch attr.Kind {
		case program.AttrStatic:
			if attr.Name == "key" {
				key = attr.Value
				explicitKey = true
				continue
			}
			domAttrs = append(domAttrs, ResolvedAttr{Name: attr.Name, Value: attr.Value})
		case program.AttrExpr:
			value := vm.Eval(attr.Expr)
			if attr.Name == "key" {
				key = value.String()
				explicitKey = true
				continue
			}
			if htmlattr.IsBoolean(attr.Name) && value.Type == program.TypeBool {
				if value.Truth() {
					domAttrs = append(domAttrs, ResolvedAttr{Name: attr.Name, Bool: true})
				}
				continue
			}
			domAttrs = append(domAttrs, ResolvedAttr{Name: attr.Name, Value: value.String()})
		case program.AttrBool:
			domAttrs = append(domAttrs, ResolvedAttr{Name: attr.Name, Bool: true})
		case program.AttrEvent:
			events = append(events, ResolvedEvent{Name: attr.Name, Handler: attr.Event})
		}
	}

	// resolved is a subslice of domAttrs covering just the static attrs.
	// Sharing the backing array means rn.Attrs reads the same memory as
	// the first `len(resolved)` entries of rn.DOMAttrs.
	if len(domAttrs) > 0 {
		resolvedCount := len(domAttrs)
		resolved = domAttrs[:resolvedCount:resolvedCount]
	}

	// Append the event markers after the static prefix.
	for _, event := range events {
		eventType := eventAttrType(event.Name)
		domAttrs = append(domAttrs, ResolvedAttr{
			Name:  eventMarkerAttr(eventType),
			Value: event.Handler,
		})
		if eventType == "click" {
			domAttrs = append(domAttrs, ResolvedAttr{
				Name:  "data-gosx-handler",
				Value: event.Handler,
			})
		}
	}
	return
}

func (vm *VM) autoKey(source int, tag string) (string, bool) {
	keyVal, hasKey := vm.props["_key"]
	if hasKey {
		return fmt.Sprintf("_auto_%s_%s_%d", keyVal.String(), tag, source), true
	}
	idxVal, hasIndex := vm.props["_index"]
	if !hasIndex {
		return "", false
	}
	return fmt.Sprintf("_auto_%d_%s_%d", int(idxVal.num), tag, source), true
}

type eachEntry struct {
	Index  int
	Key    Value
	Item   Value
	HasKey bool
}

func (vm *VM) appendForEach(tree *ResolvedTree, out []int, source int, node program.Node) []int {
	entries := valueEachEntries(vm.Eval(node.Expr))
	if len(entries) == 0 {
		return append(out, vm.resolveForEachFallback(tree, source, node.Attrs)...)
	}

	scope := resolveForEachScope(node.Attrs)
	restore := vm.captureProps(scope.propNames())
	defer vm.restoreProps(restore)

	for _, entry := range entries {
		vm.bindForEachEntry(scope, entry)
		out = vm.appendForEachChildren(out, tree, node.Children)
	}
	return out
}

func (vm *VM) appendConditional(tree *ResolvedTree, out []int, source int, node program.Node) []int {
	if valueTruthy(vm.Eval(node.Expr)) {
		for _, child := range node.Children {
			out = vm.appendNodeRefs(tree, out, child)
		}
		return out
	}
	return append(out, vm.resolveConditionalFallback(tree, source, node.Attrs)...)
}

func valueTruthy(value Value) bool {
	if value.isList() {
		return len(value.list()) > 0
	}
	if value.isMap() {
		return len(value.dict()) > 0
	}
	switch value.Type {
	case program.TypeBool:
		return value.truth()
	case program.TypeString:
		text := value.text()
		return text != "" && text != "0" && text != "false"
	case program.TypeInt, program.TypeFloat:
		return value.num != 0
	default:
		return value.truth() || value.num != 0 || value.text() != ""
	}
}

func valueEachEntries(value Value) []eachEntry {
	if value.isList() {
		return arrayEachEntries(value.list())
	}
	if value.isMap() {
		return objectEachEntries(value.dict())
	}
	return nil
}

func arrayEachEntries(items []Value) []eachEntry {
	out := make([]eachEntry, 0, len(items))
	for i, item := range items {
		out = append(out, eachEntry{
			Index:  i,
			Key:    IntVal(i),
			Item:   item,
			HasKey: true,
		})
	}
	return out
}

func objectEachEntries(fields map[string]Value) []eachEntry {
	keys := sortedEachFieldKeys(fields)
	out := make([]eachEntry, 0, len(keys))
	for i, key := range keys {
		out = append(out, eachEntry{
			Index:  i,
			Key:    StringVal(key),
			Item:   fields[key],
			HasKey: true,
		})
	}
	return out
}

func sortedEachFieldKeys(fields map[string]Value) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func resolveForEachScope(attrs []program.Attr) forEachScope {
	itemName := forEachStaticAttr(attrs, "as")
	if itemName == "" {
		itemName = "item"
	}
	return forEachScope{
		itemName:  itemName,
		indexName: forEachStaticAttr(attrs, "index"),
		keyName:   itemName + "Key",
	}
}

func (scope forEachScope) propNames() []string {
	names := []string{"_item", "_index", "_key", scope.itemName, scope.keyName}
	if scope.indexName != "" {
		names = append(names, scope.indexName)
	}
	return names
}

func (vm *VM) bindForEachEntry(scope forEachScope, entry eachEntry) {
	vm.props["_item"] = entry.Item
	vm.props["_index"] = IntVal(entry.Index)
	vm.props[scope.itemName] = entry.Item
	if scope.indexName != "" {
		vm.props[scope.indexName] = IntVal(entry.Index)
	}
	if entry.HasKey {
		vm.props["_key"] = entry.Key
		vm.props[scope.keyName] = entry.Key
		return
	}
	delete(vm.props, "_key")
	delete(vm.props, scope.keyName)
}

func (vm *VM) appendForEachChildren(out []int, tree *ResolvedTree, children []program.NodeID) []int {
	for _, child := range children {
		out = vm.appendNodeRefs(tree, out, child)
	}
	return out
}

func (vm *VM) resolveForEachFallback(tree *ResolvedTree, source int, attrs []program.Attr) []int {
	fallbackID, ok := fallbackExpr(attrs)
	if !ok {
		return nil
	}
	return vm.resolveFallbackText(tree, source, fallbackID)
}

func (vm *VM) resolveConditionalFallback(tree *ResolvedTree, source int, attrs []program.Attr) []int {
	fallbackID, ok := fallbackExpr(attrs)
	if !ok {
		return nil
	}
	return vm.resolveFallbackText(tree, source, fallbackID)
}

func (vm *VM) resolveFallbackText(tree *ResolvedTree, source int, fallbackID program.ExprID) []int {
	text := vm.Eval(fallbackID).String()
	if text == "" {
		return nil
	}
	idx := len(tree.Nodes)
	tree.Nodes = append(tree.Nodes, ResolvedNode{
		Source:    source,
		HasSource: true,
		Text:      text,
	})
	return []int{idx}
}

// captureProps records the state of every scoped name before a forEach binds
// it, so restoreProps can put the scope back exactly as it was.
//
// present records a name even when the name is ABSENT. That matters: a
// forEach binds `_item`, `_index`, `_key` and the `as`/`index` names, and
// most of those do not exist before the loop starts. Recording only the
// names that existed left the rest in vm.props after the loop returned, so
// the last iteration's index stayed visible to every later node. autoKey
// reads `_index`, so a plain element after a list picked up a key built from
// a loop it never belonged to.
func (vm *VM) captureProps(names []string) iterationContext {
	ctx := iterationContext{
		values:  make(map[string]Value, len(names)),
		present: make(map[string]bool, len(names)),
	}
	for _, name := range names {
		value, ok := vm.props[name]
		ctx.present[name] = ok
		if ok {
			ctx.values[name] = value
		}
	}
	return ctx
}

func (vm *VM) restoreProps(ctx iterationContext) {
	for name := range ctx.present {
		if ctx.present[name] {
			vm.props[name] = ctx.values[name]
			continue
		}
		delete(vm.props, name)
	}
}

func forEachStaticAttr(attrs []program.Attr, name string) string {
	for _, attr := range attrs {
		if attr.Kind == program.AttrStatic && attr.Name == name {
			return attr.Value
		}
	}
	return ""
}

func fallbackExpr(attrs []program.Attr) (program.ExprID, bool) {
	for _, attr := range attrs {
		if attr.Kind == program.AttrExpr && attr.Name == "fallback" {
			return attr.Expr, true
		}
	}
	return 0, false
}
