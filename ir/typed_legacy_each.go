//go:build !tinygo

package ir

import (
	"fmt"
	"strings"

	"m31labs.dev/gosx/internal/strictcomponent"
)

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
	var visit func(NodeID, *eachScope)
	visit = func(id NodeID, scope *eachScope) {
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
				if sourceOK {
					itemScope := &eachScope{
						parent:    scope,
						itemName:  itemName,
						itemType:  elem,
						indexName: indexName,
						reads:     make(map[string]string),
					}
					for _, child := range node.Children {
						visit(child, itemScope)
					}
					return
				}
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
	if ofCount != 1 || asCount != 1 || indexCount > 1 || ofExpr == "" || itemName == "" {
		return "", "", "", false
	}
	return itemName, indexName, ofExpr, true
}

// resolveTypedLegacyEachSource resolves the receiver path of a typed legacy
// loop source. Method calls in the source (for example
// props.Players.filter(...)) do not change the element type for the built-in
// filtering idiom, so the maximal props/binding-rooted selector is enough to
// prove the []T receiver. map-like transforms are intentionally left dynamic:
// their output element type cannot be recovered from the renderer schema.
func (l *lowerer) resolveTypedLegacyEachSource(span Span, componentName, propsType, source string, scope *eachScope) (string, bool) {
	if strings.Contains(source, ".map(") || strings.Contains(source, ".flatMap(") {
		return "", false
	}
	rooted := strictcomponent.ServerExpressionRootedPaths(source, scope.strictScope())
	var candidate *strictcomponent.RootedPath
	for i := range rooted {
		if rooted[i].Root == "props" {
			candidate = &rooted[i]
			break
		}
		if candidate == nil && scope != nil {
			if _, _, found := scope.resolve(rooted[i].Root); found {
				candidate = &rooted[i]
			}
		}
	}
	if candidate == nil || len(candidate.Path) == 0 {
		return "", false
	}
	path := candidate.Path
	// ServerExpressionRootedPaths intentionally reports the maximal selector
	// chain, so a filter receiver appears as props.Players.filter. The method
	// is a collection operation, not a struct field; remove only this known
	// non-transforming suffix before walking the declared schema.
	if len(path) > 1 && strings.Contains(source, ".filter(") && path[len(path)-1] == "filter" {
		path = path[:len(path)-1]
	}
	rootType := ""
	if candidate.Root == "props" {
		rootType = propsBaseType(propsType)
	} else if scope != nil {
		var isIndex, found bool
		rootType, isIndex, found = scope.resolve(candidate.Root)
		if !found || isIndex {
			return "", false
		}
	} else {
		return "", false
	}
	res := l.walkStrictHops(candidate.Root, rootType, path)
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

func (l *lowerer) validateTypedLegacyBindingExpressions(span Span, source, componentName string, scope *eachScope) {
	for _, rooted := range strictcomponent.ServerExpressionRootedPaths(source, scope.strictScope()) {
		if rooted.Root == "props" {
			continue
		}
		l.validateTypedLegacyBindingRead(span, componentName, scope, rooted.Root, rooted.Path)
	}
}

func (l *lowerer) validateTypedLegacyBindingRead(span Span, componentName string, scope *eachScope, root string, path []string) {
	if scope == nil {
		return
	}
	itemType, isIndex, found := scope.resolve(root)
	if !found {
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
