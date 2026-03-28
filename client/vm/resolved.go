package vm

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
