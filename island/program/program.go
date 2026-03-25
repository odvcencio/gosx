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

// CounterProgram returns a reference Program for a Counter component.
//
// The counter has a single "count" signal, two buttons (decrement/increment),
// and an expression node displaying the current count. This serves as a
// canonical test fixture and reference implementation.
func CounterProgram() *Program {
	// Expressions:
	//   0: SignalGet "count"       — reads current count (for display)
	//   1: LitInt "0"              — initial value for count signal
	//   2: SignalSet "count" <- [4] — decrement: count = count - 1
	//   3: SignalSet "count" <- [5] — increment: count = count + 1
	//   4: Sub [6, 7]              — count - 1
	//   5: Add [8, 9]              — count + 1
	//   6: SignalGet "count"       — reads count (for sub)
	//   7: LitInt "1"              — literal 1 (for sub)
	//   8: SignalGet "count"       — reads count (for add)
	//   9: LitInt "1"              — literal 1 (for add)
	exprs := []Expr{
		{Op: OpSignalGet, Value: "count", Type: TypeInt},                          // 0
		{Op: OpLitInt, Value: "0", Type: TypeInt},                                 // 1
		{Op: OpSignalSet, Operands: []ExprID{4}, Value: "count", Type: TypeInt},   // 2
		{Op: OpSignalSet, Operands: []ExprID{5}, Value: "count", Type: TypeInt},   // 3
		{Op: OpSub, Operands: []ExprID{6, 7}, Type: TypeInt},                     // 4
		{Op: OpAdd, Operands: []ExprID{8, 9}, Type: TypeInt},                     // 5
		{Op: OpSignalGet, Value: "count", Type: TypeInt},                          // 6
		{Op: OpLitInt, Value: "1", Type: TypeInt},                                 // 7
		{Op: OpSignalGet, Value: "count", Type: TypeInt},                          // 8
		{Op: OpLitInt, Value: "1", Type: TypeInt},                                 // 9
	}

	// Nodes:
	//   0: div.counter (root element)
	//   1: button "-" with click->decrement
	//   2: expr node displaying count (expr[0])
	//   3: button "+" with click->increment
	//   4: text "-"
	//   5: text "+"
	nodes := []Node{
		{ // 0: div.counter root
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "counter"},
			},
			Children: []NodeID{1, 2, 3},
		},
		{ // 1: button "-" (decrement)
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrEvent, Name: "click", Event: "decrement"},
			},
			Children: []NodeID{4},
		},
		{ // 2: expr node showing count
			Kind: NodeExpr,
			Expr: ExprID(0),
		},
		{ // 3: button "+" (increment)
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrEvent, Name: "click", Event: "increment"},
			},
			Children: []NodeID{5},
		},
		{ // 4: text "-"
			Kind: NodeText,
			Text: "-",
		},
		{ // 5: text "+"
			Kind: NodeText,
			Text: "+",
		},
	}

	return &Program{
		Name:  "Counter",
		Nodes: nodes,
		Root:  0,
		Exprs: exprs,
		Signals: []SignalDef{
			{Name: "count", Type: TypeInt, Init: ExprID(1)},
		},
		Handlers: []Handler{
			{Name: "decrement", Body: []ExprID{2}},
			{Name: "increment", Body: []ExprID{3}},
		},
		StaticMask: []bool{false, true, false, true, true, true},
	}
}
