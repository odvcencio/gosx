package program

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

func slotProgram() *Program {
	return &Program{Name: "Panels", Nodes: []Node{{Kind: NodeElement, Tag: "div", Children: []NodeID{1}}, {Kind: NodeServerSlot, Tag: "div", Text: "Profile"}}}
}

func TestServerSlotWire(t *testing.T) {
	p := slotProgram()
	jsonData, err := EncodeJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(jsonData, []byte(`"requires": "server-slots-v1"`)) {
		t.Fatal(string(jsonData))
	}
	if _, err := DecodeJSON(jsonData); err != nil {
		t.Fatal(err)
	}
	bad := bytes.Replace(jsonData, []byte(`"server-slots-v1"`), []byte(`"unknown"`), 1)
	if _, err := DecodeJSON(bad); err == nil {
		t.Fatal("accepted unsupported capability")
	}
	bin, err := EncodeBinary(p)
	if err != nil {
		t.Fatal(err)
	}
	if bin[4] != 2 {
		t.Fatalf("version %d", bin[4])
	}
	if _, err := DecodeBinary(bin); err != nil {
		t.Fatal(err)
	}
	bin[4] = 1
	if _, err := DecodeBinary(bin); err == nil {
		t.Fatal("accepted v1 projection")
	}
	legacy := &Program{Name: "Plain", Nodes: []Node{{Kind: NodeElement, Tag: "div"}}}
	bin, err = EncodeBinary(legacy)
	if err != nil || bin[4] != 1 {
		t.Fatalf("legacy version changed: %v", err)
	}
	jsonData, err = EncodeJSON(legacy)
	if err != nil || bytes.Contains(jsonData, []byte("requires")) {
		t.Fatalf("legacy metadata changed: %v", err)
	}
}

func TestServerSlotExpressionDAGAndCycle(t *testing.T) {
	p := slotProgram()
	p.Exprs = []Expr{{Op: OpLitString, Value: "x", Type: TypeString}}
	for i := 1; i < 50; i++ {
		p.Exprs = append(p.Exprs, Expr{Op: OpConcat, Operands: []ExprID{ExprID(i - 1), ExprID(i - 1)}, Type: TypeString})
	}
	p.Nodes[1].Attrs = []Attr{{Kind: AttrExpr, Name: "id", Expr: 49}}
	if err := p.ValidateServerSlots(); err != nil {
		t.Fatal(err)
	}
	p.Exprs[0] = Expr{Op: OpConcat, Operands: []ExprID{49, 49}, Type: TypeString}
	if err := p.ValidateServerSlots(); err == nil {
		t.Fatal("cyclic shell expression admitted")
	}
}

func TestServerSlotMalformedAttrWire(t *testing.T) {
	for _, attr := range []Attr{{Kind: AttrStatic, Name: "hidden", Value: "false"}, {Kind: AttrBool, Name: "id"}, {Kind: AttrStatic, Name: "tabindex", Value: "1"}, {Kind: AttrExpr, Name: "hidden", Expr: 0}} {
		p := slotProgram()
		p.Nodes[1].Attrs = []Attr{attr}
		p.Exprs = []Expr{{Op: OpLitString, Value: "true", Type: TypeBool}}
		if _, err := EncodeJSON(p); err == nil {
			t.Fatalf("JSON admitted %+v", attr)
		}
		if _, err := EncodeBinary(p); err == nil {
			t.Fatalf("binary admitted %+v", attr)
		}
	}
}

func TestServerSlotTamperedDecoderAttributes(t *testing.T) {
	for _, attr := range []Attr{{Kind: AttrStatic, Name: "hidden", Value: "false"}, {Kind: AttrBool, Name: "class"}, {Kind: AttrStatic, Name: "role", Value: "button"}, {Kind: AttrStatic, Name: "tabindex", Value: "2"}, {Kind: AttrStatic, Name: "role", Value: "tabpanel"}} {
		t.Run(attr.Name, func(t *testing.T) {
			valid := attr.Value == "tabpanel"
			p := slotProgram()
			p.Nodes[1].Attrs = []Attr{attr}
			// Marshal directly, deliberately bypassing EncodeJSON validation.
			raw, err := json.Marshal(struct {
				*Program
				Requires string `json:"requires"`
			}{p, ServerSlotCapability})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeJSON(raw); (valid && err != nil) || (!valid && (err == nil || !strings.Contains(err.Error(), "ServerSlot "+attr.Name))) {
				t.Fatalf("unexpected tampered JSON diagnostic: %v", err)
			}
			// Encode an ordinary element, then introduce the opaque node kind
			// and v2 header directly into its wire representation.
			p.Nodes[1].Kind = NodeElement
			raw, err = EncodeBinary(p)
			if err != nil {
				t.Fatal(err)
			}
			binary.LittleEndian.PutUint16(raw[4:6], 2)
			found := false
			for offset := 8; offset+5 <= len(raw); {
				size := int(binary.LittleEndian.Uint32(raw[offset+1 : offset+5]))
				start := offset + 5
				if raw[offset] == secNodes {
					pos := start + 6
					attrs := int(binary.LittleEndian.Uint16(raw[pos+7 : pos+9]))
					pos += 9 + attrs*9
					children := int(binary.LittleEndian.Uint16(raw[pos : pos+2]))
					pos += 2 + children*2
					raw[pos] = byte(NodeServerSlot)
					found = true
					break
				}
				offset = start + size
			}
			if !found {
				t.Fatal("missing node section")
			}
			if _, err := DecodeBinary(raw); (valid && err != nil) || (!valid && (err == nil || !strings.Contains(err.Error(), "ServerSlot "+attr.Name))) {
				t.Fatalf("unexpected tampered binary diagnostic: %v", err)
			}
		})
	}
}

func TestServerSlotInvalidInventory(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Program)
		want   string
	}{
		{"duplicate", func(p *Program) {
			p.Nodes = append(p.Nodes, p.Nodes[1])
			p.Nodes[0].Children = append(p.Nodes[0].Children, 2)
		}, "duplicate"},
		{"missing name", func(p *Program) { p.Nodes[1].Text = "" }, "name"},
		{"table", func(p *Program) { p.Nodes[0].Tag = "table" }, "not allowed"},
		{"dynamic", func(p *Program) { p.Nodes[0].Kind = NodeConditional }, "stable"},
		{"child", func(p *Program) { p.Nodes[1].Children = []NodeID{0} }, "stable"},
		{"event", func(p *Program) { p.Nodes[1].Attrs = []Attr{{Kind: AttrEvent, Name: "onClick"}} }, "event"},
		{"arbitrary attr", func(p *Program) { p.Nodes[1].Attrs = []Attr{{Name: "as", Value: "td"}} }, "unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := slotProgram()
			tc.mutate(p)
			if err := p.ValidateServerSlots(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v want %s", err, tc.want)
			}
		})
	}
}
