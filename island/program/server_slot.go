package program

import "fmt"

// ServerSlotCapability identifies programs requiring opaque server DOM adoption.
const ServerSlotCapability = "server-slots-v1"

// HasServerSlots reports whether this program requires the projection runtime.
func (p *Program) HasServerSlots() bool {
	if p != nil {
		for _, n := range p.Nodes {
			if n.Kind == NodeServerSlot {
				return true
			}
		}
	}
	return false
}

// ValidServerSlotName accepts the deliberately finite, literal outlet key syntax.
func ValidServerSlotName(s string) bool {
	if len(s) == 0 || len(s) > 64 || s[0] < 'A' || s[0] > 'Z' {
		return false
	}
	for _, c := range s[1:] {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// ValidateServerSlots validates only the added ownership contract. Legacy
// programs retain their existing validators and wire behavior.
func (p *Program) ValidateServerSlots() error {
	if !p.HasServerSlots() {
		return nil
	}
	seen := make(map[NodeID]bool)
	names := make(map[string]bool)
	var walk func(NodeID, string, bool) error
	walk = func(id NodeID, parent string, dynamic bool) error {
		if int(id) >= len(p.Nodes) {
			return fmt.Errorf("ServerSlot: node %d out of range", id)
		}
		if seen[id] {
			return fmt.Errorf("ServerSlot: repeated or cyclic node %d", id)
		}
		seen[id] = true
		n := p.Nodes[id]
		if n.Kind == NodeServerSlot {
			if !ValidServerSlotName(n.Text) || names[n.Text] {
				return fmt.Errorf("ServerSlot: invalid or duplicate name %q", n.Text)
			}
			if n.Tag != "div" || len(n.Children) != 0 || dynamic {
				return fmt.Errorf("ServerSlot %s requires a stable empty div shell", n.Text)
			}
			switch parent {
			case "div", "section", "article", "main", "aside", "nav", "footer", "header":
			default:
				return fmt.Errorf("ServerSlot %s is not allowed under %q", n.Text, parent)
			}
			names[n.Text] = true
			attrs := make(map[string]bool)
			for _, a := range n.Attrs {
				if attrs[a.Name] || a.Kind == AttrEvent {
					return fmt.Errorf("ServerSlot %s: duplicate or event attribute %s", n.Text, a.Name)
				}
				attrs[a.Name] = true
				switch a.Name {
				case "id", "class", "aria-labelledby":
					if a.Kind != AttrStatic && (a.Kind != AttrExpr || p.serverSlotExprType(a.Expr, true, 0) != TypeString) {
						return fmt.Errorf("ServerSlot %s must be a stable string", a.Name)
					}
				case "hidden":
					if a.Kind != AttrBool && (a.Kind != AttrExpr || p.serverSlotExprType(a.Expr, false, 0) != TypeBool) {
						return fmt.Errorf("ServerSlot hidden must be boolean")
					}
				case "tabindex":
					if a.Kind != AttrExpr || int(a.Expr) >= len(p.Exprs) || p.Exprs[a.Expr].Op != OpLitInt || (p.Exprs[a.Expr].Value != "0" && p.Exprs[a.Expr].Value != "-1") {
						return fmt.Errorf("ServerSlot tabindex must be literal integer 0 or -1")
					}
				case "role":
					if a.Kind != AttrStatic || a.Value != "tabpanel" {
						return fmt.Errorf("ServerSlot role must be literal tabpanel")
					}
				default:
					return fmt.Errorf("ServerSlot: unsupported attribute %s", a.Name)
				}
			}
		}
		dynamic = dynamic || n.Kind == NodeForEach || n.Kind == NodeConditional
		for _, child := range n.Children {
			if err := walk(child, n.Tag, dynamic); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(p.Root, "", false); err != nil {
		return err
	}
	for i, n := range p.Nodes {
		if n.Kind == NodeServerSlot && !seen[NodeID(i)] {
			return fmt.Errorf("ServerSlot: unreachable outlet %s", n.Text)
		}
	}
	return nil
}
