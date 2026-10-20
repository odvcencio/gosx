package aot

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

func namedUnit(t *testing.T, index int) Unit {
	u := staticUnit(t)
	u.Component = "example/components.Static" + strconv.Itoa(index)
	u.Contract.Component = u.Component
	u.Program.Name = "Static" + strconv.Itoa(index)
	return refreshUnit(t, u)
}

func TestArtifactInputSetDigestMatchesCanonicalContract(t *testing.T) {
	a, b := namedUnit(t, 1), namedUnit(t, 2)
	ordered, receipts, digest, err := canonicalUnits([]Unit{b, a, b}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	var identity bytes.Buffer
	identity.WriteString("gosx-aot-set-v1\x00")
	for _, field := range []uint32{1, 1, 1, 1, 256, 1024, 32, 16, 64, 64, 64, 16, 16, 16, 16, 64, 512, 4096, 32768, 16384, 128, 32, 262144, 16384, 196608, 2} {
		if err := binary.Write(&identity, binary.LittleEndian, field); err != nil {
			t.Fatal(err)
		}
	}
	if bytes.Compare(a.Digest[:], b.Digest[:]) < 0 {
		identity.Write(a.Digest[:])
		identity.Write(b.Digest[:])
	} else {
		identity.Write(b.Digest[:])
		identity.Write(a.Digest[:])
	}
	if digest != sha256.Sum256(identity.Bytes()) || len(ordered) != 2 || len(receipts) != 2 || !receipts[0].Eligible || !receipts[1].Eligible {
		t.Fatalf("input-set identity: %x %+v", digest, receipts)
	}
	if bytes.Compare(ordered[0].Digest[:], ordered[1].Digest[:]) >= 0 {
		t.Fatal("unit IDs are not byte-sorted")
	}
	a.Program.Nodes[0].Tag = "span"
	if ordered[0].Program.Nodes[0].Tag != "div" || ordered[1].Program.Nodes[0].Tag != "div" {
		t.Fatal("canonical units borrowed program memory")
	}
}

func TestArtifactMetadataAndCanonicalBrotliIdentity(t *testing.T) {
	u := fixedBindingUnit(t)
	e, err := emitDOMExpressions(u, []uint32{0})
	if err != nil {
		t.Fatal(err)
	}
	first, err := materializeArtifact(e.module, []Unit{u}, DefaultOptions(), true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := materializeArtifact(e.module, []Unit{u, u}, DefaultOptions(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("duplicate unit changed artifact bytes or receipts")
	}
	if first.Receipt.RawBytes != uint32(len(first.Bytes)) || first.Receipt.BrotliBytes != uint32(len(first.Brotli)) || first.Receipt.SHA != sha256.Sum256(first.Bytes) || first.Receipt.BrotliSHA != sha256.Sum256(first.Brotli) {
		t.Fatal("artifact byte/hash receipt mismatch")
	}
	decoded, err := io.ReadAll(brotli.NewReader(bytes.NewReader(first.Brotli)))
	if err != nil || !bytes.Equal(decoded, first.Bytes) {
		t.Fatal("Brotli sidecar does not decode to the raw artifact")
	}
	var canonical bytes.Buffer
	writer := brotli.NewWriterLevel(&canonical, 11)
	if _, err := writer.Write(first.Bytes); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical.Bytes(), first.Brotli) {
		t.Fatal("noncanonical compression quality or bytes")
	}
	metadata := moduleMetadata([]Unit{u}, DefaultOptions(), first.InputSetSHA)
	if len(metadata) != 116 || binary.LittleEndian.Uint32(metadata[48:52]) != 1 || !bytes.Equal(metadata[16:48], first.InputSetSHA[:]) || !bytes.Equal(metadata[52:84], u.ProgramSHA[:]) || !bytes.Equal(metadata[84:116], u.ContractSHA[:]) {
		t.Fatal("custom metadata offsets or identities differ")
	}
	if !bytes.HasSuffix(first.Bytes, metadata) || len(e.module.Metadata) != 0 {
		t.Fatal("metadata is not last or mutated its input")
	}
	if len(first.Standalone) != 1 || first.Standalone[0] != first.Receipt || len(first.Programs) != 1 {
		t.Fatal("standalone proof is incomplete")
	}
	first.Programs[0].Contract.Bindings[0].Attributes[0] = "changed"
	first.Programs[0].Bindings.Bindings[0].Attributes[0] = "changed"
	first.Bytes[0] = 255
	if second.Programs[0].Contract.Bindings[0].Attributes[0] != "title" || second.Bytes[0] != 0 {
		t.Fatal("artifact outputs share mutable descriptor storage")
	}
	t.Logf("fixture module: raw=%d Brotli=%d", second.Receipt.RawBytes, second.Receipt.BrotliBytes)
}

func TestArtifactCanonicalJSONAndBinaryInputs(t *testing.T) {
	u := fixedBindingUnit(t)
	raw, err := json.Marshal(u.Program)
	if err != nil {
		t.Fatal(err)
	}
	jsonUnit, err := NewJSONUnit(u.Component, raw, u.Contract)
	if err != nil {
		t.Fatal(err)
	}
	binaryUnit, err := NewBinaryUnit(u.Component, u.ProgramBytes, u.Contract)
	if err != nil {
		t.Fatal(err)
	}
	var artifacts []Artifact
	for _, unit := range []Unit{u, jsonUnit, binaryUnit} {
		e, err := emitDOMExpressions(unit, []uint32{0})
		if err != nil {
			t.Fatal(err)
		}
		artifact, err := materializeArtifact(e.module, []Unit{unit}, DefaultOptions(), true)
		if err != nil {
			t.Fatal(err)
		}
		artifacts = append(artifacts, artifact)
	}
	if !reflect.DeepEqual(artifacts[0], artifacts[1]) || !reflect.DeepEqual(artifacts[0], artifacts[2]) {
		t.Fatal("source encoding changed the canonical module")
	}
}

func TestArtifactJSONAdmissionRejectsDiscardedFields(t *testing.T) {
	u := fixedBindingUnit(t)
	raw, err := json.Marshal(u.Program)
	if err != nil {
		t.Fatal(err)
	}
	prefix := func(field string) []byte { return append([]byte("{"+field+","), raw[1:]...) }
	cases := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"null", []byte("null")},
		{"array", []byte("[]")},
		{"malformed", raw[:len(raw)-1]},
		{"second object", append(append([]byte{}, raw...), []byte(" {}")...)},
		{"trailing scalar", append(append([]byte{}, raw...), []byte(" true")...)},
		{"unknown root", prefix(`"extension":0`)},
		{"surface", prefix(`"Surface":0`)},
		{"case alias", prefix(`"NAME":"ignored"`)},
		{"duplicate", prefix(`"name":"ignored"`)},
		{"escaped duplicate", prefix(`"\u006eame":"ignored"`)},
		{"null duplicate", prefix(`"name":null`)},
		{"unknown node", bytes.Replace(raw, []byte(`"nodes":[{`), []byte(`"nodes":[{"extension":0,`), 1)},
		{"duplicate node", bytes.Replace(raw, []byte(`"nodes":[{`), []byte(`"nodes":[{"kind":0,`), 1)},
		{"unknown expression", bytes.Replace(raw, []byte(`"exprs":[{`), []byte(`"exprs":[{"extension":0,`), 1)},
		{"duplicate expression", bytes.Replace(raw, []byte(`"exprs":[{`), []byte(`"exprs":[{"op":0,`), 1)},
		{"unknown attribute", bytes.Replace(raw, []byte(`"attrs":[{`), []byte(`"attrs":[{"extension":0,`), 1)},
		{"unknown signal", bytes.Replace(raw, []byte(`"signals":[{`), []byte(`"signals":[{"extension":0,`), 1)},
		{"unknown handler", bytes.Replace(raw, []byte(`"handlers":[{`), []byte(`"handlers":[{"extension":0,`), 1)},
		{"fractional ID", bytes.Replace(raw, []byte(`"root":0`), []byte(`"root":0.5`), 1)},
		{"negative ID", bytes.Replace(raw, []byte(`"root":0`), []byte(`"root":-1`), 1)},
		{"ID overflow", bytes.Replace(raw, []byte(`"root":0`), []byte(`"root":65536`), 1)},
		{"wrong table shape", bytes.Replace(raw, []byte(`"nodes":[`), []byte(`"nodes":{`), 1)},
		{"invalid UTF-8", bytes.Replace(raw, []byte(`"name":"`), []byte{'"', 'n', 'a', 'm', 'e', '"', ':', '"', 255}, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if bytes.Equal(tc.data, raw) {
				t.Fatal("mutation did not alter the fixture")
			}
			if _, err := NewJSONUnit(u.Component, tc.data, u.Contract); err == nil {
				t.Fatal("admitted a malformed or lossy JSON envelope")
			}
		})
	}
	// The compatible VM decoder still accepts its existing extension behavior.
	var legacy program.Program
	if err := json.Unmarshal(prefix(`"extension":0`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Name != u.Program.Name {
		t.Fatal("VM JSON semantics changed")
	}
}

func TestArtifactJSONAdmissionChecksEveryTableAndSourceProof(t *testing.T) {
	u := literalUnit(t)
	for _, tc := range []struct {
		name   string
		mutate func(*program.Program)
	}{
		{"version", func(p *program.Program) { p.Version = "future" }},
		{"functions", func(p *program.Program) { p.Funcs = []program.FuncDef{{Name: "f"}} }},
		{"engine", func(p *program.Program) { p.EngineNodes = []program.EngineNode{{Kind: "scene"}} }},
		{"call depth", func(p *program.Program) { p.MaxCallDepth = 1 }},
		{"unused opcode", func(p *program.Program) { p.Exprs[0].Op = 255 }},
		{"unused type", func(p *program.Program) { p.Exprs[0].Type = program.TypeFloat }},
		{"unused operand", func(p *program.Program) { p.Exprs[0].Operands = []program.ExprID{1} }},
		{"unused node", func(p *program.Program) { p.Nodes = append(p.Nodes, program.Node{Kind: 255}) }},
		{"overlong wire string", func(p *program.Program) { p.Name = strings.Repeat("x", 65536) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := refreshUnit(t, u)
			tc.mutate(copy.Program)
			data, err := json.Marshal(copy.Program)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := NewJSONUnit(u.Component, data, u.Contract); err == nil {
				t.Fatal("admitted unsupported data before profile validation")
			}
		})
	}
	raw, err := json.Marshal(u.Program)
	if err != nil {
		t.Fatal(err)
	}
	contract := u.Contract
	contract.Expressions = append([]ExpressionContract{}, contract.Expressions...)
	contract.Expressions[0].Kind = Bool
	if _, err := NewJSONUnit(u.Component, raw, contract); err == nil {
		t.Fatal("admitted incompatible source evidence")
	}
}

type programWireSection struct {
	tag     byte
	payload []byte
}

func programSections(t *testing.T, data []byte) []programWireSection {
	t.Helper()
	sections := make([]programWireSection, 11)
	cursor := 8
	for i := range sections {
		length := int(binary.LittleEndian.Uint32(data[cursor+1 : cursor+5]))
		sections[i] = programWireSection{data[cursor], append([]byte{}, data[cursor+5:cursor+5+length]...)}
		cursor += 5 + length
	}
	if cursor != len(data) {
		t.Fatal("fixture framing is not canonical")
	}
	return sections
}

func encodeProgramSections(sections []programWireSection) []byte {
	raw := []byte{'G', 'S', 'X', 0, 1, 0, byte(len(sections)), byte(len(sections) >> 8)}
	for _, section := range sections {
		raw = append(raw, section.tag)
		raw = binary.LittleEndian.AppendUint32(raw, uint32(len(section.payload)))
		raw = append(raw, section.payload...)
	}
	return raw
}

func TestArtifactBinaryAdmissionRejectsLostFraming(t *testing.T) {
	u := staticUnit(t)
	for _, tc := range []struct {
		name   string
		mutate func([]programWireSection) []programWireSection
	}{
		{"unknown tag", func(s []programWireSection) []programWireSection { s[10].tag = 11; return s }},
		{"duplicate tag", func(s []programWireSection) []programWireSection { s[10].tag = 8; return s }},
		{"missing section", func(s []programWireSection) []programWireSection { return s[:10] }},
		{"extra section", func(s []programWireSection) []programWireSection { return append(s, programWireSection{11, nil}) }},
		{"reordered sections", func(s []programWireSection) []programWireSection { s[8], s[9] = s[9], s[8]; return s }},
		{"noncanonical boolean", func(s []programWireSection) []programWireSection { s[7].payload[2] = 2; return s }},
		{"invalid name reference", func(s []programWireSection) []programWireSection {
			binary.LittleEndian.PutUint16(s[2].payload[4:6], 65535)
			return s
		}},
		{"invalid empty text reference", func(s []programWireSection) []programWireSection {
			binary.LittleEndian.PutUint16(s[2].payload[9:11], 65535)
			return s
		}},
		{"unused string", func(s []programWireSection) []programWireSection {
			count := binary.LittleEndian.Uint16(s[0].payload[:2])
			binary.LittleEndian.PutUint16(s[0].payload[:2], count+1)
			s[0].payload = append(s[0].payload, 1, 0, 'x')
			return s
		}},
		{"invalid string bytes", func(s []programWireSection) []programWireSection { s[0].payload[4] = 255; return s }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := encodeProgramSections(tc.mutate(programSections(t, u.ProgramBytes)))
			if _, err := NewBinaryUnit(u.Component, raw, u.Contract); err == nil {
				t.Fatal("admitted a lossy binary envelope")
			}
		})
	}
	for tag := range 11 {
		t.Run("section tail "+strconv.Itoa(tag), func(t *testing.T) {
			sections := programSections(t, u.ProgramBytes)
			sections[tag].payload = append(sections[tag].payload, 0)
			raw := encodeProgramSections(sections)
			if _, err := program.DecodeBinary(raw); err != nil {
				t.Fatal("VM decoder no longer tolerates its existing section tails")
			}
			if _, err := NewBinaryUnit(u.Component, raw, u.Contract); err == nil {
				t.Fatal("admitted ignored section bytes")
			}
		})
	}
	for length := range len(u.ProgramBytes) {
		if _, err := NewBinaryUnit(u.Component, u.ProgramBytes[:length], u.Contract); err == nil {
			t.Fatalf("admitted prefix of length %d", length)
		}
	}
	for _, offset := range []int{0, 4, 6} {
		raw := append([]byte{}, u.ProgramBytes...)
		raw[offset]++
		if _, err := NewBinaryUnit(u.Component, raw, u.Contract); err == nil {
			t.Fatalf("admitted invalid header at %d", offset)
		}
	}
	raw := append(append([]byte{}, u.ProgramBytes...), 0)
	if _, err := NewBinaryUnit(u.Component, raw, u.Contract); err == nil {
		t.Fatal("admitted global trailing bytes")
	}
	binary.LittleEndian.PutUint32(raw[9:13], ^uint32(0))
	if _, err := NewBinaryUnit(u.Component, raw, u.Contract); err == nil {
		t.Fatal("admitted a wrapping section length")
	}
}

func TestArtifactInputReadersOwnUnicodeAndZeroIDs(t *testing.T) {
	u := literalUnit(t)
	u.Program.Name = "e\u0301\x00🌴"
	u.Program.Exprs[0] = program.Expr{Op: program.OpLitString, Type: program.TypeString, Value: "héllo\x00🌴"}
	u.Contract.Expressions[0].Kind = String
	u = refreshUnit(t, u)
	raw, err := json.Marshal(u.Program)
	if err != nil {
		t.Fatal(err)
	}
	// Whitespace and equivalent JSON escapes do not change canonical identity.
	escaped := bytes.ReplaceAll(raw, []byte("🌴"), []byte(`\ud83c\udf34`))
	escaped = append(append([]byte(" \n"), escaped...), '\n', '\t')
	fromJSON, err := NewJSONUnit(u.Component, escaped, u.Contract)
	if err != nil {
		t.Fatal(err)
	}
	wire := append([]byte{}, u.ProgramBytes...)
	fromBinary, err := NewBinaryUnit(u.Component, wire, u.Contract)
	if err != nil {
		t.Fatal(err)
	}
	if fromJSON.Digest != u.Digest || fromBinary.Digest != u.Digest || fromJSON.Program.Root != 0 || fromJSON.Program.Exprs[0].Value != u.Program.Exprs[0].Value {
		t.Fatal("Unicode bytes or valid ID zero changed during admission")
	}
	for i := range wire {
		wire[i] = 255
	}
	u.Contract.Expressions[0].Kind = Bool
	u.Program.Exprs[0].Value = "changed"
	if fromBinary.Contract.Expressions[0].Kind != String || fromJSON.Contract.Expressions[0].Kind != String || fromBinary.Program.Exprs[0].Value != "héllo\x00🌴" || fromJSON.Program.Exprs[0].Value != "héllo\x00🌴" {
		t.Fatal("input readers borrowed caller storage")
	}
}

func TestArtifactBinaryAdmissionChecksUnusedExpressions(t *testing.T) {
	u := literalUnit(t)
	sections := programSections(t, u.ProgramBytes)
	sections[3].payload[2] = 255
	if _, err := NewBinaryUnit(u.Component, encodeProgramSections(sections), u.Contract); err == nil {
		t.Fatal("admitted an unsupported opcode in an unused expression")
	}
	contract := u.Contract
	contract.Expressions = append([]ExpressionContract{}, contract.Expressions...)
	contract.Expressions[0].Kind = Bool
	if _, err := NewBinaryUnit(u.Component, u.ProgramBytes, contract); err == nil {
		t.Fatal("admitted a binary program with incompatible source evidence")
	}
}

func TestArtifactRejectsUnprovedConflictingAndOversizedSets(t *testing.T) {
	options := DefaultOptions()
	for _, mutate := range []func(*Options){func(o *Options) { o.ABI++ }, func(o *Options) { o.Profile++ }, func(o *Options) { o.Encoder++ }, func(o *Options) { o.Helper++ }, func(o *Options) { o.Limits.Instances-- }, func(o *Options) { o.Limits.MemoryBytes++ }} {
		bad := options
		mutate(&bad)
		if _, _, _, err := canonicalUnits([]Unit{staticUnit(t)}, bad); err == nil {
			t.Fatal("accepted unsupported compiler options")
		}
	}
	if _, _, _, err := canonicalUnits(nil, options); err == nil {
		t.Fatal("accepted empty set")
	}
	u := staticUnit(t)
	conflict := staticUnit(t)
	conflict.Program.Name = "Different"
	conflict = refreshUnit(t, conflict)
	if _, _, _, err := canonicalUnits([]Unit{u, conflict}, options); err == nil {
		t.Fatal("accepted conflicting component identities")
	}
	bad := staticUnit(t)
	bad.Program.Nodes[0].Tag = "script"
	if _, _, _, err := canonicalUnits([]Unit{u, bad}, options); err == nil {
		t.Fatal("deduplication hid an unproved unit")
	}
	var units []Unit
	for i := 0; i < 17; i++ {
		units = append(units, namedUnit(t, i))
	}
	if _, _, _, err := canonicalUnits(units, options); err == nil {
		t.Fatal("accepted too many unique programs")
	}
	units = units[:16]
	units = append(units, units...)
	if ordered, _, _, err := canonicalUnits(units, options); err != nil || len(ordered) != 16 {
		t.Fatalf("deduplicated profile maximum: %d %v", len(ordered), err)
	}
}

func TestArtifactMeasuresRawAndBrotliCeilings(t *testing.T) {
	if ProgramRawLimit != 16384 || ProgramBrotliLimit != 4096 || PageRawLimit != 65536 || PageBrotliLimit != 16384 {
		t.Fatal("module size ceilings changed")
	}
	raw, err := wasmgen.Encode(wasmgen.Module{Data: make([]byte, 16384)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := measureModule(raw, true); err == nil {
		t.Fatal("accepted excessive standalone raw bytes")
	}
	if _, receipt, err := measureModule(raw, false); err != nil || receipt.RawBytes != uint32(len(raw)) {
		t.Fatalf("page receipt: %+v %v", receipt, err)
	}
	entropy := make([]byte, 10000)
	seed := uint32(41)
	for i := range entropy {
		seed = seed*1664525 + 1013904223
		entropy[i] = byte(32 + (seed>>16)%95)
	}
	raw, err = wasmgen.Encode(wasmgen.Module{Data: entropy})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := measureModule(raw, true); err == nil {
		t.Fatal("accepted excessive standalone Brotli bytes")
	}
	br, receipt, err := measureModule(raw, false)
	if err != nil || receipt.BrotliBytes <= ProgramBrotliLimit || len(br) != int(receipt.BrotliBytes) {
		t.Fatalf("page compressed receipt: %+v %v", receipt, err)
	}
	if _, _, err := measureModule(append(raw, 0), false); err == nil {
		t.Fatal("accepted invalid module while measuring")
	}
	if _, _, err := measureModule(make([]byte, PageRawLimit+1), false); err == nil {
		t.Fatal("accepted excessive page raw bytes")
	}
}
