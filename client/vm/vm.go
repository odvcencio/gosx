package vm

import (
	"fmt"
	"strconv"

	"github.com/odvcencio/gosx/island/program"
	"github.com/odvcencio/gosx/signal"
)

// VM evaluates island expressions against props and signal state.
type VM struct {
	program   *program.Program
	props     map[string]Value
	signals   map[string]*signal.Signal[Value]
	exprs     []program.Expr
	eventData map[string]string // current event data (set during handler dispatch)
}

type iterationContext struct {
	item     Value
	index    Value
	hasItem  bool
	hasIndex bool
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
	return &VM{
		program: prog,
		props:   props,
		signals: make(map[string]*signal.Signal[Value]),
		exprs:   prog.Exprs,
	}
}

// SetSignal registers a signal by name.
func (vm *VM) SetSignal(name string, sig *signal.Signal[Value]) {
	vm.signals[name] = sig
}

// Eval evaluates an expression by ID and returns its value.
// The VM never panics — errors produce zero values.
func (vm *VM) Eval(id program.ExprID) Value {
	if int(id) >= len(vm.exprs) {
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
	return ZeroValue(program.TypeAny)
}

func (vm *VM) evalLiteralExpr(e program.Expr) (Value, bool) {
	switch e.Op {
	case program.OpLitString:
		return StringVal(e.Value), true
	case program.OpLitInt:
		n, _ := strconv.ParseInt(e.Value, 10, 64)
		return IntVal(int(n)), true
	case program.OpLitFloat:
		f, _ := strconv.ParseFloat(e.Value, 64)
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
	if sig, ok := vm.signals[e.Value]; ok && len(e.Operands) > 0 {
		sig.Set(vm.Eval(e.Operands[0]))
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
	if len(e.Operands) > 0 {
		return fn(vm.Eval(e.Operands[0]))
	}
	return ZeroValue(fallback)
}

func (vm *VM) stringUnary(e program.Expr, fn func(Value) Value, fallback Value) Value {
	if len(e.Operands) > 0 {
		return fn(vm.Eval(e.Operands[0]))
	}
	return fallback
}

func (vm *VM) intUnary(e program.Expr, fn func(Value) Value) Value {
	if len(e.Operands) > 0 {
		return fn(vm.Eval(e.Operands[0]))
	}
	return IntVal(0)
}

func (vm *VM) floatUnary(e program.Expr, fn func(Value) Value) Value {
	if len(e.Operands) > 0 {
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
	if len(e.Operands) < 3 {
		return ZeroValue(program.TypeAny)
	}
	if vm.Eval(e.Operands[0]).Bool {
		return vm.Eval(e.Operands[1])
	}
	return vm.Eval(e.Operands[2])
}

func (vm *VM) indexValue(e program.Expr) Value {
	if len(e.Operands) >= 2 {
		return vm.Eval(e.Operands[0]).IndexVal(vm.Eval(e.Operands[1]))
	}
	return ZeroValue(program.TypeAny)
}

func (vm *VM) lenValue(e program.Expr) Value {
	if len(e.Operands) > 0 {
		return IntVal(vm.Eval(e.Operands[0]).Len())
	}
	return IntVal(0)
}

func (vm *VM) mapValue(e program.Expr) Value {
	if len(e.Operands) < 2 {
		return ArrayVal(nil)
	}
	coll := vm.Eval(e.Operands[0])
	return ArrayVal(vm.mapItems(coll.Items, e.Operands[1]))
}

func (vm *VM) filterValue(e program.Expr) Value {
	if len(e.Operands) < 2 {
		return ArrayVal(nil)
	}
	coll := vm.Eval(e.Operands[0])
	return ArrayVal(vm.filterItems(coll.Items, e.Operands[1]))
}

func (vm *VM) findValue(e program.Expr) Value {
	if len(e.Operands) < 2 {
		return ZeroValue(program.TypeAny)
	}
	coll := vm.Eval(e.Operands[0])
	if found, ok := vm.findItem(coll.Items, e.Operands[1]); ok {
		return found
	}
	return ZeroValue(program.TypeAny)
}

func (vm *VM) sliceValue(e program.Expr) Value {
	if len(e.Operands) >= 3 {
		coll := vm.Eval(e.Operands[0])
		start := int(vm.Eval(e.Operands[1]).Num)
		end := int(vm.Eval(e.Operands[2]).Num)
		return coll.SliceVal(start, end)
	}
	return ArrayVal(nil)
}

func (vm *VM) appendValue(e program.Expr) Value {
	if len(e.Operands) >= 2 {
		return vm.Eval(e.Operands[0]).AppendVal(vm.Eval(e.Operands[1]))
	}
	return ArrayVal(nil)
}

func (vm *VM) containsValue(e program.Expr) Value {
	if len(e.Operands) >= 2 {
		return vm.Eval(e.Operands[0]).ContainsVal(vm.Eval(e.Operands[1]))
	}
	return BoolVal(false)
}

func (vm *VM) splitValue(e program.Expr) Value {
	if len(e.Operands) == 0 {
		return ArrayVal(nil)
	}
	return vm.Eval(e.Operands[0]).SplitVal(vm.separatorValue(e))
}

func (vm *VM) joinValue(e program.Expr) Value {
	if len(e.Operands) == 0 {
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
	if len(e.Operands) >= 3 {
		return vm.Eval(e.Operands[0]).ReplaceVal(vm.Eval(e.Operands[1]).Str, vm.Eval(e.Operands[2]).Str)
	}
	return StringVal("")
}

func (vm *VM) substringValue(e program.Expr) Value {
	if len(e.Operands) >= 3 {
		return vm.Eval(e.Operands[0]).SubstringVal(int(vm.Eval(e.Operands[1]).Num), int(vm.Eval(e.Operands[2]).Num))
	}
	return StringVal("")
}

func (vm *VM) startsWithValue(e program.Expr) Value {
	if len(e.Operands) >= 2 {
		return vm.Eval(e.Operands[0]).StartsWithVal(vm.Eval(e.Operands[1]))
	}
	return BoolVal(false)
}

func (vm *VM) endsWithValue(e program.Expr) Value {
	if len(e.Operands) >= 2 {
		return vm.Eval(e.Operands[0]).EndsWithVal(vm.Eval(e.Operands[1]))
	}
	return BoolVal(false)
}

func (vm *VM) mapItems(items []Value, exprID program.ExprID) []Value {
	result := make([]Value, len(items))
	vm.withIterationScope(func() {
		for i, item := range items {
			vm.setIterationContext(i, item)
			result[i] = vm.Eval(exprID)
		}
	})
	return result
}

func (vm *VM) filterItems(items []Value, exprID program.ExprID) []Value {
	var result []Value
	vm.withIterationScope(func() {
		for i, item := range items {
			vm.setIterationContext(i, item)
			if vm.Eval(exprID).Bool {
				result = append(result, item)
			}
		}
	})
	return result
}

func (vm *VM) findItem(items []Value, exprID program.ExprID) (Value, bool) {
	var found Value
	var ok bool
	vm.withIterationScope(func() {
		for i, item := range items {
			vm.setIterationContext(i, item)
			if vm.Eval(exprID).Bool {
				found = item
				ok = true
				return
			}
		}
	})
	return found, ok
}

func (vm *VM) withIterationScope(fn func()) {
	ctx := vm.currentIterationContext()
	defer vm.restoreIterationContext(ctx)
	fn()
}

func (vm *VM) currentIterationContext() iterationContext {
	item, hasItem := vm.props["_item"]
	index, hasIndex := vm.props["_index"]
	return iterationContext{
		item:     item,
		index:    index,
		hasItem:  hasItem,
		hasIndex: hasIndex,
	}
}

func (vm *VM) restoreIterationContext(ctx iterationContext) {
	if ctx.hasItem {
		vm.props["_item"] = ctx.item
	} else {
		delete(vm.props, "_item")
	}
	if ctx.hasIndex {
		vm.props["_index"] = ctx.index
	} else {
		delete(vm.props, "_index")
	}
}

func (vm *VM) setIterationContext(index int, item Value) {
	vm.props["_item"] = item
	vm.props["_index"] = IntVal(index)
}

func (vm *VM) evalBinary(e program.Expr, fn func(Value, Value) Value) Value {
	if len(e.Operands) >= 2 {
		return fn(vm.Eval(e.Operands[0]), vm.Eval(e.Operands[1]))
	}
	return ZeroValue(program.TypeAny)
}

// EvalTree walks the island's node tree, evaluating all dynamic expressions,
// and returns a resolved node tree for reconciliation.
func (vm *VM) EvalTree() *ResolvedTree {
	tree := &ResolvedTree{
		Nodes: make([]ResolvedNode, len(vm.program.Nodes)),
	}
	for i, node := range vm.program.Nodes {
		tree.Nodes[i] = vm.resolveNode(node)
	}
	return tree
}

func (vm *VM) resolveNode(node program.Node) ResolvedNode {
	rn := ResolvedNode{
		Tag:      node.Tag,
		Children: make([]int, len(node.Children)),
	}

	switch node.Kind {
	case program.NodeText:
		rn.Text = node.Text
	case program.NodeExpr:
		rn.Text = vm.Eval(node.Expr).String()
	case program.NodeElement:
		rn.Attrs = make([]ResolvedAttr, 0, len(node.Attrs))
		hasExplicitKey := false
		for _, attr := range node.Attrs {
			switch attr.Kind {
			case program.AttrStatic:
				if attr.Name == "key" {
					rn.Key = attr.Value
					hasExplicitKey = true
					continue
				}
				rn.Attrs = append(rn.Attrs, ResolvedAttr{Name: attr.Name, Value: attr.Value})
			case program.AttrExpr:
				val := vm.Eval(attr.Expr).String()
				if attr.Name == "key" {
					rn.Key = val
					hasExplicitKey = true
					continue
				}
				rn.Attrs = append(rn.Attrs, ResolvedAttr{Name: attr.Name, Value: val})
			case program.AttrBool:
				rn.Attrs = append(rn.Attrs, ResolvedAttr{Name: attr.Name, Value: ""})
			case program.AttrEvent:
				// Events are handled by delegation, not resolved into attrs.
			}
		}

		// Auto-key: if we're inside an iteration context (_index is set)
		// and no explicit key was provided, generate one from the index
		// and a content hash. This gives list items stable identity
		// without requiring the developer to set key= on every element.
		if !hasExplicitKey {
			if idxVal, inLoop := vm.props["_index"]; inLoop {
				idx := int(idxVal.Num)
				// Use index + tag + first attr value as a content fingerprint
				fingerprint := fmt.Sprintf("_auto_%d_%s", idx, node.Tag)
				if len(rn.Attrs) > 0 {
					fingerprint += "_" + rn.Attrs[0].Value
				}
				rn.Key = fingerprint
			}
		}
	}

	for i, childID := range node.Children {
		rn.Children[i] = int(childID)
	}

	return rn
}
