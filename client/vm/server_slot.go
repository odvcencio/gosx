package vm

import (
	"fmt"
	"m31labs.dev/gosx/island/program"
	"strconv"
)

// ValidateServerSlotValues proves actual VM values, not only serialized type
// declarations. Call before initial adoption and every projected reconciliation.
func (island *Island) ValidateServerSlotValues() error {
	for _, node := range island.program.Nodes {
		if node.Kind != program.NodeServerSlot {
			continue
		}
		for _, attr := range node.Attrs {
			if attr.Kind != program.AttrExpr {
				continue
			}
			value := island.EvalExpr(attr.Expr)
			switch attr.Name {
			case "id", "class", "aria-labelledby":
				if value.Type != program.TypeString || value.kind() != kindString {
					return fmt.Errorf("ServerSlot %s requires string %s", node.Text, attr.Name)
				}
			case "hidden":
				if value.Type != program.TypeBool || value.kind() != kindScalar {
					return fmt.Errorf("ServerSlot %s requires boolean hidden", node.Text)
				}
			case "tabindex":
				if value.Type != program.TypeInt || (value.num != 0 && value.num != -1) {
					return fmt.Errorf("ServerSlot %s requires tabindex 0/-1", node.Text)
				}
			}
		}
	}
	return nil
}

// ValidateServerSlotTransition refuses identity changes before any patch is
// produced. The browser applies an independent opaque-boundary barrier.
func ValidateServerSlotTransition(prev, next *ResolvedTree) error {
	// Inspect mutable actual trees; an ordinary tree needs no ownership maps.
	if !resolvedHasServerSlots(prev) && !resolvedHasServerSlots(next) {
		return nil
	}
	collect := func(tree *ResolvedTree) (map[string]string, error) {
		result := make(map[string]string)
		if tree == nil || len(tree.Nodes) == 0 {
			return result, nil
		}
		seen := make(map[int]bool)
		var walk func(int, string) error
		walk = func(idx int, path string) error {
			if idx < 0 || idx >= len(tree.Nodes) || seen[idx] {
				return fmt.Errorf("ServerSlot: invalid resolved ownership tree")
			}
			seen[idx] = true
			n := tree.Nodes[idx]
			if n.ServerSlot != "" {
				if n.Tag != "div" || n.Text != "" || len(n.Children) != 0 {
					return fmt.Errorf("ServerSlot: nonopaque resolved shell")
				}
				result[path] = n.ServerSlot
			}
			for i, child := range n.Children {
				key := strconv.Itoa(i)
				if path != "" {
					key = path + "/" + key
				}
				if err := walk(child, key); err != nil {
					return err
				}
			}
			return nil
		}
		return result, walk(0, "")
	}
	a, err := collect(prev)
	if err != nil {
		return err
	}
	b, err := collect(next)
	if err != nil {
		return err
	}
	if len(a) != len(b) {
		return fmt.Errorf("ServerSlot: changed outlet inventory")
	}
	for path, name := range a {
		if b[path] != name {
			return fmt.Errorf("ServerSlot: changed outlet identity at %s", path)
		}
	}
	return nil
}

func resolvedHasServerSlots(tree *ResolvedTree) bool {
	if tree == nil {
		return false
	}
	for _, node := range tree.Nodes {
		if node.ServerSlot != "" {
			return true
		}
	}
	return false
}
