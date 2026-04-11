package vm

import "strings"

// ResolvedTree is the fully evaluated DOM tree produced by the VM after
// evaluating all expressions and resolving dynamic content.
type ResolvedTree struct {
	Nodes []ResolvedNode
}

// ResolvedNode is a single node in the resolved tree.
type ResolvedNode struct {
	Source    int
	HasSource bool
	Tag       string
	Text      string
	Key       string // stable identity for list diffing (from "key" attr)
	Attrs     []ResolvedAttr
	DOMAttrs  []ResolvedAttr
	Events    []ResolvedEvent
	Children  []int
}

// ResolvedAttr is a resolved attribute with a concrete string value.
type ResolvedAttr struct {
	Name  string
	Value string
	Bool  bool
}

// ResolvedEvent is a delegated DOM event binding attached to a concrete node.
type ResolvedEvent struct {
	Name    string
	Handler string
}

func (node *ResolvedNode) effectiveDOMAttrs() []ResolvedAttr {
	if node == nil {
		return nil
	}
	if node.DOMAttrs != nil {
		return node.DOMAttrs
	}
	node.DOMAttrs = materializeDOMAttrs(node.Attrs, node.Events)
	return node.DOMAttrs
}

func materializeDOMAttrs(attrs []ResolvedAttr, events []ResolvedEvent) []ResolvedAttr {
	// Fast path: no events → alias the attrs slice directly instead of
	// allocating a copy. The returned slice is treated as read-only by
	// the reconcile walker (it only indexes DOMAttrs, never appends),
	// so aliasing is safe.
	if len(events) == 0 {
		return attrs
	}
	out := make([]ResolvedAttr, 0, len(attrs)+(len(events)*2))
	out = append(out, attrs...)
	for _, event := range events {
		eventType := eventAttrType(event.Name)
		out = append(out, ResolvedAttr{
			Name:  "data-gosx-on-" + eventType,
			Value: event.Handler,
		})
		if eventType == "click" {
			out = append(out, ResolvedAttr{
				Name:  "data-gosx-handler",
				Value: event.Handler,
			})
		}
	}
	return out
}

func eventAttrType(name string) string {
	switch name {
	case "onClick":
		return "click"
	case "onInput":
		return "input"
	case "onChange":
		return "change"
	case "onSubmit":
		return "submit"
	case "onKeyDown":
		return "keydown"
	case "onKeyUp":
		return "keyup"
	case "onFocus":
		return "focus"
	case "onBlur":
		return "blur"
	default:
		if len(name) > 2 && name[:2] == "on" {
			return strings.ToLower(name[2:3]) + name[3:]
		}
		return strings.ToLower(name)
	}
}
