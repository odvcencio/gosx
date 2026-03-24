package ir

import (
	"fmt"
	"strings"
)

// Diagnostic represents a validation error or warning.
type Diagnostic struct {
	Span    Span
	Message string
	Hint    string
}

func (d Diagnostic) String() string {
	s := fmt.Sprintf("%d:%d: %s", d.Span.StartLine, d.Span.StartCol, d.Message)
	if d.Hint != "" {
		s += " (" + d.Hint + ")"
	}
	return s
}

// Validate runs validation passes over the IR program.
// Returns diagnostics (errors and warnings). If any error is returned,
// the program should not be rendered.
func Validate(prog *Program) []Diagnostic {
	v := &validator{prog: prog}
	v.validate()
	return v.diags
}

type validator struct {
	prog  *Program
	diags []Diagnostic
}

func (v *validator) errorf(span Span, format string, args ...any) {
	v.diags = append(v.diags, Diagnostic{
		Span:    span,
		Message: fmt.Sprintf(format, args...),
	})
}

func (v *validator) validate() {
	// Validate each component
	for i := range v.prog.Components {
		v.validateComponent(&v.prog.Components[i])
	}

	// Validate all nodes
	for i := range v.prog.Nodes {
		v.validateNode(&v.prog.Nodes[i])
	}
}

func (v *validator) validateComponent(comp *Component) {
	// Component names must start with uppercase
	if len(comp.Name) > 0 && (comp.Name[0] < 'A' || comp.Name[0] > 'Z') {
		v.errorf(comp.Span, "component %q must start with an uppercase letter", comp.Name)
	}

	// Root node must exist
	if int(comp.Root) >= len(v.prog.Nodes) {
		v.errorf(comp.Span, "component %q references invalid root node", comp.Name)
	}
}

func (v *validator) validateNode(node *Node) {
	switch node.Kind {
	case NodeElement:
		v.validateElement(node)
	case NodeComponent:
		v.validateComponentRef(node)
	case NodeExpr:
		v.validateExpr(node)
	}

	// Validate children references
	for _, childID := range node.Children {
		if int(childID) >= len(v.prog.Nodes) {
			v.errorf(node.Span, "node references invalid child %d", childID)
		}
	}
}

func (v *validator) validateElement(node *Node) {
	if node.Tag == "" {
		v.errorf(node.Span, "element node has empty tag name")
	}

	// Validate attributes
	for _, attr := range node.Attrs {
		v.validateAttr(node, &attr)
	}
}

func (v *validator) validateComponentRef(node *Node) {
	if node.Tag == "" {
		v.errorf(node.Span, "component reference has empty name")
	}

	// Event handlers on components should reference valid action names
	for _, attr := range node.Attrs {
		if attr.IsEvent && attr.Kind == AttrExpr && attr.Expr == "" {
			v.errorf(node.Span, "event handler %q has empty expression", attr.Name)
		}
	}
}

func (v *validator) validateExpr(node *Node) {
	if strings.TrimSpace(node.Text) == "" {
		v.errorf(node.Span, "expression hole is empty")
	}
}

func (v *validator) validateAttr(node *Node, attr *Attr) {
	switch attr.Kind {
	case AttrExpr:
		if strings.TrimSpace(attr.Expr) == "" {
			v.errorf(node.Span, "attribute %q has empty expression", attr.Name)
		}
	case AttrSpread:
		if strings.TrimSpace(attr.Expr) == "" {
			v.errorf(node.Span, "spread attribute has empty expression")
		}
	}
}

// VoidElements are HTML elements that cannot have children.
var VoidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true,
	"embed": true, "hr": true, "img": true, "input": true,
	"link": true, "meta": true, "param": true, "source": true,
	"track": true, "wbr": true,
}
