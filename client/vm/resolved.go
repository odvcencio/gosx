package vm

import "strings"

// ResolvedTree is the fully evaluated DOM tree produced by the VM after
// evaluating all expressions and resolving dynamic content.
type ResolvedTree struct {
	Nodes []ResolvedNode
}

// reset empties the tree for a fresh walk and keeps the node array when it is
// already large enough. hint is the node count the walk expects.
//
// Reusing the array is what makes a steady-state reconcile allocation-free.
// A resolved node is 160 bytes and holds five pointers, so a six-node counter
// island allocated a 1 KB pointer-dense object on every event. That object
// dominated both the allocator and the garbage collector in a CPU profile of
// the dispatch and reconcile benchmarks.
//
// Every entry the walk writes is a whole struct assignment, so a stale field
// from the previous generation can never survive into the new tree.
func (tree *ResolvedTree) reset(hint int) {
	if cap(tree.Nodes) < hint {
		tree.Nodes = make([]ResolvedNode, 0, hint)
		return
	}
	tree.Nodes = tree.Nodes[:0]
}

// releaseTail zeroes the array entries past the new length so a tree that
// shrank does not keep the strings and slices of the previous generation
// alive. The common case is a tree of unchanged size, where the tail is empty
// and this costs nothing.
func (tree *ResolvedTree) releaseTail() {
	nodes := tree.Nodes
	clear(nodes[len(nodes):cap(nodes)])
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
			Name:  eventMarkerAttr(eventType),
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

// eventMarkerAttr returns the data-gosx-on-<type> attribute name the browser
// reconciler reads.
//
// Every reconcile rebuilds the attribute list of every element that carries an
// event, and the concatenation allocated a fresh string each time. The eight
// names below cover every event the lowerer emits today, so the hot path now
// returns a constant.
func eventMarkerAttr(eventType string) string {
	switch eventType {
	case "click":
		return "data-gosx-on-click"
	case "input":
		return "data-gosx-on-input"
	case "change":
		return "data-gosx-on-change"
	case "submit":
		return "data-gosx-on-submit"
	case "keydown":
		return "data-gosx-on-keydown"
	case "keyup":
		return "data-gosx-on-keyup"
	case "focus":
		return "data-gosx-on-focus"
	case "blur":
		return "data-gosx-on-blur"
	case "dragstart":
		return "data-gosx-on-dragstart"
	case "dragend":
		return "data-gosx-on-dragend"
	case "dragover":
		return "data-gosx-on-dragover"
	case "dragleave":
		return "data-gosx-on-dragleave"
	case "drop":
		return "data-gosx-on-drop"
	case "pointerdown":
		return "data-gosx-on-pointerdown"
	case "pointermove":
		return "data-gosx-on-pointermove"
	case "pointerup":
		return "data-gosx-on-pointerup"
	case "pointercancel":
		return "data-gosx-on-pointercancel"
	case "document-keydown":
		return "data-gosx-on-document-keydown"
	case "document-keyup":
		return "data-gosx-on-document-keyup"
	case "window-resize":
		return "data-gosx-on-window-resize"
	default:
		return "data-gosx-on-" + eventType
	}
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
	case "onDragStart":
		return "dragstart"
	case "onDragEnd":
		return "dragend"
	case "onDragOver":
		return "dragover"
	case "onDragLeave":
		return "dragleave"
	case "onDrop":
		return "drop"
	case "onPointerDown":
		return "pointerdown"
	case "onPointerMove":
		return "pointermove"
	case "onPointerUp":
		return "pointerup"
	case "onPointerCancel":
		return "pointercancel"
	case "onDocumentKeyDown":
		return "document-keydown"
	case "onDocumentKeyUp":
		return "document-keyup"
	case "onWindowResize":
		return "window-resize"
	default:
		if len(name) > 2 && name[:2] == "on" {
			return strings.ToLower(name[2:3]) + name[3:]
		}
		return strings.ToLower(name)
	}
}
