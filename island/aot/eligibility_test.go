package aot

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
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
