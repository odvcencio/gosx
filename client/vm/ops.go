package vm

// PatchOp describes a single DOM patch operation produced by the diff engine.
type PatchOp struct {
	Kind     PatchKind `json:"kind"`
	Path     string    `json:"path"`               // e.g. "0/2/1"
	Tag      string    `json:"tag,omitempty"`
	Text     string    `json:"text,omitempty"`
	AttrName string    `json:"attrName,omitempty"`
	Children []int     `json:"children,omitempty"`
}

// PatchKind identifies the type of DOM patch operation.
type PatchKind uint8

const (
	PatchSetText        PatchKind = iota // Set the text content of a node.
	PatchSetAttr                         // Set an attribute on a node.
	PatchRemoveAttr                      // Remove an attribute from a node.
	PatchCreateElement                   // Create a new element node.
	PatchRemoveElement                   // Remove an element node.
	PatchReplaceElement                  // Replace an element node.
	PatchReorder                         // Reorder children of a node.
	PatchSetValue                        // Set the value property of a node.
)
