// Binary serialization for IslandProgram (prod mode).
package program

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
)

var (
	magic     = [4]byte{'G', 'S', 'X', 0x00}
	byteOrder = binary.LittleEndian
)

const binaryVersion uint16 = 1

// Section type tags.
//
// Tags 0x08 and up were added after the format shipped. Both directions stay
// compatible because the section count is data-driven and the decoder's switch
// skips a tag it does not know: an old decoder reads past the new sections, and
// a new decoder leaves the matching fields zero when the sections are absent.
// A tag number must never be reused for different content.
const (
	secStringTable uint8 = 0x00
	secProps       uint8 = 0x01
	secNodes       uint8 = 0x02
	secExprs       uint8 = 0x03
	secSignals     uint8 = 0x04
	secComputeds   uint8 = 0x05
	secHandlers    uint8 = 0x06
	secStaticMask  uint8 = 0x07
	secFuncs       uint8 = 0x08
	secEngineNodes uint8 = 0x09
	secMeta        uint8 = 0x0A
)

// sectionCount is the number of sections EncodeBinary always writes. Raise it in
// the same change that adds a section.
const sectionCount uint16 = 11

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
//
// The section writers call st.intern again while they emit, and the string table
// section is written first. So any string a section writer touches MUST appear
// here, or the decoder gets an index past the end of its table. Add to this
// function whenever you add a string to a section.
func (st *stringTable) internAll(p *Program) {
	st.intern(p.Name)
	st.intern(p.Version)
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
	for i := range p.Funcs {
		st.intern(p.Funcs[i].Name)
		for _, param := range p.Funcs[i].Params {
			st.intern(param)
		}
	}
	for i := range p.EngineNodes {
		st.intern(p.EngineNodes[i].Kind)
		st.intern(p.EngineNodes[i].Geometry)
		st.intern(p.EngineNodes[i].Material)
		// Sorted so the table order — and therefore the encoded bytes — do
		// not depend on Go's random map iteration order. The build pipeline
		// content-hashes this output.
		for _, name := range sortedPropNames(p.EngineNodes[i].Props) {
			st.intern(name)
		}
	}
}

// sortedPropNames returns the keys of an EngineNode prop map in a stable order.
func sortedPropNames(props map[string]ExprID) []string {
	if len(props) == 0 {
		return nil
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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

// putUint32 appends `val` to buf in little-endian form. Used where a uint16
// would clip: function arities, engine-node child indices, MaxCallDepth.
func putUint32(buf *bytes.Buffer, val uint32) {
	buf.WriteByte(byte(val))
	buf.WriteByte(byte(val >> 8))
	buf.WriteByte(byte(val >> 16))
	buf.WriteByte(byte(val >> 24))
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
	putUint16(&buf, sectionCount)

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
	writeSection(secFuncs, func() { encodeFuncs(&buf, p, st) })
	writeSection(secEngineNodes, func() { encodeEngineNodes(&buf, p, st) })
	writeSection(secMeta, func() { encodeMeta(&buf, p, st) })

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

// encodeFuncs writes the user-function registry (Slice Y.D). Programs with no
// user functions write a zero count.
func encodeFuncs(buf *bytes.Buffer, p *Program, st *stringTable) {
	putUint16(buf, uint16(len(p.Funcs)))
	for _, fn := range p.Funcs {
		putUint16(buf, st.intern(fn.Name))
		putUint32(buf, uint32(fn.Results))
		putUint16(buf, uint16(len(fn.Params)))
		for _, param := range fn.Params {
			putUint16(buf, st.intern(param))
		}
		putUint16(buf, uint16(len(fn.Body)))
		for _, id := range fn.Body {
			putUint16(buf, id)
		}
	}
}

// encodeEngineNodes writes the scene-oriented node list carried by
// SurfaceScene3D and SurfaceCanvas2D programs. Prop names are written in sorted
// order so the byte output does not depend on map iteration order.
func encodeEngineNodes(buf *bytes.Buffer, p *Program, st *stringTable) {
	putUint16(buf, uint16(len(p.EngineNodes)))
	for _, node := range p.EngineNodes {
		putUint16(buf, st.intern(node.Kind))
		putUint16(buf, st.intern(node.Geometry))
		putUint16(buf, st.intern(node.Material))

		names := sortedPropNames(node.Props)
		putUint16(buf, uint16(len(names)))
		for _, name := range names {
			putUint16(buf, st.intern(name))
			putUint16(buf, node.Props[name])
		}

		putUint16(buf, uint16(len(node.Children)))
		for _, child := range node.Children {
			putUint32(buf, uint32(child))
		}

		if node.Static {
			buf.WriteByte(1)
		} else {
			buf.WriteByte(0)
		}
	}
}

// encodeMeta writes the envelope fields that live on Program itself rather than
// in a list: the reserved Version string and the OpIndirectCall depth cap.
func encodeMeta(buf *bytes.Buffer, p *Program, st *stringTable) {
	putUint16(buf, st.intern(p.Version))
	putUint32(buf, uint32(int32(p.MaxCallDepth)))
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
		case secFuncs:
			p.Funcs = decodeFuncs(sr, strings)
		case secEngineNodes:
			p.EngineNodes = decodeEngineNodes(sr, strings)
		case secMeta:
			p.Version, p.MaxCallDepth = decodeMeta(sr, strings)
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

// decodeFuncs reads the user-function registry. A zero count yields nil so a
// program with no user functions round-trips to the same value it started as.
func decodeFuncs(br *binReader, strings []string) []FuncDef {
	count := br.readU16()
	if count == 0 {
		return nil
	}
	funcs := make([]FuncDef, count)
	for i := range count {
		nameIdx := br.readU16()
		results := int(int32(br.readU32()))

		paramCount := br.readU16()
		var params []string
		if paramCount > 0 {
			params = make([]string, paramCount)
			for j := range paramCount {
				params[j] = resolveString(strings, br.readU16())
			}
		}

		bodyCount := br.readU16()
		var body []ExprID
		if bodyCount > 0 {
			body = make([]ExprID, bodyCount)
			for j := range bodyCount {
				body[j] = br.readU16()
			}
		}

		funcs[i] = FuncDef{
			Name:    resolveString(strings, nameIdx),
			Params:  params,
			Body:    body,
			Results: results,
		}
	}
	return funcs
}

// decodeEngineNodes reads the scene-oriented node list. A zero count yields nil.
func decodeEngineNodes(br *binReader, strings []string) []EngineNode {
	count := br.readU16()
	if count == 0 {
		return nil
	}
	nodes := make([]EngineNode, count)
	for i := range count {
		kindIdx := br.readU16()
		geometryIdx := br.readU16()
		materialIdx := br.readU16()

		propCount := br.readU16()
		var props map[string]ExprID
		if propCount > 0 {
			props = make(map[string]ExprID, propCount)
			for range propCount {
				name := resolveString(strings, br.readU16())
				props[name] = br.readU16()
			}
		}

		childCount := br.readU16()
		var children []int
		if childCount > 0 {
			children = make([]int, childCount)
			for j := range childCount {
				children[j] = int(int32(br.readU32()))
			}
		}

		static := br.readByte() != 0

		nodes[i] = EngineNode{
			Kind:     resolveString(strings, kindIdx),
			Geometry: resolveString(strings, geometryIdx),
			Material: resolveString(strings, materialIdx),
			Props:    props,
			Children: children,
			Static:   static,
		}
	}
	return nodes
}

// decodeMeta reads the envelope fields encodeMeta wrote.
func decodeMeta(br *binReader, strings []string) (string, int) {
	versionIdx := br.readU16()
	maxCallDepth := int(int32(br.readU32()))
	return resolveString(strings, versionIdx), maxCallDepth
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
