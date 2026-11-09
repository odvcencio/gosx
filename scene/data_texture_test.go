package scene

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"m31labs.dev/selena/bindings"
)

func TestTextureFormatNamesSizesAndParsing(t *testing.T) {
	want := map[TextureFormat]struct {
		name  string
		bytes int
	}{
		TextureRGBA8Unorm: {"rgba8unorm", 4}, TextureRGBA16Float: {"rgba16float", 8}, TextureRGBA32Float: {"rgba32float", 16},
		TextureR16Float: {"r16float", 2}, TextureR32Float: {"r32float", 4},
	}
	for format, w := range want {
		if format.String() != w.name || format.BytesPerTexel() != w.bytes || !format.Valid() {
			t.Errorf("format %d = %s/%d, want %s/%d", format, format, format.BytesPerTexel(), w.name, w.bytes)
		}
		if parsed, err := ParseTextureFormat(w.name); err != nil || parsed != format {
			t.Errorf("ParseTextureFormat(%q) = %v, %v", w.name, parsed, err)
		}
	}
	if TextureFormat(9).Valid() || TextureFormat(9).BytesPerTexel() != 0 || !strings.Contains(TextureFormat(9).String(), "9") {
		t.Error("an unknown format must be invalid")
	}
	if _, err := ParseTextureFormat("rgb565"); err == nil || !strings.Contains(err.Error(), "r16float") {
		t.Errorf("unknown name error = %v", err)
	}
}

func TestDataTextureRefAndKeyValidation(t *testing.T) {
	if DataTextureRef("spec/a") != "gosx:data:spec/a" {
		t.Fatal(DataTextureRef("spec/a"))
	}
	for _, key := range []string{"spectrum", "a/b:c", "ünï", strings.Repeat("k", MaxDataTextureKeyBytes)} {
		if err := ValidateDataTextureKey(key); err != nil {
			t.Errorf("%q rejected: %v", key, err)
		}
	}
	for key, want := range map[string]string{
		"": "empty", "a b": "whitespace", "a\tb": "whitespace", "a\x00": "whitespace", "a\x7f": "whitespace",
		strings.Repeat("k", MaxDataTextureKeyBytes+1): "limit", "\xff": "UTF-8",
	} {
		if err := ValidateDataTextureKey(key); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q error = %v, want mention of %q", key, err, want)
		}
	}
}

func TestDataTextureFrameRoundTrip(t *testing.T) {
	texels := make([]byte, 3*2*2)
	for i := range texels {
		texels[i] = byte(i + 1)
	}
	for _, f := range []DataTextureFrame{
		{Key: "a", Revision: 1, Op: DataTextureCreate, Format: TextureR16Float, Width: 8, Height: 4},
		{Key: "ring/long-key", Revision: 1 << 40, Op: DataTextureWrite, Format: TextureR16Float, Width: 8, Height: 4, OriginX: 5, OriginY: 2, RectWidth: 3, RectHeight: 2, Texels: texels},
		{Key: "gone", Revision: 3, Op: DataTextureDelete},
	} {
		data, err := f.Encode()
		if err != nil {
			t.Fatalf("%+v: %v", f, err)
		}
		if string(data[:4]) != "GSXT" || data[4] != 1 {
			t.Fatalf("header = %q %d", data[:4], data[4])
		}
		if payload := align4(dataTextureHeaderBytes + len(f.Key)); len(data) != payload+len(f.Texels) {
			t.Errorf("length %d, want %d", len(data), payload+len(f.Texels))
		}
		got, err := DecodeDataTextureFrame(data)
		if err != nil {
			t.Fatalf("decode %+v: %v", f, err)
		}
		if got.Key != f.Key || got.Revision != f.Revision || got.Op != f.Op || got.Format != f.Format || got.Width != f.Width || got.Height != f.Height ||
			got.OriginX != f.OriginX || got.OriginY != f.OriginY || got.RectWidth != f.RectWidth || got.RectHeight != f.RectHeight || !bytes.Equal(got.Texels, f.Texels) {
			t.Errorf("round trip = %+v, want %+v", got, f)
		}
	}
}

func TestDataTextureFrameWireLayout(t *testing.T) {
	data, err := DataTextureFrame{Key: "k", Revision: 7, Op: DataTextureWrite, Format: TextureR32Float, Width: 4, Height: 3, OriginX: 1, OriginY: 2, RectWidth: 2, RectHeight: 1, Texels: make([]byte, 8)}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	u32 := func(off int) uint32 { return binary.LittleEndian.Uint32(data[off:]) }
	if data[5] != byte(DataTextureWrite) || data[6] != byte(TextureR32Float) || binary.LittleEndian.Uint64(data[8:]) != 7 ||
		u32(16) != 4 || u32(20) != 3 || u32(24) != 1 || u32(28) != 2 || u32(32) != 2 || u32(36) != 1 || binary.LittleEndian.Uint16(data[40:]) != 1 || data[44] != 'k' {
		t.Fatalf("layout wrong: % x", data[:48])
	}
	if len(data) != 48+8 {
		t.Fatalf("texels must start at the 4-byte boundary after the key: length %d", len(data))
	}
}

func TestDataTextureFrameValidationNamesTheProblem(t *testing.T) {
	ok := DataTextureFrame{Key: "k", Revision: 1, Op: DataTextureWrite, Format: TextureRGBA8Unorm, Width: 4, Height: 4, RectWidth: 2, RectHeight: 2, Texels: make([]byte, 16)}
	mutate := func(f func(*DataTextureFrame)) DataTextureFrame { c := ok; f(&c); return c }
	for name, tc := range map[string]struct {
		frame DataTextureFrame
		want  string
	}{
		"zero revision":       {mutate(func(f *DataTextureFrame) { f.Revision = 0 }), "revision"},
		"empty key":           {mutate(func(f *DataTextureFrame) { f.Key = "" }), "key"},
		"unknown op":          {mutate(func(f *DataTextureFrame) { f.Op = 9 }), "op 9"},
		"unknown format":      {mutate(func(f *DataTextureFrame) { f.Format = 9 }), "format"},
		"zero width":          {mutate(func(f *DataTextureFrame) { f.Width = 0 }), "size 0x4"},
		"too wide":            {mutate(func(f *DataTextureFrame) { f.Width = MaxDataTextureSize + 1 }), "outside 1.."},
		"too many bytes":      {mutate(func(f *DataTextureFrame) { f.Format = TextureRGBA32Float; f.Width, f.Height = 8192, 8192 }), "exceeds"},
		"empty rect":          {mutate(func(f *DataTextureFrame) { f.RectWidth = 0 }), "at least 1x1"},
		"negative origin":     {mutate(func(f *DataTextureFrame) { f.OriginX = -1 }), "falls outside"},
		"rect past right":     {mutate(func(f *DataTextureFrame) { f.OriginX = 3 }), "falls outside"},
		"rect past bottom":    {mutate(func(f *DataTextureFrame) { f.OriginY = 3 }), "falls outside"},
		"rect too large":      {mutate(func(f *DataTextureFrame) { f.RectWidth = 5; f.Texels = make([]byte, 40) }), "falls outside"},
		"short texels":        {mutate(func(f *DataTextureFrame) { f.Texels = make([]byte, 15) }), "needs 16 bytes, got 15"},
		"long texels":         {mutate(func(f *DataTextureFrame) { f.Texels = make([]byte, 17) }), "needs 16 bytes, got 17"},
		"create with texels":  {DataTextureFrame{Key: "k", Revision: 1, Op: DataTextureCreate, Width: 2, Height: 2, Texels: []byte{1}}, "no rectangle or texels"},
		"create with rect":    {DataTextureFrame{Key: "k", Revision: 1, Op: DataTextureCreate, Width: 2, Height: 2, RectWidth: 1}, "no rectangle or texels"},
		"delete with texels":  {DataTextureFrame{Key: "k", Revision: 1, Op: DataTextureDelete, Texels: []byte{1}}, "delete carries no"},
		"delete with a shape": {DataTextureFrame{Key: "k", Revision: 1, Op: DataTextureDelete, Width: 2}, "delete carries no"},
	} {
		if _, err := tc.frame.Encode(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want mention of %q", name, err, tc.want)
		}
	}
}

func TestDecodeDataTextureFrameRejectsBadBytes(t *testing.T) {
	good, err := DataTextureFrame{Key: "k", Revision: 1, Op: DataTextureCreate, Format: TextureR16Float, Width: 2, Height: 2}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	badMagic := append([]byte("GSXI"), good[4:]...)
	badVersion := append([]byte(nil), good...)
	badVersion[4] = 2
	for name, data := range map[string][]byte{"nil": nil, "short": good[:20], "magic": badMagic, "version": badVersion, "key cut off": good[:dataTextureHeaderBytes-1+0]} {
		if _, err := DecodeDataTextureFrame(data); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
	longKey := append([]byte(nil), good...)
	binary.LittleEndian.PutUint16(longKey[40:], 500)
	if _, err := DecodeDataTextureFrame(longKey); err == nil || !strings.Contains(err.Error(), "shorter than its key") {
		t.Errorf("long key length error = %v", err)
	}
}

func TestEncodeIntoReusesStorageAndClearsPadding(t *testing.T) {
	f := DataTextureFrame{Key: "abc", Revision: 1, Op: DataTextureWrite, Format: TextureR16Float, Width: 4, Height: 1, RectWidth: 4, RectHeight: 1, Texels: bytes.Repeat([]byte{9}, 8)}
	dirty := bytes.Repeat([]byte{0xff}, 128)
	data, err := f.EncodeInto(dirty)
	if err != nil {
		t.Fatal(err)
	}
	if &data[0] != &dirty[0] {
		t.Error("EncodeInto must reuse a large enough buffer")
	}
	if data[7] != 0 || data[42] != 0 || data[47] != 0 {
		t.Errorf("reserved and padding bytes must be zero: % x", data[:48])
	}
	allocs := testing.AllocsPerRun(50, func() { data, _ = f.EncodeInto(data) })
	if allocs != 0 {
		t.Errorf("steady-state EncodeInto allocated %v times", allocs)
	}
}

func TestDataTextureEncoderTracksTexturesAndRevisions(t *testing.T) {
	enc := NewDataTextureEncoder()
	rev := func(b []byte) uint64 { return binary.LittleEndian.Uint64(b[8:]) }
	if _, err := enc.WriteTexture("t", 1, 1, TextureR16Float, []byte{0, 0}, 0, 0); err == nil || !strings.Contains(err.Error(), "call Create first") {
		t.Fatalf("write before create error = %v", err)
	}
	if _, err := enc.Delete("t"); err == nil {
		t.Fatal("delete before create must fail")
	}
	frame, err := enc.Create("t", 4, 2, TextureR16Float)
	if err != nil || rev(frame) != 1 || frame[5] != byte(DataTextureCreate) {
		t.Fatalf("create = % x, %v", frame[:8], err)
	}
	frame, err = enc.WriteTexture("t", 2, 1, TextureR16Float, []byte{1, 2, 3, 4}, 2, 1)
	if err != nil || rev(frame) != 2 {
		t.Fatalf("write = %v, revision %d", err, rev(frame))
	}
	decoded, _ := DecodeDataTextureFrame(frame)
	if decoded.Width != 4 || decoded.Height != 2 || decoded.OriginX != 2 || decoded.OriginY != 1 {
		t.Errorf("write frame must carry the whole-texture size: %+v", decoded)
	}
	for name, call := range map[string]func() error{
		"format mismatch": func() error { _, e := enc.WriteTexture("t", 1, 1, TextureR32Float, make([]byte, 4), 0, 0); return e },
		"out of bounds":   func() error { _, e := enc.WriteTexture("t", 2, 1, TextureR16Float, make([]byte, 4), 3, 1); return e },
		"wrong length":    func() error { _, e := enc.WriteTexture("t", 2, 1, TextureR16Float, make([]byte, 3), 0, 0); return e },
		"bad create":      func() error { _, e := enc.Create("t", 0, 4, TextureR16Float); return e },
		"bad key":         func() error { _, e := enc.Create("a b", 1, 1, TextureR16Float); return e },
	} {
		if call() == nil {
			t.Errorf("%s: no error", name)
		}
	}
	// A rejected call must not burn a revision: the next frame is still 3.
	frame, _ = enc.WriteTexture("t", 1, 1, TextureR16Float, []byte{0, 0}, 0, 0)
	if rev(frame) != 3 {
		t.Errorf("revision after rejected calls = %d, want 3", rev(frame))
	}
	frame, err = enc.Delete("t")
	if err != nil || rev(frame) != 4 || frame[5] != byte(DataTextureDelete) {
		t.Fatalf("delete = %v", err)
	}
	if _, err := enc.WriteTexture("t", 1, 1, TextureR16Float, []byte{0, 0}, 0, 0); err == nil {
		t.Error("write after delete must fail")
	}
	frame, err = enc.Create("t", 2, 2, TextureR32Float)
	if err != nil || rev(frame) != 5 {
		t.Errorf("a reused key must keep rising: revision %d, %v", rev(frame), err)
	}
}

func TestFloat16Bits(t *testing.T) {
	for _, tc := range []struct {
		in   float32
		want uint16
	}{
		{0, 0}, {float32(math.Copysign(0, -1)), 0x8000}, {1, 0x3c00}, {-2, 0xc000}, {0.5, 0x3800}, {0.25, 0x3400}, {65504, 0x7bff},
		{65520, 0x7c00}, {1e9, 0x7c00}, {-1e9, 0xfc00}, {float32(math.Inf(1)), 0x7c00}, {float32(math.Inf(-1)), 0xfc00},
		{6.103515625e-05, 0x0400},               // smallest normal
		{5.960464477539063e-08, 0x0001},         // smallest subnormal
		{float32(math.Ldexp(1, -25)), 0x0000},   // exactly half of the smallest subnormal rounds to even (0)
		{float32(math.Ldexp(1.5, -25)), 0x0001}, // just above rounds up
		{6.097555160522461e-05, 0x03ff},         // largest subnormal
		{1e-10, 0x0000}, {1.0009765625, 0x3c01}, // 1 + 2^-10
		{1.00048828125, 0x3c00}, // 1 + 2^-11: tie rounds to even
		{1.00146484375, 0x3c02}, // 1 + 3*2^-11: tie rounds to even (up)
		{2047.5, 0x6800},        // rounds within the last mantissa bit
		{0.1, 0x2e66}, {-3.14159274, 0xc248},
	} {
		if got := Float16Bits(tc.in); got != tc.want {
			t.Errorf("Float16Bits(%v) = %#04x, want %#04x", tc.in, got, tc.want)
		}
	}
	if got := Float16Bits(float32(math.NaN())); got&0x7c00 != 0x7c00 || got&0x3ff == 0 {
		t.Errorf("NaN = %#04x, want a NaN pattern", got)
	}
}

func TestAppendFloatPackers(t *testing.T) {
	half := AppendFloat16([]byte{0xaa}, []float32{1, -2})
	if !bytes.Equal(half, []byte{0xaa, 0x00, 0x3c, 0x00, 0xc0}) {
		t.Errorf("AppendFloat16 = % x", half)
	}
	full := AppendFloat32(nil, []float32{1, -2})
	if !bytes.Equal(full, []byte{0, 0, 0x80, 0x3f, 0, 0, 0, 0xc0}) {
		t.Errorf("AppendFloat32 = % x", full)
	}
}

func TestValidateSelenaDataTextureRef(t *testing.T) {
	tex := func(dim, format string) bindings.Texture {
		return bindings.Texture{Name: "spec", Dimension: dim, Format: format}
	}
	for _, tc := range []struct {
		texture bindings.Texture
		ref     string
		want    string
	}{
		{tex("2d", "r16float"), "gosx:data:spectrum", ""},
		{tex("2d", ""), " gosx:data:spectrum ", ""},
		{tex("2d", ""), "https://example.test/a.png", ""},
		{tex("2d", ""), "gosx:environment:brdf-lut", ""},
		{tex("cube", ""), "gosx:data:spectrum", "requires a 2d texture, got cube"},
		{tex("depth2d", ""), "gosx:data:spectrum", "requires a 2d texture"},
		{tex("2d", ""), "gosx:data:", "empty"},
		{tex("2d", ""), "gosx:data:a b", "whitespace"},
		{tex("2d", "rgb565"), "gosx:data:spectrum", "unknown texture format"},
	} {
		err := validateSelenaDataTextureRef(tc.texture, tc.ref)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%q on %+v: unexpected error %v", tc.ref, tc.texture, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%q on %+v: error = %v, want mention of %q", tc.ref, tc.texture, err, tc.want)
		}
	}
}
