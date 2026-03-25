package program

import (
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
		Root:   0,
		Exprs:  []Expr{{Op: OpLitString, Value: "hello", Type: TypeString}},
		Signals: []SignalDef{{Name: "count", Type: TypeInt, Init: 0}},
		Computeds: []ComputedDef{{Name: "double", Type: TypeInt, Expr: 0}},
		Handlers: []Handler{{Name: "click", Body: []ExprID{0}}},
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
	// Verify nested cond expression exists
	foundCond := 0
	for _, e := range p.Exprs {
		if e.Op == OpCond {
			foundCond++
		}
	}
	if foundCond < 2 {
		t.Errorf("expected at least 2 OpCond exprs (nested), got %d", foundCond)
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
	if len(p.Handlers) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(p.Handlers))
	}
	if p.Handlers[0].Name != "toggle" {
		t.Errorf("Handlers[0].Name = %q, want %q", p.Handlers[0].Name, "toggle")
	}
	if len(p.Nodes) != 4 {
		t.Errorf("len(Nodes) = %d, want 4", len(p.Nodes))
	}
	if len(p.StaticMask) != len(p.Nodes) {
		t.Errorf("StaticMask length %d != Nodes length %d", len(p.StaticMask), len(p.Nodes))
	}
	// Verify OpNot is used for toggle
	foundNot := false
	for _, e := range p.Exprs {
		if e.Op == OpNot {
			foundNot = true
		}
	}
	if !foundNot {
		t.Error("expected OpNot expression for toggle handler")
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
	if len(p.Handlers) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(p.Handlers))
	}
	handlerNames := map[string]bool{}
	for _, h := range p.Handlers {
		handlerNames[h.Name] = true
	}
	for _, name := range []string{"updateName", "validateForm"} {
		if !handlerNames[name] {
			t.Errorf("missing handler %q", name)
		}
	}
	if len(p.StaticMask) != len(p.Nodes) {
		t.Errorf("StaticMask length %d != Nodes length %d", len(p.StaticMask), len(p.Nodes))
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
