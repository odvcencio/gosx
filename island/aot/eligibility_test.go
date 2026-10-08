package aot

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/island/program"
)

func staticUnit(t *testing.T) Unit {
	t.Helper()
	p := &program.Program{Name: "Example", Nodes: []program.Node{{Kind: program.NodeElement, Tag: "div"}}, StaticMask: []bool{true}}
	c := ScalarContract{Version: 1, Component: "example/components.Example", Bindings: []BindingContract{{Nodes: []program.NodeID{0}, Kind: program.NodeElement, Tag: "div"}}}
	u, err := NewUnit(c.Component, p, c)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func refreshUnit(t *testing.T, u Unit) Unit {
	t.Helper()
	v, err := NewUnit(u.Component, u.Program, u.Contract)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func literalUnit(t *testing.T) Unit {
	u := staticUnit(t)
	u.Program.Exprs = []program.Expr{{Op: program.OpLitInt, Type: program.TypeInt, Value: "0"}}
	u.Contract.Expressions = []ExpressionContract{{Expr: 0, Kind: Int, Pure: true}}
	return refreshUnit(t, u)
}

func TestClassifyStableStructuralReceipts(t *testing.T) {
	cases := []struct {
		name, reason, table string
		index               int
		mutate              func(*Unit)
	}{
		{"root", "reference", "root", 2, func(u *Unit) { u.Program.Root = 2 }},
		{"unknown opcode", "opcode_unknown", "expressions", 0, func(u *Unit) { u.Program.Exprs[0].Op = 255 }},
		{"unknown type", "type_unsupported", "expressions", 0, func(u *Unit) { u.Program.Exprs[0].Type = 255 }},
		{"float", "type_unsupported", "expressions", 0, func(u *Unit) { u.Program.Exprs[0].Type = program.TypeFloat }},
		{"operand zero cycle", "expression_cycle", "expressions", 0, func(u *Unit) { u.Program.Exprs[0].Operands = []program.ExprID{0} }},
		{"operand reference", "reference", "expressions", 0, func(u *Unit) { u.Program.Exprs[0].Operands = []program.ExprID{1} }},
		{"source width", "contract_expression", "expressions", 0, func(u *Unit) { u.Contract.Expressions[0].Kind = "int64" }},
		{"literal arity", "arity", "expressions", 1, func(u *Unit) {
			u.Program.Exprs = append(u.Program.Exprs, program.Expr{Op: program.OpLitInt, Type: program.TypeInt, Value: "1", Operands: []program.ExprID{0}})
			u.Contract.Expressions = append(u.Contract.Expressions, ExpressionContract{Expr: 1, Kind: Int, Pure: true})
		}},
		{"static mask", "static_mask", "nodes", 0, func(u *Unit) { u.Program.StaticMask[0] = false }},
		{"mask count", "static_mask", "nodes", -1, func(u *Unit) { u.Program.StaticMask = nil }},
		{"duplicate ownership", "node_graph", "nodes", -1, func(u *Unit) {
			u.Program.Nodes = append(u.Program.Nodes, program.Node{Kind: program.NodeText})
			u.Program.Nodes[0].Children = []program.NodeID{1, 1}
			u.Program.StaticMask = append(u.Program.StaticMask, true)
		}},
		{"node cycle", "node_graph", "nodes", -1, func(u *Unit) { u.Program.Nodes[0].Children = []program.NodeID{0} }},
		{"unused malformed node", "node_unsupported", "nodes", 1, func(u *Unit) {
			u.Program.Nodes = append(u.Program.Nodes, program.Node{Kind: 255})
			u.Program.StaticMask = append(u.Program.StaticMask, true)
		}},
		{"fragment", "root_kind", "nodes", 0, func(u *Unit) { u.Program.Nodes[0].Kind = program.NodeFragment }},
		{"unknown attr", "attribute_unknown", "nodes", 0, func(u *Unit) { u.Program.Nodes[0].Attrs = []program.Attr{{Kind: 255}} }},
		{"handler reference", "handler_undeclared", "nodes", 0, func(u *Unit) {
			u.Program.Nodes[0].Attrs = []program.Attr{{Kind: program.AttrEvent, Name: "click", Event: "missing"}}
		}},
		{"signal reference", "state_undeclared", "expressions", 0, func(u *Unit) {
			u.Program.Exprs[0] = program.Expr{Op: program.OpSignalGet, Value: "missing", Type: program.TypeInt}
		}},
		{"bindings", "contract_binding", "bindings", -1, func(u *Unit) { u.Contract.Bindings[0].Tag = "span" }},
		{"unknown source version", "contract_missing", "contract", -1, func(u *Unit) { u.Contract.Version = 2 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := literalUnit(t)
			tc.mutate(&u)
			// Structural failures are checked independently of the retained hash.
			r := Classify(u, ScalarDOMV1)
			if r.Eligible || r.Reason != tc.reason || r.Table != tc.table || r.Index != tc.index {
				t.Fatalf("receipt=%+v", r)
			}
			if r.UnitDigest != u.Digest || !reflect.DeepEqual(r, Classify(u, ScalarDOMV1)) {
				t.Fatal("unstable rejection receipt")
			}
		})
	}
	if r := Classify(Unit{}, ScalarDOMV1); r.Reason != "program_missing" {
		t.Fatalf("absent unit: %+v", r)
	}
	if r := Classify(staticUnit(t), 99); r.Reason != "profile_unsupported" {
		t.Fatalf("unknown profile: %+v", r)
	}
}

func TestClassifyLiteralDomainAndUnknownOperations(t *testing.T) {
	for _, value := range []string{"-2147483648", "-1", "0", "2147483647"} {
		u := literalUnit(t)
		u.Program.Exprs[0].Value = value
		u = refreshUnit(t, u)
		if r := Classify(u, ScalarDOMV1); !r.Eligible {
			t.Fatalf("in-domain %s: %+v", value, r)
		}
	}
	for _, value := range []string{"2147483648", "-2147483649", "9007199254740993", "+1", "01", "0x1", "1.0", ""} {
		u := literalUnit(t)
		u.Program.Exprs[0].Value = value
		u = refreshUnit(t, u)
		if r := Classify(u, ScalarDOMV1); r.Reason != "integer_literal" {
			t.Fatalf("literal %s: %+v", value, r)
		}
	}
	for _, op := range []program.OpCode{program.OpDiv, program.OpMod, program.OpCall, program.OpToRunes, program.OpHostCall, program.OpClosure, program.OpAdd} {
		u := literalUnit(t)
		u.Program.Exprs[0].Op = op
		u = refreshUnit(t, u)
		if r := Classify(u, ScalarDOMV1); r.Reason != "opcode_unsupported" {
			t.Fatalf("opcode %v: %+v", op, r)
		}
	}
	for _, tc := range []struct {
		op       program.OpCode
		typ      program.ExprType
		kind     ScalarKind
		value    string
		eligible bool
	}{
		{program.OpLitString, program.TypeString, String, "héllo 🌴\x00", true},
		{program.OpLitString, program.TypeString, String, strings.Repeat("x", 4096), true},
		{program.OpLitString, program.TypeString, String, strings.Repeat("x", 4097), false},
		{program.OpLitBool, program.TypeBool, Bool, "false", true},
		{program.OpLitBool, program.TypeBool, Bool, "true", true},
		{program.OpLitBool, program.TypeBool, Bool, "TRUE", false},
		{program.OpLitInt, program.TypeAny, Int, "1", false},
	} {
		u := literalUnit(t)
		u.Program.Exprs[0] = program.Expr{Op: tc.op, Type: tc.typ, Value: tc.value}
		u.Contract.Expressions[0].Kind = tc.kind
		u = refreshUnit(t, u)
		if r := Classify(u, ScalarDOMV1); r.Eligible != tc.eligible {
			t.Fatalf("literal case %+v: %+v", tc, r)
		}
	}
}

func TestClassifyParserTopologyAndUnsafeAttributes(t *testing.T) {
	for _, tag := range []string{"svg", "math", "script", "style", "table", "custom-element", "form"} {
		u := staticUnit(t)
		u.Program.Nodes[0].Tag = tag
		if r := Classify(u, ScalarDOMV1); r.Reason != "topology_unsupported" {
			t.Fatalf("tag %s: %+v", tag, r)
		}
	}
	for _, tags := range [][2]string{{"p", "div"}, {"input", "span"}, {"select", "div"}, {"li", "li"}} {
		u := staticUnit(t)
		u.Program.Nodes = []program.Node{{Kind: program.NodeElement, Tag: tags[0], Children: []program.NodeID{1}}, {Kind: program.NodeElement, Tag: tags[1]}}
		u.Program.StaticMask = []bool{true, true}
		u.Contract.Bindings, _ = ContractBindings(u.Program)
		u = refreshUnit(t, u)
		if r := Classify(u, ScalarDOMV1); r.Reason != "parser_topology" {
			t.Fatalf("parser shape %v: %+v", tags, r)
		}
	}
	for _, attr := range []program.Attr{{Name: "onclick", Value: "run()"}, {Name: "href", Value: "javascript:run()"}, {Name: "data-gosx-island", Value: "nested"}, {Name: "formaction", Value: "/save"}, {Name: "data-action", Value: "save"}, {Name: "bad name"}} {
		u := staticUnit(t)
		u.Program.Nodes[0].Attrs = []program.Attr{attr}
		if r := Classify(u, ScalarDOMV1); r.Reason != "attribute_unsupported" {
			t.Fatalf("unsafe attribute %+v: %+v", attr, r)
		}
	}
	u := staticUnit(t)
	u.Program.Nodes[0].Tag = "button"
	if r := Classify(u, ScalarDOMV1); r.Reason != "submit_ambiguous" {
		t.Fatalf("implicit submit: %+v", r)
	}
}

func TestClassifyDependencyDepthAndComputedCycle(t *testing.T) {
	for _, count := range []int{64, 65} {
		u := literalUnit(t)
		for i := 1; i < count; i++ {
			u.Program.Exprs = append(u.Program.Exprs, program.Expr{Op: program.OpSeq, Type: program.TypeInt, Operands: []program.ExprID{program.ExprID(i - 1)}})
			u.Contract.Expressions = append(u.Contract.Expressions, ExpressionContract{Expr: program.ExprID(i), Kind: Int, Pure: true})
		}
		u = refreshUnit(t, u)
		r := Classify(u, ScalarDOMV1)
		want := "opcode_unsupported"
		if count == 65 {
			want = "expression_depth"
		}
		if r.Reason != want {
			t.Fatalf("depth %d: %+v", count, r)
		}
	}
	u := literalUnit(t)
	u.Program.Exprs[0] = program.Expr{Op: program.OpSignalGet, Value: "derived", Type: program.TypeAny}
	u.Program.Computeds = []program.ComputedDef{{Name: "derived", Expr: 0, Type: program.TypeAny}}
	u.Contract.Computeds = []StateContract{{Name: "derived", Kind: Int}}
	u = refreshUnit(t, u)
	if r := Classify(u, ScalarDOMV1); r.Reason != "expression_cycle" {
		t.Fatalf("computed cycle: %+v", r)
	}
}

func TestClassifyBoundsIdentityAndStateTags(t *testing.T) {
	for _, count := range []int{256, 257} {
		u := staticUnit(t)
		for i := 1; i < count; i++ {
			u.Program.Nodes = append(u.Program.Nodes, program.Node{Kind: program.NodeText, Text: "x"})
			u.Program.Nodes[0].Children = append(u.Program.Nodes[0].Children, program.NodeID(i))
			u.Program.StaticMask = append(u.Program.StaticMask, true)
		}
		if count == 256 {
			u.Contract.Bindings, _ = ContractBindings(u.Program)
			u = refreshUnit(t, u)
		}
		r := Classify(u, ScalarDOMV1)
		if count == 256 && !r.Eligible || count == 257 && r.Reason != "limit" {
			t.Fatalf("node bound %d: %+v", count, r)
		}
	}
	for _, mutate := range []func(*Unit){func(u *Unit) { u.Program.Name = "changed" }, func(u *Unit) { u.ProgramBytes[0] ^= 1 }, func(u *Unit) { u.ProgramSHA[0] ^= 1 }, func(u *Unit) { u.ContractSHA[0] ^= 1 }, func(u *Unit) { u.Digest[0] ^= 1 }} {
		u := staticUnit(t)
		mutate(&u)
		if r := Classify(u, ScalarDOMV1); r.Reason != "unit_identity" {
			t.Fatalf("mutated identity: %+v", r)
		}
	}
	u := literalUnit(t)
	u.Program.Signals = []program.SignalDef{{Name: "count", Type: program.TypeBool, Init: 0}}
	u.Contract.Signals = []StateContract{{Name: "count", Kind: Bool}}
	u = refreshUnit(t, u)
	if r := Classify(u, ScalarDOMV1); r.Reason != "state_kind" {
		t.Fatalf("incompatible state tag: %+v", r)
	}
	u.Program.Signals = append(u.Program.Signals, u.Program.Signals[0])
	u.Contract.Signals = append(u.Contract.Signals, StateContract{Slot: 1, Name: "count", Kind: Bool})
	if r := Classify(u, ScalarDOMV1); r.Reason != "duplicate_name" {
		t.Fatalf("duplicate state: %+v", r)
	}
}

func TestUnitCanonicalIdentityAndOwnership(t *testing.T) {
	u := staticUnit(t)
	contractBytes, err := json.Marshal(u.Contract)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":1,"component":"example/components.Example","expressions":[],"inputs":[],"signals":[],"computeds":[],"bindings":[{"id":0,"nodes":[0],"kind":0,"tag":"div","attributes":[]}]}`
	if string(contractBytes) != want {
		t.Fatalf("canonical contract: %s", contractBytes)
	}
	identity := []byte("gosx-aot-unit-v1\x00")
	identity = binary.LittleEndian.AppendUint32(identity, uint32(len(u.Component)))
	identity = append(identity, u.Component...)
	identity = append(identity, u.ProgramSHA[:]...)
	identity = append(identity, u.ContractSHA[:]...)
	if u.ProgramSHA != sha256.Sum256(u.ProgramBytes) || u.ContractSHA != sha256.Sum256(contractBytes) || u.Digest != sha256.Sum256(identity) {
		t.Fatal("incorrect full identity")
	}
	p, c := u.Program, u.Contract
	u2, err := NewUnit(u.Component, p, c)
	if err != nil || u.Digest != u2.Digest {
		t.Fatal("same inputs changed identity")
	}
	p.Name = "Changed"
	c.Bindings[0].Nodes[0] = 5
	if u2.Program.Name != "Example" || u2.Contract.Bindings[0].Nodes[0] != 0 {
		t.Fatal("unit shares mutable input tables")
	}
	u3, err := NewUnit(u2.Component, u2.Program, ScalarContract{Version: 1, Component: u2.Component})
	if err != nil || u3.Digest == u2.Digest {
		t.Fatal("contract identity did not enter the digest")
	}
}

func TestUnitBinaryRangesAndEnvelope(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*program.Program)
	}{
		{"version", func(p *program.Program) { p.Version = "2" }},
		{"surface", func(p *program.Program) { p.Surface = program.SurfaceScene3D }},
		{"functions", func(p *program.Program) { p.Funcs = []program.FuncDef{{}} }},
		{"engine", func(p *program.Program) { p.EngineNodes = []program.EngineNode{{}} }},
		{"depth", func(p *program.Program) { p.MaxCallDepth = 1 }},
		{"string", func(p *program.Program) { p.Name = strings.Repeat("x", 65536) }},
		{"UTF-8", func(p *program.Program) { p.Name = string([]byte{255}) }},
		{"count", func(p *program.Program) { p.Props = make([]program.PropDef, 65536) }},
		{"operands", func(p *program.Program) { p.Exprs = []program.Expr{{Operands: make([]program.ExprID, 65536)}} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := staticUnit(t)
			tc.mutate(u.Program)
			if _, err := NewUnit(u.Component, u.Program, u.Contract); err == nil {
				t.Fatal("accepted unsupported binary input")
			}
		})
	}
	u := staticUnit(t)
	if _, err := NewUnit(u.Component, nil, u.Contract); err == nil {
		t.Fatal("accepted absent program")
	}
	if _, err := NewUnit("", u.Program, u.Contract); err == nil {
		t.Fatal("accepted absent component")
	}
	decoded, err := program.DecodeBinary(u.ProgramBytes)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := program.EncodeBinary(decoded)
	if err != nil || !bytes.Equal(encoded, u.ProgramBytes) {
		t.Fatal("binary round trip changed canonical program")
	}
}

func TestContractBindingsGroupsTextAndRejectsGraphs(t *testing.T) {
	p := &program.Program{Nodes: []program.Node{
		{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1, 2, 3, 4}},
		{Kind: program.NodeText, Text: "prefix"}, {Kind: program.NodeExpr}, {Kind: program.NodeText},
		{Kind: program.NodeElement, Tag: "span", Attrs: []program.Attr{{Name: "class"}, {Kind: program.AttrEvent, Name: "click", Event: "tap"}}},
	}}
	b, err := ContractBindings(p)
	if err != nil || len(b) != 3 || len(b[1].Nodes) != 3 || b[2].ID != 2 || len(b[2].Attributes) != 1 {
		t.Fatalf("bindings=%+v error=%v", b, err)
	}
	for _, children := range [][]program.NodeID{{0}, {1, 1}, {7}} {
		p.Nodes[0].Children = children
		if _, err := ContractBindings(p); err == nil {
			t.Fatal("accepted invalid binding graph")
		}
	}
	p.Nodes[0].Children = []program.NodeID{1, 2, 3, 4}
	p.Nodes[2].Kind = program.NodeConditional
	if _, err := ContractBindings(p); err == nil {
		t.Fatal("accepted structural node")
	}
}
