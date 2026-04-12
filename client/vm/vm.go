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
	fallbackID, ok := forEachFallbackExpr(attrs)
	if !ok {
		return nil
	}
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
	for _, attr := range attrs {
		if attr.Kind == program.AttrExpr && attr.Name == "fallback" {
			return attr.Expr, true
		}
	}
	return 0, false
}
