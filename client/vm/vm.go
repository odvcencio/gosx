package vm

import (
	"fmt"
	"sort"
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
	values  map[string]Value
	present map[string]bool
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
	if len(e.Operands) >= 2 {
		return fn(vm.Eval(e.Operands[0]), vm.Eval(e.Operands[1]))
	}
	return ZeroValue(program.TypeAny)
}

// EvalTree walks the island's node tree, evaluating all dynamic expressions,
// and returns a resolved node tree for reconciliation.
func (vm *VM) EvalTree() *ResolvedTree {
	tree := &ResolvedTree{}
	vm.resolveNodeRefs(tree, vm.program.Root)
	return tree
}

func (vm *VM) resolveNode(node program.Node) ResolvedNode {
	rn := ResolvedNode{
		Tag: node.Tag,
	}

	switch node.Kind {
	case program.NodeText:
		rn.Text = node.Text
	case program.NodeExpr:
		rn.Text = vm.Eval(node.Expr).String()
	case program.NodeElement:
		vm.resolveElementNode(&rn, node)
	}

	return rn
}

func (vm *VM) resolveNodeRefs(tree *ResolvedTree, nodeID program.NodeID) []int {
	if int(nodeID) >= len(vm.program.Nodes) {
		return nil
	}
	node := vm.program.Nodes[nodeID]
	switch node.Kind {
	case program.NodeFragment:
		return vm.resolveChildren(tree, node.Children)
	case program.NodeForEach:
		return vm.resolveForEach(tree, int(nodeID), node)
	default:
		idx := vm.appendResolvedNode(tree, int(nodeID), node)
		return []int{idx}
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
		vm.resolveElementNode(&tree.Nodes[idx], node)
		tree.Nodes[idx].Children = vm.resolveChildren(tree, node.Children)
	}

	return idx
}

func (vm *VM) resolveChildren(tree *ResolvedTree, children []program.NodeID) []int {
	var resolved []int
	for _, childID := range children {
		resolved = append(resolved, vm.resolveNodeRefs(tree, childID)...)
	}
	return resolved
}

func (vm *VM) resolveElementNode(rn *ResolvedNode, node program.Node) {
	attrs, key, explicitKey, events := vm.resolveElementAttrs(node.Attrs)
	rn.Attrs = attrs
	rn.DOMAttrs = materializeDOMAttrs(attrs, events)
	rn.Key = key
	rn.Events = events
	if explicitKey {
		return
	}
	if autoKey, ok := vm.autoKey(node.Tag, attrs); ok {
		rn.Key = autoKey
	}
}

func (vm *VM) resolveElementAttrs(attrs []program.Attr) ([]ResolvedAttr, string, bool, []ResolvedEvent) {
	resolved := make([]ResolvedAttr, 0, len(attrs))
	events := make([]ResolvedEvent, 0, len(attrs))
	key := ""
	explicitKey := false
	for _, attr := range attrs {
		switch attr.Kind {
		case program.AttrStatic:
			if attr.Name == "key" {
				key = attr.Value
				explicitKey = true
				continue
			}
			resolved = append(resolved, ResolvedAttr{Name: attr.Name, Value: attr.Value})
		case program.AttrExpr:
			value := vm.Eval(attr.Expr).String()
			if attr.Name == "key" {
				key = value
				explicitKey = true
				continue
			}
			resolved = append(resolved, ResolvedAttr{Name: attr.Name, Value: value})
		case program.AttrBool:
			resolved = append(resolved, ResolvedAttr{Name: attr.Name, Bool: true})
		case program.AttrEvent:
			events = append(events, ResolvedEvent{Name: attr.Name, Handler: attr.Event})
		}
	}
	return resolved, key, explicitKey, events
}

func (vm *VM) autoKey(tag string, attrs []ResolvedAttr) (string, bool) {
	keyVal, hasKey := vm.props["_key"]
	if hasKey {
		fingerprint := fmt.Sprintf("_auto_%s_%s", keyVal.String(), tag)
		if len(attrs) > 0 {
			fingerprint += "_" + attrs[0].Value
		}
		return fingerprint, true
	}
	idxVal, hasIndex := vm.props["_index"]
	if !hasIndex {
		return "", false
	}
	fingerprint := fmt.Sprintf("_auto_%d_%s", int(idxVal.Num), tag)
	if len(attrs) > 0 {
		fingerprint += "_" + attrs[0].Value
	}
	return fingerprint, true
}

type eachEntry struct {
	Index  int
	Key    Value
	Item   Value
	HasKey bool
}

func (vm *VM) resolveForEach(tree *ResolvedTree, source int, node program.Node) []int {
	entries := valueEachEntries(vm.Eval(node.Expr))
	if len(entries) == 0 {
		if fallbackID, ok := forEachFallbackExpr(node.Attrs); ok {
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
		return nil
	}

	itemName := forEachStaticAttr(node.Attrs, "as")
	if itemName == "" {
		itemName = "item"
	}
	indexName := forEachStaticAttr(node.Attrs, "index")
	keyName := itemName + "Key"

	names := []string{"_item", "_index", "_key", itemName, keyName}
	if indexName != "" {
		names = append(names, indexName)
	}
	restore := vm.captureProps(names)
	defer vm.restoreProps(restore)

	var out []int
	for _, entry := range entries {
		vm.props["_item"] = entry.Item
		vm.props["_index"] = IntVal(entry.Index)
		vm.props[itemName] = entry.Item
		if indexName != "" {
			vm.props[indexName] = IntVal(entry.Index)
		}
		if entry.HasKey {
			vm.props["_key"] = entry.Key
			vm.props[keyName] = entry.Key
		} else {
			delete(vm.props, "_key")
			delete(vm.props, keyName)
		}
		for _, child := range node.Children {
			out = append(out, vm.resolveNodeRefs(tree, child)...)
		}
	}
	return out
}

func valueEachEntries(value Value) []eachEntry {
	if value.Items != nil {
		out := make([]eachEntry, 0, len(value.Items))
		for i, item := range value.Items {
			out = append(out, eachEntry{
				Index:  i,
				Key:    IntVal(i),
				Item:   item,
				HasKey: true,
			})
		}
		return out
	}
	if value.Fields != nil {
		keys := make([]string, 0, len(value.Fields))
		for key := range value.Fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make([]eachEntry, 0, len(keys))
		for i, key := range keys {
			out = append(out, eachEntry{
				Index:  i,
				Key:    StringVal(key),
				Item:   value.Fields[key],
				HasKey: true,
			})
		}
		return out
	}
	return nil
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
	for _, attr := range attrs {
		if attr.Kind == program.AttrExpr && attr.Name == "fallback" {
			return attr.Expr, true
		}
	}
	return 0, false
}
