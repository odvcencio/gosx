// Binary serialization for IslandProgram (prod mode).
package program

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

var (
	magic     = [4]byte{'G', 'S', 'X', 0x00}
	byteOrder = binary.LittleEndian
)

const binaryVersion uint16 = 1

// Section type tags.
const (
	secStringTable uint8 = 0x00
	secProps       uint8 = 0x01
	secNodes       uint8 = 0x02
	secExprs       uint8 = 0x03
	secSignals     uint8 = 0x04
	secComputeds   uint8 = 0x05
	secHandlers    uint8 = 0x06
	secStaticMask  uint8 = 0x07
)

// --- String table helpers ---

type stringTable struct {
	index   map[string]uint16
	strings []string
}

func newStringTable() *stringTable {
	return &stringTable{index: make(map[string]uint16)}
}

func (st *stringTable) intern(s string) uint16 {
	if id, ok := st.index[s]; ok {
		return id
	}
	id := uint16(len(st.strings))
	st.index[s] = id
	st.strings = append(st.strings, s)
	return id
}

// internAll pre-interns every string in the program so the table is stable
// before encoding begins.
func (st *stringTable) internAll(p *Program) {
	st.intern(p.Name)
	for i := range p.Props {
		st.intern(p.Props[i].Name)
	}
	for i := range p.Nodes {
		st.intern(p.Nodes[i].Tag)
		st.intern(p.Nodes[i].Text)
		for j := range p.Nodes[i].Attrs {
			st.intern(p.Nodes[i].Attrs[j].Name)
			st.intern(p.Nodes[i].Attrs[j].Value)
			st.intern(p.Nodes[i].Attrs[j].Event)
		}
	}
	for i := range p.Exprs {
		st.intern(p.Exprs[i].Value)
	}
	for i := range p.Signals {
		st.intern(p.Signals[i].Name)
	}
	for i := range p.Computeds {
		st.intern(p.Computeds[i].Name)
	}
	for i := range p.Handlers {
		st.intern(p.Handlers[i].Name)
	}
}

// --- Encoder ---

// EncodeBinary serializes an IslandProgram to a compact binary format.
func EncodeBinary(p *Program) ([]byte, error) {
	st := newStringTable()
	st.internAll(p)

	var buf bytes.Buffer

	// Header: magic + version + section count
	buf.Write(magic[:])
	binary.Write(&buf, byteOrder, binaryVersion)

	sectionCount := uint16(8) // always 8 sections
	binary.Write(&buf, byteOrder, sectionCount)

	// Encode each section into a temp buffer, then write type+len+data.
	writeSection := func(tag uint8, data []byte) {
		buf.WriteByte(tag)
		binary.Write(&buf, byteOrder, uint32(len(data)))
		buf.Write(data)
	}

	// --- Section 0x00: StringTable ---
	writeSection(secStringTable, encodeStringTable(st))

	// --- Section 0x01: Props ---
	writeSection(secProps, encodeProps(p, st))

	// --- Section 0x02: Nodes ---
	writeSection(secNodes, encodeNodes(p, st))

	// --- Section 0x03: Exprs ---
	writeSection(secExprs, encodeExprs(p, st))

	// --- Section 0x04: Signals ---
	writeSection(secSignals, encodeSignals(p, st))

	// --- Section 0x05: Computeds ---
	writeSection(secComputeds, encodeComputeds(p, st))

	// --- Section 0x06: Handlers ---
	writeSection(secHandlers, encodeHandlers(p, st))

	// --- Section 0x07: StaticMask ---
	writeSection(secStaticMask, encodeStaticMask(p))

	return buf.Bytes(), nil
}

func encodeStringTable(st *stringTable) []byte {
	var buf bytes.Buffer
	// program name is always string 0 — but we just store the full table.
	// count of strings
	binary.Write(&buf, byteOrder, uint16(len(st.strings)))
	for _, s := range st.strings {
		binary.Write(&buf, byteOrder, uint16(len(s)))
		buf.WriteString(s)
	}
	return buf.Bytes()
}

func encodeProps(p *Program, st *stringTable) []byte {
	var buf bytes.Buffer
	binary.Write(&buf, byteOrder, uint16(len(p.Props)))
	for _, prop := range p.Props {
		binary.Write(&buf, byteOrder, st.intern(prop.Name))
		buf.WriteByte(byte(prop.Type))
	}
	return buf.Bytes()
}

func encodeNodes(p *Program, st *stringTable) []byte {
	var buf bytes.Buffer
	// node count + root ID
	binary.Write(&buf, byteOrder, uint16(len(p.Nodes)))
	binary.Write(&buf, byteOrder, p.Root)
	// name (as string index)
	binary.Write(&buf, byteOrder, st.intern(p.Name))

	for _, n := range p.Nodes {
		buf.WriteByte(byte(n.Kind))
		binary.Write(&buf, byteOrder, st.intern(n.Tag))
		binary.Write(&buf, byteOrder, st.intern(n.Text))
		binary.Write(&buf, byteOrder, n.Expr)

		// Attrs
		binary.Write(&buf, byteOrder, uint16(len(n.Attrs)))
		for _, a := range n.Attrs {
			buf.WriteByte(byte(a.Kind))
			binary.Write(&buf, byteOrder, st.intern(a.Name))
			binary.Write(&buf, byteOrder, st.intern(a.Value))
			binary.Write(&buf, byteOrder, a.Expr)
			binary.Write(&buf, byteOrder, st.intern(a.Event))
		}

		// Children
		binary.Write(&buf, byteOrder, uint16(len(n.Children)))
		for _, c := range n.Children {
			binary.Write(&buf, byteOrder, c)
		}
	}
	return buf.Bytes()
}

func encodeExprs(p *Program, st *stringTable) []byte {
	var buf bytes.Buffer
	binary.Write(&buf, byteOrder, uint16(len(p.Exprs)))
	for _, e := range p.Exprs {
		buf.WriteByte(byte(e.Op))
		buf.WriteByte(byte(e.Type))
		binary.Write(&buf, byteOrder, st.intern(e.Value))
		// Operands
		binary.Write(&buf, byteOrder, uint16(len(e.Operands)))
		for _, op := range e.Operands {
			binary.Write(&buf, byteOrder, op)
		}
	}
	return buf.Bytes()
}

func encodeSignals(p *Program, st *stringTable) []byte {
	var buf bytes.Buffer
	binary.Write(&buf, byteOrder, uint16(len(p.Signals)))
	for _, s := range p.Signals {
		binary.Write(&buf, byteOrder, st.intern(s.Name))
		buf.WriteByte(byte(s.Type))
		binary.Write(&buf, byteOrder, s.Init)
	}
	return buf.Bytes()
}

func encodeComputeds(p *Program, st *stringTable) []byte {
	var buf bytes.Buffer
	binary.Write(&buf, byteOrder, uint16(len(p.Computeds)))
	for _, c := range p.Computeds {
		binary.Write(&buf, byteOrder, st.intern(c.Name))
		buf.WriteByte(byte(c.Type))
		binary.Write(&buf, byteOrder, c.Expr)
	}
	return buf.Bytes()
}

func encodeHandlers(p *Program, st *stringTable) []byte {
	var buf bytes.Buffer
	binary.Write(&buf, byteOrder, uint16(len(p.Handlers)))
	for _, h := range p.Handlers {
		binary.Write(&buf, byteOrder, st.intern(h.Name))
		binary.Write(&buf, byteOrder, uint16(len(h.Body)))
		for _, id := range h.Body {
			binary.Write(&buf, byteOrder, id)
		}
	}
	return buf.Bytes()
}

func encodeStaticMask(p *Program) []byte {
	var buf bytes.Buffer
	binary.Write(&buf, byteOrder, uint16(len(p.StaticMask)))
	for _, b := range p.StaticMask {
		if b {
			buf.WriteByte(1)
		} else {
			buf.WriteByte(0)
		}
	}
	return buf.Bytes()
}

// --- Decoder ---

// binReader is an error-accumulating reader. Every read in DecodeBinary
// goes through this wrapper so errors propagate without repeated checks.
type binReader struct {
	r   io.Reader
	err error
}

func (br *binReader) read(data any) {
	if br.err != nil {
		return
	}
	br.err = binary.Read(br.r, byteOrder, data)
}

func (br *binReader) readFull(buf []byte) {
	if br.err != nil {
		return
	}
	_, br.err = io.ReadFull(br.r, buf)
}

func (br *binReader) readByte() byte {
	var b [1]byte
	br.readFull(b[:])
	return b[0]
}

func (br *binReader) readU16() uint16 {
	var v uint16
	br.read(&v)
	return v
}

func (br *binReader) readU32() uint32 {
	var v uint32
	br.read(&v)
	return v
}

// DecodeBinary deserializes an IslandProgram from the compact binary format.
func DecodeBinary(data []byte) (*Program, error) {
	br := &binReader{r: bytes.NewReader(data)}

	// --- Header ---
	var hdr [4]byte
	br.readFull(hdr[:])
	if br.err != nil {
		return nil, fmt.Errorf("binary decode: reading magic: %w", br.err)
	}
	if hdr != magic {
		return nil, fmt.Errorf("binary decode: invalid magic %q", hdr[:])
	}

	version := br.readU16()
	if br.err != nil {
		return nil, fmt.Errorf("binary decode: reading version: %w", br.err)
	}
	if version != binaryVersion {
		return nil, fmt.Errorf("binary decode: unsupported version %d", version)
	}

	sectionCount := br.readU16()
	if br.err != nil {
		return nil, fmt.Errorf("binary decode: reading section count: %w", br.err)
	}

	// Read all sections by tag.
	var p Program
	var strings []string

	for i := uint16(0); i < sectionCount; i++ {
		tag := br.readByte()
		length := br.readU32()
		if br.err != nil {
			return nil, fmt.Errorf("binary decode: reading section %d header: %w", i, br.err)
		}

		sectionData := make([]byte, length)
		br.readFull(sectionData)
		if br.err != nil {
			return nil, fmt.Errorf("binary decode: reading section %d data: %w", i, br.err)
		}

		sr := &binReader{r: bytes.NewReader(sectionData)}

		switch tag {
		case secStringTable:
			strings = decodeStringTable(sr)
		case secProps:
			p.Props = decodeProps(sr, strings)
		case secNodes:
			p.Nodes, p.Root, p.Name = decodeNodes(sr, strings)
		case secExprs:
			p.Exprs = decodeExprs(sr, strings)
		case secSignals:
			p.Signals = decodeSignals(sr, strings)
		case secComputeds:
			p.Computeds = decodeComputeds(sr, strings)
		case secHandlers:
			p.Handlers = decodeHandlers(sr, strings)
		case secStaticMask:
			p.StaticMask = decodeStaticMask(sr)
		}

		if sr.err != nil {
			return nil, fmt.Errorf("binary decode: section 0x%02x: %w", tag, sr.err)
		}
	}

	return &p, nil
}

func resolveString(strings []string, idx uint16) string {
	if int(idx) < len(strings) {
		return strings[idx]
	}
	return ""
}

func decodeStringTable(br *binReader) []string {
	count := br.readU16()
	strs := make([]string, count)
	for i := uint16(0); i < count; i++ {
		slen := br.readU16()
		buf := make([]byte, slen)
		br.readFull(buf)
		strs[i] = string(buf)
	}
	return strs
}

func decodeProps(br *binReader, strings []string) []PropDef {
	count := br.readU16()
	props := make([]PropDef, count)
	for i := uint16(0); i < count; i++ {
		nameIdx := br.readU16()
		typ := br.readByte()
		props[i] = PropDef{
			Name: resolveString(strings, nameIdx),
			Type: ExprType(typ),
		}
	}
	return props
}

func decodeNodes(br *binReader, strings []string) ([]Node, NodeID, string) {
	nodeCount := br.readU16()
	root := br.readU16()
	nameIdx := br.readU16()
	name := resolveString(strings, nameIdx)

	nodes := make([]Node, nodeCount)
	for i := uint16(0); i < nodeCount; i++ {
		kind := br.readByte()
		tagIdx := br.readU16()
		textIdx := br.readU16()
		expr := br.readU16()

		attrCount := br.readU16()
		attrs := make([]Attr, attrCount)
		for j := uint16(0); j < attrCount; j++ {
			ak := br.readByte()
			anIdx := br.readU16()
			avIdx := br.readU16()
			aExpr := br.readU16()
			aeIdx := br.readU16()
			attrs[j] = Attr{
				Kind:  AttrKind(ak),
				Name:  resolveString(strings, anIdx),
				Value: resolveString(strings, avIdx),
				Expr:  aExpr,
				Event: resolveString(strings, aeIdx),
			}
		}

		childCount := br.readU16()
		children := make([]NodeID, childCount)
		for j := uint16(0); j < childCount; j++ {
			children[j] = br.readU16()
		}

		nodes[i] = Node{
			Kind:     NodeKind(kind),
			Tag:      resolveString(strings, tagIdx),
			Text:     resolveString(strings, textIdx),
			Expr:     expr,
			Attrs:    attrs,
			Children: children,
		}
	}
	return nodes, root, name
}

func decodeExprs(br *binReader, strings []string) []Expr {
	count := br.readU16()
	exprs := make([]Expr, count)
	for i := uint16(0); i < count; i++ {
		op := br.readByte()
		typ := br.readByte()
		valIdx := br.readU16()
		opCount := br.readU16()
		operands := make([]ExprID, opCount)
		for j := uint16(0); j < opCount; j++ {
			operands[j] = br.readU16()
		}
		exprs[i] = Expr{
			Op:       OpCode(op),
			Type:     ExprType(typ),
			Value:    resolveString(strings, valIdx),
			Operands: operands,
		}
	}
	return exprs
}

func decodeSignals(br *binReader, strings []string) []SignalDef {
	count := br.readU16()
	signals := make([]SignalDef, count)
	for i := uint16(0); i < count; i++ {
		nameIdx := br.readU16()
		typ := br.readByte()
		init := br.readU16()
		signals[i] = SignalDef{
			Name: resolveString(strings, nameIdx),
			Type: ExprType(typ),
			Init: init,
		}
	}
	return signals
}

func decodeComputeds(br *binReader, strings []string) []ComputedDef {
	count := br.readU16()
	computeds := make([]ComputedDef, count)
	for i := uint16(0); i < count; i++ {
		nameIdx := br.readU16()
		typ := br.readByte()
		expr := br.readU16()
		computeds[i] = ComputedDef{
			Name: resolveString(strings, nameIdx),
			Type: ExprType(typ),
			Expr: expr,
		}
	}
	return computeds
}

func decodeHandlers(br *binReader, strings []string) []Handler {
	count := br.readU16()
	handlers := make([]Handler, count)
	for i := uint16(0); i < count; i++ {
		nameIdx := br.readU16()
		bodyCount := br.readU16()
		body := make([]ExprID, bodyCount)
		for j := uint16(0); j < bodyCount; j++ {
			body[j] = br.readU16()
		}
		handlers[i] = Handler{
			Name: resolveString(strings, nameIdx),
			Body: body,
		}
	}
	return handlers
}

func decodeStaticMask(br *binReader) []bool {
	count := br.readU16()
	mask := make([]bool, count)
	for i := uint16(0); i < count; i++ {
		b := br.readByte()
		mask[i] = b != 0
	}
	return mask
}
