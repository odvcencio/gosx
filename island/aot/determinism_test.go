package aot

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"io"
	"reflect"
	"strconv"
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
	var decoded program.Program
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	jsonUnit, err := NewUnit(u.Component, &decoded, u.Contract)
	if err != nil {
		t.Fatal(err)
	}
	fromBinary, err := program.DecodeBinary(u.ProgramBytes)
	if err != nil {
		t.Fatal(err)
	}
	binaryUnit, err := NewUnit(u.Component, fromBinary, u.Contract)
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
