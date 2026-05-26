// Package program defines the client-side island program types.
//
// These types represent the VM-oriented artifact shipped to the browser for
// island hydration. Unlike the compiler IR (ir.NodeID is uint32 with raw Go
// source), this uses typed opcodes and uint16 IDs, limiting islands to 65,535
// nodes/expressions — which is intentional for client-side constraints.
package program

import "fmt"

// NodeID identifies a node within a program. Limited to uint16 (65,535 nodes).
type NodeID = uint16

// ExprID identifies an expression within a program. Limited to uint16.
type ExprID = uint16

// NodeKind describes the type of a Node.
type NodeKind uint8

const (
	NodeElement     NodeKind = iota // HTML element
	NodeText                        // Static text
	NodeExpr                        // Expression (dynamic content)
	NodeFragment                    // Fragment (no wrapper element)
	NodeForEach                     // Iteration
	NodeConditional                 // Conditional children
)

// String returns a human-readable name for the node kind.
func (k NodeKind) String() string {
	switch k {
	case NodeElement:
		return "Element"
	case NodeText:
		return "Text"
	case NodeExpr:
		return "Expr"
	case NodeFragment:
		return "Fragment"
	case NodeForEach:
		return "ForEach"
	case NodeConditional:
		return "Conditional"
	default:
		return fmt.Sprintf("NodeKind(%d)", k)
	}
}

// AttrKind describes the type of an attribute.
type AttrKind uint8

const (
	AttrStatic AttrKind = iota // Static string value
	AttrExpr                   // Dynamic expression value
	AttrBool                   // Boolean attribute
	AttrEvent                  // Event handler binding
)

// OpCode is a VM instruction opcode.
type OpCode uint8

const (
	OpLitString    OpCode = iota // Push string literal
	OpLitInt                     // Push integer literal
	OpLitFloat                   // Push float literal
	OpLitBool                    // Push boolean literal
	OpPropGet                    // Read a prop by name
	OpSignalGet                  // Read a signal's current value
	OpSignalSet                  // Set a signal to a new value
	OpSignalUpdate               // Update a signal via transform
	OpAdd                        // Arithmetic: a + b
	OpSub                        // Arithmetic: a - b
	OpMul                        // Arithmetic: a * b
	OpDiv                        // Arithmetic: a / b
	OpMod                        // Arithmetic: a % b
	OpNeg                        // Arithmetic: -a
	OpEq                         // Comparison: a == b
	OpNeq                        // Comparison: a != b
	OpLt                         // Comparison: a < b
	OpGt                         // Comparison: a > b
	OpLte                        // Comparison: a <= b
	OpGte                        // Comparison: a >= b
	OpAnd                        // Logical: a && b
	OpOr                         // Logical: a || b
	OpNot                        // Logical: !a
	OpConcat                     // String concatenation
	OpFormat                     // String formatting
	OpCond                       // Conditional (ternary)
	OpCall                       // Function call
	OpIndex                      // Index into collection
	OpLen                        // Length of collection
	OpRange                      // Range expression
	OpEventGet                   // Read field from current event data (Value = field name)

	// Array/slice operations
	OpMap      // Operands[0] = collection, Operands[1] = body template expr
	OpFilter   // Operands[0] = collection, Operands[1] = predicate expr
	OpFind     // Operands[0] = collection, Operands[1] = predicate expr
	OpSlice    // Operands[0] = collection, Operands[1] = start, Operands[2] = end
	OpAppend   // Operands[0] = collection, Operands[1] = element
	OpContains // Operands[0] = collection/string, Operands[1] = element/substring

	// String methods
	OpToUpper    // Operands[0] = string
	OpToLower    // Operands[0] = string
	OpTrim       // Operands[0] = string
	OpSplit      // Operands[0] = string, Value = separator
	OpJoin       // Operands[0] = collection, Value = separator
	OpReplace    // Operands[0] = string, Operands[1] = old, Operands[2] = new
	OpSubstring  // Operands[0] = string, Operands[1] = start, Operands[2] = end
	OpStartsWith // Operands[0] = string, Operands[1] = prefix
	OpEndsWith   // Operands[0] = string, Operands[1] = suffix

	// Type conversion
	OpToString // Operands[0] = any → string
	OpToInt    // Operands[0] = string/float → int
	OpToFloat  // Operands[0] = string/int → float

	// Statement sequencing + locals (Slice X.A — AST-compiler initiative).
	// These opcodes let a Program carry multi-statement function bodies, not
	// just single expressions. Backwards-compatible additions per ADR 0002:
	// existing programs that never emit them keep evaluating exactly as before.
	OpSeq       // Sequence: Operands = ordered expressions; evaluate all, return last.
	OpAssign    // Assign Operands[0] to target named in Value (signal or local).
	OpLocalDecl // Reserve a local slot in the current frame; Value = local name.
	OpLocalGet  // Read a local by name (Value); panic-free zero if unset.
	OpLocalSet  // Write Operands[0] to local named in Value (frame-scoped).

	// Imperative iteration (Slice X.C — AST-compiler initiative).
	// Backwards-compatible like the sequencing opcodes above. Both
	// carry a max-iteration safety cap (configurable via VM.SetForCap)
	// so a runaway loop in lowered Go can't hang the shared client WASM.
	OpFor      // 3-clause: Operands=[init, cond, post, body]. Evaluates init; while cond is truthy, evaluates body then post. Returns last body value (or zero).
	OpForRange // range: Operands=[collection, body]. Body reads "_index" + "_item" props (or "_key" + "_item" for maps). Returns last body value (or zero).

	// Control flow exit (Slice X.C). OpReturn evaluates Operands[0]
	// (or yields the zero value when absent) and unwinds the enclosing
	// OpSeq / OpFor / OpForRange / OpCond chain. The VM implements this
	// via a sentinel that callers check; the unwind stops at the nearest
	// EvalWithFrame boundary so handler bodies "return" while the
	// surrounding bytecode keeps running.
	OpReturn
	// OpBreak unwinds the nearest enclosing loop without exiting the
	// handler. OpContinue advances to the next iteration. Both honor
	// the same sentinel mechanism as OpReturn.
	OpBreak
	OpContinue
)

// SurfaceKind identifies the rendering surface a program targets.
// Carried as a runtime-only field on Program — not serialized.
// SurfaceDOM is the zero value so legacy island JSON deserializes correctly.
type SurfaceKind uint8

const (
	SurfaceDOM      SurfaceKind = iota // HTML DOM patches
	SurfaceCanvas2D                    // 2D canvas board (Miro/Figma-style)
	SurfaceScene3D                     // 3D scene graph
)

// String returns the canonical surface name.
func (s SurfaceKind) String() string {
	switch s {
	case SurfaceDOM:
		return "dom"
	case SurfaceCanvas2D:
		return "canvas2d"
	case SurfaceScene3D:
		return "scene3d"
	default:
		return fmt.Sprintf("SurfaceKind(%d)", s)
	}
}

// ExprType describes the type of an expression result.
type ExprType uint8

const (
	TypeString ExprType = iota
	TypeInt
	TypeFloat
	TypeBool
	TypeNode
	TypeAny
)

// Program is the client-side representation of an island component.
// It is the VM-oriented artifact shipped to the browser, distinct from the
// compiler IR.
//
// Version is a reserved envelope field per ADR 0002 (versioning posture: stay
// on v1). It is never default-emitted today; absent → treat as v1. A future
// v2 wire format would set this explicitly and add wire-level discriminators.
//
// Surface is a runtime-only field per ADR 0001 (per-decoder surface injection,
// no wire field). It is set by the surface-specific decoder (island, engine,
// future canvas2d) and is never serialized.
type Program struct {
	Version     string        `json:"version,omitempty"`
	Name        string        `json:"name"`
	Props       []PropDef     `json:"props"`
	Nodes       []Node        `json:"nodes"`
	Root        NodeID        `json:"root"`
	Exprs       []Expr        `json:"exprs"`
	Signals     []SignalDef   `json:"signals"`
	Computeds   []ComputedDef `json:"computeds"`
	Handlers    []Handler     `json:"handlers"`
	StaticMask  []bool        `json:"static_mask"`
	EngineNodes []EngineNode  `json:"engineNodes,omitempty"` // populated for SurfaceScene3D/Canvas2D
	Surface     SurfaceKind   `json:"-"`
}

// Node represents a single node in the island's DOM tree.
type Node struct {
	Kind     NodeKind `json:"kind"`
	Tag      string   `json:"tag"`
	Text     string   `json:"text"`
	Expr     ExprID   `json:"expr"` // NOT omitempty — 0 is a valid ExprID
	Attrs    []Attr   `json:"attrs"`
	Children []NodeID `json:"children"`
}

// Attr represents an attribute on a Node.
type Attr struct {
	Kind  AttrKind `json:"kind"`
	Name  string   `json:"name"`
	Value string   `json:"value"`
	Expr  ExprID   `json:"expr"` // NOT omitempty — 0 is a valid ExprID
	Event string   `json:"event"`
}

// Expr represents a single expression in the program's expression table.
type Expr struct {
	Op       OpCode   `json:"op"`
	Operands []ExprID `json:"operands"`
	Value    string   `json:"value"`
	Type     ExprType `json:"type"`
}

// SignalDef defines a reactive signal in the island.
type SignalDef struct {
	Name string   `json:"name"`
	Type ExprType `json:"type"`
	Init ExprID   `json:"init"`
}

// ComputedDef defines a computed (derived) value in the island.
type ComputedDef struct {
	Name string   `json:"name"`
	Type ExprType `json:"type"`
	Expr ExprID   `json:"expr"`
}

// Handler defines a named event handler with a body of expressions.
type Handler struct {
	Name string   `json:"name"`
	Body []ExprID `json:"body"`
}

// PropDef defines a prop accepted by the island component.
type PropDef struct {
	Name string   `json:"name"`
	Type ExprType `json:"type"`
}

// EngineNode is the scene-oriented node carried by SurfaceScene3D and
// SurfaceCanvas2D programs. It lives here (exported) so that engine.Node can
// alias it during Phase 1a, keeping the engine package as a thin adapter over
// the unified Program type.
//
// TODO(phase-1c): move this into a dedicated scene/ subpackage alongside the
// scene reconciler; engine.Node can then alias the new home and this re-export
// gets dropped.
type EngineNode struct {
	Kind     string            `json:"kind"`
	Geometry string            `json:"geometry,omitempty"`
	Material string            `json:"material,omitempty"`
	Props    map[string]ExprID `json:"props,omitempty"`
	Children []int             `json:"children,omitempty"`
	Static   bool              `json:"static,omitempty"`
}
