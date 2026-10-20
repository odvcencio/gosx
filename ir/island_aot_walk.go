//go:build !tinygo && !js

package ir

import (
	"fmt"
	"strings"
)

// The source graph is checked before lowering. Component bodies and slot
// payloads are separate edges; names only identify structural component calls.
func aotWalkSource(src *Program, root Component) (map[string]bool, error) {
	names := map[string]bool{root.Name: true}
	active := map[NodeID]bool{}
	heights := map[NodeID]int{}
	var visit func(NodeID, int) error
	visit = func(id NodeID, depth int) error {
		if int(id) >= len(src.Nodes) || depth > 64 || active[id] {
			return fmt.Errorf("invalid source node graph at %d", id)
		}
		if height := heights[id]; height != 0 {
			if depth+height-1 > 64 {
				return fmt.Errorf("invalid source node graph at %d", id)
			}
			return nil
		}
		active[id] = true
		height := 1
		edge := func(child NodeID) error {
			if err := visit(child, depth+1); err != nil {
				return err
			}
			height = max(height, 1+heights[child])
			return nil
		}
		defer delete(active, id)
		n := src.Nodes[id]
		switch n.Kind {
		case NodeElement, NodeComponent, NodeText, NodeExpr, NodeFragment, NodeRawHTML:
		default:
			return fmt.Errorf("unknown node kind: %d", n.Kind)
		}
		for _, attr := range n.Attrs {
			switch attr.Kind {
			case AttrStatic, AttrExpr, AttrBool, AttrSpread:
			default:
				return fmt.Errorf("unknown attr kind: %d", attr.Kind)
			}
			if strings.HasPrefix(attr.Name, "data-gosx-") {
				return fmt.Errorf("reserved_attribute: %s", attr.Name)
			}
		}
		for _, child := range n.Children {
			if err := edge(child); err != nil {
				return err
			}
		}
		for _, name := range sortedNodeSlotNames(n.Slots) {
			if err := edge(n.Slots[name]); err != nil {
				return err
			}
		}
		if n.Kind == NodeComponent && !n.IsSyntheticConditional() {
			for _, callee := range src.Components {
				if callee.Name == n.Tag {
					names[callee.Name] = true
					if err := edge(callee.Root); err != nil {
						return err
					}
				}
			}
		}
		heights[id] = height
		return nil
	}
	if err := visit(root.Root, 1); err != nil {
		return nil, err
	}
	for _, comp := range src.Components {
		if names[comp.Name] && comp.Scope != nil {
			keys := map[string]bool{}
			for _, sig := range comp.Scope.Signals {
				if keys[sig.Name] {
					return nil, fmt.Errorf("duplicate_signal_key: %s", sig.Name)
				}
				keys[sig.Name] = true
			}
		}
	}
	return names, nil
}
