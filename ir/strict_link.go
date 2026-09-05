//go:build !tinygo

package ir

import (
	"fmt"
	gotreesitter "github.com/odvcencio/gotreesitter"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

func (l *lowerer) isStrictBuiltinLink(tag string) bool {
	if tag != "Link" {
		return false
	}
	if _, ok := l.strictNames[tag]; ok {
		return false
	}
	if _, ok := l.legacyNames[tag]; ok {
		return false
	}
	return !l.isSharedComponentTag(tag)
}

func strictLinkAttrTypes(name string) string {
	switch name {
	case "href", "class", "className", "id", "title", "role", "lang", "dir", "style", "accesskey", "target", "rel", "hreflang", "type", "referrerpolicy", "ping", "current", "data-gosx-link-current", "data-gosx-link-current-policy", "data-gosx-link-state", "data-gosx-prefetch-state", "data-gosx-enhance", "data-gosx-enhance-layer", "data-gosx-fallback":
		return "string"
	case "prefetch", "data-gosx-prefetch", "download":
		return "string|bool"
	case "tabindex":
		return "string|int"
	case "hidden":
		return "bool"
	case "ariaCurrent", "tabIndex":
		return ""
	}
	if strings.HasPrefix(name, "data-") || strings.HasPrefix(name, "aria-") {
		return "scalar"
	}
	return ""
}

func (l *lowerer) validateStrictLinkCall(n *gotreesitter.Node, attrs []Attr) {
	seen := map[string]bool{}
	for _, attr := range attrs {
		// Parent slot partitioning validates and removes this routing marker.
		if attr.Name == "slot" {
			continue
		}
		if attr.Kind == AttrSpread {
			l.errorf(n, "strict <Link> does not accept spread attributes")
			continue
		}
		name := attr.Name
		if name == "className" {
			name = "class"
		}
		if seen[name] {
			l.errorf(n, "strict <Link> has duplicate attribute %q", name)
		}
		seen[name] = true
		if strictLinkAttrTypes(attr.Name) == "" {
			l.errorf(n, "strict <Link> does not support attribute %q; use canonical anchor attributes without event callbacks", attr.Name)
		}
	}
	if !seen["href"] {
		l.errorf(n, "strict <Link> requires a string href attribute")
	}
	// ManagedLink's typed href is emitted first in generated Go. Canonicalize
	// only this newly admitted strict builtin; legacy attribute order is intact.
	for i, attr := range attrs {
		if attr.Name == "href" {
			copy(attrs[1:i+1], attrs[:i])
			attrs[0] = attr
			break
		}
	}
}

func (l *lowerer) validateStrictLinkAttrType(span Span, attr Attr, propsType string, scope *eachScope) {
	if attr.Name == "slot" {
		l.errs = append(l.errs, Diagnostic{Span: span, Message: "slot attribute is only meaningful on a direct child of a component call"})
		return
	}
	want := strictLinkAttrTypes(attr.Name)
	if want == "" || attr.Kind == AttrSpread {
		return
	}
	got := ""
	switch attr.Kind {
	case AttrStatic:
		got = "string"
	case AttrBool:
		got = "bool"
	case AttrExpr:
		expression, err := parser.ParseExpr(attr.Expr)
		if err != nil {
			return
		}
		got = l.strictLinkExpressionType(expression, propsType, scope)
	}
	// Unknown field paths remain the package checker's precise diagnostics.
	if got == "" {
		return
	}
	allowed := want == "scalar" && strictRendererScalarType(got)
	for _, typ := range strings.Split(want, "|") {
		allowed = allowed || typ == got
	}
	if !allowed {
		l.errs = append(l.errs, Diagnostic{Span: span, Message: fmt.Sprintf("strict <Link> attribute %q requires %s, got %s", attr.Name, want, got)})
	}
}

func (l *lowerer) strictLinkExpressionType(e ast.Expr, propsType string, scope *eachScope) string {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return l.strictLinkExpressionType(e.X, propsType, scope)
	case *ast.BasicLit:
		switch e.Kind {
		case token.STRING:
			return "string"
		case token.CHAR:
			return "int32"
		case token.INT:
			return "int"
		case token.FLOAT:
			return "float64"
		}
	case *ast.Ident:
		if e.Name == "true" || e.Name == "false" {
			return "bool"
		}
		if _, index, ok := scope.resolve(e.Name); ok && index {
			return "int"
		}
	case *ast.UnaryExpr:
		return l.strictLinkExpressionType(e.X, propsType, scope)
	case *ast.BinaryExpr:
		if e.Op == token.ADD && l.strictLinkExpressionType(e.X, propsType, scope) == "string" && l.strictLinkExpressionType(e.Y, propsType, scope) == "string" {
			return "string"
		}
	case *ast.SelectorExpr:
		var path []string
		var current ast.Expr = e
		for {
			for {
				paren, ok := current.(*ast.ParenExpr)
				if !ok {
					break
				}
				current = paren.X
			}
			selector, ok := current.(*ast.SelectorExpr)
			if !ok {
				break
			}
			path = append([]string{selector.Sel.Name}, path...)
			current = selector.X
		}
		if root, ok := current.(*ast.Ident); ok {
			typ, _ := l.resolveScopedFieldType(propsType, scope, root.Name, path)
			return typ
		}
	}
	return ""
}
