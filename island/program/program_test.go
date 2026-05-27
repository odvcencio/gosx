package program

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestProgramBasicFields(t *testing.T) {
	p := Program{
		Name: "TestComponent",
		Props: []PropDef{
			{Name: "title", Type: TypeString},
		},
		Nodes: []Node{
			{Kind: NodeElement, Tag: "div"},
			{Kind: NodeText, Text: "hello"},
		},
		Root:       0,
		Exprs:      []Expr{{Op: OpLitString, Value: "hello", Type: TypeString}},
		Signals:    []SignalDef{{Name: "count", Type: TypeInt, Init: 0}},
		Computeds:  []ComputedDef{{Name: "double", Type: TypeInt, Expr: 0}},
		Handlers:   []Handler{{Name: "click", Body: []ExprID{0}}},
		StaticMask: []bool{true, false},
	}

	if p.Name != "TestComponent" {
		t.Errorf("Name = %q, want %q", p.Name, "TestComponent")
	}
	if len(p.Props) != 1 {
		t.Errorf("len(Props) = %d, want 1", len(p.Props))
	}
	if p.Props[0].Name != "title" || p.Props[0].Type != TypeString {
		t.Errorf("Props[0] = %+v, want {title, TypeString}", p.Props[0])
	}
	if len(p.Nodes) != 2 {
		t.Errorf("len(Nodes) = %d, want 2", len(p.Nodes))
	}
	if p.Root != 0 {
		t.Errorf("Root = %d, want 0", p.Root)
	}
	if len(p.Exprs) != 1 {
		t.Errorf("len(Exprs) = %d, want 1", len(p.Exprs))
	}
	if len(p.Signals) != 1 {
		t.Errorf("len(Signals) = %d, want 1", len(p.Signals))
	}
	if len(p.Computeds) != 1 {
		t.Errorf("len(Computeds) = %d, want 1", len(p.Computeds))
	}
	if len(p.Handlers) != 1 {
		t.Errorf("len(Handlers) = %d, want 1", len(p.Handlers))
	}
	if len(p.StaticMask) != 2 {
		t.Errorf("len(StaticMask) = %d, want 2", len(p.StaticMask))
	}

	// Verify JSON round-trip
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var p2 Program
	if err := json.Unmarshal(data, &p2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if p2.Name != p.Name {
		t.Errorf("round-trip Name = %q, want %q", p2.Name, p.Name)
	}
	if len(p2.Nodes) != len(p.Nodes) {
		t.Errorf("round-trip len(Nodes) = %d, want %d", len(p2.Nodes), len(p.Nodes))
	}
}

func TestNodeKinds(t *testing.T) {
	tests := []struct {
		kind NodeKind
		want string
	}{
		{NodeElement, "Element"},
		{NodeText, "Text"},
		{NodeExpr, "Expr"},
		{NodeFragment, "Fragment"},
		{NodeForEach, "ForEach"},
		{NodeConditional, "Conditional"},
	}
	for _, tt := range tests {
		got := tt.kind.String()
		if got != tt.want {
			t.Errorf("NodeKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}

	// Verify unknown kind
	unknown := NodeKind(255)
	s := unknown.String()
	if s == "" {
		t.Error("unknown NodeKind.String() should not be empty")
	}
}

func TestOpCodeRange(t *testing.T) {
	// Verify all opcodes are distinct by checking they have sequential iota values
	opcodes := []OpCode{
		OpLitString, OpLitInt, OpLitFloat, OpLitBool,
		OpPropGet, OpSignalGet, OpSignalSet, OpSignalUpdate,
		OpAdd, OpSub, OpMul, OpDiv, OpMod, OpNeg,
		OpEq, OpNeq, OpLt, OpGt, OpLte, OpGte,
		OpAnd, OpOr, OpNot,
		OpConcat, OpFormat,
		OpCond, OpCall, OpIndex, OpLen, OpRange,
	}

	seen := make(map[OpCode]bool)
	for i, op := range opcodes {
		if seen[op] {
			t.Errorf("OpCode %d (index %d) is a duplicate", op, i)
		}
		seen[op] = true

		// Verify sequential iota: each opcode should equal its index
		if uint8(op) != uint8(i) {
			t.Errorf("OpCode at index %d has value %d, want %d (sequential iota)", i, op, i)
		}
	}

	if len(seen) != 30 {
		t.Errorf("expected 30 distinct opcodes, got %d", len(seen))
	}
}

func TestExprConstruction(t *testing.T) {
	// SignalGet: reads signal index from Value, produces a value
	signalGet := Expr{
		Op:       OpSignalGet,
		Operands: nil,
		Value:    "count",
		Type:     TypeInt,
	}
	if signalGet.Op != OpSignalGet {
		t.Errorf("Op = %d, want OpSignalGet(%d)", signalGet.Op, OpSignalGet)
	}
	if signalGet.Value != "count" {
		t.Errorf("Value = %q, want %q", signalGet.Value, "count")
	}
	if signalGet.Type != TypeInt {
		t.Errorf("Type = %d, want TypeInt(%d)", signalGet.Type, TypeInt)
	}

	// SignalSet: sets a signal value using an expression result
	signalSet := Expr{
		Op:       OpSignalSet,
		Operands: []ExprID{0, 1}, // signal index ref, value expr
		Value:    "count",
		Type:     TypeInt,
	}
	if signalSet.Op != OpSignalSet {
		t.Errorf("Op = %d, want OpSignalSet(%d)", signalSet.Op, OpSignalSet)
	}
	if len(signalSet.Operands) != 2 {
		t.Errorf("len(Operands) = %d, want 2", len(signalSet.Operands))
	}

	// Verify JSON serialization preserves zero ExprIDs
	node := Node{
		Kind: NodeExpr,
		Expr: ExprID(0),
	}
	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var node2 Node
	if err := json.Unmarshal(data, &node2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if node2.Expr != 0 {
		t.Errorf("Expr round-trip = %d, want 0", node2.Expr)
	}
	// Check that "expr" key is present in JSON (not omitted)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}
	if _, ok := raw["expr"]; !ok {
		t.Error("expr field omitted from JSON when value is 0; must NOT use omitempty")
	}

	// Same check for Attr.Expr
	attr := Attr{
		Kind: AttrExpr,
		Name: "class",
		Expr: ExprID(0),
	}
	attrData, err := json.Marshal(attr)
	if err != nil {
		t.Fatalf("json.Marshal attr: %v", err)
	}
	var attrRaw map[string]json.RawMessage
	if err := json.Unmarshal(attrData, &attrRaw); err != nil {
		t.Fatalf("json.Unmarshal attr to map: %v", err)
	}
	if _, ok := attrRaw["expr"]; !ok {
		t.Error("Attr.Expr field omitted from JSON when value is 0; must NOT use omitempty")
	}
}

func TestCounterProgram(t *testing.T) {
	p := CounterProgram()
	if p == nil {
		t.Fatal("CounterProgram() returned nil")
	}

	// Name
	if p.Name != "Counter" {
		t.Errorf("Name = %q, want %q", p.Name, "Counter")
	}

	// 6 nodes
	if len(p.Nodes) != 6 {
		t.Fatalf("len(Nodes) = %d, want 6", len(p.Nodes))
	}

	// Root is node 0
	if p.Root != 0 {
		t.Errorf("Root = %d, want 0", p.Root)
	}

	// Node 0: div.counter (element, root)
	root := p.Nodes[0]
	if root.Kind != NodeElement {
		t.Errorf("Nodes[0].Kind = %v, want NodeElement", root.Kind)
	}
	if root.Tag != "div" {
		t.Errorf("Nodes[0].Tag = %q, want %q", root.Tag, "div")
	}
	// Should have a class attr
	foundClass := false
	for _, a := range root.Attrs {
		if a.Name == "class" && a.Value == "counter" {
			foundClass = true
		}
	}
	if !foundClass {
		t.Error("Nodes[0] missing class='counter' attribute")
	}

	// Check that at least two buttons exist with event attrs
	buttonCount := 0
	eventAttrCount := 0
	for _, n := range p.Nodes {
		if n.Kind == NodeElement && n.Tag == "button" {
			buttonCount++
			for _, a := range n.Attrs {
				if a.Kind == AttrEvent {
					eventAttrCount++
				}
			}
		}
	}
	if buttonCount != 2 {
		t.Errorf("button count = %d, want 2", buttonCount)
	}
	if eventAttrCount < 2 {
		t.Errorf("event attr count = %d, want >= 2", eventAttrCount)
	}

	// Check expr node exists
	exprNodeCount := 0
	for _, n := range p.Nodes {
		if n.Kind == NodeExpr {
			exprNodeCount++
		}
	}
	if exprNodeCount != 1 {
		t.Errorf("expr node count = %d, want 1", exprNodeCount)
	}

	// Check text nodes
	textNodeCount := 0
	for _, n := range p.Nodes {
		if n.Kind == NodeText {
			textNodeCount++
		}
	}
	if textNodeCount != 2 {
		t.Errorf("text node count = %d, want 2", textNodeCount)
	}

	// 10 expressions
	if len(p.Exprs) != 10 {
		t.Errorf("len(Exprs) = %d, want 10", len(p.Exprs))
	}

	// expr[0]: SignalGet count (for display)
	if p.Exprs[0].Op != OpSignalGet {
		t.Errorf("Exprs[0].Op = %d, want OpSignalGet", p.Exprs[0].Op)
	}

	// expr[1]: LitInt 0 (init value)
	if p.Exprs[1].Op != OpLitInt {
		t.Errorf("Exprs[1].Op = %d, want OpLitInt", p.Exprs[1].Op)
	}
	if p.Exprs[1].Value != "0" {
		t.Errorf("Exprs[1].Value = %q, want %q", p.Exprs[1].Value, "0")
	}

	// expr[2]: SignalSet (decrement result)
	if p.Exprs[2].Op != OpSignalSet {
		t.Errorf("Exprs[2].Op = %d, want OpSignalSet", p.Exprs[2].Op)
	}

	// expr[3]: SignalSet (increment result)
	if p.Exprs[3].Op != OpSignalSet {
		t.Errorf("Exprs[3].Op = %d, want OpSignalSet", p.Exprs[3].Op)
	}

	// 1 signal: count
	if len(p.Signals) != 1 {
		t.Fatalf("len(Signals) = %d, want 1", len(p.Signals))
	}
	if p.Signals[0].Name != "count" {
		t.Errorf("Signals[0].Name = %q, want %q", p.Signals[0].Name, "count")
	}
	if p.Signals[0].Type != TypeInt {
		t.Errorf("Signals[0].Type = %d, want TypeInt(%d)", p.Signals[0].Type, TypeInt)
	}
	if p.Signals[0].Init != ExprID(1) {
		t.Errorf("Signals[0].Init = %d, want 1", p.Signals[0].Init)
	}

	// 2 handlers
	if len(p.Handlers) != 2 {
		t.Fatalf("len(Handlers) = %d, want 2", len(p.Handlers))
	}
	if p.Handlers[0].Name != "decrement" {
		t.Errorf("Handlers[0].Name = %q, want %q", p.Handlers[0].Name, "decrement")
	}
	if len(p.Handlers[0].Body) != 1 || p.Handlers[0].Body[0] != ExprID(2) {
		t.Errorf("Handlers[0].Body = %v, want [2]", p.Handlers[0].Body)
	}
	if p.Handlers[1].Name != "increment" {
		t.Errorf("Handlers[1].Name = %q, want %q", p.Handlers[1].Name, "increment")
	}
	if len(p.Handlers[1].Body) != 1 || p.Handlers[1].Body[0] != ExprID(3) {
		t.Errorf("Handlers[1].Body = %v, want [3]", p.Handlers[1].Body)
	}

	// StaticMask
	expectedMask := []bool{false, true, false, true, true, true}
	if len(p.StaticMask) != len(expectedMask) {
		t.Fatalf("len(StaticMask) = %d, want %d", len(p.StaticMask), len(expectedMask))
	}
	for i, want := range expectedMask {
		if p.StaticMask[i] != want {
			t.Errorf("StaticMask[%d] = %v, want %v", i, p.StaticMask[i], want)
		}
	}

	// Verify JSON round-trip
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var p2 Program
	if err := json.Unmarshal(data, &p2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if p2.Name != p.Name {
		t.Errorf("round-trip Name mismatch")
	}
	if len(p2.Nodes) != len(p.Nodes) {
		t.Errorf("round-trip Nodes length mismatch")
	}
	if len(p2.Exprs) != len(p.Exprs) {
		t.Errorf("round-trip Exprs length mismatch")
	}
}

func TestTabsProgram(t *testing.T) {
	p := TabsProgram()
	if p == nil {
		t.Fatal("TabsProgram() returned nil")
	}
	if p.Name != "Tabs" {
		t.Fatalf("Name = %q, want %q", p.Name, "Tabs")
	}
	if len(p.Signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(p.Signals))
	}
	if p.Signals[0].Name != "activeTab" {
		t.Errorf("Signals[0].Name = %q, want %q", p.Signals[0].Name, "activeTab")
	}
	if p.Signals[0].Type != TypeInt {
		t.Errorf("Signals[0].Type = %d, want TypeInt", p.Signals[0].Type)
	}
	if len(p.Handlers) != 3 {
		t.Fatalf("expected 3 handlers, got %d", len(p.Handlers))
	}
	handlerNames := map[string]bool{}
	for _, h := range p.Handlers {
		handlerNames[h.Name] = true
	}
	for _, name := range []string{"showAbout", "showFeatures", "showContact"} {
		if !handlerNames[name] {
			t.Errorf("missing handler %q", name)
		}
	}
	if len(p.Nodes) != 10 {
		t.Errorf("len(Nodes) = %d, want 10", len(p.Nodes))
	}
	if len(p.StaticMask) != len(p.Nodes) {
		t.Errorf("StaticMask length %d != Nodes length %d", len(p.StaticMask), len(p.Nodes))
	}
	// Verify nested cond expression exists (content conds + 3 class conds = 5+)
	foundCond := 0
	for _, e := range p.Exprs {
		if e.Op == OpCond {
			foundCond++
		}
	}
	if foundCond < 5 {
		t.Errorf("expected at least 5 OpCond exprs (2 content + 3 class), got %d", foundCond)
	}

	// Verify CSS class toggling: each tab button has an AttrExpr for class
	attrExprCount := 0
	for _, n := range p.Nodes {
		if n.Kind == NodeElement && n.Tag == "button" {
			for _, a := range n.Attrs {
				if a.Kind == AttrExpr && a.Name == "class" {
					attrExprCount++
				}
			}
		}
	}
	if attrExprCount != 3 {
		t.Errorf("expected 3 AttrExpr class attrs on tab buttons, got %d", attrExprCount)
	}

	// JSON round-trip
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var p2 Program
	if err := json.Unmarshal(data, &p2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if p2.Name != p.Name {
		t.Error("round-trip Name mismatch")
	}
}

func TestToggleProgram(t *testing.T) {
	p := ToggleProgram()
	if p == nil {
		t.Fatal("ToggleProgram() returned nil")
	}
	if p.Name != "Toggle" {
		t.Fatalf("Name = %q, want %q", p.Name, "Toggle")
	}
	if len(p.Signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(p.Signals))
	}
	if p.Signals[0].Name != "visible" {
		t.Errorf("Signals[0].Name = %q, want %q", p.Signals[0].Name, "visible")
	}
	if p.Signals[0].Type != TypeBool {
		t.Errorf("Signals[0].Type = %d, want TypeBool", p.Signals[0].Type)
	}
	if len(p.Handlers) != 2 {
		t.Fatalf("expected 2 handlers (toggle + toggleKey), got %d", len(p.Handlers))
	}
	handlerNames := map[string]bool{}
	for _, h := range p.Handlers {
		handlerNames[h.Name] = true
	}
	for _, name := range []string{"toggle", "toggleKey"} {
		if !handlerNames[name] {
			t.Errorf("missing handler %q", name)
		}
	}
	if len(p.Nodes) != 5 {
		t.Errorf("len(Nodes) = %d, want 5", len(p.Nodes))
	}
	if len(p.StaticMask) != len(p.Nodes) {
		t.Errorf("StaticMask length %d != Nodes length %d", len(p.StaticMask), len(p.Nodes))
	}
	// Verify OpNot is used for toggle (should appear twice: click + keydown)
	notCount := 0
	for _, e := range p.Exprs {
		if e.Op == OpNot {
			notCount++
		}
	}
	if notCount < 2 {
		t.Errorf("expected at least 2 OpNot expressions (click + keydown), got %d", notCount)
	}
	// Verify button has keydown event attr
	foundKeydown := false
	for _, n := range p.Nodes {
		if n.Kind == NodeElement && n.Tag == "button" {
			for _, a := range n.Attrs {
				if a.Kind == AttrEvent && a.Name == "keydown" {
					foundKeydown = true
				}
			}
		}
	}
	if !foundKeydown {
		t.Error("expected keydown event attr on button")
	}

	// JSON round-trip
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var p2 Program
	if err := json.Unmarshal(data, &p2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if p2.Name != p.Name {
		t.Error("round-trip Name mismatch")
	}
}

func TestTodoProgram(t *testing.T) {
	p := TodoProgram()
	if p == nil {
		t.Fatal("TodoProgram() returned nil")
	}
	if p.Name != "Todo" {
		t.Fatalf("Name = %q, want %q", p.Name, "Todo")
	}
	if len(p.Signals) != 2 {
		t.Fatalf("expected 2 signals, got %d", len(p.Signals))
	}
	sigNames := map[string]bool{}
	for _, s := range p.Signals {
		sigNames[s.Name] = true
	}
	for _, name := range []string{"items", "input"} {
		if !sigNames[name] {
			t.Errorf("missing signal %q", name)
		}
	}
	if len(p.Handlers) != 3 {
		t.Fatalf("expected 3 handlers, got %d", len(p.Handlers))
	}
	handlerNames := map[string]bool{}
	for _, h := range p.Handlers {
		handlerNames[h.Name] = true
	}
	for _, name := range []string{"updateInput", "addItem", "clearAll"} {
		if !handlerNames[name] {
			t.Errorf("missing handler %q", name)
		}
	}
	// addItem handler should have 2 body exprs (set items, set input)
	for _, h := range p.Handlers {
		if h.Name == "addItem" && len(h.Body) != 2 {
			t.Errorf("addItem handler body length = %d, want 2", len(h.Body))
		}
	}
	if len(p.StaticMask) != len(p.Nodes) {
		t.Errorf("StaticMask length %d != Nodes length %d", len(p.StaticMask), len(p.Nodes))
	}

	// JSON round-trip
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var p2 Program
	if err := json.Unmarshal(data, &p2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if p2.Name != p.Name {
		t.Error("round-trip Name mismatch")
	}
}

func TestFormProgram(t *testing.T) {
	p := FormProgram()
	if p == nil {
		t.Fatal("FormProgram() returned nil")
	}
	if p.Name != "Form" {
		t.Fatalf("Name = %q, want %q", p.Name, "Form")
	}
	if len(p.Signals) != 2 {
		t.Fatalf("expected 2 signals, got %d", len(p.Signals))
	}
	sigNames := map[string]bool{}
	for _, s := range p.Signals {
		sigNames[s.Name] = true
	}
	for _, name := range []string{"name", "valid"} {
		if !sigNames[name] {
			t.Errorf("missing signal %q", name)
		}
	}
	if len(p.Handlers) != 3 {
		t.Fatalf("expected 3 handlers (updateName, fillName, validateForm), got %d", len(p.Handlers))
	}
	handlerNames := map[string]bool{}
	for _, h := range p.Handlers {
		handlerNames[h.Name] = true
	}
	for _, name := range []string{"updateName", "fillName", "validateForm"} {
		if !handlerNames[name] {
			t.Errorf("missing handler %q", name)
		}
	}
	if len(p.StaticMask) != len(p.Nodes) {
		t.Errorf("StaticMask length %d != Nodes length %d", len(p.StaticMask), len(p.Nodes))
	}
	// Verify OpEventGet is used for two-way input binding
	foundEventGet := false
	for _, e := range p.Exprs {
		if e.Op == OpEventGet {
			foundEventGet = true
		}
	}
	if !foundEventGet {
		t.Error("expected OpEventGet expression for two-way input binding")
	}
	// Verify conditional expression for validation display
	foundCond := false
	for _, e := range p.Exprs {
		if e.Op == OpCond {
			foundCond = true
		}
	}
	if !foundCond {
		t.Error("expected OpCond expression for validation display")
	}
	// Verify input element exists with input event
	foundInput := false
	for _, n := range p.Nodes {
		if n.Kind == NodeElement && n.Tag == "input" {
			for _, a := range n.Attrs {
				if a.Kind == AttrEvent && a.Name == "input" && a.Event == "updateName" {
					foundInput = true
				}
			}
		}
	}
	if !foundInput {
		t.Error("expected input element with input event -> updateName")
	}

	// JSON round-trip
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var p2 Program
	if err := json.Unmarshal(data, &p2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if p2.Name != p.Name {
		t.Error("round-trip Name mismatch")
	}
}

func TestDerivedProgram(t *testing.T) {
	p := DerivedProgram()
	if p == nil {
		t.Fatal("DerivedProgram() returned nil")
	}
	if p.Name != "Derived" {
		t.Fatalf("Name = %q, want %q", p.Name, "Derived")
	}
	if len(p.Signals) != 3 {
		t.Fatalf("expected 3 signals, got %d", len(p.Signals))
	}
	sigNames := map[string]bool{}
	for _, s := range p.Signals {
		sigNames[s.Name] = true
	}
	for _, name := range []string{"price", "quantity", "discount"} {
		if !sigNames[name] {
			t.Errorf("missing signal %q", name)
		}
	}
	if len(p.Handlers) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(p.Handlers))
	}
	handlerNames := map[string]bool{}
	for _, h := range p.Handlers {
		handlerNames[h.Name] = true
	}
	for _, name := range []string{"incQuantity", "toggleDiscount"} {
		if !handlerNames[name] {
			t.Errorf("missing handler %q", name)
		}
	}
	if len(p.StaticMask) != len(p.Nodes) {
		t.Errorf("StaticMask length %d != Nodes length %d", len(p.StaticMask), len(p.Nodes))
	}
	// Verify total computation uses OpMul and OpSub
	foundMul, foundSub := false, false
	for _, e := range p.Exprs {
		if e.Op == OpMul {
			foundMul = true
		}
		if e.Op == OpSub {
			foundSub = true
		}
	}
	if !foundMul {
		t.Error("expected OpMul for price * quantity")
	}
	if !foundSub {
		t.Error("expected OpSub for total - discount")
	}

	// JSON round-trip
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var p2 Program
	if err := json.Unmarshal(data, &p2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if p2.Name != p.Name {
		t.Error("round-trip Name mismatch")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	original := CounterProgram()
	data, err := EncodeJSON(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("empty output")
	}
	if data[0] != '{' {
		t.Fatal("expected JSON object")
	}
	decoded, err := DecodeJSON(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Name != original.Name {
		t.Fatalf("name: expected %s, got %s", original.Name, decoded.Name)
	}
	if len(decoded.Nodes) != len(original.Nodes) {
		t.Fatalf("nodes: expected %d, got %d", len(original.Nodes), len(decoded.Nodes))
	}
	if len(decoded.Exprs) != len(original.Exprs) {
		t.Fatalf("exprs: expected %d, got %d", len(original.Exprs), len(decoded.Exprs))
	}
	if decoded.Exprs[0].Op != OpSignalGet {
		t.Fatalf("expr[0] op: expected SignalGet, got %d", decoded.Exprs[0].Op)
	}
	if decoded.Exprs[0].Value != "count" {
		t.Fatalf("expr[0] value: expected count, got %s", decoded.Exprs[0].Value)
	}
	if len(decoded.Signals) != 1 {
		t.Fatal("expected 1 signal")
	}
	if len(decoded.Handlers) != 2 {
		t.Fatal("expected 2 handlers")
	}
	if len(decoded.StaticMask) != len(original.StaticMask) {
		t.Fatal("static mask length mismatch")
	}
}

func TestBinaryRoundTrip(t *testing.T) {
	original := CounterProgram()
	data, err := EncodeBinary(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Verify magic header
	if string(data[:3]) != "GSX" {
		t.Fatalf("expected GSX magic, got %q", string(data[:3]))
	}
	if data[3] != 0x00 {
		t.Fatal("expected null terminator after magic")
	}
	decoded, err := DecodeBinary(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Verify all fields survive round-trip
	if decoded.Name != original.Name {
		t.Fatalf("name mismatch")
	}
	if len(decoded.Nodes) != len(original.Nodes) {
		t.Fatalf("nodes mismatch")
	}
	if len(decoded.Exprs) != len(original.Exprs) {
		t.Fatalf("exprs mismatch")
	}
	if decoded.Exprs[2].Op != OpSignalSet {
		t.Fatalf("expr[2] op mismatch")
	}
	if decoded.Exprs[2].Value != "count" {
		t.Fatalf("expr[2] value mismatch")
	}
	if len(decoded.Signals) != len(original.Signals) {
		t.Fatal("signals mismatch")
	}
	if len(decoded.Handlers) != len(original.Handlers) {
		t.Fatal("handlers mismatch")
	}
	if len(decoded.StaticMask) != len(original.StaticMask) {
		t.Fatal("mask mismatch")
	}
	// Verify children survived
	if len(decoded.Nodes[0].Children) != 3 {
		t.Fatal("root children mismatch")
	}
	// Verify attrs survived
	if len(decoded.Nodes[0].Attrs) != 1 {
		t.Fatal("root attrs mismatch")
	}
	if decoded.Nodes[0].Attrs[0].Value != "counter" {
		t.Fatal("class attr mismatch")
	}
}

func TestBinarySmallerThanJSON(t *testing.T) {
	p := CounterProgram()
	jsonData, _ := EncodeJSON(p)
	binData, _ := EncodeBinary(p)
	if len(binData) >= len(jsonData) {
		t.Errorf("binary (%d bytes) should be smaller than JSON (%d bytes)", len(binData), len(jsonData))
	}
}

func TestBinaryTruncatedInput(t *testing.T) {
	p := CounterProgram()
	data, _ := EncodeBinary(p)
	// Truncate to just the header
	_, err := DecodeBinary(data[:6])
	if err == nil {
		t.Fatal("expected error on truncated input")
	}
}

func TestJSONEmptyProgram(t *testing.T) {
	p := &Program{Name: "Empty", Root: 0}
	data, err := EncodeJSON(p)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeJSON(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Name != "Empty" {
		t.Fatalf("expected Empty, got %s", decoded.Name)
	}
}

func TestListProgram(t *testing.T) {
	p := ListProgram()
	if p == nil {
		t.Fatal("ListProgram() returned nil")
	}
	if p.Name != "List" {
		t.Fatalf("Name = %q, want %q", p.Name, "List")
	}
	if len(p.Signals) != 3 {
		t.Fatalf("expected 3 signals (items, input, count), got %d", len(p.Signals))
	}
	sigNames := map[string]bool{}
	for _, s := range p.Signals {
		sigNames[s.Name] = true
	}
	for _, name := range []string{"items", "input", "count"} {
		if !sigNames[name] {
			t.Errorf("missing signal %q", name)
		}
	}
	if len(p.Handlers) != 3 {
		t.Fatalf("expected 3 handlers, got %d", len(p.Handlers))
	}
	handlerNames := map[string]bool{}
	for _, h := range p.Handlers {
		handlerNames[h.Name] = true
	}
	for _, name := range []string{"addItem", "removeLastItem", "clearItems"} {
		if !handlerNames[name] {
			t.Errorf("missing handler %q", name)
		}
	}
	// addItem handler should have 2 body exprs (set items, set count)
	for _, h := range p.Handlers {
		if h.Name == "addItem" && len(h.Body) != 2 {
			t.Errorf("addItem handler body length = %d, want 2", len(h.Body))
		}
	}
	// clearItems handler should have 2 body exprs (set items, set count)
	for _, h := range p.Handlers {
		if h.Name == "clearItems" && len(h.Body) != 2 {
			t.Errorf("clearItems handler body length = %d, want 2", len(h.Body))
		}
	}
	if len(p.StaticMask) != len(p.Nodes) {
		t.Errorf("StaticMask length %d != Nodes length %d", len(p.StaticMask), len(p.Nodes))
	}
	// Verify OpEventGet is used for reading input value
	foundEventGet := false
	for _, e := range p.Exprs {
		if e.Op == OpEventGet {
			foundEventGet = true
		}
	}
	if !foundEventGet {
		t.Error("expected OpEventGet expression in addItem handler")
	}
	// Verify OpConcat is used for item concatenation in addItem
	concatCount := 0
	for _, e := range p.Exprs {
		if e.Op == OpConcat {
			concatCount++
		}
	}
	if concatCount < 2 {
		t.Errorf("expected at least 2 OpConcat exprs for item building, got %d", concatCount)
	}
	// Verify count is displayed via a separate expr node (not concat)
	if len(p.Nodes) != 16 {
		t.Errorf("len(Nodes) = %d, want 16", len(p.Nodes))
	}

	// JSON round-trip
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var p2 Program
	if err := json.Unmarshal(data, &p2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if p2.Name != p.Name {
		t.Error("round-trip Name mismatch")
	}
	if len(p2.Nodes) != len(p.Nodes) {
		t.Errorf("round-trip Nodes length mismatch: %d != %d", len(p2.Nodes), len(p.Nodes))
	}
	if len(p2.Exprs) != len(p.Exprs) {
		t.Errorf("round-trip Exprs length mismatch: %d != %d", len(p2.Exprs), len(p.Exprs))
	}
}

func TestSurfaceKindString(t *testing.T) {
	cases := []struct {
		kind SurfaceKind
		want string
	}{
		{SurfaceDOM, "dom"},
		{SurfaceCanvas2D, "canvas2d"},
		{SurfaceScene3D, "scene3d"},
	}
	for _, c := range cases {
		if got := c.kind.String(); got != c.want {
			t.Errorf("SurfaceKind(%d).String() = %q, want %q", c.kind, got, c.want)
		}
	}
}

func TestProgramSurfaceZeroValue(t *testing.T) {
	var p Program
	if p.Surface != SurfaceDOM {
		t.Errorf("zero-value Program.Surface = %v, want SurfaceDOM (kind 0)", p.Surface)
	}
}

func TestProgramVersionOmittedRoundTrip(t *testing.T) {
	// Programs without a version field decode with empty Version and re-encode
	// without emitting the field (per omitempty). Reserved per ADR 0002.
	src := `{"nodes":[],"exprs":[]}`
	var p Program
	if err := json.Unmarshal([]byte(src), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Version != "" {
		t.Errorf("Version = %q, want empty for legacy payloads", p.Version)
	}
	out, err := json.Marshal(&p)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if bytes.Contains(out, []byte(`"version"`)) {
		t.Errorf("re-encoded payload should not contain \"version\" key: %s", out)
	}
}

func TestProgramVersionRoundTripWhenSet(t *testing.T) {
	src := `{"version":"1","nodes":[],"exprs":[]}`
	var p Program
	if err := json.Unmarshal([]byte(src), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Version != "1" {
		t.Errorf("Version = %q, want \"1\"", p.Version)
	}
}

// --- Slice X.A: statement sequencing + locals opcodes ---

// TestSequencingOpcodesAreDistinct guards against accidental reuse of the
// iota slots the AST-compiler initiative added. These five opcodes underpin
// every multi-statement handler body lowered from Go source; collisions
// would silently corrupt any program that uses sequencing or locals.
func TestSequencingOpcodesAreDistinct(t *testing.T) {
	added := []OpCode{OpSeq, OpAssign, OpLocalDecl, OpLocalGet, OpLocalSet}
	seen := map[OpCode]bool{}
	for _, op := range added {
		if seen[op] {
			t.Errorf("OpCode %d appears twice in the X.A additions", op)
		}
		seen[op] = true
	}
	// Cross-check: none of the additions collides with the pre-existing
	// opcode space that TestOpCodeRange already covers (OpLitString..OpRange).
	prior := []OpCode{
		OpLitString, OpLitInt, OpLitFloat, OpLitBool,
		OpPropGet, OpSignalGet, OpSignalSet, OpSignalUpdate,
		OpAdd, OpSub, OpMul, OpDiv, OpMod, OpNeg,
		OpEq, OpNeq, OpLt, OpGt, OpLte, OpGte,
		OpAnd, OpOr, OpNot,
		OpConcat, OpFormat,
		OpCond, OpCall, OpIndex, OpLen, OpRange,
		OpEventGet,
		OpMap, OpFilter, OpFind, OpSlice, OpAppend, OpContains,
		OpToUpper, OpToLower, OpTrim, OpSplit, OpJoin, OpReplace,
		OpSubstring, OpStartsWith, OpEndsWith,
		OpToString, OpToInt, OpToFloat,
	}
	for _, op := range prior {
		if seen[op] {
			t.Errorf("X.A addition collides with prior opcode %d", op)
		}
	}
}

// TestSequencingOpcodeShapes documents the operand shape each new opcode
// expects so callers (the X.C lowerer, hand-rolled tests) can construct
// programs with confidence. The constructors here are illustrative — the
// VM-level behavior tests live in client/vm/vm_test.go.
func TestSequencingOpcodeShapes(t *testing.T) {
	// OpSeq: any positive count of operands; returns last.
	seq := Expr{Op: OpSeq, Operands: []ExprID{0, 1, 2}}
	if len(seq.Operands) != 3 {
		t.Errorf("OpSeq operands = %d, want 3", len(seq.Operands))
	}

	// OpAssign: Value is target name, Operands[0] is value expr.
	assign := Expr{Op: OpAssign, Value: "x", Operands: []ExprID{0}}
	if assign.Value != "x" || len(assign.Operands) != 1 {
		t.Errorf("OpAssign shape unexpected: %+v", assign)
	}

	// OpLocalDecl: Value is local name; no operands.
	decl := Expr{Op: OpLocalDecl, Value: "i"}
	if decl.Value != "i" || len(decl.Operands) != 0 {
		t.Errorf("OpLocalDecl shape unexpected: %+v", decl)
	}

	// OpLocalGet: Value is local name; no operands.
	get := Expr{Op: OpLocalGet, Value: "i"}
	if get.Value != "i" {
		t.Errorf("OpLocalGet shape unexpected: %+v", get)
	}

	// OpLocalSet: Value is local name; Operands[0] is value expr.
	set := Expr{Op: OpLocalSet, Value: "i", Operands: []ExprID{0}}
	if set.Value != "i" || len(set.Operands) != 1 {
		t.Errorf("OpLocalSet shape unexpected: %+v", set)
	}
}

// --- Slice Y.A: composite literal opcode ---

// TestCompositeOpcodeIsDistinct guards the new Y.A iota slot against
// accidental collision with the X.A/X.B/X.C additions or any prior
// opcode. OpComposite is the single new opcode introduced by Slice Y.A
// (composite-literal lowering); a collision would silently corrupt
// every struct/slice/map literal in lowered handlers.
func TestCompositeOpcodeIsDistinct(t *testing.T) {
	all := []OpCode{
		// pre-existing (TestOpCodeRange)
		OpLitString, OpLitInt, OpLitFloat, OpLitBool,
		OpPropGet, OpSignalGet, OpSignalSet, OpSignalUpdate,
		OpAdd, OpSub, OpMul, OpDiv, OpMod, OpNeg,
		OpEq, OpNeq, OpLt, OpGt, OpLte, OpGte,
		OpAnd, OpOr, OpNot,
		OpConcat, OpFormat,
		OpCond, OpCall, OpIndex, OpLen, OpRange,
		OpEventGet,
		OpMap, OpFilter, OpFind, OpSlice, OpAppend, OpContains,
		OpToUpper, OpToLower, OpTrim, OpSplit, OpJoin, OpReplace,
		OpSubstring, OpStartsWith, OpEndsWith,
		OpToString, OpToInt, OpToFloat,
		// X.A
		OpSeq, OpAssign, OpLocalDecl, OpLocalGet, OpLocalSet,
		// X.C
		OpFor, OpForRange, OpReturn, OpBreak, OpContinue,
		// Y.A
		OpComposite,
		// Y.B
		OpMapLookup,
		// Y.C (new)
		OpFieldSet, OpIndexSet,
	}
	seen := map[OpCode]bool{}
	for _, op := range all {
		if seen[op] {
			t.Errorf("OpCode %d appears twice in the opcode ladder", op)
		}
		seen[op] = true
	}
}

// TestCompositeOpcodeShape documents the operand encoding for the three
// kinds of composite literal (struct/slice/map). Operand pairs are
// (keyExpr, valueExpr); the lowerer is responsible for pre-emitting the
// keys as OpLitString / OpLitInt as appropriate.
func TestCompositeOpcodeShape(t *testing.T) {
	// struct: Value = "struct:vec2"; operands are (keyExpr, valueExpr) pairs.
	structLit := Expr{
		Op:       OpComposite,
		Value:    "struct:vec2",
		Operands: []ExprID{0, 1, 2, 3}, // ("X", x), ("Y", y)
	}
	if structLit.Value != "struct:vec2" {
		t.Errorf("struct Value = %q, want %q", structLit.Value, "struct:vec2")
	}
	if len(structLit.Operands)%2 != 0 {
		t.Errorf("struct operand count %d must be even (key/value pairs)", len(structLit.Operands))
	}

	// slice: Value = "slice"; operands are (indexExpr, valueExpr) pairs.
	sliceLit := Expr{
		Op:       OpComposite,
		Value:    "slice",
		Operands: []ExprID{0, 1, 2, 3, 4, 5}, // (0, a), (1, b), (2, c)
	}
	if sliceLit.Value != "slice" {
		t.Errorf("slice Value = %q, want %q", sliceLit.Value, "slice")
	}

	// map: Value = "map"; operands are arbitrary (keyExpr, valueExpr) pairs.
	mapLit := Expr{
		Op:       OpComposite,
		Value:    "map",
		Operands: []ExprID{0, 1, 2, 3}, // ("x", 1.5), ("y", 2.5)
	}
	if mapLit.Value != "map" {
		t.Errorf("map Value = %q, want %q", mapLit.Value, "map")
	}
}

// --- Slice Y.B: two-value map lookup opcode ---

// TestMapLookupOpcodeShape documents the operand encoding for the
// comma-ok map lookup (`v, ok := m[k]`). Operands[0] is the map
// expression; Operands[1] is the key expression. The VM materializes
// the result as an ObjectVal with "value" and "ok" fields so the
// lowerer can emit two OpIndex reads against it for the LHS bindings.
func TestMapLookupOpcodeShape(t *testing.T) {
	lookup := Expr{
		Op:       OpMapLookup,
		Operands: []ExprID{0, 1}, // (map, key)
	}
	if lookup.Op != OpMapLookup {
		t.Errorf("opcode = %d, want OpMapLookup", lookup.Op)
	}
	if len(lookup.Operands) != 2 {
		t.Errorf("OpMapLookup must carry exactly 2 operands (map, key); got %d", len(lookup.Operands))
	}
}

// --- Slice Y.C: LHS selector / indexed-set opcodes ---

// TestFieldSetOpcodeShape documents the operand encoding for
// `target.<Value> = expr`. Operands[0] is the target expression (which
// must evaluate to an ObjectVal with a populated Fields map); the
// field name lives in Value because it's always a compile-time
// identifier in Go; Operands[1] is the value expression.
func TestFieldSetOpcodeShape(t *testing.T) {
	fset := Expr{
		Op:       OpFieldSet,
		Value:    "X",
		Operands: []ExprID{0, 1}, // (target, value)
	}
	if fset.Op != OpFieldSet {
		t.Errorf("opcode = %d, want OpFieldSet", fset.Op)
	}
	if fset.Value != "X" {
		t.Errorf("OpFieldSet field name lives in Value, want %q got %q", "X", fset.Value)
	}
	if len(fset.Operands) != 2 {
		t.Errorf("OpFieldSet must carry exactly 2 operands (target, value); got %d", len(fset.Operands))
	}
}

// TestIndexSetOpcodeShape documents the operand encoding for
// `target[key] = expr`. Operands[0] is the collection, Operands[1] is
// the key expression (evaluated at runtime — string for maps, int for
// slices), Operands[2] is the value expression.
func TestIndexSetOpcodeShape(t *testing.T) {
	iset := Expr{
		Op:       OpIndexSet,
		Operands: []ExprID{0, 1, 2}, // (collection, key, value)
	}
	if iset.Op != OpIndexSet {
		t.Errorf("opcode = %d, want OpIndexSet", iset.Op)
	}
	if len(iset.Operands) != 3 {
		t.Errorf("OpIndexSet must carry exactly 3 operands (collection, key, value); got %d", len(iset.Operands))
	}
	if iset.Value != "" {
		t.Errorf("OpIndexSet Value must stay empty (key lives in Operands[1]); got %q", iset.Value)
	}
}

// --- Slice Y.D: user-defined function call opcode ---

// TestIndirectCallOpcodeIsDistinct guards Y.D's iota slot from
// colliding with Y.A/Y.B/Y.C additions. A collision would route every
// user-function call into the wrong evaluator silently.
func TestIndirectCallOpcodeIsDistinct(t *testing.T) {
	all := []OpCode{
		// pre-existing (TestOpCodeRange + Y.A/Y.B/Y.C)
		OpLitString, OpLitInt, OpLitFloat, OpLitBool,
		OpPropGet, OpSignalGet, OpSignalSet, OpSignalUpdate,
		OpAdd, OpSub, OpMul, OpDiv, OpMod, OpNeg,
		OpEq, OpNeq, OpLt, OpGt, OpLte, OpGte,
		OpAnd, OpOr, OpNot,
		OpConcat, OpFormat,
		OpCond, OpCall, OpIndex, OpLen, OpRange,
		OpEventGet,
		OpMap, OpFilter, OpFind, OpSlice, OpAppend, OpContains,
		OpToUpper, OpToLower, OpTrim, OpSplit, OpJoin, OpReplace,
		OpSubstring, OpStartsWith, OpEndsWith,
		OpToString, OpToInt, OpToFloat,
		OpSeq, OpAssign, OpLocalDecl, OpLocalGet, OpLocalSet,
		OpFor, OpForRange, OpReturn, OpBreak, OpContinue,
		OpComposite, OpMapLookup, OpFieldSet, OpIndexSet,
		// Y.D
		OpIndirectCall,
		// Y.E (new)
		OpMake,
	}
	seen := map[OpCode]bool{}
	for _, op := range all {
		if seen[op] {
			t.Errorf("OpCode %d appears twice in the opcode ladder", op)
		}
		seen[op] = true
	}
}

// TestIndirectCallOpcodeShape documents the operand encoding for a
// user-function call. The callee name lives in Value (the FuncDef
// lookup key); Operands are the argument expressions in source order.
func TestIndirectCallOpcodeShape(t *testing.T) {
	call := Expr{
		Op:       OpIndirectCall,
		Value:    "helper",
		Operands: []ExprID{0, 1, 2}, // three args
	}
	if call.Op != OpIndirectCall {
		t.Errorf("opcode = %d, want OpIndirectCall", call.Op)
	}
	if call.Value != "helper" {
		t.Errorf("OpIndirectCall callee name lives in Value, want %q got %q", "helper", call.Value)
	}
	if len(call.Operands) != 3 {
		t.Errorf("OpIndirectCall Operands carry the call args; got len=%d", len(call.Operands))
	}
}

// TestFuncDefShape verifies the FuncDef carrier fields are wired so the
// lowerer can stash the per-surface registry. Params is the ordered
// parameter-name list; Body is the OpSeq root for the function's
// statement body; Results is the count of return values (0 for void,
// 1 for the common case, 2+ for multi-return).
func TestFuncDefShape(t *testing.T) {
	def := FuncDef{
		Name:    "split",
		Params:  []string{"n"},
		Body:    []ExprID{0},
		Results: 2,
	}
	if def.Name != "split" {
		t.Errorf("FuncDef.Name = %q, want %q", def.Name, "split")
	}
	if len(def.Params) != 1 || def.Params[0] != "n" {
		t.Errorf("FuncDef.Params = %v, want [n]", def.Params)
	}
	if len(def.Body) != 1 {
		t.Errorf("FuncDef.Body should carry the OpSeq root; got len=%d", len(def.Body))
	}
	if def.Results != 2 {
		t.Errorf("FuncDef.Results = %d, want 2", def.Results)
	}
}

// TestProgramFuncsField verifies the Program.Funcs slice serializes
// round-trip through JSON without dropping FuncDef metadata. This is
// the carrier that ferries a surface's user-helpers to the VM.
func TestProgramFuncsField(t *testing.T) {
	p := Program{
		Name: "TestSurface",
		Funcs: []FuncDef{
			{Name: "greet", Params: nil, Body: []ExprID{0}, Results: 1},
			{Name: "split", Params: []string{"n"}, Body: []ExprID{1}, Results: 2},
		},
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var p2 Program
	if err := json.Unmarshal(data, &p2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(p2.Funcs) != 2 {
		t.Fatalf("round-trip len(Funcs) = %d, want 2", len(p2.Funcs))
	}
	if p2.Funcs[1].Results != 2 {
		t.Errorf("round-trip Funcs[1].Results = %d, want 2", p2.Funcs[1].Results)
	}
	if len(p2.Funcs[1].Params) != 1 || p2.Funcs[1].Params[0] != "n" {
		t.Errorf("round-trip Funcs[1].Params = %v, want [n]", p2.Funcs[1].Params)
	}
}

// TestProgramMaxCallDepthDefault documents that an unset (zero)
// MaxCallDepth means "use DefaultMaxCallDepth". The VM reads the field
// at OpIndirectCall dispatch time; surfaces opt into a deeper stack by
// raising the value at lowering time.
func TestProgramMaxCallDepthDefault(t *testing.T) {
	p := Program{Name: "TestSurface"}
	if p.MaxCallDepth != 0 {
		t.Errorf("zero MaxCallDepth = %d, want 0 (sentinel for DefaultMaxCallDepth)", p.MaxCallDepth)
	}
	if DefaultMaxCallDepth != 256 {
		t.Errorf("DefaultMaxCallDepth = %d, want 256", DefaultMaxCallDepth)
	}
}

// TestMakeOpcodeShape documents the operand encoding for the make()
// builtin (Slice Y.E). Maps carry no operands (the optional capacity
// hint is dropped at lowering time); slices carry the length expression
// in Operands[0] (the optional cap argument is dropped). The kind tag
// lives in Value — "map" or "slice".
func TestMakeOpcodeShape(t *testing.T) {
	mk := Expr{Op: OpMake, Value: "map"}
	if mk.Op != OpMake {
		t.Errorf("opcode = %d, want OpMake", mk.Op)
	}
	if mk.Value != "map" {
		t.Errorf("OpMake kind tag lives in Value, got %q", mk.Value)
	}
	if len(mk.Operands) != 0 {
		t.Errorf("OpMake(map) has no operands; got len=%d", len(mk.Operands))
	}

	sl := Expr{Op: OpMake, Value: "slice", Operands: []ExprID{42}}
	if sl.Value != "slice" {
		t.Errorf("OpMake(slice) kind tag = %q, want \"slice\"", sl.Value)
	}
	if len(sl.Operands) != 1 || sl.Operands[0] != 42 {
		t.Errorf("OpMake(slice) Operands[0] carries the length expr, got %v", sl.Operands)
	}
}
