package route

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"strings"

	"m31labs.dev/gosx/ir"
)

// RenderError locates a render failure in the authored GSX source.
type RenderError struct {
	Span       ir.Span
	Expression string
	Err        error
}

func (e *RenderError) Error() string {
	position := fmt.Sprintf("%d:%d", e.Span.StartLine, e.Span.StartCol)
	if e.Span.File != "" {
		position = e.Span.File + ":" + position
	}
	message := fmt.Sprintf("%s: %v", position, e.Err)
	if e.Expression != "" {
		message += "\n    expression: " + e.Expression
	}
	return message
}

func (e *RenderError) Unwrap() error { return e.Err }

func locateRenderError(err error, span ir.Span, expression string) error {
	var located *RenderError
	if err == nil || errors.As(err, &located) || span.StartLine == 0 {
		return err
	}
	return &RenderError{Span: span, Expression: expression, Err: err}
}

func locateAttrError(err error, attr ir.Attr) error {
	expression := attr.Expr
	if expression == "" {
		expression = attr.Name + "=" + attr.Value
	}
	return locateRenderError(err, attr.Span, expression)
}

func renderErrorFile(err error, file string) error {
	var located *RenderError
	if !errors.As(err, &located) {
		return fmt.Errorf("render %s: %w", file, err)
	}
	if located.Span.File != "" {
		return err
	}
	copy := *located
	copy.Span.File = file
	if prefix, ok := strings.CutSuffix(err.Error(), located.Error()); ok && prefix != "" {
		copy.Err = fmt.Errorf("%s%w", prefix, copy.Err)
	}
	return &copy
}

// A Load value is proved before rendering the entry. Locate a failed field
// proof at its authored props read instead of at the component declaration.
func locateEntryRenderError(err error, prog *ir.Program, comp *ir.Component) error {
	var field string
	for name := range comp.PropsFields {
		if strings.Contains(err.Error(), "prop "+name+" (") {
			field = "props." + name
			break
		}
	}
	if field == "" {
		return err
	}
	var find func(ir.NodeID) error
	find = func(id ir.NodeID) error {
		node := prog.NodeAt(id)
		if node.Kind == ir.NodeExpr && readsPropsField(node.Text, field) {
			return locateRenderError(err, node.Span, node.Text)
		}
		for _, attr := range node.Attrs {
			if readsPropsField(attr.Expr, field) {
				return locateAttrError(err, attr)
			}
		}
		for _, child := range node.Children {
			if found := find(child); found != nil {
				return found
			}
		}
		return nil
	}
	if found := find(comp.Root); found != nil {
		return found
	}
	return err
}

func readsPropsField(expression, field string) bool {
	expr, err := parser.ParseExpr(expression)
	if err != nil {
		return false
	}
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if selector, ok := n.(*ast.SelectorExpr); ok {
			if root, ok := selector.X.(*ast.Ident); ok && root.Name == "props" && selector.Sel.Name == strings.TrimPrefix(field, "props.") {
				found = true
			}
		}
		return !found
	})
	return found
}
