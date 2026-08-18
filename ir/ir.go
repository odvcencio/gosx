// Package ir defines the intermediate representation for GoSX components.
//
// The IR is the contract between syntax, validation, server renderer,
// and client hydration. All references are index-based (no recursive pointers)
// following the same pattern as Arbiter's flat-array IR.
//
// # Compatibility
//
// This package is experimental while gosx is pre-1.0. Breaking changes to
// exported ir types are called out in CHANGELOG.md with a migration note.
// A consumer that compiles against ir directly — for example gsxmail, or
// any other tool that imports this package instead of going through gosx's
// higher-level entry points — should pin an exact gosx version rather than
// a version range.
package ir

import "strings"

// NodeID is an index into Program.Nodes.
type NodeID uint32

// ComponentID is an index into Program.Components.
type ComponentID uint32

// Program is the top-level IR container for a GoSX compilation unit.
type Program struct {
	// Package is the Go package name.
	Package string

	// PackagePath is the Go import path for this package (e.g.
	// "m31labs.dev/gosx/examples/mygraph"). It is empty when the
	// program is produced by ir.Lower alone; callers that need the full import
	// path (e.g. the build pipeline) must set it after Lower returns.
	PackagePath string

	// Dir is the absolute path to the directory that contains the source files
	// for this package. It is empty when the program is produced by ir.Lower
	// alone; the build pipeline sets it so that LowerEngineSurface can read
	// the package's *.go files.
	Dir string

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

// ComponentSyntax records which source spelling declared a component. The
// zero value is the legacy Go function form for backward compatibility with
// programs serialized before strict components existed.
type ComponentSyntax uint8

const (
	ComponentSyntaxLegacy ComponentSyntax = iota
	ComponentSyntaxStrict
)

// Component represents a GoSX component function.
type Component struct {
	// Name of the component function.
	Name string

	// PropsType is the Go type name for the props parameter (empty if none).
	PropsType string

	// PropsName is the declared props parameter name. Strict components require
	// the exact name "props" because the file renderer binds that identifier.
	PropsName string

	// PropsFields records, for the root field of every rendered props read,
	// that root's own declared type: an exact renderer builtin for a direct
	// read, or a same-file struct type name for a nested-selector read's
	// root. The file renderer uses it to apply the same untyped-literal
	// conversions as generated Go (for a builtin) or the struct boundary
	// check (for a struct name) before the component observes the value.
	PropsFields map[string]string

	// PropsPaths records exact builtin leaf types for nested props reads
	// (props.A.B[.C], added alongside PropsFields' struct-typed roots),
	// keyed by the dot-joined path under its root ("Player.Name" ->
	// "string"). It does not repeat PropsFields' direct-read entries: a
	// path appears here only when it has at least one hop past its root.
	// The zero value (a nil map) is absent and decodes unchanged for
	// programs serialized before this field existed, matching the
	// ComponentSyntax zero-value convention above.
	PropsPaths map[string]string

	// PropsSlices records loop-source props reads for strict components: a
	// same-file <Each of={props.Field}> whose element type is a same-file
	// value struct. Keys are the dot-joined props path the of attribute
	// resolves (this release resolves only depth-1 paths — see
	// resolveStrictEachSourceType's doc comment in ir/lower.go — so a key
	// is always a bare field name today, but the dot-joined convention
	// matches PropsPaths so a future nested of source needs no key-shape
	// migration). The zero value (a nil map) is absent and decodes
	// unchanged for programs serialized before this field existed,
	// matching the ComponentSyntax zero-value convention above.
	PropsSlices map[string]SlicePropSchema

	// Syntax distinguishes legacy `func` components from strict
	// `component Name(props: Type)` declarations.
	Syntax ComponentSyntax

	// PropsTyped is true when PropsType names a struct declared in the
	// same .gsx file. It is the third component category gosx#240 adds: a
	// strict component, a TYPED legacy component (Syntax is
	// ComponentSyntaxLegacy and PropsTyped is true), and an UNTYPED legacy
	// component (PropsTyped is false — `props any`, an AttrList, a type
	// from another file, or no props parameter at all).
	//
	// A typed legacy component declares the same schema a strict one does,
	// so it carries PropsFields and PropsPaths and it takes part in strict
	// spread boundaries in both directions. It keeps the legacy render
	// frame's flattened map binding, because every existing legacy body
	// reads props through that map — the retrofit widens which
	// compositions compile, it does not change how a legacy body observes
	// its props.
	//
	// A strict component leaves this false: Syntax already says its props
	// are typed, and no consumer needs a second way to ask.
	//
	// The zero value (false) is the untyped legacy meaning, so a program
	// serialized before this field existed decodes unchanged, matching the
	// ComponentSyntax zero-value convention above.
	PropsTyped bool

	// Root is the index of the root node in Program.Nodes.
	Root NodeID

	// IsIsland marks this component as requiring client hydration.
	IsIsland bool

	// IsEngine marks this component as a client compute engine (worker, surface, or video).
	IsEngine bool

	// EngineKind is "worker", "surface", or "video" (only set when IsEngine is true).
	EngineKind string

	// EngineCapabilities declares required browser APIs (canvas, webgl, animation, etc).
	EngineCapabilities []string

	// ServerOnly marks this component as server-render only (no hydration possible).
	ServerOnly bool

	// Span tracks source location for diagnostics.
	Span Span

	// EngineSurface is true when EngineKind == "surface" and the root element
	// is <canvas>. Set by the lowering pass after surface-specific validation.
	EngineSurface bool

	// SurfaceHandlers holds the canvas on* event handler bindings for engine
	// surface components. Populated during lowering when EngineSurface is true.
	SurfaceHandlers []SurfaceHandlerRef

	// Scope holds extracted signals, computeds, and handlers from the
	// component's function body. Populated by the body analyzer when
	// the source is a .gsx file with a full component body.
	Scope *ComponentScope
}

// SlicePropSchema records the element struct type and the binding-relative
// read paths a strict <Each> loop needs, so the file renderer boundary can
// re-prove a loop source's runtime value once per call (type-level, O(read
// paths)) instead of walking every element — see requireStrictSliceValue in
// route/fileprogram.go.
type SlicePropSchema struct {
	// Elem is the bare same-file struct name every element of the loop
	// source must have, e.g. "BreakdownRow".
	Elem string

	// Reads maps each binding-relative field path the loop body reads to
	// its exact declared leaf type, dot-joined for a nested selector
	// through a same-file value struct (e.g. "Label" -> "string",
	// "Stat.Label" -> "string"), matching PropsPaths' convention.
	Reads map[string]string
}

// SurfaceHandlerRef records a single on* attribute binding on a surface
// component's root <canvas> element.
type SurfaceHandlerRef struct {
	// EventName is the camelCase JSX attribute name, e.g. "onMount", "onClick".
	EventName string

	// FunctionName is the top-level package function referenced by the attribute,
	// e.g. "mount", "onSelect".
	FunctionName string
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
// Pattern: name := signal.New(initExpr) or name := signal.NewShared(key, initExpr)
type SignalInfo struct {
	Name     string // runtime signal name (e.g., "count" or "$dashboard.state")
	Local    string // local variable name used inside the component (e.g., "count")
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

// IsComponent returns true if the tag name resolves to an exported GoSX
// component symbol rather than an HTML element.
//
// Plain component tags start with an uppercase letter:
//
//	<Card />
//
// Dotted component tags are also supported and are treated as components when
// the final segment is exported:
//
//	<cms.Card />
//	<design.Hero />
func IsComponent(tag string) bool {
	if len(tag) == 0 {
		return false
	}
	segment := tag
	if idx := strings.LastIndex(segment, "."); idx >= 0 && idx < len(segment)-1 {
		segment = segment[idx+1:]
	}
	if len(segment) == 0 {
		return false
	}
	return segment[0] >= 'A' && segment[0] <= 'Z'
}
