package vm

import (
	"fmt"
	"sort"
	"strconv"

	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// VM evaluates island expressions against props and signal state.
type VM struct {
	program        *program.Program
	props          map[string]Value
	signals        map[string]*signal.Signal[Value]
	exprs          []program.Expr
	eventData      map[string]string    // current event data (set during handler dispatch)
	frame          *frame               // locals table for the current handler evaluation (X.A)
	forCap         int                  // per-loop iteration cap (X.C); 0 → default
	funcs          map[string]*program.FuncDef // user-function registry (Y.D)
	callDepth      int                  // current OpIndirectCall recursion depth (Y.D)
	hosts          map[string]HostReceiver // per-VM host-receiver bindings for OpHostCall (Y.E)
	diagnostics    []Diagnostic
	diagnosticSink DiagnosticSink
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
func (vm *VM) SetEventData(data map[string]string) {
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
}

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
	return vm.evalExpr(vm.exprs[id])
}

func (vm *VM) evalExpr(e program.Expr) Value {
	if value, ok := vm.evalLiteralExpr(e); ok {
		return value
	}
	if value, ok := vm.evalAccessExpr(e); ok {
		return value
	}
	if value, ok := vm.evalArithmeticExpr(e); ok {
		return value
	}
	if value, ok := vm.evalComparisonExpr(e); ok {
		return value
	}
	if value, ok := vm.evalBooleanExpr(e); ok {
		return value
	}
	if value, ok := vm.evalStringExpr(e); ok {
		return value
	}
	if value, ok := vm.evalControlExpr(e); ok {
		return value
	}
	if value, ok := vm.evalCollectionExpr(e); ok {
		return value
	}
	if value, ok := vm.evalIterationExpr(e); ok {
		return value
	}
	if value, ok := vm.evalStringMethodExpr(e); ok {
		return value
	}
	if value, ok := vm.evalConversionExpr(e); ok {
		return value
	}
	if value, ok := vm.evalSequencingExpr(e); ok {
		return value
	}
	if value, ok := vm.evalCompositeExpr(e); ok {
		return value
	}
	if value, ok := vm.evalMapLookupExpr(e); ok {
		return value
	}
	if value, ok := vm.evalLHSSetExpr(e); ok {
		return value
	}
	if value, ok := vm.evalIndirectCallExpr(e); ok {
		return value
	}
	if value, ok := vm.evalMakeExpr(e); ok {
		return value
	}
	if value, ok := vm.evalHostCallExpr(e); ok {
		return value
	}
	if value, ok := vm.evalClosureExpr(e); ok {
		return value
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
func (vm *VM) evalCompositeExpr(e program.Expr) (Value, bool) {
	if e.Op != program.OpComposite {
		return Value{}, false
	}
	if len(e.Operands)%2 != 0 {
		vm.recordExprDiagnostic(
			"invalid_composite",
			fmt.Sprintf("OpComposite %q requires an even operand count (key/value pairs), got %d", e.Value, len(e.Operands)),
			e.Op,
			e.Value,
		)
		return ZeroValue(program.TypeAny), true
	}
	switch {
	case e.Value == "slice":
		return vm.compositeSlice(e), true
	case e.Value == "map":
		return vm.compositeMap(e), true
	case len(e.Value) >= 7 && e.Value[:7] == "struct:":
		return vm.compositeStruct(e), true
	default:
		vm.recordExprDiagnostic(
			"invalid_composite",
			fmt.Sprintf("OpComposite has unknown kind tag %q (want struct:<Name>, slice, or map)", e.Value),
			e.Op,
			e.Value,
		)
		return ZeroValue(program.TypeAny), true
	}
}

// compositeStruct materializes a struct Value from interleaved
// (keyExpr, valueExpr) operand pairs. Keys must evaluate to strings —
// the lowerer always emits OpLitString for them, so this is a near-
// noop string read at runtime.
func (vm *VM) compositeStruct(e program.Expr) Value {
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
func (vm *VM) compositeSlice(e program.Expr) Value {
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
func (vm *VM) compositeMap(e program.Expr) Value {
	fields := make(map[string]Value, len(e.Operands)/2)
	for i := 0; i < len(e.Operands); i += 2 {
		key := vm.Eval(e.Operands[i]).String()
		fields[key] = vm.Eval(e.Operands[i+1])
	}
	return ObjectVal(fields)
}

// evalMapLookupExpr dispatches the Slice Y.B two-value map lookup
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
func (vm *VM) evalMapLookupExpr(e program.Expr) (Value, bool) {
	if e.Op != program.OpMapLookup {
		return Value{}, false
	}
	if !vm.requireOperands(e, 2) {
		return ObjectVal(map[string]Value{
			"value": ZeroValue(program.TypeAny),
			"ok":    BoolVal(false),
		}), true
	}
	coll := vm.Eval(e.Operands[0])
	key := vm.Eval(e.Operands[1]).String()
	if coll.Fields == nil {
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
		}), true
	}
	if got, ok := coll.Fields[key]; ok {
		return ObjectVal(map[string]Value{
			"value": got,
			"ok":    BoolVal(true),
		}), true
	}
	return ObjectVal(map[string]Value{
		"value": ZeroValue(program.TypeAny),
		"ok":    BoolVal(false),
	}), true
}

// evalSequencingExpr dispatches the Slice X.A statement-sequencing opcodes:
// OpSeq, OpAssign, OpLocalDecl, OpLocalGet, OpLocalSet, plus the Slice X.C
// imperative iteration opcodes OpFor and OpForRange. These let a Program
// carry multi-statement handler bodies as a single Expr tree.
func (vm *VM) evalSequencingExpr(e program.Expr) (Value, bool) {
	switch e.Op {
	case program.OpSeq:
		return vm.seqValue(e), true
	case program.OpAssign:
		return vm.assignValue(e), true
	case program.OpLocalDecl:
		return vm.localDeclValue(e), true
	case program.OpLocalGet:
		return vm.localGetValue(e), true
	case program.OpLocalSet:
		return vm.localSetValue(e), true
	case program.OpFor:
		return vm.forValue(e), true
	case program.OpForRange:
		return vm.forRangeValue(e), true
	case program.OpReturn:
		return vm.returnValue(e), true
	case program.OpBreak:
		return Value{Control: ControlBreak}, true
	case program.OpContinue:
		return Value{Control: ControlContinue}, true
	default:
		return Value{}, false
	}
}

// returnValue evaluates Operands[0] (or yields zero when absent) and
// marks the result with ControlReturn so OpSeq and EvalWithFrame can
// unwind to the handler boundary.
func (vm *VM) returnValue(e program.Expr) Value {
	var payload Value
	if len(e.Operands) > 0 {
		payload = vm.Eval(e.Operands[0])
	}
	payload.Control = ControlReturn
	return payload
}

// seqValue evaluates each operand in order and returns the last one's
// value. An empty OpSeq is harmless — it produces the zero Value of
// TypeAny so handler bodies can be no-ops without a missing-operand
// diagnostic.
//
// If any operand returns a Control signal (return / break / continue
// from X.C), evaluation stops and the signal propagates up; the
// enclosing loop or EvalWithFrame is responsible for catching it.
func (vm *VM) seqValue(e program.Expr) Value {
	if len(e.Operands) == 0 {
		return ZeroValue(program.TypeAny)
	}
	var last Value
	for _, op := range e.Operands {
		last = vm.Eval(op)
		if last.Control != ControlNone {
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
func (vm *VM) assignValue(e program.Expr) Value {
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
func (vm *VM) localDeclValue(e program.Expr) Value {
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
func (vm *VM) localGetValue(e program.Expr) Value {
	if v, ok := vm.frame.get(e.Value); ok {
		return v
	}
	if sig, ok := vm.signals[e.Value]; ok {
		return sig.Get()
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
func (vm *VM) localSetValue(e program.Expr) Value {
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
	if v.Control == ControlReturn {
		v.Control = ControlNone
	}
	return v
}

func (vm *VM) evalLiteralExpr(e program.Expr) (Value, bool) {
	switch e.Op {
	case program.OpLitString:
		return StringVal(e.Value), true
	case program.OpLitInt:
		n, err := strconv.ParseInt(e.Value, 10, 64)
		if err != nil {
			vm.recordExprDiagnostic("invalid_int_literal", fmt.Sprintf("invalid integer literal %q: %v", e.Value, err), e.Op, e.Value)
			return ZeroValue(program.TypeInt), true
		}
		return IntVal(int(n)), true
	case program.OpLitFloat:
		f, err := strconv.ParseFloat(e.Value, 64)
		if err != nil {
			vm.recordExprDiagnostic("invalid_float_literal", fmt.Sprintf("invalid float literal %q: %v", e.Value, err), e.Op, e.Value)
			return ZeroValue(program.TypeFloat), true
		}
		return FloatVal(f), true
	case program.OpLitBool:
		return BoolVal(e.Value == "true"), true
	default:
		return Value{}, false
	}
}

func (vm *VM) evalAccessExpr(e program.Expr) (Value, bool) {
	switch e.Op {
	case program.OpPropGet:
		return vm.propValue(e.Value, e.Type), true
	case program.OpSignalGet:
		return vm.signalValue(e.Value, e.Type), true
	case program.OpSignalSet, program.OpSignalUpdate:
		return vm.updateSignal(e), true
	case program.OpEventGet:
		return vm.eventValue(e.Value), true
	default:
		return Value{}, false
	}
}

func (vm *VM) evalArithmeticExpr(e program.Expr) (Value, bool) {
	switch e.Op {
	case program.OpAdd:
		return vm.evalBinary(e, Value.Add), true
	case program.OpSub:
		return vm.evalBinary(e, Value.Sub), true
	case program.OpMul:
		return vm.evalBinary(e, Value.Mul), true
	case program.OpDiv:
		return vm.evalBinary(e, Value.Div), true
	case program.OpMod:
		return vm.evalBinary(e, Value.Mod), true
	case program.OpNeg:
		return vm.evalUnary(e, Value.Neg, program.TypeInt), true
	default:
		return Value{}, false
	}
}

func (vm *VM) evalComparisonExpr(e program.Expr) (Value, bool) {
	switch e.Op {
	case program.OpEq:
		return vm.evalBinary(e, Value.Eq), true
	case program.OpNeq:
		return vm.evalBinary(e, Value.Neq), true
	case program.OpLt:
		return vm.evalBinary(e, Value.Lt), true
	case program.OpGt:
		return vm.evalBinary(e, Value.Gt), true
	case program.OpLte:
		return vm.evalBinary(e, Value.Lte), true
	case program.OpGte:
		return vm.evalBinary(e, Value.Gte), true
	default:
		return Value{}, false
	}
}

func (vm *VM) evalBooleanExpr(e program.Expr) (Value, bool) {
	switch e.Op {
	case program.OpAnd:
		return vm.evalBinary(e, Value.And), true
	case program.OpOr:
		return vm.evalBinary(e, Value.Or), true
	case program.OpNot:
		return vm.evalUnary(e, Value.Not, program.TypeBool), true
	default:
		return Value{}, false
	}
}

func (vm *VM) evalStringExpr(e program.Expr) (Value, bool) {
	switch e.Op {
	case program.OpConcat:
		return vm.evalBinary(e, Value.Concat), true
	case program.OpFormat:
		return vm.formatValue(e), true
	default:
		return Value{}, false
	}
}

func (vm *VM) evalControlExpr(e program.Expr) (Value, bool) {
	switch e.Op {
	case program.OpCond:
		return vm.conditionalValue(e), true
	case program.OpCall:
		// Slice X.B: OpCall first tries the stdlib intrinsic registry
		// (math.Sin, strings.Split, ...). Unknown callee names fall back
		// to the existing zero-Value behavior so legacy programs that
		// never registered an intrinsic keep evaluating identically.
		//
		// sort.Slice is dispatched via a dedicated path because its
		// comparator operand is a body expression that must be
		// re-evaluated for each comparison, not pre-evaluated once.
		if e.Value == "sort.Slice" {
			return vm.sortSliceValue(e), true
		}
		if v, ok := vm.callIntrinsic(e); ok {
			return v, true
		}
		return ZeroValue(program.TypeAny), true
	default:
		return Value{}, false
	}
}

func (vm *VM) evalCollectionExpr(e program.Expr) (Value, bool) {
	switch e.Op {
	case program.OpIndex:
		return vm.indexValue(e), true
	case program.OpLen:
		return vm.lenValue(e), true
	case program.OpRange:
		return ZeroValue(program.TypeAny), true
	default:
		return Value{}, false
	}
}

func (vm *VM) evalIterationExpr(e program.Expr) (Value, bool) {
	switch e.Op {
	case program.OpMap:
		return vm.mapValue(e), true
	case program.OpFilter:
		return vm.filterValue(e), true
	case program.OpFind:
		return vm.findValue(e), true
	case program.OpSlice:
		return vm.sliceValue(e), true
	case program.OpAppend:
		return vm.appendValue(e), true
	case program.OpContains:
		return vm.containsValue(e), true
	default:
		return Value{}, false
	}
}

func (vm *VM) evalStringMethodExpr(e program.Expr) (Value, bool) {
	switch e.Op {
	case program.OpToUpper:
		return vm.stringUnary(e, Value.ToUpper, StringVal("")), true
	case program.OpToLower:
		return vm.stringUnary(e, Value.ToLower, StringVal("")), true
	case program.OpTrim:
		return vm.stringUnary(e, Value.TrimVal, StringVal("")), true
	case program.OpSplit:
		return vm.splitValue(e), true
	case program.OpJoin:
		return vm.joinValue(e), true
	case program.OpReplace:
		return vm.replaceValue(e), true
	case program.OpSubstring:
		return vm.substringValue(e), true
	case program.OpStartsWith:
		return vm.startsWithValue(e), true
	case program.OpEndsWith:
		return vm.endsWithValue(e), true
	default:
		return Value{}, false
	}
}

func (vm *VM) evalConversionExpr(e program.Expr) (Value, bool) {
	switch e.Op {
	case program.OpToString:
		return vm.stringUnary(e, Value.ToStringVal, StringVal("")), true
	case program.OpToInt:
		return vm.intUnary(e, Value.ToIntVal), true
	case program.OpToFloat:
		return vm.floatUnary(e, Value.ToFloatVal), true
	case program.OpToRunes:
		// Slice Y.E.3: `[]rune(s)` / `[]byte(s)` — convert a string
		// into an ArrayVal whose Items are one-rune StringVals. Reading
		// len() returns the rune count; slicing returns a rune
		// subsequence; OpToString concatenates back to a string via
		// the ToStringVal join path.
		if !vm.requireOperands(e, 1) {
			return ArrayVal(nil), true
		}
		src := vm.Eval(e.Operands[0]).Str
		items := make([]Value, 0, len(src))
		for _, r := range src {
			items = append(items, StringVal(string(r)))
		}
		return ArrayVal(items), true
	default:
		return Value{}, false
	}
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
	return ZeroValue(typ)
}

func (vm *VM) updateSignal(e program.Expr) Value {
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

func (vm *VM) eventValue(name string) Value {
	if vm.eventData != nil {
		if v, ok := vm.eventData[name]; ok {
			return StringVal(v)
		}
	}
	return StringVal("")
}

func (vm *VM) evalUnary(e program.Expr, fn func(Value) Value, fallback program.ExprType) Value {
	if vm.requireOperands(e, 1) {
		return fn(vm.Eval(e.Operands[0]))
	}
	return ZeroValue(fallback)
}

func (vm *VM) stringUnary(e program.Expr, fn func(Value) Value, fallback Value) Value {
	if vm.requireOperands(e, 1) {
		return fn(vm.Eval(e.Operands[0]))
	}
	return fallback
}

func (vm *VM) intUnary(e program.Expr, fn func(Value) Value) Value {
	if vm.requireOperands(e, 1) {
		return fn(vm.Eval(e.Operands[0]))
	}
	return IntVal(0)
}

func (vm *VM) floatUnary(e program.Expr, fn func(Value) Value) Value {
	if vm.requireOperands(e, 1) {
		return fn(vm.Eval(e.Operands[0]))
	}
	return FloatVal(0)
}

func (vm *VM) formatValue(e program.Expr) Value {
	result := e.Value
	for _, op := range e.Operands {
		result += vm.Eval(op).String()
	}
	return StringVal(result)
}

func (vm *VM) conditionalValue(e program.Expr) Value {
	if !vm.requireOperands(e, 3) {
		return ZeroValue(program.TypeAny)
	}
	if vm.Eval(e.Operands[0]).Bool {
		return vm.Eval(e.Operands[1])
	}
	return vm.Eval(e.Operands[2])
}

func (vm *VM) indexValue(e program.Expr) Value {
	if vm.requireOperands(e, 2) {
		return vm.Eval(e.Operands[0]).IndexVal(vm.Eval(e.Operands[1]))
	}
	return ZeroValue(program.TypeAny)
}

func (vm *VM) lenValue(e program.Expr) Value {
	if vm.requireOperands(e, 1) {
		return IntVal(vm.Eval(e.Operands[0]).Len())
	}
	return IntVal(0)
}

func (vm *VM) mapValue(e program.Expr) Value {
	if !vm.requireOperands(e, 2) {
		return ArrayVal(nil)
	}
	coll := vm.Eval(e.Operands[0])
	return ArrayVal(vm.mapItems(coll.Items, e.Operands[1]))
}

func (vm *VM) filterValue(e program.Expr) Value {
	if !vm.requireOperands(e, 2) {
		return ArrayVal(nil)
	}
	coll := vm.Eval(e.Operands[0])
	return ArrayVal(vm.filterItems(coll.Items, e.Operands[1]))
}

func (vm *VM) findValue(e program.Expr) Value {
	if !vm.requireOperands(e, 2) {
		return ZeroValue(program.TypeAny)
	}
	coll := vm.Eval(e.Operands[0])
	if found, ok := vm.findItem(coll.Items, e.Operands[1]); ok {
		return found
	}
	return ZeroValue(program.TypeAny)
}

func (vm *VM) sliceValue(e program.Expr) Value {
	if vm.requireOperands(e, 3) {
		coll := vm.Eval(e.Operands[0])
		start := int(vm.Eval(e.Operands[1]).Num)
		end := int(vm.Eval(e.Operands[2]).Num)
		// Slice Y.E.3: OpSlice now dispatches on the runtime collection
		// kind so the lowerer's *ast.SliceExpr handler can emit a single
		// opcode without knowing whether the source operand is a slice
		// or a string. String operands route through SubstringVal;
		// rune-array operands (produced by Y.E's `[]rune(s)` cast)
		// route through the existing SliceVal path because they carry
		// Items, not Str.
		if coll.Items == nil && coll.Str != "" {
			return coll.SubstringVal(start, end)
		}
		return coll.SliceVal(start, end)
	}
	return ArrayVal(nil)
}

func (vm *VM) appendValue(e program.Expr) Value {
	if vm.requireOperands(e, 2) {
		return vm.Eval(e.Operands[0]).AppendVal(vm.Eval(e.Operands[1]))
	}
	return ArrayVal(nil)
}

func (vm *VM) containsValue(e program.Expr) Value {
	if vm.requireOperands(e, 2) {
		return vm.Eval(e.Operands[0]).ContainsVal(vm.Eval(e.Operands[1]))
	}
	return BoolVal(false)
}

func (vm *VM) splitValue(e program.Expr) Value {
	if !vm.requireOperands(e, 1) {
		return ArrayVal(nil)
	}
	return vm.Eval(e.Operands[0]).SplitVal(vm.separatorValue(e))
}

func (vm *VM) joinValue(e program.Expr) Value {
	if !vm.requireOperands(e, 1) {
		return StringVal("")
	}
	return vm.Eval(e.Operands[0]).JoinVal(vm.separatorValue(e))
}

func (vm *VM) separatorValue(e program.Expr) string {
	sep := e.Value
	if len(e.Operands) >= 2 {
		sep = vm.Eval(e.Operands[1]).String()
	}
	return sep
}

func (vm *VM) replaceValue(e program.Expr) Value {
	if vm.requireOperands(e, 3) {
		return vm.Eval(e.Operands[0]).ReplaceVal(vm.Eval(e.Operands[1]).Str, vm.Eval(e.Operands[2]).Str)
	}
	return StringVal("")
}

func (vm *VM) substringValue(e program.Expr) Value {
	if vm.requireOperands(e, 3) {
		return vm.Eval(e.Operands[0]).SubstringVal(int(vm.Eval(e.Operands[1]).Num), int(vm.Eval(e.Operands[2]).Num))
	}
	return StringVal("")
}

func (vm *VM) startsWithValue(e program.Expr) Value {
	if vm.requireOperands(e, 2) {
		return vm.Eval(e.Operands[0]).StartsWithVal(vm.Eval(e.Operands[1]))
	}
	return BoolVal(false)
}

func (vm *VM) endsWithValue(e program.Expr) Value {
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
	var result []Value
	restore := vm.captureProps([]string{"_item", "_index"})
	defer vm.restoreProps(restore)
	for i, item := range items {
		vm.props["_item"] = item
		vm.props["_index"] = IntVal(i)
		if vm.Eval(exprID).Bool {
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
		if vm.Eval(exprID).Bool {
			return item, true
		}
	}
	return Value{}, false
}

func (vm *VM) evalBinary(e program.Expr, fn func(Value, Value) Value) Value {
	if vm.requireOperands(e, 2) {
		return fn(vm.Eval(e.Operands[0]), vm.Eval(e.Operands[1]))
	}
	return ZeroValue(program.TypeAny)
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
		tree.Nodes[idx].Children = vm.resolveChildren(tree, node.Children)
	}

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
	return resolved
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
			value := vm.Eval(attr.Expr).String()
			if attr.Name == "key" {
				key = value
				explicitKey = true
				continue
			}
			domAttrs = append(domAttrs, ResolvedAttr{Name: attr.Name, Value: value})
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
		resolved = domAttrs[:staticCount:staticCount]
	}

	// Append the event markers after the static prefix.
	for _, event := range events {
		eventType := eventAttrType(event.Name)
		domAttrs = append(domAttrs, ResolvedAttr{
			Name:  "data-gosx-on-" + eventType,
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
	return fmt.Sprintf("_auto_%d_%s_%d", int(idxVal.Num), tag, source), true
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
	if value.Items != nil {
		return len(value.Items) > 0
	}
	if value.Fields != nil {
		return len(value.Fields) > 0
	}
	switch value.Type {
	case program.TypeBool:
		return value.Bool
	case program.TypeString:
		return value.Str != "" && value.Str != "0" && value.Str != "false"
	case program.TypeInt, program.TypeFloat:
		return value.Num != 0
	default:
		return value.Bool || value.Num != 0 || value.Str != ""
	}
}

func valueEachEntries(value Value) []eachEntry {
	if value.Items != nil {
		return arrayEachEntries(value.Items)
	}
	if value.Fields != nil {
		return objectEachEntries(value.Fields)
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

func (vm *VM) captureProps(names []string) iterationContext {
	ctx := iterationContext{
		values:  make(map[string]Value, len(names)),
		present: make(map[string]bool, len(names)),
	}
	for _, name := range names {
		if value, ok := vm.props[name]; ok {
			ctx.values[name] = value
			ctx.present[name] = true
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

func forEachFallbackExpr(attrs []program.Attr) (program.ExprID, bool) {
	return fallbackExpr(attrs)
}

func fallbackExpr(attrs []program.Attr) (program.ExprID, bool) {
	for _, attr := range attrs {
		if attr.Kind == program.AttrExpr && attr.Name == "fallback" {
			return attr.Expr, true
		}
	}
	return 0, false
}
