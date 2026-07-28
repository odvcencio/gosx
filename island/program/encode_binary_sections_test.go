package program

import (
	"bytes"
	"reflect"
	"testing"
)

// TestEncodeBinaryCarriesFuncsEngineNodesAndMeta pins the three fields the
// binary format used to drop in silence.
//
// EncodeBinary is the production wire format. Before sections 0x08 to 0x0A
// existed, a program with user functions, engine nodes or a raised
// MaxCallDepth encoded cleanly and decoded with those fields empty, so a
// closure call or a scene node vanished with no error anywhere.
func TestEncodeBinaryCarriesFuncsEngineNodesAndMeta(t *testing.T) {
	want := &Program{
		Version: "1",
		Name:    "Widget",
		Root:    0,
		Nodes: []Node{{
			Kind:     NodeElement,
			Tag:      "div",
			Attrs:    []Attr{},
			Children: []NodeID{},
		}},
		Props:      []PropDef{},
		Exprs:      []Expr{{Op: OpLitInt, Value: "7", Type: TypeInt, Operands: []ExprID{}}},
		Signals:    []SignalDef{},
		Computeds:  []ComputedDef{},
		Handlers:   []Handler{},
		StaticMask: []bool{true},
		Funcs: []FuncDef{
			{Name: "double", Params: []string{"n"}, Body: []ExprID{0}, Results: 1},
			{Name: "noop"},
			{Name: "split", Params: []string{"a", "b"}, Body: []ExprID{0, 0}, Results: 2},
		},
		EngineNodes: []EngineNode{
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "standard",
				Props:    map[string]ExprID{"z": 0, "a": 0, "m": 0},
				Children: []int{1, 2},
				Static:   true,
			},
			{Kind: "group"},
		},
		MaxCallDepth: 1024,
	}

	data, err := EncodeBinary(want)
	if err != nil {
		t.Fatalf("EncodeBinary: %v", err)
	}
	got, err := DecodeBinary(data)
	if err != nil {
		t.Fatalf("DecodeBinary: %v", err)
	}

	if !reflect.DeepEqual(got.Funcs, want.Funcs) {
		t.Errorf("Funcs did not round-trip\n got: %#v\nwant: %#v", got.Funcs, want.Funcs)
	}
	if !reflect.DeepEqual(got.EngineNodes, want.EngineNodes) {
		t.Errorf("EngineNodes did not round-trip\n got: %#v\nwant: %#v", got.EngineNodes, want.EngineNodes)
	}
	if got.MaxCallDepth != want.MaxCallDepth {
		t.Errorf("MaxCallDepth = %d, want %d", got.MaxCallDepth, want.MaxCallDepth)
	}
	if got.Version != want.Version {
		t.Errorf("Version = %q, want %q", got.Version, want.Version)
	}
}

// TestEncodeBinaryLeavesNewSectionsEmpty confirms a program without the new
// fields decodes to the same zero values it started with, so nothing regressed
// for programs that never carried them.
func TestEncodeBinaryLeavesNewSectionsEmpty(t *testing.T) {
	want := &Program{
		Name:       "Plain",
		Nodes:      []Node{{Kind: NodeText, Text: "hi", Attrs: []Attr{}, Children: []NodeID{}}},
		Props:      []PropDef{},
		Exprs:      []Expr{},
		Signals:    []SignalDef{},
		Computeds:  []ComputedDef{},
		Handlers:   []Handler{},
		StaticMask: []bool{true},
	}
	data, err := EncodeBinary(want)
	if err != nil {
		t.Fatalf("EncodeBinary: %v", err)
	}
	got, err := DecodeBinary(data)
	if err != nil {
		t.Fatalf("DecodeBinary: %v", err)
	}
	if got.Funcs != nil {
		t.Errorf("Funcs = %#v, want nil", got.Funcs)
	}
	if got.EngineNodes != nil {
		t.Errorf("EngineNodes = %#v, want nil", got.EngineNodes)
	}
	if got.MaxCallDepth != 0 {
		t.Errorf("MaxCallDepth = %d, want 0", got.MaxCallDepth)
	}
	if got.Version != "" {
		t.Errorf("Version = %q, want the empty string", got.Version)
	}
}

// TestEncodeBinaryIsDeterministic pins byte-stable output.
//
// The build pipeline content-hashes the encoded program to name the asset, so
// two encodes of the same program must produce the same bytes. EngineNode.Props
// is a map, and Go randomizes map iteration order, so the encoder sorts its
// keys. This test fails if that sort is ever dropped.
func TestEncodeBinaryIsDeterministic(t *testing.T) {
	build := func() *Program {
		return &Program{
			Name:       "Scene",
			Nodes:      []Node{{Kind: NodeElement, Tag: "div", Attrs: []Attr{}, Children: []NodeID{}}},
			Props:      []PropDef{},
			Exprs:      []Expr{},
			Signals:    []SignalDef{},
			Computeds:  []ComputedDef{},
			Handlers:   []Handler{},
			StaticMask: []bool{},
			EngineNodes: []EngineNode{{
				Kind: "mesh",
				Props: map[string]ExprID{
					"alpha": 0, "beta": 0, "gamma": 0, "delta": 0,
					"epsilon": 0, "zeta": 0, "eta": 0, "theta": 0,
				},
			}},
		}
	}

	first, err := EncodeBinary(build())
	if err != nil {
		t.Fatalf("EncodeBinary: %v", err)
	}
	for round := 0; round < 40; round++ {
		next, err := EncodeBinary(build())
		if err != nil {
			t.Fatalf("round %d: EncodeBinary: %v", round, err)
		}
		if !bytes.Equal(first, next) {
			t.Fatalf("round %d: encoded bytes differ between runs; map key order leaked into the output", round)
		}
	}
}

// TestDecodeBinaryIgnoresUnknownSections proves an older decoder tolerates a
// newer encoding. The loop reads the section count from the header and skips any
// tag it does not know, so adding a section never breaks a shipped client.
func TestDecodeBinaryIgnoresUnknownSections(t *testing.T) {
	source := &Program{
		Name:       "Widget",
		Nodes:      []Node{{Kind: NodeText, Text: "x", Attrs: []Attr{}, Children: []NodeID{}}},
		Props:      []PropDef{},
		Exprs:      []Expr{},
		Signals:    []SignalDef{},
		Computeds:  []ComputedDef{},
		Handlers:   []Handler{},
		StaticMask: []bool{true},
	}
	data, err := EncodeBinary(source)
	if err != nil {
		t.Fatalf("EncodeBinary: %v", err)
	}

	// Append one unknown section and raise the header's section count.
	extended := append([]byte(nil), data...)
	extended = append(extended, 0x7f, 0x03, 0x00, 0x00, 0x00, 'a', 'b', 'c')
	byteOrder.PutUint16(extended[6:8], sectionCount+1)

	got, err := DecodeBinary(extended)
	if err != nil {
		t.Fatalf("DecodeBinary with an unknown section: %v", err)
	}
	if got.Name != source.Name {
		t.Errorf("Name = %q, want %q", got.Name, source.Name)
	}
	if len(got.Nodes) != 1 || got.Nodes[0].Text != "x" {
		t.Errorf("Nodes = %#v, want the single text node", got.Nodes)
	}
}
