// Package render provides the server-side HTML renderer for GoSX IR.
//
// The renderer walks the IR node tree and produces HTML string output.
// Expression holes are evaluated by calling a user-provided evaluator function.
package render

import (
	"fmt"
	"html"
	"strings"

	"github.com/odvcencio/gosx/ir"
)

// ExprEvaluator is called to evaluate Go expression holes during rendering.
// It receives the expression source text and returns the string representation.
type ExprEvaluator func(expr string) string

// ComponentRenderer is called to render component references.
// It receives the component tag name, resolved attributes, and children HTML.
type ComponentRenderer func(tag string, attrs map[string]any, childrenHTML string) string

// Options configures the HTML renderer.
type Options struct {
	// Eval evaluates expression holes. If nil, expressions render as empty strings.
	Eval ExprEvaluator

	// RenderComponent renders component references. If nil, components render as divs.
	RenderComponent ComponentRenderer

	// Indent enables pretty-printing with the given indent string.
	Indent string

	// DebugComments adds HTML comments marking component boundaries.
	DebugComments bool
}

// HTML renders an IR program's component to an HTML string.
func HTML(prog *ir.Program, componentName string, opts Options) (string, error) {
	r := &htmlRenderer{
		prog: prog,
		opts: opts,
	}

	// Find the component
	var comp *ir.Component
	for i := range prog.Components {
		if prog.Components[i].Name == componentName {
			comp = &prog.Components[i]
			break
		}
	}
	if comp == nil {
		return "", fmt.Errorf("component %q not found", componentName)
	}

	var b strings.Builder
	if opts.DebugComments {
		fmt.Fprintf(&b, "<!-- gosx:%s -->\n", comp.Name)
	}
	r.renderNode(&b, comp.Root, 0)
	if opts.DebugComments {
		fmt.Fprintf(&b, "\n<!-- /gosx:%s -->", comp.Name)
	}
	return b.String(), nil
}

// RenderNode renders a single IR node to HTML.
func RenderNode(prog *ir.Program, nodeID ir.NodeID, opts Options) string {
	r := &htmlRenderer{prog: prog, opts: opts}
	var b strings.Builder
	r.renderNode(&b, nodeID, 0)
	return b.String()
}

type htmlRenderer struct {
	prog *ir.Program
	opts Options
}

func (r *htmlRenderer) renderNode(b *strings.Builder, nodeID ir.NodeID, depth int) {
	node := r.prog.NodeAt(nodeID)

	switch node.Kind {
	case ir.NodeElement:
		r.renderElement(b, node, depth)
	case ir.NodeComponent:
		r.renderComponent(b, node, depth)
	case ir.NodeText:
		r.renderText(b, node)
	case ir.NodeExpr:
		r.renderExpr(b, node)
	case ir.NodeFragment:
		r.renderFragment(b, node, depth)
	case ir.NodeRawHTML:
		b.WriteString(node.Text)
	}
}

func (r *htmlRenderer) renderElement(b *strings.Builder, node *ir.Node, depth int) {
	r.writeIndent(b, depth)
	b.WriteByte('<')
	b.WriteString(node.Tag)

	// Render attributes
	r.renderAttrs(b, node.Attrs)

	// Check if void element
	if ir.VoidElements[node.Tag] {
		b.WriteString(" />")
		return
	}

	b.WriteByte('>')

	// Render children
	hasBlockChildren := r.hasBlockChildren(node)
	for _, childID := range node.Children {
		if hasBlockChildren {
			b.WriteByte('\n')
		}
		r.renderNode(b, childID, depth+1)
	}

	if hasBlockChildren {
		b.WriteByte('\n')
		r.writeIndent(b, depth)
	}

	b.WriteString("</")
	b.WriteString(node.Tag)
	b.WriteByte('>')
}

func (r *htmlRenderer) renderComponent(b *strings.Builder, node *ir.Node, depth int) {
	if r.opts.RenderComponent != nil {
		// Collect attributes into a map
		attrs := make(map[string]any)
		for _, attr := range node.Attrs {
			switch attr.Kind {
			case ir.AttrStatic:
				attrs[attr.Name] = attr.Value
			case ir.AttrExpr:
				if r.opts.Eval != nil {
					attrs[attr.Name] = r.opts.Eval(attr.Expr)
				}
			case ir.AttrBool:
				attrs[attr.Name] = true
			}
		}

		// Render children to HTML
		var childBuf strings.Builder
		for _, childID := range node.Children {
			r.renderNode(&childBuf, childID, depth+1)
		}

		result := r.opts.RenderComponent(node.Tag, attrs, childBuf.String())
		b.WriteString(result)
		return
	}

	// Default: render as a div with data-component attribute
	r.writeIndent(b, depth)
	fmt.Fprintf(b, `<div data-gosx-component="%s"`, html.EscapeString(node.Tag))
	r.renderAttrs(b, node.Attrs)
	b.WriteByte('>')

	for _, childID := range node.Children {
		r.renderNode(b, childID, depth+1)
	}

	b.WriteString("</div>")
}

func (r *htmlRenderer) renderText(b *strings.Builder, node *ir.Node) {
	b.WriteString(html.EscapeString(node.Text))
}

func (r *htmlRenderer) renderExpr(b *strings.Builder, node *ir.Node) {
	if r.opts.Eval != nil {
		result := r.opts.Eval(node.Text)
		b.WriteString(html.EscapeString(result))
	}
}

func (r *htmlRenderer) renderFragment(b *strings.Builder, node *ir.Node, depth int) {
	for i, childID := range node.Children {
		if i > 0 && r.opts.Indent != "" {
			b.WriteByte('\n')
		}
		r.renderNode(b, childID, depth)
	}
}

func (r *htmlRenderer) renderAttrs(b *strings.Builder, attrs []ir.Attr) {
	for _, attr := range attrs {
		switch attr.Kind {
		case ir.AttrStatic:
			fmt.Fprintf(b, ` %s="%s"`, attr.Name, html.EscapeString(attr.Value))
		case ir.AttrExpr:
			if r.opts.Eval != nil {
				val := r.opts.Eval(attr.Expr)
				fmt.Fprintf(b, ` %s="%s"`, attr.Name, html.EscapeString(val))
			}
		case ir.AttrBool:
			fmt.Fprintf(b, " %s", attr.Name)
		case ir.AttrSpread:
			// Spread attributes need runtime evaluation - skip in static render
		}
	}
}

func (r *htmlRenderer) writeIndent(b *strings.Builder, depth int) {
	if r.opts.Indent == "" {
		return
	}
	for i := 0; i < depth; i++ {
		b.WriteString(r.opts.Indent)
	}
}

func (r *htmlRenderer) hasBlockChildren(node *ir.Node) bool {
	if r.opts.Indent == "" {
		return false
	}
	for _, childID := range node.Children {
		child := r.prog.NodeAt(childID)
		if child.Kind == ir.NodeElement || child.Kind == ir.NodeComponent || child.Kind == ir.NodeFragment {
			return true
		}
	}
	return false
}
