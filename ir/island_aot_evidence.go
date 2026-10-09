//go:build !tinygo

package ir

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"m31labs.dev/gosx/island/aot"
	"m31labs.dev/gosx/island/program"
)

type aotExpressionProof struct {
	kind  aot.ScalarKind
	value constant.Value
	input *aot.InputContract
	pure  bool
}
type aotEvidence struct {
	checked        *aotCheckedSource
	component      Component
	props          types.Object
	propsObjects   map[types.Object]bool
	events         map[types.Object]string
	states         map[types.Object]string
	stateKinds     map[string]aot.ScalarKind
	declarations   map[string]ast.Expr
	handlers       map[string]*ast.FuncLit
	handlerObjects map[string]types.Object
	proofs         map[program.ExprID]aotExpressionProof
	shared         bool
}

func aotGoKind(typ types.Type) aot.ScalarKind {
	if typ == nil {
		return ""
	}
	basic, ok := types.Unalias(types.Default(typ)).(*types.Basic)
	if !ok {
		return ""
	}
	switch basic.Kind() {
	case types.Int:
		return aot.Int
	case types.Int32:
		return aot.Int32
	case types.Bool:
		return aot.Bool
	case types.String:
		return aot.String
	default:
		return ""
	}
}

func newAOTEvidence(c *aotCheckedSource, src *Program, comp Component) (*aotEvidence, error) {
	e := &aotEvidence{checked: c, component: comp, states: map[types.Object]string{}, stateKinds: map[string]aot.ScalarKind{}, declarations: map[string]ast.Expr{}, handlers: map[string]*ast.FuncLit{}, handlerObjects: map[string]types.Object{}, proofs: map[program.ExprID]aotExpressionProof{}, propsObjects: map[types.Object]bool{}, events: map[types.Object]string{}}
	fn := c.functions[comp.Name]
	if fn == nil {
		return nil, fmt.Errorf("evidence_shape_mismatch: candidate function missing")
	}
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			for _, name := range field.Names {
				if name.Name == comp.PropsName {
					e.props = c.info.Defs[name]
				}
			}
		}
	}
	for _, component := range src.Components {
		function := c.functions[component.Name]
		if function == nil || function.Type.Params == nil {
			continue
		}
		for _, field := range function.Type.Params.List {
			for _, name := range field.Names {
				if name.Name == component.PropsName {
					if obj := c.info.Defs[name]; obj != nil {
						e.propsObjects[obj] = true
					}
				}
			}
		}
	}
	assignments := map[string]*ast.AssignStmt{}
	for _, stmt := range fn.Body.List {
		if assign, ok := stmt.(*ast.AssignStmt); ok && len(assign.Lhs) == 1 && len(assign.Rhs) == 1 {
			if name, ok := assign.Lhs[0].(*ast.Ident); ok {
				assignments[name.Name] = assign
			}
		}
	}
	if comp.Scope == nil {
		return e, nil
	}
	constructor := func(local string) (*ast.CallExpr, types.Object, aot.ScalarKind, error) {
		assign := assignments[local]
		if assign == nil {
			return nil, nil, "", fmt.Errorf("evidence_shape_mismatch: declaration %s missing", local)
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return nil, nil, "", fmt.Errorf("evidence_shape_mismatch: constructor missing")
		}
		ident := aotCalleeIdent(call.Fun)
		function, ok := c.info.Uses[ident].(*types.Func)
		if !ok {
			return nil, nil, "", fmt.Errorf("evidence_binding_mismatch: constructor is not an allowlisted function")
		}
		switch function.FullName() {
		case signalImportPath + ".New", signalImportPath + ".NewShared", signalImportPath + ".Shared", signalImportPath + ".Derive":
		default:
			return nil, nil, "", fmt.Errorf("evidence_binding_mismatch: constructor %s", function.FullName())
		}
		instance, ok := c.info.Instances[ident]
		if !ok || instance.TypeArgs.Len() != 1 {
			return nil, nil, "", fmt.Errorf("type_error: constructor instance missing")
		}
		target := instance.TypeArgs.At(0)
		kind := aotGoKind(target)
		if kind == "" {
			return nil, nil, "", fmt.Errorf("unsupported_type: constructor %s", target)
		}
		object := c.info.Defs[assign.Lhs[0].(*ast.Ident)]
		if function.Name() == "Derive" {
			if len(call.Args) != 1 {
				return nil, nil, "", fmt.Errorf("type_error: derive arity")
			}
			body, ok := call.Args[0].(*ast.FuncLit)
			if !ok {
				return nil, nil, "", fmt.Errorf("evidence_shape_mismatch: derive function missing")
			}
			signature, ok := c.info.Types[body].Type.(*types.Signature)
			if !ok || signature.Params().Len() != 0 || signature.Results().Len() != 1 || !types.Identical(signature.Results().At(0).Type(), target) {
				return nil, nil, "", fmt.Errorf("type_error: derive signature")
			}
		} else {
			if function.Name() != "New" {
				e.shared = true
			}
			if len(call.Args) == 0 || !types.Identical(c.info.Types[call.Args[len(call.Args)-1]].Type, target) {
				return nil, nil, "", fmt.Errorf("type_error: initializer differs from type argument")
			}
		}
		return call, object, kind, nil
	}
	for _, sig := range comp.Scope.Signals {
		call, obj, kind, err := constructor(sig.Local)
		if err != nil {
			return nil, err
		}
		e.states[obj] = sig.Name
		e.stateKinds[sig.Name] = kind
		e.declarations["signal/"+sig.Local] = call.Args[len(call.Args)-1]
	}
	for _, computed := range comp.Scope.Computeds {
		call, obj, kind, err := constructor(computed.Name)
		if err != nil {
			return nil, err
		}
		body, ok := call.Args[0].(*ast.FuncLit)
		if !ok {
			return nil, fmt.Errorf("evidence_shape_mismatch: computed constructor")
		}
		if len(body.Body.List) != 1 {
			return nil, fmt.Errorf("evidence_shape_mismatch: computed body")
		}
		ret, ok := body.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return nil, fmt.Errorf("evidence_shape_mismatch: computed result")
		}
		e.states[obj] = computed.Name
		e.stateKinds[computed.Name] = kind
		e.declarations["computed/"+computed.Name] = ret.Results[0]
	}
	for _, handler := range comp.Scope.Handlers {
		assign := assignments[handler.Name]
		if assign == nil {
			return nil, fmt.Errorf("evidence_shape_mismatch: handler declaration")
		}
		fn, ok := assign.Rhs[0].(*ast.FuncLit)
		if !ok {
			return nil, fmt.Errorf("evidence_shape_mismatch: handler function")
		}
		if fn.Type.Params != nil {
			for _, field := range fn.Type.Params.List {
				for _, name := range field.Names {
					if slices.Contains(islandEventFields, name.Name) {
						e.events[c.info.Defs[name]] = name.Name
					}
				}
			}
		}
		e.handlers[handler.Name] = fn
		e.handlerObjects[handler.Name] = c.info.Defs[assign.Lhs[0].(*ast.Ident)]
	}
	return e, nil
}

func aotCalleeIdent(expr ast.Expr) *ast.Ident {
	switch n := expr.(type) {
	case *ast.IndexExpr:
		return aotCalleeIdent(n.X)
	case *ast.IndexListExpr:
		return aotCalleeIdent(n.X)
	case *ast.SelectorExpr:
		return n.Sel
	case *ast.Ident:
		return n
	}
	return nil
}

func (e *aotEvidence) sourceNode(origin islandExprOrigin) (ast.Expr, error) {
	if node := e.declarations[origin.declaration]; node != nil {
		return node, nil
	}
	if strings.HasPrefix(origin.declaration, "handler/") {
		fn := e.handlers[strings.TrimPrefix(origin.declaration, "handler/")]
		if fn != nil && origin.statement < len(fn.Body.List) {
			if stmt, ok := fn.Body.List[origin.statement].(*ast.ExprStmt); ok {
				return stmt.X, nil
			}
		}
		return nil, fmt.Errorf("evidence_shape_mismatch: handler statement")
	}
	region, ok := e.checked.projection.regions[origin.span]
	if !ok {
		return nil, fmt.Errorf("evidence_shape_mismatch: source span missing")
	}
	var result ast.Expr
	ast.Inspect(e.checked.file, func(node ast.Node) bool {
		if candidate, ok := node.(ast.Expr); ok && e.checked.fset.Position(candidate.Pos()).Offset >= region.start && e.checked.fset.Position(candidate.End()).Offset <= region.end {
			result = candidate
			return false
		}
		return result == nil
	})
	if result == nil {
		return nil, fmt.Errorf("evidence_shape_mismatch: checked expression missing")
	}
	return result, nil
}

// Parse the same authored span with go/parser, then pair both ASTs and the
// restricted opcode tree in lockstep. Object identities always come from the
// checked AST, never from ParseExpr's name registry.
func (e *aotEvidence) pair(source string, origin islandExprOrigin, exprs []program.Expr, root program.ExprID) (func(program.ExprID, program.ExprID), error) {
	node, err := e.sourceNode(origin)
	if err != nil {
		return nil, err
	}
	parsed, err := parser.ParseExprFrom(token.NewFileSet(), "expression.go", source, 0)
	if err != nil {
		return nil, fmt.Errorf("evidence_shape_mismatch: %w", err)
	}
	proofs := map[program.ExprID]aotExpressionProof{}
	var visit func(program.ExprID, ast.Expr, ast.Expr) error
	visit = func(id program.ExprID, native, checked ast.Expr) error {
		for {
			p, ok := native.(*ast.ParenExpr)
			if !ok {
				break
			}
			native = p.X
		}
		for {
			p, ok := checked.(*ast.ParenExpr)
			if !ok {
				break
			}
			checked = p.X
		}
		if int(id) >= len(exprs) {
			return fmt.Errorf("evidence_shape_mismatch: missing opcode")
		}
		op := exprs[id]
		value := e.checked.info.Types[checked]
		proof := aotExpressionProof{kind: aotGoKind(value.Type), value: value.Value, pure: true}
		child := func(index int, a, b ast.Expr) error {
			if index >= len(op.Operands) {
				return fmt.Errorf("evidence_shape_mismatch: missing operand")
			}
			return visit(op.Operands[index], a, b)
		}
		mismatch := func() error { return fmt.Errorf("evidence_shape_mismatch: opcode %d differs from %T", op.Op, checked) }
		switch op.Op {
		case program.OpLitInt:
			if !aotSameLeaf(native, checked) {
				return mismatch()
			}
			if _, ok := native.(*ast.BasicLit); !ok {
				if unary, ok := native.(*ast.UnaryExpr); !ok || unary.Op != token.SUB {
					return mismatch()
				}
			}
			actual, err := strconv.ParseInt(op.Value, 10, 64)
			var exact int64
			ok := false
			if value.Value != nil && value.Value.Kind() == constant.Int {
				exact, ok = constant.Int64Val(value.Value)
			}
			if err != nil || !ok || actual != exact {
				return mismatch()
			}
		case program.OpLitString:
			if !aotSameLeaf(native, checked) {
				return mismatch()
			}
			literal, ok := native.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING || value.Value == nil || value.Value.Kind() != constant.String || constant.StringVal(value.Value) != op.Value {
				return mismatch()
			}
		case program.OpLitBool:
			if !aotSameLeaf(native, checked) {
				return mismatch()
			}
			name, ok := native.(*ast.Ident)
			if !ok || name.Name != op.Value || value.Value == nil || value.Value.Kind() != constant.Bool || strconv.FormatBool(constant.BoolVal(value.Value)) != op.Value {
				return mismatch()
			}
		case program.OpEventGet:
			nativeName, ok := native.(*ast.Ident)
			checkedName, ok2 := checked.(*ast.Ident)
			if !ok || !ok2 || nativeName.Name != op.Value || e.events[e.checked.info.Uses[checkedName]] != op.Value {
				return fmt.Errorf("evidence_binding_mismatch: event parameter")
			}
			proof.input = &aot.InputContract{Source: "event", Root: op.Value, Kind: proof.kind}
		case program.OpPropGet:
			name, ok := native.(*ast.Ident)
			bound, ok2 := checked.(*ast.Ident)
			if !ok || !ok2 || name.Name != op.Value || op.Value != "props" || !e.propsObjects[e.checked.info.Uses[bound]] {
				return fmt.Errorf("evidence_binding_mismatch: prop receiver")
			}
			proof.kind = aot.SelectorPath
		case program.OpIndex:
			selector, ok := native.(*ast.SelectorExpr)
			bound, ok2 := checked.(*ast.SelectorExpr)
			if !ok || !ok2 || selector.Sel.Name != bound.Sel.Name || len(op.Operands) != 2 {
				return mismatch()
			}
			selection := e.checked.info.Selections[bound]
			if selection == nil || selection.Kind() != types.FieldVal || selection.Indirect() {
				return fmt.Errorf("evidence_binding_mismatch: selector is not a value struct field")
			}
			key := op.Operands[1]
			if int(key) >= len(exprs) || exprs[key].Op != program.OpLitString || exprs[key].Value != selection.Obj().Name() {
				return mismatch()
			}
			proofs[key] = aotExpressionProof{kind: aotGoKind(types.Typ[types.String]), value: constant.MakeString(selection.Obj().Name()), pure: true}
			if err := child(0, selector.X, bound.X); err != nil {
				return err
			}
			input, err := e.input(bound)
			if err != nil {
				return err
			}
			if proof.kind == "" {
				if _, ok := types.Unalias(value.Type).Underlying().(*types.Struct); ok {
					proof.kind = aot.SelectorPath
				}
			} else {
				input.Kind = proof.kind
				proof.input = &input
			}
		case program.OpSignalGet, program.OpSignalSet, program.OpLen:
			call, ok := native.(*ast.CallExpr)
			bound, ok2 := checked.(*ast.CallExpr)
			if !ok || !ok2 {
				return mismatch()
			}
			if op.Op == program.OpLen {
				name, ok := bound.Fun.(*ast.Ident)
				if !ok || e.checked.info.Uses[name] != types.Universe.Lookup("len") || len(call.Args) != 1 {
					return fmt.Errorf("evidence_binding_mismatch: len")
				}
			} else {
				selector, ok := bound.Fun.(*ast.SelectorExpr)
				nativeSelector, nativeOK := call.Fun.(*ast.SelectorExpr)
				if !ok || !nativeOK || nativeSelector.Sel.Name != selector.Sel.Name || !aotSameLeaf(nativeSelector.X, selector.X) {
					return mismatch()
				}
				receiver, ok := selector.X.(*ast.Ident)
				if !ok || e.states[e.checked.info.Uses[receiver]] != op.Value {
					return fmt.Errorf("evidence_binding_mismatch: signal receiver")
				}
				selection := e.checked.info.Selections[selector]
				if selection == nil || selection.Obj().Pkg() == nil || selection.Obj().Pkg().Path() != signalImportPath {
					return fmt.Errorf("evidence_binding_mismatch: signal method")
				}
				method := selection.Obj().Name()
				if op.Op == program.OpSignalGet && method != "Get" || op.Op == program.OpSignalSet && method != "Set" {
					return mismatch()
				}
				if op.Op == program.OpSignalSet {
					tuple, ok := value.Type.(*types.Tuple)
					if !ok || tuple.Len() != 0 {
						return mismatch()
					}
					proof.kind = aot.AnyZero
					proof.pure = false
				}
			}
			if len(call.Args) != len(op.Operands) || len(bound.Args) != len(call.Args) {
				return mismatch()
			}
			for i := range call.Args {
				if err := child(i, call.Args[i], bound.Args[i]); err != nil {
					return err
				}
			}
		case program.OpNeg, program.OpNot:
			unary, ok := native.(*ast.UnaryExpr)
			bound, ok2 := checked.(*ast.UnaryExpr)
			want := token.SUB
			if op.Op == program.OpNot {
				want = token.NOT
			}
			if !ok || !ok2 || unary.Op != want || bound.Op != want || len(op.Operands) != 1 {
				return mismatch()
			}
			if err := child(0, unary.X, bound.X); err != nil {
				return err
			}
		case program.OpAdd, program.OpSub, program.OpMul, program.OpEq, program.OpNeq, program.OpLt, program.OpLte, program.OpGt, program.OpGte, program.OpAnd, program.OpOr:
			binary, ok := native.(*ast.BinaryExpr)
			bound, ok2 := checked.(*ast.BinaryExpr)
			want := map[program.OpCode]token.Token{program.OpAdd: token.ADD, program.OpSub: token.SUB, program.OpMul: token.MUL, program.OpEq: token.EQL, program.OpNeq: token.NEQ, program.OpLt: token.LSS, program.OpLte: token.LEQ, program.OpGt: token.GTR, program.OpGte: token.GEQ, program.OpAnd: token.LAND, program.OpOr: token.LOR}[op.Op]
			if !ok || !ok2 || binary.Op != want || bound.Op != want || len(op.Operands) != 2 {
				return mismatch()
			}
			if err := child(0, binary.X, bound.X); err != nil {
				return err
			}
			if err := child(1, binary.Y, bound.Y); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported_operation: opcode %d", op.Op)
		}
		if proof.kind == "" {
			return fmt.Errorf("unsupported_type: %v", value.Type)
		}
		if proof.value != nil && (proof.kind == aot.Int || proof.kind == aot.Int32) && !aotConstantFits(proof.value, proof.kind) {
			return fmt.Errorf("constant_range: signed int32 domain")
		}
		for _, operand := range op.Operands {
			if child, ok := proofs[operand]; ok {
				proof.pure = proof.pure && child.pure
			}
		}
		proofs[id] = proof
		return nil
	}
	if err := visit(root, parsed, node); err != nil {
		return nil, err
	}
	return func(local, destination program.ExprID) {
		if proof, ok := proofs[local]; ok {
			e.proofs[destination] = proof
		}
	}, nil
}

func aotSameLeaf(a, b ast.Expr) bool {
	for {
		p, ok := a.(*ast.ParenExpr)
		if !ok {
			break
		}
		a = p.X
	}
	for {
		p, ok := b.(*ast.ParenExpr)
		if !ok {
			break
		}
		b = p.X
	}
	switch x := a.(type) {
	case *ast.BasicLit:
		y, ok := b.(*ast.BasicLit)
		return ok && x.Kind == y.Kind && x.Value == y.Value
	case *ast.Ident:
		y, ok := b.(*ast.Ident)
		return ok && x.Name == y.Name
	case *ast.UnaryExpr:
		y, ok := b.(*ast.UnaryExpr)
		return ok && x.Op == y.Op && aotSameLeaf(x.X, y.X)
	}
	return false
}

func (e *aotEvidence) input(expr *ast.SelectorExpr) (aot.InputContract, error) {
	var fields []string
	var root ast.Expr = expr
	for {
		selector, ok := root.(*ast.SelectorExpr)
		if !ok {
			break
		}
		selection := e.checked.info.Selections[selector]
		if selection == nil || selection.Kind() != types.FieldVal || selection.Indirect() {
			return aot.InputContract{}, fmt.Errorf("evidence_binding_mismatch: selector path")
		}
		fields = append([]string{selection.Obj().Name()}, fields...)
		root = selector.X
	}
	name, ok := root.(*ast.Ident)
	if !ok || !e.propsObjects[e.checked.info.Uses[name]] || len(fields) == 0 {
		return aot.InputContract{}, fmt.Errorf("evidence_binding_mismatch: selector root")
	}
	return aot.InputContract{Source: "prop", Root: fields[0], Path: fields[1:]}, nil
}

func (e *aotEvidence) validateSourceRoots(src *Program, names map[string]bool) error {
	seen := map[NodeID]bool{}
	var visit func(NodeID, Component) error
	visit = func(id NodeID, owner Component) error {
		if seen[id] {
			return nil
		}
		seen[id] = true
		n := src.Nodes[id]
		check := func(source string, span Span) error {
			exprs, root, err := ParseExpr(source, mergedIslandScope(src, owner))
			if err != nil {
				return fmt.Errorf("evidence_shape_mismatch: %w", err)
			}
			_, err = e.pair(source, islandExprOrigin{span: span}, exprs, root)
			return err
		}
		if n.Kind == NodeExpr {
			if _, projection := islandProjectionExpression(n.Text); !projection {
				if err := check(n.Text, n.Span); err != nil {
					return err
				}
			}
		}
		for _, attr := range n.Attrs {
			if attr.Kind == AttrStatic {
				if _, inline := legacyInlineEventType(attr.Name); inline {
					return fmt.Errorf("evidence_shape_mismatch: inline string handler has no Go expression span")
				}
				continue
			}
			if attr.IsEvent {
				node, err := e.sourceNode(islandExprOrigin{span: attr.Span})
				if err != nil {
					return err
				}
				ident, ok := node.(*ast.Ident)
				handler := e.handlers[attr.Expr]
				if !ok || handler == nil || e.checked.info.Uses[ident] == nil || !types.Identical(e.checked.info.Uses[ident].Type(), e.checked.info.Types[handler].Type) {
					return fmt.Errorf("evidence_binding_mismatch: event handler")
				}
				if e.checked.info.Uses[ident] != e.handlerObjects[attr.Expr] {
					return fmt.Errorf("evidence_binding_mismatch: event handler object")
				}

				continue
			}
			if attr.Expr != "" {
				if err := check(attr.Expr, attr.Span); err != nil {
					return err
				}
			}
		}
		for _, child := range n.Children {
			if err := visit(child, owner); err != nil {
				return err
			}
		}
		for _, name := range sortedNodeSlotNames(n.Slots) {
			if err := visit(n.Slots[name], owner); err != nil {
				return err
			}
		}
		return nil
	}
	for _, comp := range src.Components {
		if names[comp.Name] {
			if err := visit(comp.Root, comp); err != nil {
				return err
			}
		}
	}
	return nil
}
