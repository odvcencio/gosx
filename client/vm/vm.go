package vm

import (
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
	e := vm.exprs[id]
	switch e.Op {
	// Literals
	case program.OpLitString:
		return StringVal(e.Value)
	case program.OpLitInt:
		n, _ := strconv.ParseInt(e.Value, 10, 64)
		return IntVal(int(n))
	case program.OpLitFloat:
		f, _ := strconv.ParseFloat(e.Value, 64)
		return FloatVal(f)
	case program.OpLitBool:
		return BoolVal(e.Value == "true")

	// Access
	case program.OpPropGet:
		if v, ok := vm.props[e.Value]; ok {
			return v
		}
		return ZeroValue(e.Type)
	case program.OpSignalGet:
		if sig, ok := vm.signals[e.Value]; ok {
			return sig.Get()
		}
		return ZeroValue(e.Type)
	case program.OpSignalSet:
		if sig, ok := vm.signals[e.Value]; ok && len(e.Operands) > 0 {
			val := vm.Eval(e.Operands[0])
			sig.Set(val)
		}
		return ZeroValue(program.TypeAny)
	case program.OpSignalUpdate:
		// Operands[0] references a handler name via an expression.
		// For now, treat as signal set with computed value.
		if sig, ok := vm.signals[e.Value]; ok && len(e.Operands) > 0 {
			val := vm.Eval(e.Operands[0])
			sig.Set(val)
		}
		return ZeroValue(program.TypeAny)

	// Arithmetic
	case program.OpAdd:
		return vm.evalBinary(e, Value.Add)
	case program.OpSub:
		return vm.evalBinary(e, Value.Sub)
	case program.OpMul:
		return vm.evalBinary(e, Value.Mul)
	case program.OpDiv:
		return vm.evalBinary(e, Value.Div)
	case program.OpMod:
		return vm.evalBinary(e, Value.Mod)
	case program.OpNeg:
		if len(e.Operands) > 0 {
			return vm.Eval(e.Operands[0]).Neg()
		}
		return ZeroValue(program.TypeInt)

	// Comparison
	case program.OpEq:
		return vm.evalBinary(e, Value.Eq)
	case program.OpNeq:
		return vm.evalBinary(e, Value.Neq)
	case program.OpLt:
		return vm.evalBinary(e, Value.Lt)
	case program.OpGt:
		return vm.evalBinary(e, Value.Gt)
	case program.OpLte:
		return vm.evalBinary(e, Value.Lte)
	case program.OpGte:
		return vm.evalBinary(e, Value.Gte)

	// Boolean
	case program.OpAnd:
		return vm.evalBinary(e, Value.And)
	case program.OpOr:
		return vm.evalBinary(e, Value.Or)
	case program.OpNot:
		if len(e.Operands) > 0 {
			return vm.Eval(e.Operands[0]).Not()
		}
		return BoolVal(false)

	// String
	case program.OpConcat:
		return vm.evalBinary(e, Value.Concat)
	case program.OpFormat:
		// Simple format: concatenate all operands as strings with format value as prefix.
		result := e.Value
		for _, op := range e.Operands {
			result += vm.Eval(op).String()
		}
		return StringVal(result)

	// Control
	case program.OpCond:
		if len(e.Operands) >= 3 {
			cond := vm.Eval(e.Operands[0])
			if cond.Bool {
				return vm.Eval(e.Operands[1])
			}
			return vm.Eval(e.Operands[2])
		}
		return ZeroValue(program.TypeAny)

	// Dispatch
	case program.OpCall:
		// Handler calls are dispatched by the Island, not the VM directly.
		// The VM just evaluates the arguments.
		return ZeroValue(program.TypeAny)

	// Collection
	case program.OpIndex:
		return ZeroValue(program.TypeAny)
	case program.OpLen:
		if len(e.Operands) > 0 {
			v := vm.Eval(e.Operands[0])
			return IntVal(v.Len())
		}
		return IntVal(0)
	case program.OpRange:
		return ZeroValue(program.TypeAny)

	case program.OpEventGet:
		// Read a field from the current event data
		if vm.eventData != nil {
			if v, ok := vm.eventData[e.Value]; ok {
				return StringVal(v)
			}
		}
		return StringVal("")

	// Array/slice operations
	case program.OpMap:
		if len(e.Operands) >= 2 {
			coll := vm.Eval(e.Operands[0])
			// Save previous iteration context for nested iteration safety
			prevItem, hadItem := vm.props["_item"]
			prevIndex, hadIndex := vm.props["_index"]

			result := make([]Value, len(coll.Items))
			for i, item := range coll.Items {
				vm.props["_item"] = item
				vm.props["_index"] = IntVal(i)
				result[i] = vm.Eval(e.Operands[1])
			}

			// Restore previous context
			if hadItem {
				vm.props["_item"] = prevItem
			} else {
				delete(vm.props, "_item")
			}
			if hadIndex {
				vm.props["_index"] = prevIndex
			} else {
				delete(vm.props, "_index")
			}
			return ArrayVal(result)
		}
		return ArrayVal(nil)

	case program.OpFilter:
		if len(e.Operands) >= 2 {
			coll := vm.Eval(e.Operands[0])
			// Save previous iteration context for nested iteration safety
			prevItem, hadItem := vm.props["_item"]
			prevIndex, hadIndex := vm.props["_index"]

			var result []Value
			for i, item := range coll.Items {
				vm.props["_item"] = item
				vm.props["_index"] = IntVal(i)
				if vm.Eval(e.Operands[1]).Bool {
					result = append(result, item)
				}
			}

			// Restore previous context
			if hadItem {
				vm.props["_item"] = prevItem
			} else {
				delete(vm.props, "_item")
			}
			if hadIndex {
				vm.props["_index"] = prevIndex
			} else {
				delete(vm.props, "_index")
			}
			return ArrayVal(result)
		}
		return ArrayVal(nil)

	case program.OpFind:
		if len(e.Operands) >= 2 {
			coll := vm.Eval(e.Operands[0])
			// Save previous iteration context for nested iteration safety
			prevItem, hadItem := vm.props["_item"]
			prevIndex, hadIndex := vm.props["_index"]

			var found Value
			foundAny := false
			for i, item := range coll.Items {
				vm.props["_item"] = item
				vm.props["_index"] = IntVal(i)
				if vm.Eval(e.Operands[1]).Bool {
					found = item
					foundAny = true
					break
				}
			}

			// Restore previous context
			if hadItem {
				vm.props["_item"] = prevItem
			} else {
				delete(vm.props, "_item")
			}
			if hadIndex {
				vm.props["_index"] = prevIndex
			} else {
				delete(vm.props, "_index")
			}

			if foundAny {
				return found
			}
		}
		return ZeroValue(program.TypeAny)

	case program.OpSlice:
		if len(e.Operands) >= 3 {
			coll := vm.Eval(e.Operands[0])
			start := int(vm.Eval(e.Operands[1]).Num)
			end := int(vm.Eval(e.Operands[2]).Num)
			return coll.SliceVal(start, end)
		}
		return ArrayVal(nil)

	case program.OpAppend:
		if len(e.Operands) >= 2 {
			return vm.Eval(e.Operands[0]).AppendVal(vm.Eval(e.Operands[1]))
		}
		return ArrayVal(nil)

	case program.OpContains:
		if len(e.Operands) >= 2 {
			return vm.Eval(e.Operands[0]).ContainsVal(vm.Eval(e.Operands[1]))
		}
		return BoolVal(false)

	// String methods
	case program.OpToUpper:
		if len(e.Operands) > 0 {
			return vm.Eval(e.Operands[0]).ToUpper()
		}
		return StringVal("")
	case program.OpToLower:
		if len(e.Operands) > 0 {
			return vm.Eval(e.Operands[0]).ToLower()
		}
		return StringVal("")
	case program.OpTrim:
		if len(e.Operands) > 0 {
			return vm.Eval(e.Operands[0]).TrimVal()
		}
		return StringVal("")
	case program.OpSplit:
		if len(e.Operands) > 0 {
			return vm.Eval(e.Operands[0]).SplitVal(e.Value)
		}
		return ArrayVal(nil)
	case program.OpJoin:
		if len(e.Operands) > 0 {
			return vm.Eval(e.Operands[0]).JoinVal(e.Value)
		}
		return StringVal("")
	case program.OpReplace:
		if len(e.Operands) >= 3 {
			return vm.Eval(e.Operands[0]).ReplaceVal(vm.Eval(e.Operands[1]).Str, vm.Eval(e.Operands[2]).Str)
		}
		return StringVal("")
	case program.OpSubstring:
		if len(e.Operands) >= 3 {
			return vm.Eval(e.Operands[0]).SubstringVal(int(vm.Eval(e.Operands[1]).Num), int(vm.Eval(e.Operands[2]).Num))
		}
		return StringVal("")
	case program.OpStartsWith:
		if len(e.Operands) >= 2 {
			return vm.Eval(e.Operands[0]).StartsWithVal(vm.Eval(e.Operands[1]))
		}
		return BoolVal(false)
	case program.OpEndsWith:
		if len(e.Operands) >= 2 {
			return vm.Eval(e.Operands[0]).EndsWithVal(vm.Eval(e.Operands[1]))
		}
		return BoolVal(false)

	// Type conversions
	case program.OpToString:
		if len(e.Operands) > 0 {
			return vm.Eval(e.Operands[0]).ToStringVal()
		}
		return StringVal("")
	case program.OpToInt:
		if len(e.Operands) > 0 {
			return vm.Eval(e.Operands[0]).ToIntVal()
		}
		return IntVal(0)
	case program.OpToFloat:
		if len(e.Operands) > 0 {
			return vm.Eval(e.Operands[0]).ToFloatVal()
		}
		return FloatVal(0)

	default:
		return ZeroValue(program.TypeAny)
	}
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
		for _, attr := range node.Attrs {
			switch attr.Kind {
			case program.AttrStatic:
				if attr.Name == "key" {
					rn.Key = attr.Value // extract key for list diffing
					continue            // don't emit key as a DOM attribute
				}
				rn.Attrs = append(rn.Attrs, ResolvedAttr{Name: attr.Name, Value: attr.Value})
			case program.AttrExpr:
				val := vm.Eval(attr.Expr).String()
				if attr.Name == "key" {
					rn.Key = val
					continue
				}
				rn.Attrs = append(rn.Attrs, ResolvedAttr{Name: attr.Name, Value: val})
			case program.AttrBool:
				rn.Attrs = append(rn.Attrs, ResolvedAttr{Name: attr.Name, Value: ""})
			case program.AttrEvent:
				// Events are handled by delegation, not resolved into attrs.
			}
		}
	}

	for i, childID := range node.Children {
		rn.Children[i] = int(childID)
	}

	return rn
}
