package wasmgen

import (
	"bytes"
	"math"
	"testing"
)

type codeFixture struct {
	name   string
	result ValueType
	body   []byte
	valid  bool
}

func codeFixtures() []codeFixture {
	return []codeFixture{
		{"integer", I32, []byte{0x41, 0, 0x0b}, true},
		{"extend", I64, []byte{0x41, 0x7f, 0xac, 0x0b}, true},
		{"block result", I64, []byte{2, 0x7e, 0x42, 1, 0x0b, 0x0b}, true},
		{"block branch", I64, []byte{2, 0x7e, 0x42, 1, 0x0c, 0, 0x0b, 0x0b}, true},
		{"loop branch", Void, []byte{2, 0x40, 3, 0x40, 0x41, 0, 0x0d, 0, 0x0b, 0x0b, 0x0b}, true},
		{"if else", I64, []byte{0x41, 1, 4, 0x7e, 0x42, 1, 5, 0x42, 2, 0x0b, 0x0b}, true},
		{"branch table", Void, []byte{2, 0x40, 0x41, 0, 0x0e, 1, 0, 0, 0x0b, 0x0b}, true},
		{"return", I64, []byte{0x42, 1, 0x0f, 0x0b}, true},
		{"polymorphic", I64, []byte{0, 0x7c, 0x0b}, true},
		{"select", I32, []byte{0x41, 1, 0x41, 2, 0x41, 0, 0x1b, 0x0b}, true},
		{"load", I32, []byte{0x41, 0, 0x28, 2, 0, 0x0b}, true},
		{"store byte", Void, []byte{0x41, 0, 0x42, 1, 0x3c, 0, 0, 0x0b}, true},
		{"memory size", I32, []byte{0x3f, 0, 0x0b}, true},
		{"underflow", I32, []byte{0x6a, 0x0b}, false},
		{"wrong result", I32, []byte{0x42, 0, 0x0b}, false},
		{"extra result", I32, []byte{0x41, 0, 0x41, 1, 0x0b}, false},
		{"block underflow", I32, []byte{0x41, 1, 2, 0x40, 0x1a, 0x0b, 0x0b}, false},
		{"unreachable known type", I64, []byte{0, 0x41, 0, 0x7c, 0x0b}, false},
		{"branch depth", Void, []byte{0x0c, 1, 0x0b}, false},
		{"branch result", I64, []byte{2, 0x7e, 0x41, 1, 0x0c, 0, 0x0b, 0x0b}, false},
		{"table label types", I32, []byte{2, 0x7f, 3, 0x40, 0x41, 0, 0x0e, 1, 0, 1, 0x0b, 0x41, 0, 0x0b, 0x0b}, false},
		{"if predicate", Void, []byte{0x42, 0, 4, 0x40, 0x0b, 0x0b}, false},
		{"if no else", I32, []byte{0x41, 1, 4, 0x7f, 0x41, 0, 0x0b, 0x0b}, false},
		{"duplicate else", Void, []byte{0x41, 1, 4, 0x40, 5, 5, 0x0b, 0x0b}, false},
		{"unexpected else", Void, []byte{5, 0x0b}, false},
		{"unclosed block", Void, []byte{2, 0x40, 0x0b}, false},
		{"trailing end", Void, []byte{0x0b, 0x0b}, false},
		{"indexed block", Void, []byte{2, 0, 0x0b, 0x0b}, false},
		{"select kinds", I32, []byte{0x41, 1, 0x42, 2, 0x41, 0, 0x1b, 0x0b}, false},
		{"load address", I32, []byte{0x42, 0, 0x28, 2, 0, 0x0b}, false},
		{"load alignment", I32, []byte{0x41, 0, 0x28, 3, 0, 0x0b}, false},
		{"memory index", I32, []byte{0x3f, 1, 0x0b}, false},
		{"memory grow", I32, []byte{0x41, 0, 0x40, 0, 0x0b}, false},
		{"float", Void, []byte{0x43, 0, 0, 0, 0, 0x1a, 0x0b}, false},
		{"indirect call", Void, []byte{0x11, 0, 0, 0x0b}, false},
		{"bulk memory", Void, []byte{0xfc, 0x0a, 0, 0, 0x0b}, false},
		{"SIMD", Void, []byte{0xfd, 0, 0x0b}, false},
		{"reference", Void, []byte{0xd0, 0x70, 0x1a, 0x0b}, false},
		{"sign extension proposal", I32, []byte{0x41, 0, 0xc0, 0x0b}, false},
		{"nonminimal constant", I32, []byte{0x41, 0x80, 0, 0x0b}, false},
		{"constant overflow", I32, []byte{0x41, 0x80, 0x80, 0x80, 0x80, 8, 0x0b}, false},
		{"truncated constant", I64, []byte{0x42, 0x80, 0x0b}, false},
	}
}

func fixtureBinary(t *testing.T, fixture codeFixture) []byte {
	t.Helper()
	binary, err := Encode(Module{Functions: []Function{{Signature: Signature{Result: fixture.result}, Body: fixture.body}}})
	if err != nil {
		t.Fatal(err)
	}
	return binary
}

func TestValidateCodeFixtures(t *testing.T) {
	for _, fixture := range codeFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			if err := Validate(fixtureBinary(t, fixture)); (err == nil) != fixture.valid {
				t.Fatalf("valid=%v error=%v", fixture.valid, err)
			}
		})
	}
}

func TestValidateFunctionLocalAndGlobalIndices(t *testing.T) {
	for _, tc := range []struct {
		body  []byte
		valid bool
	}{
		{[]byte{0x20, 2, 0x0b}, true}, // Two i32 locals followed by one i64.
		{[]byte{0x20, 3, 0x0b}, false},
		{[]byte{0x42, 0, 0x21, 0, 0x42, 0, 0x0b}, false},
		{[]byte{0x23, 0, 0xac, 0x0b}, true},
		{[]byte{0x23, 2, 0xac, 0x0b}, false},
		{[]byte{0x41, 0, 0x24, 0, 0x42, 0, 0x0b}, true},
		{[]byte{0x41, 0, 0x24, 1, 0x42, 0, 0x0b}, false},
		{[]byte{0x41, 0, 0x10, 0, 0xac, 0x0b}, true},
		{[]byte{0x42, 0, 0x10, 0, 0xac, 0x0b}, false},
		{[]byte{0x10, 0, 0xac, 0x0b}, false},
		{[]byte{0x10, 4, 0x0b}, false},
	} {
		m := encoderFixture()
		m.Functions[0].Body = tc.body
		binary, err := Encode(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate(binary); (err == nil) != tc.valid {
			t.Fatalf("body %x valid=%v error=%v", tc.body, tc.valid, err)
		}
	}
}

func replaceSection(t *testing.T, binary []byte, id byte, replacement []byte) []byte {
	t.Helper()
	ids, sections := testSections(t, binary)
	out := append([]byte{}, header[:]...)
	for _, section := range ids {
		payload := sections[section]
		if section == id {
			payload = replacement
		}
		out = appendSection(out, section, payload)
	}
	return out
}

func TestValidateRejectsMalformedSections(t *testing.T) {
	binary, _ := Encode(Module{})
	for size := 0; size < len(binary); size++ {
		if Validate(binary[:size]) == nil {
			t.Fatalf("accepted truncated prefix %d", size)
		}
	}
	for _, tc := range []struct {
		id      byte
		payload []byte
	}{
		{1, []byte{0xff, 0xff, 0xff, 0xff, 0x0f}},
		{1, []byte{1, 0x60, 0, 1, 0x7d}},
		{1, []byte{1, 0x60, 0, 1, 0}},
		{1, []byte{1, 0x60, 0, 2, 0x7f, 0x7e}},
		{1, []byte{2, 0x60, 0, 0, 0x60, 0, 0}},
		{2, []byte{1, 1, 'h', 1, 'f', 0, 0}},
		{2, []byte{1, 1, 'h', 1, 'm', 2, 1, 3, 3}},
		{3, []byte{1, 0}},
		{5, []byte{1, 0, 3}}, {5, []byte{1, 1, 3, 4}}, {5, []byte{2, 1, 3, 3, 1, 3, 3}},
		{6, []byte{1, 0x7e, 0, 0x42, 0, 0x0b}},
		{6, []byte{1, 0x7f, 2, 0x41, 0, 0x0b}},
		{7, []byte{1, 6, 'm', 'e', 'm', 'o', 'r', 'y', 3, 0}},
		{7, []byte{1, 6, 'm', 'e', 'm', 'o', 'r', 'y', 2, 1}},
		{10, []byte{1, 1, 0x0b}},
		{11, []byte{1, 0, 0x41, 0, 0x0b, 0}},
		{0, []byte{1, 0xff}}, {0, appendName(nil, "unknown")},
	} {
		if err := Validate(replaceSection(t, binary, tc.id, tc.payload)); err == nil {
			t.Fatalf("accepted section %d: %x", tc.id, tc.payload)
		}
	}
	for _, malformed := range [][]byte{
		append(append([]byte{}, binary...), 0),
		append(append([]byte{}, header[:]...), appendSection(nil, 4, []byte{0})...),
		append(append([]byte{}, header[:]...), []byte{1, 0x81, 0, 0}...),
		append(append([]byte{}, header[:]...), []byte{1, 0xff, 0xff, 0xff, 0xff, 0x1f}...),
	} {
		if Validate(malformed) == nil {
			t.Fatalf("accepted malformed framing %x", malformed)
		}
	}
	m := encoderFixture()
	binary, _ = Encode(m)
	_, sections := testSections(t, binary)
	if Validate(replaceSection(t, binary, 1, []byte{2, 0x60, 1, 0x7f, 1, 0x7f, 0x60, 0, 1, 0x7e})) == nil {
		t.Fatal("accepted unsorted signatures")
	}
	if Validate(replaceSection(t, binary, 3, []byte{2, 0, 2})) == nil {
		t.Fatal("accepted invalid function type")
	}
	exports := append([]byte{4}, sections[7][1:]...)
	exports = append(exports, sections[7][18:]...)
	if Validate(replaceSection(t, binary, 7, exports)) == nil {
		t.Fatal("accepted duplicate exports")
	}
	// Duplicate/reordered section headers are rejected before body decoding.
	duplicate := append(append([]byte{}, binary[:8]...), appendSection(nil, 1, sections[1])...)
	duplicate = append(duplicate, binary[8:]...)
	if Validate(duplicate) == nil {
		t.Fatal("accepted duplicate type section")
	}
}

func TestCanonicalLEBReader(t *testing.T) {
	for _, value := range []int64{math.MinInt64, math.MinInt32, -65, -1, 0, 63, 64, math.MaxInt32, math.MaxInt64} {
		r := binaryReader{data: AppendI64(nil, value)}
		if got := r.signed(64); got != value || r.err != nil {
			t.Fatalf("signed %d: %d %v", value, got, r.err)
		}
	}
	for _, value := range []uint32{0, 127, 128, math.MaxUint32} {
		r := binaryReader{data: AppendU32(nil, value)}
		if got := r.u32(); got != value || r.err != nil {
			t.Fatalf("unsigned %d: %d %v", value, got, r.err)
		}
	}
	for _, data := range [][]byte{{0x80}, {0x80, 0}, {0xff, 0x7f}, bytes.Repeat([]byte{0x80}, 11), {0xff, 0xff, 0xff, 0xff, 0x0f}} {
		r := binaryReader{data: data}
		r.signed(32)
		if r.err == nil {
			t.Fatalf("accepted signed i32 %x", data)
		}
	}
	for _, data := range [][]byte{{0x80}, {0x80, 0}, {0xff, 0xff, 0xff, 0xff, 0x10}} {
		r := binaryReader{data: data}
		r.u32()
		if r.err == nil {
			t.Fatalf("accepted u32 %x", data)
		}
	}
}

func TestValidateLocalDeclarations(t *testing.T) {
	binary := fixtureBinary(t, codeFixture{result: I32, body: []byte{0x41, 0, 0x0b}})
	for _, locals := range [][]byte{
		{1, 0, 0x7f}, {2, 1, 0x7e, 1, 0x7f}, {2, 1, 0x7f, 1, 0x7f},
		{1, 1, 0x7d}, {1, 0xff, 0xff, 0xff, 0xff, 0x0f, 0x7f}, {0x80, 0},
	} {
		body := append(append([]byte{}, locals...), 0x41, 0, 0x0b)
		section := AppendU32([]byte{1}, uint32(len(body)))
		section = append(section, body...)
		if Validate(replaceSection(t, binary, 10, section)) == nil {
			t.Fatalf("accepted local declarations %x", locals)
		}
	}
}
