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
//
// The v0.16.x perf sweep replaced the per-section bytes.Buffer allocations
// and binary.Write reflection calls with direct append-to-main-buffer
// writes. For a counter-sized island program that's a 149-alloc → ~5-alloc
// reduction and a ~5x end-to-end speedup on EncodeBinary.
//
// Implementation notes:
//
// - The encoder maintains one growing bytes.Buffer for the full output.
//   Section length prefixes are written as 4-byte placeholders and
//   back-patched once the section body is known.
//
// - binary.Write(&buf, byteOrder, uint16(x)) → two buf.WriteByte calls.
//   Avoids the interface box + reflect walk that binary.Write does per
//   call. putUint16 / putUint32 helpers below make the intent explicit.
//
// - The stringTable's interned index still allocates (the map grows),
//   but that's proportional to unique string count, not call count.
//   Unavoidable without a pre-sized map, and the map is inherently
//   needed for dedup.

// putUint16 appends `val` to buf in little-endian form without the
// reflect-based box that binary.Write(..., uint16) incurs.
func putUint16(buf *bytes.Buffer, val uint16) {
	buf.WriteByte(byte(val))
	buf.WriteByte(byte(val >> 8))
}

// EncodeBinary serializes an IslandProgram to a compact binary format.
func EncodeBinary(p *Program) ([]byte, error) {
	st := newStringTable()
	st.internAll(p)

	// Pre-size the main buffer to something reasonable for a typical
	// counter/form-sized program — avoids several regrows during the
	// encode. Actual size is data-dependent; the Grow is a hint.
	var buf bytes.Buffer
	buf.Grow(1024)

	// Header: magic + version + section count
	buf.Write(magic[:])
	putUint16(&buf, binaryVersion)
	putUint16(&buf, 8) // always 8 sections

	// writeSection writes tag + 4-byte length placeholder, invokes the
	// section body writer (which appends directly to the main buffer),
	// and then back-patches the length into the placeholder. Avoids
	// the intermediate per-section bytes.Buffer the old encoder created.
	writeSection := func(tag uint8, body func()) {
		buf.WriteByte(tag)
		// Record position of the length prefix so we can back-patch it.
		lenPos := buf.Len()
		// Write a 4-byte zero placeholder.
		buf.Write([]byte{0, 0, 0, 0})
		startPos := buf.Len()
		body()
		endPos := buf.Len()
		length := uint32(endPos - startPos)
		// Back-patch length in place.
		data := buf.Bytes()
		byteOrder.PutUint32(data[lenPos:lenPos+4], length)
	}

	writeSection(secStringTable, func() { encodeStringTable(&buf, st) })
	writeSection(secProps, func() { encodeProps(&buf, p, st) })
	writeSection(secNodes, func() { encodeNodes(&buf, p, st) })
	writeSection(secExprs, func() { encodeExprs(&buf, p, st) })
	writeSection(secSignals, func() { encodeSignals(&buf, p, st) })
	writeSection(secComputeds, func() { encodeComputeds(&buf, p, st) })
	writeSection(secHandlers, func() { encodeHandlers(&buf, p, st) })
	writeSection(secStaticMask, func() { encodeStaticMask(&buf, p) })

	return buf.Bytes(), nil
}

func encodeStringTable(buf *bytes.Buffer, st *stringTable) {
	putUint16(buf, uint16(len(st.strings)))
	for _, s := range st.strings {
		putUint16(buf, uint16(len(s)))
		buf.WriteString(s)
	}
}

func encodeProps(buf *bytes.Buffer, p *Program, st *stringTable) {
	putUint16(buf, uint16(len(p.Props)))
	for _, prop := range p.Props {
		putUint16(buf, st.intern(prop.Name))
		buf.WriteByte(byte(prop.Type))
	}
}

func encodeNodes(buf *bytes.Buffer, p *Program, st *stringTable) {
	putUint16(buf, uint16(len(p.Nodes)))
	putUint16(buf, p.Root)
	putUint16(buf, st.intern(p.Name))

	for _, n := range p.Nodes {
		buf.WriteByte(byte(n.Kind))
		putUint16(buf, st.intern(n.Tag))
		putUint16(buf, st.intern(n.Text))
		putUint16(buf, n.Expr)

		putUint16(buf, uint16(len(n.Attrs)))
		for _, a := range n.Attrs {
			buf.WriteByte(byte(a.Kind))
			putUint16(buf, st.intern(a.Name))
			putUint16(buf, st.intern(a.Value))
			putUint16(buf, a.Expr)
			putUint16(buf, st.intern(a.Event))
		}

		putUint16(buf, uint16(len(n.Children)))
		for _, c := range n.Children {
			putUint16(buf, c)
		}
	}
}

func encodeExprs(buf *bytes.Buffer, p *Program, st *stringTable) {
	putUint16(buf, uint16(len(p.Exprs)))
	for _, e := range p.Exprs {
		buf.WriteByte(byte(e.Op))
		buf.WriteByte(byte(e.Type))
		putUint16(buf, st.intern(e.Value))
		putUint16(buf, uint16(len(e.Operands)))
		for _, op := range e.Operands {
			putUint16(buf, op)
		}
	}
}

func encodeSignals(buf *bytes.Buffer, p *Program, st *stringTable) {
	putUint16(buf, uint16(len(p.Signals)))
	for _, s := range p.Signals {
		putUint16(buf, st.intern(s.Name))
		buf.WriteByte(byte(s.Type))
		putUint16(buf, s.Init)
	}
}

func encodeComputeds(buf *bytes.Buffer, p *Program, st *stringTable) {
	putUint16(buf, uint16(len(p.Computeds)))
	for _, c := range p.Computeds {
		putUint16(buf, st.intern(c.Name))
		buf.WriteByte(byte(c.Type))
		putUint16(buf, c.Expr)
	}
}

func encodeHandlers(buf *bytes.Buffer, p *Program, st *stringTable) {
	putUint16(buf, uint16(len(p.Handlers)))
	for _, h := range p.Handlers {
		putUint16(buf, st.intern(h.Name))
		putUint16(buf, uint16(len(h.Body)))
		for _, id := range h.Body {
			putUint16(buf, id)
		}
	}
}

func encodeStaticMask(buf *bytes.Buffer, p *Program) {
	putUint16(buf, uint16(len(p.StaticMask)))
	for _, b := range p.StaticMask {
		if b {
			buf.WriteByte(1)
		} else {
			buf.WriteByte(0)
		}
	}
}

// --- Decoder ---
//
// The v0.16.x perf sweep replaced the io.Reader + reflect-based
// binary.Read path with an offset-indexed byte slice reader. That
// eliminates the per-section bytes.NewReader allocation, the per-read
// binary.Read reflect call, and the per-section sectionData copy —
// together roughly 90% of the old allocation count.
//
// binReader now carries the full input slice and an offset cursor.
// Section readers slice into the same backing buffer rather than
// copying bytes into a new section buffer.

type binReader struct {
	data []byte
	off  int
	err  error
}

func (br *binReader) remaining() int {
	return len(br.data) - br.off
}

func (br *binReader) readFull(buf []byte) {
	if br.err != nil {
		return
	}
	if br.remaining() < len(buf) {
		br.err = io.ErrUnexpectedEOF
		return
	}
	copy(buf, br.data[br.off:])
	br.off += len(buf)
}

func (br *binReader) readByte() byte {
	if br.err != nil {
		return 0
	}
	if br.remaining() < 1 {
		br.err = io.ErrUnexpectedEOF
		return 0
	}
	b := br.data[br.off]
	br.off++
	return b
}

func (br *binReader) readU16() uint16 {
	if br.err != nil {
		return 0
	}
	if br.remaining() < 2 {
		br.err = io.ErrUnexpectedEOF
		return 0
	}
	v := byteOrder.Uint16(br.data[br.off:])
	br.off += 2
	return v
}

func (br *binReader) readU32() uint32 {
	if br.err != nil {
		return 0
	}
	if br.remaining() < 4 {
		br.err = io.ErrUnexpectedEOF
		return 0
	}
	v := byteOrder.Uint32(br.data[br.off:])
	br.off += 4
	return v
}

// readSection returns a sub-reader positioned at a section of `length`
// bytes starting from the current offset and advances the parent offset
// past it. The sub-reader shares the backing slice with the parent —
// no copy.
func (br *binReader) readSection(length uint32) *binReader {
	if br.err != nil {
		return &binReader{err: br.err}
	}
	if uint32(br.remaining()) < length {
		br.err = io.ErrUnexpectedEOF
		return &binReader{err: br.err}
	}
	sub := &binReader{data: br.data[br.off : br.off+int(length)]}
	br.off += int(length)
	return sub
}

// DecodeBinary deserializes an IslandProgram from the compact binary format.
func DecodeBinary(data []byte) (*Program, error) {
	br := &binReader{data: data}

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

	for i := range sectionCount {
		tag := br.readByte()
		length := br.readU32()
		if br.err != nil {
			return nil, fmt.Errorf("binary decode: reading section %d header: %w", i, br.err)
		}

		sr := br.readSection(length)
		if sr.err != nil {
			return nil, fmt.Errorf("binary decode: reading section %d data: %w", i, sr.err)
		}

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
	for i := range count {
		slen := int(br.readU16())
		if br.err != nil {
			return strs
		}
		if br.remaining() < slen {
			br.err = io.ErrUnexpectedEOF
			return strs
		}
		// Read directly from the backing slice — avoids the
		// intermediate make([]byte, slen) allocation per string
		// that readFull would incur. string() copies the bytes
		// into a new string header, which is the one unavoidable
		// allocation per interned string.
		strs[i] = string(br.data[br.off : br.off+slen])
		br.off += slen
	}
	return strs
}

func decodeProps(br *binReader, strings []string) []PropDef {
	count := br.readU16()
	props := make([]PropDef, count)
	for i := range count {
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
	for i := range nodeCount {
		kind := br.readByte()
		tagIdx := br.readU16()
		textIdx := br.readU16()
		expr := br.readU16()

		attrCount := br.readU16()
		attrs := make([]Attr, attrCount)
		for j := range attrCount {
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
		for j := range childCount {
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
	for i := range count {
		op := br.readByte()
		typ := br.readByte()
		valIdx := br.readU16()
		opCount := br.readU16()
		operands := make([]ExprID, opCount)
		for j := range opCount {
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
	for i := range count {
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
	for i := range count {
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
	for i := range count {
		nameIdx := br.readU16()
		bodyCount := br.readU16()
		body := make([]ExprID, bodyCount)
		for j := range bodyCount {
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
	for i := range count {
		b := br.readByte()
		mask[i] = b != 0
	}
	return mask
}
