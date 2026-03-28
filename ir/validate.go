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

	// For island components, validate expression subset
	if comp.IsIsland {
		v.diags = append(v.diags, validateIslandExprs(v.prog, comp)...)
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

// validateIslandExprs validates that all expressions in an island component
// are within the allowed island expression subset.
func validateIslandExprs(prog *Program, comp *Component) []Diagnostic {
	var diags []Diagnostic

	if int(comp.Root) >= len(prog.Nodes) {
		return diags
	}

	// Collect all node IDs reachable from the component root.
	var nodeIDs []NodeID
	var collect func(id NodeID)
	collect = func(id NodeID) {
		if int(id) >= len(prog.Nodes) {
			return
		}
		nodeIDs = append(nodeIDs, id)
		for _, child := range prog.Nodes[id].Children {
			collect(child)
		}
	}
	collect(comp.Root)

	// Build validation scope from the component's body analysis.
	// This lets the expression parser resolve signal/handler identifiers.
	scope := &ExprScope{
		Signals:       map[string]bool{},
		SignalAliases: map[string]string{},
		Props:         map[string]bool{},
		Handlers:      map[string]bool{},
	}
	if comp.Scope != nil {
		for _, sig := range comp.Scope.Signals {
			scope.Signals[sig.Name] = true
			if sig.Local != "" {
				scope.SignalAliases[sig.Local] = sig.Name
			}
		}
		for _, c := range comp.Scope.Computeds {
			scope.Signals[c.Name] = true
		}
		for _, h := range comp.Scope.Handlers {
			scope.Handlers[h.Name] = true
		}
	}

	for _, id := range nodeIDs {
		node := &prog.Nodes[id]

		if node.Kind == NodeComponent && isUnsupportedIslandComponentRef(node.Tag) {
			diags = append(diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("component <%s> is not supported inside island components yet", node.Tag),
				Hint:    "Use plain elements inside the island or move the component outside the hydrated subtree.",
			})
			continue
		}

		// Check node-level expression text (NodeExpr).
		if node.Kind == NodeExpr && strings.TrimSpace(node.Text) != "" {
			text := strings.TrimSpace(node.Text)

			// Check for rejected patterns in expression text.
			if idx := strings.Index(text, "go "); idx >= 0 && strings.Contains(text[idx:], "func") {
				diags = append(diags, Diagnostic{
					Span:    node.Span,
					Message: fmt.Sprintf("goroutine launch not allowed in island components: %q", text),
				})
				continue
			}
			if strings.Contains(text, "<-") {
				diags = append(diags, Diagnostic{
					Span:    node.Span,
					Message: fmt.Sprintf("channel operations not allowed in island components: %q", text),
				})
				continue
			}
			if strings.Contains(text, "make(chan") {
				diags = append(diags, Diagnostic{
					Span:    node.Span,
					Message: fmt.Sprintf("channel creation not allowed in island components: %q", text),
				})
				continue
			}

			// Try parsing the expression to validate it.
			_, _, err := ParseExpr(text, scope)
			if err != nil {
				diags = append(diags, Diagnostic{
					Span:    node.Span,
					Message: fmt.Sprintf("island expression error: %v", err),
				})
			}
		}

		// Check attributes on element/component nodes.
		for _, attr := range node.Attrs {
			// Reject spread attributes in islands.
			if attr.Kind == AttrSpread {
				diags = append(diags, Diagnostic{
					Span:    node.Span,
					Message: "spread attributes not allowed in island components",
				})
				continue
			}

			// Check expression attributes.
			if attr.Kind == AttrExpr {
				// Event handlers must have a non-empty handler name.
				if attr.IsEvent {
					if strings.TrimSpace(attr.Expr) == "" {
						diags = append(diags, Diagnostic{
							Span:    node.Span,
							Message: fmt.Sprintf("event handler %q has empty handler name in island component", attr.Name),
						})
					}
					continue
				}

				// Non-event expression attributes — check for rejected patterns.
				text := strings.TrimSpace(attr.Expr)
				if text == "" {
					continue
				}

				if idx := strings.Index(text, "go "); idx >= 0 && strings.Contains(text[idx:], "func") {
					diags = append(diags, Diagnostic{
						Span:    node.Span,
						Message: fmt.Sprintf("goroutine launch not allowed in island components: %q", text),
					})
					continue
				}
				if strings.Contains(text, "<-") {
					diags = append(diags, Diagnostic{
						Span:    node.Span,
						Message: fmt.Sprintf("channel operations not allowed in island components: %q", text),
					})
					continue
				}
				if strings.Contains(text, "make(chan") {
					diags = append(diags, Diagnostic{
						Span:    node.Span,
						Message: fmt.Sprintf("channel creation not allowed in island components: %q", text),
					})
					continue
				}

				// Try parsing the expression.
				_, _, err := ParseExpr(text, scope)
				if err != nil {
					diags = append(diags, Diagnostic{
						Span:    node.Span,
						Message: fmt.Sprintf("island expression error: %v", err),
					})
				}
			}
		}
	}

	return diags
}

func isUnsupportedIslandComponentRef(tag string) bool {
	switch strings.TrimSpace(tag) {
	case "If", "Show", "When", "Link", "Image", "TextBlock", "Stylesheet", "Surface", "Worker", "Scene3D":
		return true
	default:
		return false
	}
}

// VoidElements are HTML elements that cannot have children.
var VoidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true,
	"embed": true, "hr": true, "img": true, "input": true,
	"link": true, "meta": true, "param": true, "source": true,
	"track": true, "wbr": true,
}
