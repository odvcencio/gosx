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
	NodeElement  NodeKind = iota // HTML element
	NodeText                     // Static text
	NodeExpr                     // Expression (dynamic content)
	NodeFragment                 // Fragment (no wrapper element)
	NodeForEach                  // Iteration
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
	OpMap        // Operands[0] = collection, Operands[1] = body template expr
	OpFilter     // Operands[0] = collection, Operands[1] = predicate expr
	OpFind       // Operands[0] = collection, Operands[1] = predicate expr
	OpSlice      // Operands[0] = collection, Operands[1] = start, Operands[2] = end
	OpAppend     // Operands[0] = collection, Operands[1] = element
	OpContains   // Operands[0] = collection/string, Operands[1] = element/substring

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
	OpToString   // Operands[0] = any → string
	OpToInt      // Operands[0] = string/float → int
	OpToFloat    // Operands[0] = string/int → float
)

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
type Program struct {
	Name       string        `json:"name"`
	Props      []PropDef     `json:"props"`
	Nodes      []Node        `json:"nodes"`
	Root       NodeID        `json:"root"`
	Exprs      []Expr        `json:"exprs"`
	Signals    []SignalDef   `json:"signals"`
	Computeds  []ComputedDef `json:"computeds"`
	Handlers   []Handler     `json:"handlers"`
	StaticMask []bool        `json:"static_mask"`
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
