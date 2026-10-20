package aot

import (
	"fmt"
	"slices"
	"strconv"

	"m31labs.dev/gosx/island/program"
)

// InputDefaultType returns the VM's absent-value tag. A selector is read from
// the original JSON object, before typed top-level defaults are installed.
// A missing string event/prop is a typed zero, not StringVal("").
func InputDefaultType(p *program.Program, input InputContract) (program.ExprType, error) {
	if p == nil || !scalarKind(input.Kind) {
		return 0, fmt.Errorf("invalid scalar input")
	}
	if input.Source == "event" && len(input.Path) == 0 {
		typ, ok := eventScalarType(input.Root)
		if ok && typeMatchesKind(typ, input.Kind) {
			return typ, nil
		}
	}
	if input.Source == "prop" {
		if input.Root == "props" && len(input.Path) != 0 {
			return program.TypeAny, nil
		}
		for _, prop := range p.Props {
			if prop.Name != input.Root {
				continue
			}
			if len(input.Path) != 0 {
				if prop.Type == program.TypeAny {
					return program.TypeAny, nil
				}
			} else if typeMatchesKind(prop.Type, input.Kind) {
				return prop.Type, nil
			}
		}
	}
	return 0, fmt.Errorf("input has no compatible declaration")
}

func eventScalarType(name string) (program.ExprType, bool) {
	switch name {
	case "type", "value", "key", "code", "targetID", "currentTargetID", "eventData":
		return program.TypeString, true
	case "checked", "ctrlKey", "metaKey", "altKey", "shiftKey", "repeat", "editable":
		return program.TypeBool, true
	case "selectedIndex", "button", "buttons":
		return program.TypeInt, true
	default:
		return 0, false
	}
}

func selector(p *program.Program, id program.ExprID) (string, []string, bool) {
	path := []string{}
	for depth := 0; depth <= 64 && int(id) < len(p.Exprs); depth++ {
		e := p.Exprs[id]
		if e.Op == program.OpPropGet && len(e.Operands) == 0 {
			slices.Reverse(path)
			return e.Value, path, true
		}
		if e.Op != program.OpIndex || len(e.Operands) != 2 || int(e.Operands[1]) >= len(p.Exprs) {
			break
		}
		key := p.Exprs[e.Operands[1]]
		if key.Op != program.OpLitString || len(key.Operands) != 0 {
			break
		}
		path = append(path, key.Value)
		id = e.Operands[0]
	}
	return "", nil, false
}

func inputRules(u Unit) *rejection {
	p, c := u.Program, u.Contract
	bound := make([]bool, len(p.Exprs))
	for i, input := range c.Inputs {
		if _, err := InputDefaultType(p, input); err != nil {
			return reject("input_declaration", "inputs", i)
		}
		for _, id := range input.Exprs {
			e := p.Exprs[id]
			root, path, ok := selector(p, id)
			if input.Source == "event" {
				root, path, ok = e.Value, []string{}, e.Op == program.OpEventGet
			}
			if !ok || root != input.Root || !slices.Equal(path, input.Path) || !typeMatchesKind(e.Type, input.Kind) {
				return reject("input_expression", "expressions", int(id))
			}
			if input.Source == "event" {
				typ, _ := eventScalarType(root)
				if e.Type != typ {
					return reject("input_expression", "expressions", int(id))
				}
			}
			bound[id] = true
		}
	}
	for i, e := range p.Exprs {
		kind := c.Expressions[i].Kind
		if e.Op == program.OpPropGet || e.Op == program.OpIndex || e.Op == program.OpEventGet {
			if kind != SelectorPath && !bound[i] {
				return reject("input_missing", "expressions", i)
			}
			if kind == SelectorPath {
				root, _, ok := selector(p, program.ExprID(i))
				if !ok || root == "" || e.Type != program.TypeAny {
					return reject("selector_path", "expressions", i)
				}
				if root != "props" {
					declared := false
					for _, prop := range p.Props {
						declared = declared || prop.Name == root && prop.Type == program.TypeAny
					}
					if !declared {
						return reject("selector_path", "expressions", i)
					}
				}
			}
		}
		if e.Op == program.OpIndex {
			if c.Expressions[e.Operands[0]].Kind != SelectorPath || p.Exprs[e.Operands[1]].Op != program.OpLitString {
				return reject("selector_path", "expressions", i)
			}
		}
	}
	return nil
}

func expressionTypes(u Unit) *rejection {
	p, c := u.Program, u.Contract
	states := map[string]ScalarKind{}
	for _, s := range append(append([]StateContract{}, c.Signals...), c.Computeds...) {
		states[s.Name] = s.Kind
	}
	for i, e := range p.Exprs {
		kind := c.Expressions[i].Kind
		args := make([]ScalarKind, len(e.Operands))
		for j, id := range e.Operands {
			args[j] = c.Expressions[id].Kind
		}
		valid := false
		switch e.Op {
		case program.OpLitString:
			valid = kind == String && e.Type == program.TypeString
		case program.OpLitInt:
			value, err := strconv.ParseInt(e.Value, 10, 32)
			if err != nil || strconv.FormatInt(value, 10) != e.Value {
				return reject("integer_literal", "expressions", i)
			}
			valid = integerKind(kind) && e.Type == program.TypeInt
		case program.OpLitBool:
			if e.Value != "true" && e.Value != "false" {
				return reject("boolean_literal", "expressions", i)
			}
			valid = kind == Bool && e.Type == program.TypeBool
		case program.OpPropGet, program.OpEventGet, program.OpIndex:
			valid = scalarKind(kind) || kind == SelectorPath
		case program.OpSignalGet:
			valid = kind == states[e.Value]
		case program.OpSignalSet:
			valid = kind == AnyZero && sameScalar(args[0], states[e.Value]) && e.Type == program.TypeAny
		case program.OpAdd:
			valid = integerKind(args[0]) && integerKind(args[1]) && integerKind(kind) || args[0] == String && args[1] == String && kind == String
		case program.OpSub, program.OpMul:
			valid = integerKind(args[0]) && integerKind(args[1]) && integerKind(kind)
		case program.OpNeg:
			valid = integerKind(args[0]) && integerKind(kind)
		case program.OpEq, program.OpNeq, program.OpLt, program.OpGt, program.OpLte, program.OpGte:
			valid = kind == Bool && sameScalar(args[0], args[1]) && (integerKind(args[0]) || args[0] == String || args[0] == Bool && (e.Op == program.OpEq || e.Op == program.OpNeq))
		case program.OpAnd, program.OpOr:
			valid = kind == Bool && args[0] == Bool && args[1] == Bool
		case program.OpNot:
			valid = kind == Bool && args[0] == Bool
		case program.OpConcat, program.OpFormat, program.OpToString:
			valid = kind == String
			for _, arg := range args {
				valid = valid && (scalarKind(arg) || arg == AnyZero)
			}
		case program.OpCond:
			valid = args[0] == Bool && scalarKind(kind) && sameScalar(args[1], args[2]) && kind == args[2]
		case program.OpLen:
			valid = args[0] == String && integerKind(kind)
		case program.OpSeq:
			valid = kind == AnyZero && len(args) == 0 || len(args) != 0 && kind == args[len(args)-1] && kind != SelectorPath
		}
		if !valid || kind != SelectorPath && !typeMatchesKind(e.Type, kind) {
			return reject("type_mismatch", "expressions", i)
		}
	}
	return nil
}

func integerKind(k ScalarKind) bool { return k == Int || k == Int32 }
