//go:build !tinygo

package ir

import (
	"fmt"
	"go/ast"
	"go/parser"
	"strings"

	"m31labs.dev/gosx/internal/strictcomponent"
)

// typedLegacyEachScope is deliberately separate from eachScope: strict loops
// are always schema-backed, while a legacy loop can bind a value whose
// element type the compiler cannot prove. opaque levels still shadow outer
// bindings, matching the runtime's lexical scope, but their selectors remain
// under the legacy dynamic contract.
type typedLegacyEachScope struct {
	parent    *typedLegacyEachScope
	itemName  string
	itemType  string
	indexName string
	opaque    bool
}

func (s *typedLegacyEachScope) strictScope() strictcomponent.Scope {
	var items, indices []string
	for cur := s; cur != nil; cur = cur.parent {
		items = append(items, cur.itemName)
		if cur.indexName != "" {
			indices = append(indices, cur.indexName)
		}
	}
	return strictcomponent.Scope{Items: items, Indices: indices}
}

func (s *typedLegacyEachScope) resolve(name string) (itemType string, isIndex, opaque, ok bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if cur.itemName == name {
			return cur.itemType, false, cur.opaque, true
		}
		if cur.indexName != "" && cur.indexName == name {
			return "", true, cur.opaque, true
		}
	}
	return "", false, false, false
}

// validateTypedLegacyEachBindings applies the same schema-aware binding
// resolution used by strict components to the one legacy shape whose element
// type is statically knowable: a legacy renderer whose props parameter names
// a struct declared in this .gsx file. Legacy renderers intentionally keep
// their dynamic expression language, so this pass does not attempt to
// validate arbitrary props expressions or map-backed legacy components. It
// only closes the proven gap where a typed []T loop body can spell a field
// that the generated Go would compile in a legacy stub but the file renderer
// cannot resolve through reflect.FieldByName at runtime.
func (l *lowerer) validateTypedLegacyEachBindings(root NodeID, componentName, propsType string) {
	seen := make(map[NodeID]bool)
	_, strictEachShadowed := l.strictNames["Each"]
	_, legacyEachShadowed := l.legacyNames["Each"]
	eachShadowed := strictEachShadowed || legacyEachShadowed
	var visit func(NodeID, *typedLegacyEachScope)
	visit = func(id NodeID, scope *typedLegacyEachScope) {
		if seen[id] || int(id) >= len(l.prog.Nodes) {
			return
		}
		seen[id] = true
		node := &l.prog.Nodes[id]
		isBuiltinEach := node.Kind == NodeComponent && node.Tag == "Each" && !eachShadowed
		if isBuiltinEach {
			itemName, indexName, ofExpr, shapeOK := l.typedLegacyEachShape(node)
			if shapeOK {
				elem, sourceOK := l.resolveTypedLegacyEachSource(node.Span, componentName, propsType, ofExpr, scope)
				itemScope := &typedLegacyEachScope{
					parent:    scope,
					itemName:  itemName,
					itemType:  elem,
					indexName: indexName,
					opaque:    !sourceOK,
				}
				for _, child := range node.Children {
					visit(child, itemScope)
				}
				return
			}
		}
		if node.Kind == NodeExpr {
			l.validateTypedLegacyBindingExpressions(node.Span, node.Text, componentName, scope)
		}
		for _, attr := range node.Attrs {
			if attr.Kind == AttrExpr && !(isBuiltinEach && attr.Name == "of") {
				l.validateTypedLegacyBindingExpressions(node.Span, attr.Expr, componentName, scope)
			}
		}
		for _, child := range node.Children {
			visit(child, scope)
		}
	}
	visit(root, nil)
}

// typedLegacyEachShape extracts only the binding shape needed by the
// schema-aware pass. Unlike strictEachShape it deliberately does not reject
// legacy-only attributes such as fallback, and it stays silent for dynamic or
// malformed shapes so the legacy runtime contract remains intact outside the
// statically provable path.
func (l *lowerer) typedLegacyEachShape(node *Node) (itemName, indexName, ofExpr string, ok bool) {
	if node == nil {
		return "", "", "", false
	}
	var ofCount, asCount, indexCount int
	for _, attr := range node.Attrs {
		if attr.Kind == AttrSpread {
			return "", "", "", false
		}
		switch attr.Name {
		case "of":
			ofCount++
			if attr.Kind == AttrExpr {
				ofExpr = strings.TrimSpace(attr.Expr)
			}
		case "as":
			asCount++
			if attr.Kind == AttrStatic {
				itemName = strings.TrimSpace(attr.Value)
			}
		case "index":
			indexCount++
			if attr.Kind == AttrStatic {
				indexName = strings.TrimSpace(attr.Value)
			}
		}
	}
	if asCount == 0 {
		itemName = "item"
	}
	if ofCount != 1 || asCount > 1 || indexCount > 1 || ofExpr == "" || itemName == "" {
		return "", "", "", false
	}
	return itemName, indexName, ofExpr, true
}

// resolveTypedLegacyEachSource resolves only the structural receiver of a
// collection expression. In particular, filter predicate reads are never
// candidates for the collection schema. Parentheses, comments, and spacing
// around a filter call do not affect the Go AST; map-like transforms remain
// opaque because they can change the output element type.
func (l *lowerer) resolveTypedLegacyEachSource(span Span, componentName, propsType, source string, scope *typedLegacyEachScope) (string, bool) {
	root, path, ok := typedLegacyCollectionSelector(source)
	if !ok || len(path) == 0 {
		return "", false
	}
	rootType := ""
	if root == "props" {
		rootType = propsBaseType(propsType)
	} else if scope != nil {
		var isIndex, opaque, found bool
		rootType, isIndex, opaque, found = scope.resolve(root)
		if !found || isIndex || opaque {
			return "", false
		}
	} else {
		return "", false
	}
	if l.typedLegacyPathBecomesOpaque(rootType, path) {
		return "", false
	}
	res := l.walkStrictHops(root, rootType, path)
	if res.failKind != strictHopOK {
		l.errs = append(l.errs, Diagnostic{
			Span:    span,
			Message: componentHopMessage("typed legacy component", componentName, res),
			Hint:    strictHopHint(res),
		})
		return "", false
	}
	trimmed := strings.TrimSpace(res.leafType)
	if !strings.HasPrefix(trimmed, "[]") {
		return "", false
	}
	elem := strings.TrimSpace(strings.TrimPrefix(trimmed, "[]"))
	if elem == "" || strings.HasPrefix(elem, "*") {
		return "", false
	}
	if _, declared := l.structTypes[elem]; !declared {
		// Legacy <Each> remains dynamic for scalar, pointer, named, or
		// cross-file element types. There is no typed selector boundary to
		// prove in those cases; preserve the existing runtime contract.
		return "", false
	}
	return elem, true
}

// typedLegacyPathBecomesOpaque reports whether a selector leaves a known
// struct schema through a map or interface before its final hop. Those values
// intentionally retain legacy key-selection semantics, so the checker must
// stop proving the path rather than reinterpret the next key as a Go struct
// field. Structural failures before that dynamic boundary still fall through
// to walkStrictHops and keep their existing diagnostics.
func (l *lowerer) typedLegacyPathBecomesOpaque(rootType string, path []string) bool {
	currentType := strings.TrimSpace(rootType)
	for i, field := range path {
		if typedLegacyDynamicSelectorType(currentType) {
			return true
		}
		fields, isStruct := l.structTypes[currentType]
		if !isStruct {
			return false
		}
		fieldType, known := fields[field]
		if !known {
			return false
		}
		trimmed := strings.TrimSpace(fieldType)
		if i == len(path)-1 {
			return false
		}
		if typedLegacyDynamicSelectorType(trimmed) {
			return true
		}
		if _, isStruct := l.structTypes[trimmed]; !isStruct {
			return false
		}
		currentType = trimmed
	}
	return false
}

func typedLegacyDynamicSelectorType(typeText string) bool {
	expr, err := parser.ParseExpr(strings.TrimSpace(typeText))
	if err != nil {
		return false
	}
	switch node := typedLegacyUnwrapParens(expr).(type) {
	case *ast.MapType, *ast.InterfaceType:
		return true
	case *ast.Ident:
		return node.Name == "any"
	default:
		return false
	}
}

func typedLegacyCollectionSelector(source string) (root string, path []string, ok bool) {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return "", nil, false
	}
	expr = typedLegacyUnwrapParens(expr)
	for {
		call, isCall := expr.(*ast.CallExpr)
		if !isCall {
			break
		}
		fun := typedLegacyUnwrapParens(call.Fun)
		sel, isSelector := fun.(*ast.SelectorExpr)
		if !isSelector || sel.Sel == nil {
			return "", nil, false
		}
		switch sel.Sel.Name {
		case "filter":
			expr = typedLegacyUnwrapParens(sel.X)
		case "map", "flatMap":
			return "", nil, false
		default:
			return "", nil, false
		}
	}
	return typedLegacySelectorPath(expr)
}

func typedLegacySelectorPath(expr ast.Expr) (root string, path []string, ok bool) {
	switch node := typedLegacyUnwrapParens(expr).(type) {
	case *ast.Ident:
		return node.Name, nil, true
	case *ast.SelectorExpr:
		root, path, ok = typedLegacySelectorPath(node.X)
		if !ok || node.Sel == nil {
			return "", nil, false
		}
		return root, append(path, node.Sel.Name), true
	default:
		return "", nil, false
	}
}

func typedLegacyUnwrapParens(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

func (l *lowerer) validateTypedLegacyBindingExpressions(span Span, source, componentName string, scope *typedLegacyEachScope) {
	for _, rooted := range strictcomponent.ServerExpressionRootedPaths(source, scope.strictScope()) {
		if rooted.Root == "props" {
			continue
		}
		l.validateTypedLegacyBindingRead(span, componentName, scope, rooted.Root, rooted.Path)
	}
}

func (l *lowerer) validateTypedLegacyBindingRead(span Span, componentName string, scope *typedLegacyEachScope, root string, path []string) {
	if scope == nil {
		return
	}
	itemType, isIndex, opaque, found := scope.resolve(root)
	if !found {
		return
	}
	if opaque {
		return
	}
	if isIndex {
		l.errs = append(l.errs, Diagnostic{
			Span:    span,
			Message: fmt.Sprintf("typed legacy component %s cannot use index binding %s in a selector; the index is an int value", componentName, root),
		})
		return
	}
	res := l.walkStrictHops(root, itemType, path)
	if res.failKind != strictHopOK {
		l.errs = append(l.errs, Diagnostic{
			Span:    span,
			Message: componentHopMessage("typed legacy component", componentName, res),
			Hint:    strictHopHint(res),
		})
		return
	}
	if !strictRendererScalarType(res.leafType) {
		l.errs = append(l.errs, Diagnostic{
			Span:    span,
			Message: fmt.Sprintf("typed legacy component %s cannot render %s of type %s; loop selectors must reach an exact scalar field", componentName, res.pathText, res.leafType),
		})
	}
}
