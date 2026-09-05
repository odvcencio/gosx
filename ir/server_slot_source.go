//go:build !tinygo

package ir

import (
	"fmt"
	"github.com/odvcencio/gotreesitter"
	"sort"
)

// componentServerSlots is separate from generic expression-slot discovery.
// Full shape/type/context validation happens after island lowering.
func (l *lowerer) componentServerSlots(decl *gotreesitter.Node) []string {
	var names []string
	var walk func(*gotreesitter.Node)
	walk = func(n *gotreesitter.Node) {
		if n == nil {
			return
		}
		if l.nodeType(n) == "jsx_self_closing_element" && l.extractTagName(n) == "ServerSlot" {
			for _, a := range l.extractAttrs(n) {
				if a.Name == "name" && a.Kind == AttrStatic {
					names = append(names, a.Value)
				}
			}
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(l.childByField(decl, "body"))
	sort.Strings(names)
	return names
}

// Check the complete local call graph before either SSR backend can execute
// caller content. Nested client ownership is not admitted by this first subset.
func (l *lowerer) validateServerSlotCallerOwnership() {
	components := make(map[string]*Component, len(l.prog.Components))
	for i := range l.prog.Components {
		c := &l.prog.Components[i]
		components[c.Name] = c
	}
	for _, call := range l.prog.Nodes {
		callee := components[call.Tag]
		if call.Kind != NodeComponent || callee == nil || len(callee.ServerSlots) == 0 {
			continue
		}
		names := make([]string, 0, len(call.Slots))
		for name := range call.Slots {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			seen := map[NodeID]bool{}
			var visit func(NodeID) string
			visit = func(id NodeID) string {
				if int(id) >= len(l.prog.Nodes) || seen[id] {
					return ""
				}
				seen[id] = true
				n := l.prog.Nodes[id]
				if n.Kind == NodeComponent {
					if c := components[n.Tag]; c != nil {
						if c.IsIsland || c.IsEngine {
							return c.Name
						}
						if owner := visit(c.Root); owner != "" {
							return owner
						}
					}
				}
				for _, child := range n.Children {
					if owner := visit(child); owner != "" {
						return owner
					}
				}
				keys := make([]string, 0, len(n.Slots))
				for key := range n.Slots {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					if owner := visit(n.Slots[key]); owner != "" {
						return owner
					}
				}
				return ""
			}
			if owner := visit(call.Slots[name]); owner != "" {
				l.errs = append(l.errs, Diagnostic{Span: call.Span, Message: fmt.Sprintf("ServerSlot %s cannot contain nested owner %s; nested island/engine ownership is not supported", name, owner)})
			}
		}
	}
}
