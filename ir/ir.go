// Package ir defines the intermediate representation for GoSX components.
//
// The IR is the contract between syntax, validation, server renderer,
// and client hydration. All references are index-based (no recursive pointers)
// following the same pattern as Arbiter's flat-array IR.
package ir

// NodeID is an index into Program.Nodes.
type NodeID uint32

// ComponentID is an index into Program.Components.
type ComponentID uint32

// Program is the top-level IR container for a GoSX compilation unit.
type Program struct {
	// Package is the Go package name.
	Package string

	// Imports collected from the source file.
	Imports []Import

	// Components declared in this compilation unit.
	Components []Component

	// Nodes is the flat array of all IR nodes (elements, text, expressions, etc).
	Nodes []Node
}

// Import represents a Go import.
type Import struct {
	Alias string
	Path  string
}

// Component represents a GoSX component function.
type Component struct {
	// Name of the component function.
	Name string

	// PropsType is the Go type name for the props parameter (empty if none).
	PropsType string

	// Root is the index of the root node in Program.Nodes.
	Root NodeID

	// IsIsland marks this component as requiring client hydration.
	IsIsland bool

	// IsEngine marks this component as a client compute engine (worker or surface).
	IsEngine bool

	// EngineKind is "worker" or "surface" (only set when IsEngine is true).
	EngineKind string

	// EngineCapabilities declares required browser APIs (canvas, webgl, animation, etc).
	EngineCapabilities []string

	// ServerOnly marks this component as server-render only (no hydration possible).
	ServerOnly bool

	// Span tracks source location for diagnostics.
	Span Span

	// Scope holds extracted signals, computeds, and handlers from the
	// component's function body. Populated by the body analyzer when
	// the source is a .gsx file with a full component body.
	Scope *ComponentScope
}

// ComponentScope holds declarations extracted from a component function body
// via CST pattern matching. This is the bridge between Go source analysis
// and IslandProgram generation.
type ComponentScope struct {
	Signals   []SignalInfo
	Computeds []ComputedInfo
	Handlers  []HandlerInfo
	Locals    map[string]string // variable name → kind ("signal", "computed", "handler")
}

// SignalInfo describes a signal declaration found in the component body.
// Pattern: name := signal.New(initExpr)
type SignalInfo struct {
	Name     string // variable name (e.g., "count")
	InitExpr string // source text of the init expression (e.g., "0")
	TypeHint string // inferred type from init value (e.g., "int", "string")
}

// ComputedInfo describes a computed/derived signal declaration.
// Pattern: name := signal.Derive(func() T { return expr })
type ComputedInfo struct {
	Name     string // variable name
	BodyExpr string // source text of the return expression
}

// HandlerInfo describes a handler function declaration.
// Pattern: name := func() { ...statements... }
type HandlerInfo struct {
	Name       string   // variable name (e.g., "increment")
	Statements []string // source text of each statement in the body
}

// NodeKind discriminates the kind of IR node.
type NodeKind uint8

const (
	NodeElement   NodeKind = iota // HTML element (<div>, <span>, etc.)
	NodeComponent                 // GoSX component (<Counter />, etc.)
	NodeText                      // Static text content
	NodeExpr                      // Go expression hole {expr}
	NodeFragment                  // Fragment <>...</>
	NodeRawHTML                   // Pre-rendered HTML (escape bypass)
)

// Node is a single node in the component IR tree.
// Children and attributes are referenced by index ranges to keep the node flat.
type Node struct {
	Kind NodeKind

	// Tag is the element/component name (for NodeElement and NodeComponent).
	Tag string

	// Text is the literal text content (for NodeText) or raw Go expression source (for NodeExpr).
	Text string

	// Attrs holds the attribute list for elements and components.
	Attrs []Attr

	// Children holds indices into Program.Nodes.
	Children []NodeID

	// IsStatic is true when this subtree contains no expressions or dynamic content.
	// The renderer can skip hydration for static subtrees.
	IsStatic bool

	// IsIslandRoot marks this node as the root of a hydrated island.
	IsIslandRoot bool

	// Span tracks source location.
	Span Span
}

// AttrKind discriminates attribute types.
type AttrKind uint8

const (
	AttrStatic AttrKind = iota // Static string value: class="counter"
	AttrExpr                   // Expression value: onClick={handler}
	AttrBool                   // Boolean attribute: disabled
	AttrSpread                 // Spread: {...props}
)

// Attr represents a single attribute on an element or component.
type Attr struct {
	Kind AttrKind

	// Name is the attribute name (empty for AttrSpread).
	Name string

	// Value is the static string value (for AttrStatic).
	Value string

	// Expr is the Go expression source (for AttrExpr and AttrSpread).
	Expr string

	// IsEvent is true for event handler attributes (onClick, onSubmit, etc.).
	IsEvent bool
}

// Span records a source location for diagnostics.
type Span struct {
	File      string
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
}

// Helper methods

// AddNode appends a node to the program and returns its ID.
func (p *Program) AddNode(n Node) NodeID {
	id := NodeID(len(p.Nodes))
	p.Nodes = append(p.Nodes, n)
	return id
}

// NodeAt returns the node at the given ID.
func (p *Program) NodeAt(id NodeID) *Node {
	return &p.Nodes[id]
}

// IsComponent returns true if the tag name starts with an uppercase letter,
// indicating it's a GoSX component rather than an HTML element.
func IsComponent(tag string) bool {
	if len(tag) == 0 {
		return false
	}
	return tag[0] >= 'A' && tag[0] <= 'Z'
}
