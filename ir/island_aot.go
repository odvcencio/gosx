//go:build !tinygo

package ir

import (
	"fmt"
	"go/constant"
	"go/token"
	"slices"
	"strconv"
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
	identity := src.PackagePath + "." + comp.Name
	if src.PackagePath == "" {
		return aot.Unit{}, aotSourceError(comp, "component_identity", "an import path is required")
	}
	resolved, err := aotPackageSource(src)
	if err != nil {
		return aot.Unit{}, aotSourceError(comp, "source_type", err.Error())
	}
	src = resolved
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
					if aotSourceKind(src, typ) == "" {
						return fmt.Errorf("composed prop has an unproved scalar type")
					}
				}
				if err := visit(callee.Root, depth+1); err != nil {
					return err
				}
			}
		}
		if n.Kind == NodeExpr {
			if err := aotSourceTokens(src, comp, n.Text); err != nil {
				return err
			}
		}
		for _, attr := range n.Attrs {
			if attr.Expr != "" {
				if err := aotSourceTokens(src, comp, attr.Expr); err != nil {
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
			kind := aotSourceKind(src, sig.SourceType)
			if kind == "" || sig.aotConstructor == "" || src.aotBindings.declared[sig.aotConstructor] {
				return aot.Unit{}, aotSourceError(comp, "source_type", "signal "+sig.Name+" has an unproved source type")
			}
			if err := aotSourceTokens(src, comp, sig.InitExpr); err != nil {
				return aot.Unit{}, aotSourceError(comp, "source_literal", err.Error())
			}
			// The VM may preserve an unparsed initializer as a string. Admission
			// requires successful parsing, including generic calls and indexing.
			if _, _, err := ParseExpr(sig.InitExpr, mergedIslandScope(src, comp)); err != nil {
				return aot.Unit{}, aotSourceError(comp, "source_literal", "signal initializer has no proved expression lowering")
			}
			stateKinds[sig.Name] = kind
			c.Signals = append(c.Signals, aot.StateContract{Slot: uint32(slot), Name: sig.Name, Kind: kind})
		}
		for slot, computed := range comp.Scope.Computeds {
			kind := aotSourceKind(src, computed.ReturnType)
			if kind == "" || computed.aotConstructor == "" || src.aotBindings.declared[computed.aotConstructor] {
				return aot.Unit{}, aotSourceError(comp, "source_type", "computed "+computed.Name+" has an unproved source type")
			}
			if err := aotSourceTokens(src, comp, computed.BodyExpr); err != nil {
				return aot.Unit{}, aotSourceError(comp, "source_literal", err.Error())
			}
			stateKinds[computed.Name] = kind
			c.Computeds = append(c.Computeds, aot.StateContract{Slot: uint32(slot), Name: computed.Name, Kind: kind})
		}
		for _, handler := range comp.Scope.Handlers {
			for _, stmt := range handler.Statements {
				if err := aotSourceTokens(src, comp, stmt); err != nil {
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
		if err := aotScalarOperands(e, args); err != nil {
			return "", fmt.Errorf("expression %d: %w", id, err)
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
			// The VM reads decimal integers; Go also permits octal spelling.
			// Admit only literals whose exact values agree in both paths.
			vmValue, err := strconv.ParseInt(e.Value, 10, 64)
			goValue, exact := constant.Int64Val(constants[id])
			if err != nil || !exact || vmValue != goValue {
				return "", fmt.Errorf("expression %d has incompatible integer literal semantics", id)
			}
		case program.OpLitBool:
			kind = aot.Bool
		case program.OpSignalGet:
			kind = stateKinds[e.Value]
		case program.OpSignalSet:
			if len(args) == 1 && aotValueFits(args[0], constants[e.Operands[0]], stateKinds[e.Value]) {
				aotContextualConstant(p, e.Operands[0], kinds, constants, stateKinds[e.Value])
				kind, isPure = aot.AnyZero, false
			}
		case program.OpPropGet, program.OpIndex, program.OpEventGet:
			input, prefix := aotSourceInput(src, p, id, comp)
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
				aotContextualConstant(p, e.Operands[0], kinds, constants, args[1])
				kind = args[1]
			} else if right != nil && aotConstantFits(right, args[0]) {
				aotContextualConstant(p, e.Operands[1], kinds, constants, args[0])
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
		case program.OpCond:
			if len(args) != 3 || args[0] != aot.Bool {
				break
			}
			left, right := constants[e.Operands[1]], constants[e.Operands[2]]
			if left != nil && right == nil {
				if aotConstantFits(left, args[2]) {
					aotContextualConstant(p, e.Operands[1], kinds, constants, args[2])
					kind = args[2]
				}
			} else if right != nil && left == nil {
				if aotConstantFits(right, args[1]) {
					aotContextualConstant(p, e.Operands[2], kinds, constants, args[1])
					kind = args[1]
				}
			} else if args[1] == args[2] {
				switch args[1] {
				case aot.Int, aot.Int32, aot.Bool, aot.String:
					kind = args[1]
				}
			}
		case program.OpSeq:
			if len(args) > 0 {
				kind = args[len(args)-1]
			}
			if len(args) == 0 {
				kind = aot.AnyZero
			}
		case program.OpEq, program.OpNeq, program.OpLt, program.OpGt, program.OpLte, program.OpGte:
			if len(args) == 2 && (aotValueFits(args[0], constants[e.Operands[0]], args[1]) || aotValueFits(args[1], constants[e.Operands[1]], args[0])) {
				aotContextualConstant(p, e.Operands[0], kinds, constants, args[1])
				aotContextualConstant(p, e.Operands[1], kinds, constants, args[0])
				if e.Op == program.OpEq || e.Op == program.OpNeq || args[0] != aot.Bool {
					kind = aot.Bool
				}
			}
		case program.OpAnd, program.OpOr:
			if len(args) == 2 && args[0] == aot.Bool && args[1] == aot.Bool {
				kind = aot.Bool
			}
		case program.OpNot:
			if len(args) == 1 && args[0] == aot.Bool {
				kind = aot.Bool
			}
		case program.OpConcat, program.OpFormat, program.OpToString:
			kind = aot.String
		case program.OpLen:
			if len(args) == 1 && args[0] == aot.String {
				kind = aot.Int
			}
		}
		if kind == "" {
			return "", fmt.Errorf("expression %d has no supported scalar source evidence", id)
		}
		// Check exact literals and constant arithmetic before recording their
		// scalar kinds. Operand inference also validates both conditional arms
		// and every constant intermediate, even when a later result fits.
		if value := constants[id]; value != nil && !aotConstantFits(value, kind) {
			return "", fmt.Errorf("expression %d has an integer constant outside the signed int32 domain", id)
		}
		kinds[id], pure[id], visiting[id] = kind, isPure, false
		return kind, nil
	}
	for id := range p.Exprs {
		_, err := infer(program.ExprID(id))
		if err != nil {
			return aot.Unit{}, aotSourceError(comp, "source_type", err.Error())
		}
	}
	if err := aotScalarRoots(p, kinds, constants, stateKinds); err != nil {
		return aot.Unit{}, aotSourceError(comp, "source_type", err.Error())
	}
	for id := range p.Exprs {
		c.Expressions = append(c.Expressions, aot.ExpressionContract{Expr: program.ExprID(id), Kind: kinds[id], Pure: pure[id]})
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

func aotSourceKind(src *Program, name string) aot.ScalarKind {
	name = strings.TrimSpace(name)
	if !src.aotBindings.universe(name) {
		return ""
	}
	switch name {
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

func aotSourceTokens(src *Program, comp Component, source string) error {
	tokens, err := lexExpr(source)
	if err != nil {
		return err
	}
	for i, tok := range tokens {
		if tok.kind == tokenString && strings.HasPrefix(tok.text, "'") {
			return fmt.Errorf("rune literal is outside the scalar profile")
		}
		if tok.kind != tokenIdent {
			continue
		}
		member := i > 0 && tokens[i-1].kind == tokenDot
		call := i+1 < len(tokens) && tokens[i+1].kind == tokenLParen
		if !member && (tok.text == "true" || tok.text == "false") && !src.aotBindings.universe(tok.text) {
			return fmt.Errorf("boolean constant %s has no universe binding", tok.text)
		}
		if member && (strings.EqualFold(tok.text, "len") || strings.EqualFold(tok.text, "length")) {
			return fmt.Errorf("length alias has no proved Go binding")
		}
		if !call {
			continue
		}
		if !member {
			// Conversions and other calls are outside this profile. Do not let
			// the VM's initializer fallback turn them into certified strings.
			if tok.text != "len" || !src.aotBindings.universe(tok.text) {
				return fmt.Errorf("call %s has no supported universe binding", tok.text)
			}
			continue
		}
		proved := false
		if i >= 2 && tokens[i-2].kind == tokenIdent && (i < 3 || tokens[i-3].kind != tokenDot) && comp.Scope != nil {
			receiver := tokens[i-2].text
			for _, sig := range comp.Scope.Signals {
				if sig.aotConstructor != "" && (receiver == sig.Local || receiver == sig.Name) && (tok.text == "Get" || tok.text == "Set") {
					proved = true
				}
			}
			for _, computed := range comp.Scope.Computeds {
				if computed.aotConstructor != "" && receiver == computed.Name && tok.text == "Get" {
					proved = true
				}
			}
		}
		if !proved {
			return fmt.Errorf("method %s has no proved scalar receiver", tok.text)
		}
	}
	return nil
}

func aotSourceError(comp Component, code, message string) error {
	return NewDiagnosticsError("island-aot", []Diagnostic{{Span: comp.Span, Code: "aot_" + code, Message: message}})
}

func aotSourceInput(src *Program, p *program.Program, id program.ExprID, comp Component) (aot.InputContract, bool) {
	e := p.Exprs[id]
	if e.Op == program.OpEventGet {
		// Event codec types are compiler-owned, not names in Go source scope.
		kind := map[program.ExprType]aot.ScalarKind{program.TypeString: aot.String, program.TypeInt: aot.Int, program.TypeBool: aot.Bool}[eventFieldType(e.Value)]
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
	if root == "props" {
		root, keys = keys[0], keys[1:]
	}
	path := strings.Join(append([]string{root}, keys...), ".")
	typ := comp.PropsFields[root]
	if len(keys) > 0 {
		typ = comp.PropsPaths[path]
	}
	if kind := aotSourceKind(src, typ); kind != "" {
		return aot.InputContract{Source: "prop", Root: root, Path: keys, Kind: kind}, false
	}
	for leaf := range comp.PropsPaths {
		if strings.HasPrefix(leaf, path+".") {
			return aot.InputContract{}, true
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
