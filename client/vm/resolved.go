package vm

// ResolvedTree is the fully evaluated DOM tree produced by the VM after
// evaluating all expressions and resolving dynamic content.
type ResolvedTree struct {
	Nodes []ResolvedNode
}

// ResolvedNode is a single node in the resolved tree.
type ResolvedNode struct {
	Tag      string
	Text     string
	Attrs    []ResolvedAttr
	Children []int
}

// ResolvedAttr is a resolved attribute with a concrete string value.
type ResolvedAttr struct {
	Name  string
	Value string
}
