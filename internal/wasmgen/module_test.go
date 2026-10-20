package wasmgen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/internal/chrometest"
)

func testSections(t *testing.T, binary []byte) ([]byte, map[byte][]byte) {
	t.Helper()
	if len(binary) < 8 || !bytes.Equal(binary[:8], header[:]) {
		t.Fatal("invalid header")
	}
	ids, sections := []byte{}, map[byte][]byte{}
	for pos := 8; pos < len(binary); {
		id := binary[pos]
		pos++
		var size uint32
		for shift := uint(0); ; shift += 7 {
			if pos >= len(binary) || shift >= 35 {
				t.Fatal("invalid section length")
			}
			b := binary[pos]
			pos++
			size |= uint32(b&0x7f) << shift
			if b&0x80 == 0 {
				break
			}
		}
		end := uint64(pos) + uint64(size)
		if end > uint64(len(binary)) {
			t.Fatal("section exceeds binary")
		}
		if _, duplicate := sections[id]; duplicate {
			t.Fatal("duplicate section")
		}
		ids = append(ids, id)
		sections[id] = binary[pos:int(end)]
		pos = int(end)
	}
	return ids, sections
}

func TestEmptyModuleGolden(t *testing.T) {
	got, err := Encode(Module{})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hex.DecodeString("0061736d01000000010100020100030100050401010303060100070a01066d656d6f727902000a01000b0701004180080b00000c0b676f73782e616f742e7631")
	if !bytes.Equal(got, want) {
		t.Fatalf("module=%x\nwant  =%x", got, want)
	}
	if err := Validate(got); err != nil {
		t.Fatal(err)
	}
	ids, sections := testSections(t, got)
	if !bytes.Equal(ids, []byte{1, 2, 3, 5, 6, 7, 10, 11, 0}) || !bytes.Equal(sections[5], []byte{1, 1, 3, 3}) {
		t.Fatalf("section order or memory: %v", ids)
	}
}

func encoderFixture() Module {
	sig := Signature{Params: []ValueType{I32}, Result: I32}
	return Module{
		Imports: []Import{{Module: "host", Name: "alpha", Signature: sig}, {Module: "host", Name: "beta", Signature: sig}},
		Functions: []Function{
			{Signature: Signature{Result: I64}, I32Locals: 2, I64Locals: 1, Body: []byte{0x42, 0x7f, 0x0b}},
			{Signature: sig, Body: []byte{0x20, 0x00, 0x0b}},
		},
		Globals: []Global{{Mutable: true, Initial: math.MinInt32}, {}},
		Exports: []Export{{Name: "zeta", Function: 2}, {Name: "alpha", Function: 3}},
		Data:    []byte("héllo 🌴\x00"), Metadata: []byte{0, 0xff, 0x7f},
	}
}

func TestModuleTablesIndicesAndLocals(t *testing.T) {
	m := encoderFixture()
	binary, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(binary); err != nil {
		t.Fatal(err)
	}
	_, sections := testSections(t, binary)
	for id, want := range map[byte][]byte{
		1:  {2, 0x60, 0, 1, 0x7e, 0x60, 1, 0x7f, 1, 0x7f},
		2:  {2, 4, 'h', 'o', 's', 't', 5, 'a', 'l', 'p', 'h', 'a', 0, 1, 4, 'h', 'o', 's', 't', 4, 'b', 'e', 't', 'a', 0, 1},
		3:  {2, 0, 1},
		6:  {2, 0x7f, 1, 0x41, 0x80, 0x80, 0x80, 0x80, 0x78, 0x0b, 0x7f, 0, 0x41, 0, 0x0b},
		7:  {3, 5, 'a', 'l', 'p', 'h', 'a', 0, 3, 6, 'm', 'e', 'm', 'o', 'r', 'y', 2, 0, 4, 'z', 'e', 't', 'a', 0, 2},
		10: {2, 8, 2, 2, 0x7f, 1, 0x7e, 0x42, 0x7f, 0x0b, 4, 0, 0x20, 0, 0x0b},
	} {
		if !bytes.Equal(sections[id], want) {
			t.Fatalf("section %d: %x want %x", id, sections[id], want)
		}
	}
	wantData := append([]byte{1, 0, 0x41, 0x80, 0x08, 0x0b, byte(len(m.Data))}, m.Data...)
	if !bytes.Equal(sections[11], wantData) || !bytes.Equal(sections[0], append(appendName(nil, CustomName), m.Metadata...)) {
		t.Fatal("constant or metadata bytes changed")
	}
	for local := uint32(0); local < 2; local++ {
		if index, err := m.FunctionIndex(local); err != nil || index != local+2 {
			t.Fatalf("function index %d: %d %v", local, index, err)
		}
	}
	if _, err := m.FunctionIndex(2); err == nil {
		t.Fatal("accepted missing function")
	}
}

func TestModuleBrowserValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("browser validation skipped in short mode")
	}
	executable := os.Getenv("GOSX_CHROME_BIN")
	if executable == "" {
		for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
			if path, err := exec.LookPath(name); err == nil {
				executable = path
				break
			}
		}
	}
	if executable == "" {
		t.Skip("Chrome is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	browser, err := chrometest.Start(ctx, executable, "--no-sandbox")
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	invalidCore := map[string]bool{"underflow": true, "wrong result": true, "extra result": true, "block underflow": true, "unreachable known type": true, "branch depth": true, "branch result": true, "table label types": true, "if predicate": true, "if no else": true, "duplicate else": true, "unexpected else": true, "unclosed block": true, "trailing end": true, "select kinds": true, "load address": true, "load alignment": true, "truncated constant": true}
	var cases []struct {
		Binary string
		Valid  bool
	}
	var names []string
	for _, fixture := range codeFixtures() {
		if !fixture.valid && !invalidCore[fixture.name] {
			continue
		}
		cases = append(cases, struct {
			Binary string
			Valid  bool
		}{base64.StdEncoding.EncodeToString(fixtureBinary(t, fixture)), fixture.valid})
		names = append(names, fixture.name)
	}
	payload, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	script := "(" + string(payload) + ").map(f => WebAssembly.validate(Uint8Array.from(atob(f.Binary), c => c.charCodeAt(0))))"
	var actual []bool
	if err := chromedp.Run(browser.Context, chromedp.Evaluate(script, &actual)); err != nil {
		t.Fatal(err)
	}
	if len(actual) != len(cases) {
		t.Fatal("browser result count")
	}
	for i, result := range actual {
		if result != cases[i].Valid {
			t.Fatalf("browser %s: %v want %v", names[i], result, cases[i].Valid)
		}
	}
}

func TestCanonicalTypesAndExportPermutation(t *testing.T) {
	m := encoderFixture()
	original := append([]Export{}, m.Exports...)
	a, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.Exports, original) {
		t.Fatal("encoder mutated export order")
	}
	slices.Reverse(m.Exports)
	b, err := Encode(m)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("export order changed bytes: %v", err)
	}
	m = Module{Functions: []Function{
		{Signature: Signature{Params: []ValueType{I32, I64}, Result: I32}, Body: []byte{0x41, 0, 0x0b}},
		{Signature: Signature{Params: []ValueType{I64}, Result: I64}, Body: []byte{0x42, 0, 0x0b}},
		{Signature: Signature{Params: []ValueType{I32}, Result: I32}, Body: []byte{0x41, 0, 0x0b}},
		{Signature: Signature{}, Body: []byte{0x0b}},
	}}
	b, err = Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	_, sections := testSections(t, b)
	want := []byte{4, 0x60, 0, 0, 0x60, 1, 0x7e, 1, 0x7e, 0x60, 1, 0x7f, 1, 0x7f, 0x60, 2, 0x7f, 0x7e, 1, 0x7f}
	if !bytes.Equal(sections[1], want) || !bytes.Equal(sections[3], []byte{4, 3, 1, 2, 0}) {
		t.Fatalf("canonical types: %x indices: %x", sections[1], sections[3])
	}
}

func TestEncoderRejectsUnsupportedDeclarations(t *testing.T) {
	for _, mutate := range []func(*Module){
		func(m *Module) { m.Functions[0].Signature.Result = 0x7d },
		func(m *Module) { m.Functions[0].Signature.Params = []ValueType{Void} },
		func(m *Module) { m.Functions[0].Body = nil },
		func(m *Module) { m.Functions[0].Body = []byte{0x41, 0} },
		func(m *Module) { m.Functions[0].I32Locals = math.MaxUint32 },
		func(m *Module) { m.Functions[0].I32Locals = 65536; m.Functions[0].I64Locals = 1 },
		func(m *Module) { m.Imports[0].Signature.Params = []ValueType{0x7d} },
		func(m *Module) { m.Exports[0].Function = 4 },
		func(m *Module) { m.Exports[0].Name = "memory" },
		func(m *Module) { m.Exports[0].Name = "alpha" },
		func(m *Module) { m.Exports[0].Name = string([]byte{0xff}) },
		func(m *Module) { m.Imports[0].Module = string([]byte{0xff}) },
		func(m *Module) { m.Imports[0].Name = string([]byte{0xff}) },
		func(m *Module) { m.Data = make([]byte, MaxDataBytes+1) },
		func(m *Module) { m.Metadata = make([]byte, MaxModuleBytes+1) },
		func(m *Module) { m.Functions[0].Body = append(make([]byte, MaxModuleBytes), 0x0b) },
	} {
		m := encoderFixture()
		mutate(&m)
		if binary, err := Encode(m); err == nil || binary != nil {
			t.Fatalf("accepted invalid module: %d bytes, %v", len(binary), err)
		}
	}
	for _, size := range []int{0, MaxDataBytes} {
		if _, err := Encode(Module{Data: make([]byte, size)}); err != nil {
			t.Fatalf("data boundary %d: %v", size, err)
		}
	}
}

func TestModuleFramingCountsTowardByteLimit(t *testing.T) {
	// At this length the custom section's size uses three LEB bytes rather
	// than the one byte in the empty golden, adding two framing bytes.
	metadata := MaxModuleBytes - 64 - 2
	binary, err := Encode(Module{Metadata: make([]byte, metadata)})
	if err != nil || len(binary) != MaxModuleBytes {
		t.Fatalf("exact module limit: %d %v", len(binary), err)
	}
	if _, err := Encode(Module{Metadata: make([]byte, metadata+1)}); err == nil {
		t.Fatal("ignored section framing at module limit")
	}
}

func TestSharedSignaturesDoNotConsumeByteLimit(t *testing.T) {
	for _, tc := range []struct {
		name               string
		imports, functions int
	}{
		{"functions", 0, 252},
		{"imports", 252, 0},
		{"imports and functions", 126, 126},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := sharedSignatureModule(tc.imports, tc.functions, 256)
			binary, err := Encode(m)
			if err != nil {
				t.Fatalf("shared signature rejected: %v", err)
			}
			_, sections := testSections(t, binary)
			want := append([]byte{1, 0x60, 0x80, 0x02}, bytes.Repeat([]byte{byte(I32)}, 256)...)
			want = append(want, 0)
			if !bytes.Equal(sections[1], want) {
				t.Fatal("shared signature was not encoded exactly once")
			}
			if tc.name == "functions" && len(binary) != 1337 {
				t.Fatalf("function module size = %d, want 1337", len(binary))
			}
		})
	}
}

func TestSharedSignatureEncodedSizeBoundary(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		imports, functions, params int
	}{
		{"functions", 0, 252, 256},
		{"imports", 252, 0, 256},
		{"imports and functions", 126, 126, 256},
		{"many functions", 0, 500, 256},
		{"small signature", 0, 252, 1},
		{"empty signature", 0, 252, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := sharedSignatureModule(tc.imports, tc.functions, tc.params)
			base, err := Encode(m)
			if err != nil {
				t.Fatal(err)
			}
			// The custom section's length prefix grows from one to three LEB bytes.
			padding := MaxModuleBytes - len(base) - 2
			for _, delta := range []int{-1, 0, 1} {
				m.Metadata = make([]byte, padding+delta)
				if err := m.check(); err != nil {
					t.Fatalf("preflight rejected bounded payload at delta %d: %v", delta, err)
				}
				binary, err := Encode(m)
				if delta > 0 {
					if err == nil || binary != nil {
						t.Fatalf("accepted oversized encoded module: %d bytes, %v", len(binary), err)
					}
				} else if err != nil || len(binary) != MaxModuleBytes+delta {
					t.Fatalf("encoded size boundary %d: %d bytes, %v", delta, len(binary), err)
				}
			}
		})
	}
}

func TestSignatureBytesCountTowardEncodedLimit(t *testing.T) {
	m := sharedSignatureModule(0, 1, 65536)
	if err := m.check(); err != nil {
		t.Fatalf("preflight rejected bounded declarations: %v", err)
	}
	if binary, err := Encode(m); err == nil || binary != nil {
		t.Fatalf("accepted signature exceeding encoded limit: %d bytes, %v", len(binary), err)
	}
}

func sharedSignatureModule(imports, functions, params int) Module {
	sig := Signature{Params: make([]ValueType, params)}
	for i := range sig.Params {
		sig.Params[i] = I32
	}
	m := Module{}
	for i := 0; i < imports; i++ {
		m.Imports = append(m.Imports, Import{Module: "host", Name: "call", Signature: sig})
	}
	for i := 0; i < functions; i++ {
		m.Functions = append(m.Functions, Function{Signature: sig, Body: []byte{0x0b}})
	}
	return m
}
