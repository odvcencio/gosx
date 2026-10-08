//go:build !tinygo

package ir

import (
	"fmt"
	"go/constant"
	"go/token"
	"slices"
	"strings"

	"m31labs.dev/gosx/island/aot"
	"m31labs.dev/gosx/island/program"
)

// LowerIslandAOT uses the existing island lowerer and retains scalar source
// evidence. Missing evidence is a diagnostic, never an inferred VM width.
func LowerIslandAOT(src *Program, index int) (aot.Unit, error) {
	if src == nil || index < 0 || index >= len(src.Components) {
		return aot.Unit{}, fmt.Errorf("invalid island component index")
	}
	comp := src.Components[index]
	for _, declarations := range []map[string]string{comp.PropsFields, comp.PropsPaths} {
		for _, typ := range declarations {
			if src.aotScalarShadows[typ] {
				return aot.Unit{}, aotSourceError(comp, "source_type", "a scalar type name is shadowed")
			}
		}
	}
	identity := src.PackagePath + "." + comp.Name
	if src.PackagePath == "" {
		return aot.Unit{}, aotSourceError(comp, "component_identity", "an import path is required")
	}
	// Source graphs are bounded before calling the ordinary tree lowerer.
	active := make(map[NodeID]bool)
	var visit func(NodeID, int) error
	visit = func(id NodeID, depth int) error {
		if int(id) >= len(src.Nodes) || depth > 64 || active[id] {
			return fmt.Errorf("invalid source node graph")
		}
		active[id] = true
		n := src.Nodes[id]
		if n.Kind == NodeComponent && !n.IsSyntheticConditional() {
			for _, callee := range src.Components {
				if callee.Name != n.Tag {
					continue
				}
				for _, typ := range callee.PropsFields {
					if aotSourceKind(typ) == "" {
						return fmt.Errorf("composed prop has an unproved scalar type")
					}
				}
				if err := visit(callee.Root, depth+1); err != nil {
					return err
				}
			}
		}
		if n.Kind == NodeExpr {
			if err := aotSourceTokens(n.Text); err != nil {
				return err
			}
		}
		for _, attr := range n.Attrs {
			if attr.Expr != "" {
				if err := aotSourceTokens(attr.Expr); err != nil {
					return err
				}
			}
		}
		for _, child := range n.Children {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		delete(active, id)
		return nil
	}
	if err := visit(comp.Root, 1); err != nil {
		return aot.Unit{}, aotSourceError(comp, "source_graph", err.Error())
	}
	p, err := LowerIsland(src, index)
	if err != nil {
		return aot.Unit{}, err
	}
	limits := aot.ProfileLimits()
	if len(p.Nodes) > int(limits.Nodes) || len(p.Exprs) > int(limits.Expressions) {
		return aot.Unit{}, aotSourceError(comp, "source_graph", "program exceeds scalar profile limits")
	}
	c := aot.ScalarContract{Version: 1, Component: identity, Expressions: []aot.ExpressionContract{}, Inputs: []aot.InputContract{}, Signals: []aot.StateContract{}, Computeds: []aot.StateContract{}}
	stateKinds := make(map[string]aot.ScalarKind)
	if comp.Scope != nil {
		for slot, sig := range comp.Scope.Signals {
			kind := aotSourceKind(sig.SourceType)
			if kind == "" {
				return aot.Unit{}, aotSourceError(comp, "source_type", "signal "+sig.Name+" has an unproved source type")
			}
			if err := aotSourceTokens(sig.InitExpr); err != nil {
				return aot.Unit{}, aotSourceError(comp, "source_literal", err.Error())
			}
			stateKinds[sig.Name] = kind
			c.Signals = append(c.Signals, aot.StateContract{Slot: uint32(slot), Name: sig.Name, Kind: kind})
		}
		for slot, computed := range comp.Scope.Computeds {
			kind := aotSourceKind(computed.ReturnType)
			if kind == "" {
				return aot.Unit{}, aotSourceError(comp, "source_type", "computed "+computed.Name+" has an unproved source type")
			}
			if err := aotSourceTokens(computed.BodyExpr); err != nil {
				return aot.Unit{}, aotSourceError(comp, "source_literal", err.Error())
			}
			stateKinds[computed.Name] = kind
			c.Computeds = append(c.Computeds, aot.StateContract{Slot: uint32(slot), Name: computed.Name, Kind: kind})
		}
		for _, handler := range comp.Scope.Handlers {
			for _, stmt := range handler.Statements {
				if err := aotSourceTokens(stmt); err != nil {
					return aot.Unit{}, aotSourceError(comp, "source_literal", err.Error())
				}
			}
		}
	}
	kinds := make([]aot.ScalarKind, len(p.Exprs))
	// A default int kind does not distinguish an untyped constant from a
	// typed int expression. Retain exact constant values for contextual typing.
	constants := make([]constant.Value, len(p.Exprs))
	pure := make([]bool, len(p.Exprs))
	visiting := make([]bool, len(p.Exprs))
	inferDepth := 0
	var infer func(program.ExprID) (aot.ScalarKind, error)
	infer = func(id program.ExprID) (aot.ScalarKind, error) {
		inferDepth++
		defer func() { inferDepth-- }()
		if int(id) >= len(p.Exprs) || visiting[id] || inferDepth > 64 {
			return "", fmt.Errorf("invalid expression graph")
		}
		if kinds[id] != "" {
			return kinds[id], nil
		}
		visiting[id] = true
		e := p.Exprs[id]
		args := make([]aot.ScalarKind, len(e.Operands))
		isPure := true
		for i, operand := range e.Operands {
			k, err := infer(operand)
			if err != nil {
				return "", err
			}
			args[i], isPure = k, isPure && pure[operand]
		}
		var kind aot.ScalarKind
		switch e.Op {
		case program.OpLitString:
			kind = aot.String
		case program.OpLitInt:
			kind = aot.Int
			constants[id] = constant.MakeFromLiteral(e.Value, token.INT, 0)
			if constants[id].Kind() != constant.Int {
				return "", fmt.Errorf("expression %d has an unproved integer literal", id)
			}
		case program.OpLitBool:
			kind = aot.Bool
		case program.OpSignalGet:
			kind = stateKinds[e.Value]
		case program.OpSignalSet:
			kind, isPure = aot.AnyZero, false
		case program.OpPropGet, program.OpIndex, program.OpEventGet:
			input, prefix := aotSourceInput(p, id, comp)
			kind = input.Kind
			if prefix {
				kind = aot.SelectorPath
			} else if kind != "" {
				input.Exprs = []program.ExprID{id}
				c.Inputs = append(c.Inputs, input)
			}
		case program.OpAdd, program.OpSub, program.OpMul:
			if len(args) != 2 {
				break
			}
			left, right := constants[e.Operands[0]], constants[e.Operands[1]]
			if left != nil && right != nil {
				op := map[program.OpCode]token.Token{program.OpAdd: token.ADD, program.OpSub: token.SUB, program.OpMul: token.MUL}[e.Op]
				constants[id] = constant.BinaryOp(left, op, right)
				kind = aot.Int
			} else if left != nil && aotConstantFits(left, args[1]) {
				kind = args[1]
			} else if right != nil && aotConstantFits(right, args[0]) {
				kind = args[0]
			} else if left == nil && right == nil && args[0] == args[1] {
				if args[0] == aot.Int || args[0] == aot.Int32 || args[0] == aot.String && e.Op == program.OpAdd {
					kind = args[0]
				}
			}
		case program.OpNeg:
			if len(args) == 1 && (args[0] == aot.Int || args[0] == aot.Int32) {
				kind = args[0]
				if value := constants[e.Operands[0]]; value != nil {
					constants[id] = constant.UnaryOp(token.SUB, value, 0)
				}
			}
		case program.OpCond, program.OpSeq:
			if len(args) > 0 {
				kind = args[len(args)-1]
			}
			if e.Op == program.OpSeq && len(args) == 0 {
				kind = aot.AnyZero
			}
		case program.OpEq, program.OpNeq, program.OpLt, program.OpGt, program.OpLte, program.OpGte, program.OpAnd, program.OpOr, program.OpNot:
			kind = aot.Bool
		case program.OpConcat, program.OpFormat, program.OpToString:
			kind = aot.String
		case program.OpLen:
			kind = aot.Int
		}
		if kind == "" {
			return "", fmt.Errorf("expression %d has no supported scalar source evidence", id)
		}
		kinds[id], pure[id], visiting[id] = kind, isPure, false
		return kind, nil
	}
	for id := range p.Exprs {
		kind, err := infer(program.ExprID(id))
		if err != nil {
			return aot.Unit{}, aotSourceError(comp, "source_type", err.Error())
		}
		c.Expressions = append(c.Expressions, aot.ExpressionContract{Expr: program.ExprID(id), Kind: kind, Pure: pure[id]})
	}
	c.Inputs = aotInternInputs(c.Inputs)
	c.Bindings, err = aot.ContractBindings(p)
	if err != nil {
		return aot.Unit{}, aotSourceError(comp, "source_graph", err.Error())
	}
	return aot.NewUnit(identity, p, c)
}

func aotConstantFits(value constant.Value, kind aot.ScalarKind) bool {
	if kind != aot.Int && kind != aot.Int32 {
		return false
	}
	// Both admitted integer kinds execute in the signed int32 domain. Never
	// contextualize an out-of-domain constant by narrowing its value.
	n, exact := constant.Int64Val(value)
	return exact && n >= -1<<31 && n <= 1<<31-1
}

func aotSourceKind(name string) aot.ScalarKind {
	switch strings.TrimSpace(name) {
	case "int":
		return aot.Int
	case "int32":
		return aot.Int32
	case "bool":
		return aot.Bool
	case "string":
		return aot.String
	default:
		return ""
	}
}

func aotSourceTokens(source string) error {
	tokens, err := lexExpr(source)
	if err != nil {
		return err
	}
	for _, tok := range tokens {
		if tok.kind == tokenString && strings.HasPrefix(tok.text, "'") {
			return fmt.Errorf("rune literal is outside the scalar profile")
		}
	}
	return nil
}

func aotSourceError(comp Component, code, message string) error {
	return NewDiagnosticsError("island-aot", []Diagnostic{{Span: comp.Span, Code: "aot_" + code, Message: message}})
}

func aotSourceInput(p *program.Program, id program.ExprID, comp Component) (aot.InputContract, bool) {
	e := p.Exprs[id]
	if e.Op == program.OpEventGet {
		kind := aotSourceKind(map[program.ExprType]string{program.TypeString: "string", program.TypeInt: "int", program.TypeBool: "bool"}[eventFieldType(e.Value)])
		return aot.InputContract{Source: "event", Root: e.Value, Path: []string{}, Kind: kind}, false
	}
	keys := []string{}
	for e.Op == program.OpIndex && len(e.Operands) == 2 {
		key := p.Exprs[e.Operands[1]]
		if key.Op != program.OpLitString {
			return aot.InputContract{}, false
		}
		keys = append([]string{key.Value}, keys...)
		e = p.Exprs[e.Operands[0]]
	}
	if e.Op != program.OpPropGet {
		return aot.InputContract{}, false
	}
	if e.Value == "props" && len(keys) == 0 {
		return aot.InputContract{}, true
	}
	root := e.Value
	inputRoot, inputPath := root, keys
	if root == "props" {
		root, keys = keys[0], keys[1:]
	}
	path := strings.Join(append([]string{root}, keys...), ".")
	typ := comp.PropsFields[root]
	if len(keys) > 0 {
		typ = comp.PropsPaths[path]
	}
	if comp.Scope != nil && typ == "" {
		typ = comp.Scope.SourcePropsPaths[path]
	}
	if kind := aotSourceKind(typ); kind != "" {
		return aot.InputContract{Source: "prop", Root: inputRoot, Path: inputPath, Kind: kind}, false
	}
	for leaf := range comp.PropsPaths {
		if strings.HasPrefix(leaf, path+".") {
			return aot.InputContract{}, true
		}
	}
	if comp.Scope != nil {
		for leaf := range comp.Scope.SourcePropsPaths {
			if strings.HasPrefix(leaf, path+".") {
				return aot.InputContract{}, true
			}
		}
	}
	return aot.InputContract{}, false
}

func aotInternInputs(inputs []aot.InputContract) []aot.InputContract {
	compare := func(a, b aot.InputContract) int {
		if n := strings.Compare(a.Source, b.Source); n != 0 {
			return n
		}
		if n := strings.Compare(a.Root, b.Root); n != 0 {
			return n
		}
		if n := slices.Compare(a.Path, b.Path); n != 0 {
			return n
		}
		return strings.Compare(string(a.Kind), string(b.Kind))
	}
	slices.SortFunc(inputs, compare)
	result := []aot.InputContract{}
	for _, input := range inputs {
		if len(result) > 0 && compare(result[len(result)-1], input) == 0 {
			result[len(result)-1].Exprs = append(result[len(result)-1].Exprs, input.Exprs...)
		} else {
			input.ID = uint32(len(result))
			result = append(result, input)
		}
	}
	for i := range result {
		slices.Sort(result[i].Exprs)
	}
	return result
}
