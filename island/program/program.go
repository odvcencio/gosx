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

	// Composite literals (Slice Y.A — AST-compiler initiative). OpComposite
	// materializes a struct, slice, or map value at runtime. Value carries
	// the kind tag — one of:
	//   - "struct:<TypeName>" — named or positional struct literal.
	//   - "slice"             — slice/array literal.
	//   - "map"               — map literal.
	// Operands are an interleaved key/value list:
	//   - struct: pairs of (string-literal key expr, value expr); for
	//     positional literals the lowerer pre-emits the field name as
	//     the key from a compile-time type registry built from the
	//     surrounding file's TypeSpec declarations.
	//   - slice:  pairs of (int-literal index expr, value expr); indices
	//     run 0..len-1 and are emitted by the lowerer.
	//   - map:    pairs of (key expr, value expr); keys may be any
	//     expression whose String() form becomes the map key.
	// Backwards-compatible per ADR 0002 — programs that never emit
	// OpComposite keep evaluating exactly as before.
	OpComposite

	// Two-value map lookup (Slice Y.B — AST-compiler initiative).
	// OpMapLookup evaluates Operands[0] as the map (any Value with
	// Fields populated) and Operands[1] as the lookup key, and returns
	// an ObjectVal carrying two named fields:
	//
	//   - "value" — the looked-up value, or the zero Value when the key
	//     is absent.
	//   - "ok"    — BoolVal(true) when the key exists in the map's
	//     Fields, BoolVal(false) otherwise.
	//
	// This encoding lets the comma-ok idiom (`v, ok := m[k]`) lower
	// without expanding the Value model with a Tuple kind — the lowerer
	// emits a single OpMapLookup plus per-LHS OpIndex reads of "value"
	// and "ok" against the resulting ObjectVal. Y.A's Decision Point
	// chose the ObjectVal carrier over a dedicated TupleVal because
	// it reuses the existing OpIndex / Value.Fields machinery without
	// touching formatters, equality, or JSON marshaling.
	//
	// Backwards-compatible per ADR 0002 — programs that never emit
	// OpMapLookup keep evaluating exactly as before.
	OpMapLookup

	// LHS selector / indexed-set (Slice Y.C — AST-compiler initiative).
	// Two opcodes that mutate a collection in place so engine-surface
	// handlers can write through their package-level state:
	//
	//   OpFieldSet — `target.<Value> = Operands[1]` where Operands[0]
	//     evaluates to an ObjectVal. The field name lives in Value
	//     (it's always a compile-time identifier in Go). The VM looks
	//     up Operands[0] (an OpLocalGet, an OpIndex, another OpFieldGet
	//     read — anything that returns a Value with a non-nil Fields
	//     map) and writes Operands[1]'s evaluated result into
	//     Fields[Value]. The mutation is in place: since Value.Fields
	//     is a map (reference), every other Value that aliases the same
	//     Fields map sees the change.
	//
	//   OpIndexSet — `target[Operands[1]] = Operands[2]` where
	//     Operands[0] is the collection expression. The collection's
	//     materialized Value is inspected at runtime: ArrayVal (Items
	//     non-nil) writes Items[int(key)]; ObjectVal (Fields non-nil)
	//     writes Fields[key.String()]. A non-collection target records
	//     an `index_set_non_collection` diagnostic and is a no-op so
	//     the VM stays panic-free.
	//
	// **Mutation semantics.** Both opcodes mutate the underlying
	// map/slice — they do NOT clone. Y.A's exit decision flagged the
	// copy-on-write vs in-place question for Y.C; in-place won because
	// (a) graph_surface.go's `gPos[id] = vec2{...}` and
	// `fx[a.ID] += force` already rely on the map/slice being mutated
	// by reference, (b) Y.B's range loop already mutates `_item` in
	// place, and (c) Go's actual value-semantics for struct assignment
	// surface here as the author's explicit `gVel[n.ID] = v` writeback
	// in stepLayout, which Y.C lowers as an explicit OpIndexSet — the
	// Go-level copy already happened on the OpLocalGet read; the
	// writeback restores the mutated value to the shared collection.
	//
	// Backwards-compatible per ADR 0002 — programs that never emit
	// OpFieldSet / OpIndexSet keep evaluating exactly as before.
	OpFieldSet
	OpIndexSet

	// User-defined function call (Slice Y.D — AST-compiler initiative).
	// OpIndirectCall dispatches into a user-function registered on the
	// containing Program (FuncDef table). The callee name lives in
	// Value; the call arguments are evaluated left-to-right and bound
	// to the callee's parameter names in a fresh call frame.
	//
	//   Value    — callee function name (the FuncDef.Name).
	//   Operands — argument expressions, in source order.
	//
	// The result is the value returned by the callee. Multi-return
	// callees materialize their results into an ObjectVal carrier
	// keyed by the callee's output param names ("__ret_0", "__ret_1",
	// ...) so the lowerer can bind each LHS via OpIndex reads — same
	// shape as Slice Y.B's OpMapLookup result. Void callees return
	// the zero Value of TypeAny.
	//
	// **Recursion safety.** The VM enforces a per-evaluation call-stack
	// depth cap (Program.MaxCallDepth; default 256) so a runaway
	// recursion records a structured `call_depth_exceeded` diagnostic
	// instead of stack-overflowing the host. Tunable per Program for
	// surfaces that genuinely need deeper recursion.
	//
	// **Parameter semantics.** Composite params (struct/slice/map) pass
	// by reference because Value.Fields and Value.Items are map / slice
	// reference types — the callee's mutations via OpFieldSet /
	// OpIndexSet land in the caller's storage. This matches Slice Y.C's
	// in-place mutation contract and explicitly preserves the Y.C
	// retrospective's "OpFieldSet on parameter-typed receivers already
	// propagates" guarantee. Scalar params (int/float/bool/string) are
	// passed by Value-by-value copy — Go's normal scalar semantics.
	//
	// Backwards-compatible per ADR 0002 — programs that never emit
	// OpIndirectCall keep evaluating exactly as before.
	OpIndirectCall

	// make(...) builtin (Slice Y.E — AST-compiler initiative).
	// OpMake allocates an empty collection: a fresh ObjectVal carrying a
	// Fields map (for `make(map[K]V)`) or an ArrayVal carrying an Items
	// slice (for `make([]T, n)`). Y.A's OpComposite handles populated
	// composite literals; OpMake covers the explicit allocation form
	// graph_surface.go's stepLayout uses to build per-tick force tables.
	//
	//   Value    — kind tag: "map" (always empty), "slice" (length-only)
	//   Operands — for "slice", Operands[0] is the length expression
	//              (cap is ignored — the VM has no capacity concept);
	//              for "map", Operands is empty (the optional hint is
	//              ignored, matching Go's runtime behavior).
	//
	// The lowerer detects `make(...)` BEFORE the user-function registry
	// probe so a user can't accidentally shadow the builtin. Per Y.D's
	// retrospective handoff, this lives alongside len/append/int/
	// float64/string in lowerCallExpr's `switch id.Name` block.
	//
	// Backwards-compatible per ADR 0002 — programs that never emit
	// OpMake keep evaluating exactly as before.
	OpMake

	// Host-receiver method call (Slice Y.E — AST-compiler initiative).
	// OpHostCall dispatches a method call into a runtime-bound host
	// receiver — the engine-surface canvas, the surface context, or any
	// other host-side object whose methods cannot be expressed as a pure
	// Value transformation. Bound by the surface bootstrap via
	// vm.BindHost(name, receiver).
	//
	//   Value    — qualified call target "<receiver>.<MethodName>"
	//              (e.g., "c.MoveTo", "ctx.PropsInto"). The receiver
	//              prefix is the source-level identifier the author
	//              wrote, NOT a type name — multiple surfaces may bind
	//              the same identifier to different concrete objects.
	//   Operands — argument expressions, in source order, evaluated by
	//              the VM left-to-right before the host call fires.
	//
	// The result is whatever the host receiver returns, as a Value. Void
	// methods return the zero Value of TypeAny. Errors from the host
	// receiver surface as `host_call_error` diagnostics — the VM stays
	// panic-free.
	//
	// Unlike OpCall (stdlib intrinsics — global registry, pure
	// functions), OpHostCall reaches into per-VM state: the bound
	// receivers live on the VM struct, so each engine-surface instance
	// has its own canvas/context bindings. This is the cleanest place
	// to land the "host-supplied capability dispatch" surface that the
	// engine-surface authoring contract has implicitly assumed since
	// X.A but never had a first-class opcode for.
	//
	// Backwards-compatible per ADR 0002 — programs that never emit
	// OpHostCall keep evaluating exactly as before.
	OpHostCall

	// String-to-rune-array conversion (Slice Y.E.3 — AST-compiler
	// initiative). OpToRunes corresponds to Go's `[]rune(s)`
	// conversion. The operand is a string Value; the result is an
	// ArrayVal whose Items are one-rune StringVals (each containing
	// exactly one UTF-8-encoded rune).
	//
	// This lets graph_surface.go's label truncation pattern
	// (`len([]rune(label))` + `string([]rune(label)[:22])`) lower
	// cleanly: OpLen on the resulting array gives the rune count,
	// OpSlice gives a rune subsequence, and OpToString concatenates
	// it back into a string (via the Y.E.3 ToStringVal join path).
	//
	//   Operands — Operands[0] evaluates to the source string.
	//   Value    — unused.
	//
	// Backwards-compatible per ADR 0002 — programs that never emit
	// OpToRunes keep evaluating exactly as before.
	OpToRunes
)

// FuncDef defines a user-defined function callable from a handler or
// from another user function via OpIndirectCall (Slice Y.D). The lowerer
// (ir/golower/decl.go's pre-pass) walks the surface's top-level FuncDecls
// and emits one FuncDef per declaration; the VM looks them up by Name
// when an OpIndirectCall fires.
//
// Params is the ordered list of parameter names; the call site evaluates
// each argument expression and binds it to the corresponding name in the
// callee's fresh frame. Body is the OpSeq root for the function's
// statement body, identical in shape to Handler.Body.
//
// Results is the count of return values (0 for void, 1 for the common
// case, 2+ for multi-return like `func split(n int) (int, int)`). The
// VM uses Results to decide whether to materialize the return as an
// ObjectVal carrier (Y.B-style) for the lowerer's multi-LHS bindings.
type FuncDef struct {
	Name    string   `json:"name"`
	Params  []string `json:"params"`
	Body    []ExprID `json:"body"`
	Results int      `json:"results"`
}

// DefaultMaxCallDepth is the cap applied when Program.MaxCallDepth is
// zero. 256 is comfortably below typical Go goroutine stack limits
// (typically 8MB starting size on 64-bit) yet deep enough that any
// recursive engine-surface helper a human would write fits within it.
// Surfaces that genuinely need deeper recursion can raise the cap via
// Program.MaxCallDepth.
const DefaultMaxCallDepth = 256

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
	Funcs       []FuncDef     `json:"funcs,omitempty"` // user-defined helpers (Slice Y.D)
	StaticMask  []bool        `json:"static_mask"`
	EngineNodes []EngineNode  `json:"engineNodes,omitempty"` // populated for SurfaceScene3D/Canvas2D
	Surface     SurfaceKind   `json:"-"`

	// MaxCallDepth caps the OpIndirectCall recursion depth (Slice Y.D).
	// Zero means "use DefaultMaxCallDepth (256)". Surfaces with
	// genuinely-deep recursion can raise the value at lowering time.
	MaxCallDepth int `json:"maxCallDepth,omitempty"`
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
