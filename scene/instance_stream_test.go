package scene

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"testing"
)

func float32Slice(n, stride int) []float32 {
	data := make([]float32, n*stride)
	for i := range data {
		data[i] = float32(i) * 0.5
	}
	return data
}

func TestInstanceStreamEncodeIntoReusesStorageAndClearsPadding(t *testing.T) {
	frame := InstanceStreamFrame{BatchID: "ab", Revision: 9, Kind: InstanceStreamTransformColor, Count: 180, Data: float32Slice(180, 20)}
	want, err := frame.Encode()
	if err != nil {
		t.Fatal(err)
	}
	storage := bytes.Repeat([]byte{0xff}, len(want)+128)
	got, err := frame.EncodeInto(storage[:0])
	if err != nil || !bytes.Equal(got, want) || &got[0] != &storage[0] {
		t.Fatalf("reused frame differs from owned encoding: %v", err)
	}
	if allocations := testing.AllocsPerRun(100, func() {
		if _, err := frame.EncodeInto(storage[:0]); err != nil {
			panic(err)
		}
	}); allocations != 0 {
		t.Fatalf("steady encoding allocations = %v, want 0", allocations)
	}
	frame.BatchID, frame.Revision, frame.Count, frame.Data = "x", 10, 0, nil
	got, err = frame.EncodeInto(got)
	want, _ = frame.Encode()
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("smaller reused frame leaked previous header/padding: %v", err)
	}
}

func TestInstanceStreamEncodeRejectsCountOverflowBeforeMultiplication(t *testing.T) {
	frame := InstanceStreamFrame{BatchID: "x", Revision: 1, Kind: InstanceStreamTransform, Count: int(^uint(0) >> 1)}
	if _, err := frame.EncodeInto(make([]byte, 128)); err == nil {
		t.Fatal("overflowing count accepted")
	}
}

func TestInstanceStreamFrameEncodeDecodeRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		kind  InstanceStreamKind
		count int
	}{
		{"transform-zero", InstanceStreamTransform, 0},
		{"transform-one", InstanceStreamTransform, 1},
		{"transform-many", InstanceStreamTransform, 180},
		{"transform-color", InstanceStreamTransformColor, 42},
		{"skinned-pose", InstanceStreamSkinnedPose, 64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame := InstanceStreamFrame{
				BatchID:  "crowd-actors",
				Revision: 9,
				Kind:     tc.kind,
				Count:    tc.count,
				Data:     float32Slice(tc.count, tc.kind.Stride()),
			}
			encoded, err := frame.Encode()
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			got, err := DecodeInstanceStreamFrame(encoded)
			if err != nil {
				t.Fatalf("DecodeInstanceStreamFrame: %v", err)
			}
			if got.BatchID != frame.BatchID {
				t.Fatalf("BatchID = %q, want %q", got.BatchID, frame.BatchID)
			}
			if got.Revision != frame.Revision {
				t.Fatalf("Revision = %d, want %d", got.Revision, frame.Revision)
			}
			if got.Kind != frame.Kind {
				t.Fatalf("Kind = %v, want %v", got.Kind, frame.Kind)
			}
			if got.Count != frame.Count {
				t.Fatalf("Count = %d, want %d", got.Count, frame.Count)
			}
			if len(got.Data) != len(frame.Data) {
				t.Fatalf("Data len = %d, want %d", len(got.Data), len(frame.Data))
			}
			for i := range frame.Data {
				if got.Data[i] != frame.Data[i] {
					t.Fatalf("Data[%d] = %v, want %v", i, got.Data[i], frame.Data[i])
				}
			}
		})
	}
}

// TestInstanceStreamFrameGoldenLayout pins the exact wire bytes for a tiny
// fixture. A change here is a wire-format break: client/runtime/scene3d/
// instance-stream.ts must change in lockstep, on purpose, not by accident.
func TestInstanceStreamFrameGoldenLayout(t *testing.T) {
	frame := InstanceStreamFrame{
		BatchID:  "ab", // 2 bytes -> padded to 4
		Revision: 1,
		Kind:     InstanceStreamTransform,
		Count:    1,
		Data:     []float32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
	}
	got, err := frame.Encode()
	if err != nil {
		t.Fatal(err)
	}

	var want bytes.Buffer
	want.WriteString("GSXI")
	want.WriteByte(1)        // version
	want.WriteByte(0)        // kind = InstanceStreamTransform
	want.Write([]byte{0, 0}) // reserved
	var revision [8]byte
	binary.LittleEndian.PutUint64(revision[:], 1)
	want.Write(revision[:])
	var count [4]byte
	binary.LittleEndian.PutUint32(count[:], 1)
	want.Write(count[:])
	var idLen [2]byte
	binary.LittleEndian.PutUint16(idLen[:], 2)
	want.Write(idLen[:])
	want.Write([]byte{0, 0}) // reserved
	want.WriteString("ab")
	want.Write([]byte{0, 0}) // pad id to 4 bytes
	for _, v := range frame.Data {
		var f [4]byte
		binary.LittleEndian.PutUint32(f[:], math.Float32bits(v))
		want.Write(f[:])
	}

	if !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("wire bytes changed:\n got  = % x\n want = % x", got, want.Bytes())
	}
	if len(got) != 24+4+16*4 {
		t.Fatalf("unexpected total length %d", len(got))
	}
}

func TestInstanceStreamFrameEncodeRejectsZeroRevision(t *testing.T) {
	frame := InstanceStreamFrame{BatchID: "x", Revision: 0, Kind: InstanceStreamTransform, Count: 0}
	if _, err := frame.Encode(); err == nil {
		t.Fatal("expected zero revision to be rejected")
	}
}

func TestInstanceStreamFrameEncodeRejectsEmptyBatchID(t *testing.T) {
	frame := InstanceStreamFrame{BatchID: "", Revision: 1, Kind: InstanceStreamTransform, Count: 0}
	if _, err := frame.Encode(); err == nil {
		t.Fatal("expected empty batch id to be rejected")
	}
}

func TestInstanceStreamFrameEncodeRejectsUnknownKind(t *testing.T) {
	frame := InstanceStreamFrame{BatchID: "x", Revision: 1, Kind: InstanceStreamKind(99), Count: 0}
	if _, err := frame.Encode(); err == nil {
		t.Fatal("expected unknown kind to be rejected")
	}
}

func TestInstanceStreamFrameEncodeRejectsCountDataMismatch(t *testing.T) {
	frame := InstanceStreamFrame{
		BatchID: "x", Revision: 1, Kind: InstanceStreamTransform, Count: 2,
		Data: float32Slice(1, 16), // only one instance's worth of floats
	}
	if _, err := frame.Encode(); err == nil {
		t.Fatal("expected count/data mismatch to be rejected")
	}
}

func TestInstanceStreamFrameEncodeRejectsNegativeCount(t *testing.T) {
	frame := InstanceStreamFrame{BatchID: "x", Revision: 1, Kind: InstanceStreamTransform, Count: -1}
	if _, err := frame.Encode(); err == nil {
		t.Fatal("expected negative count to be rejected")
	}
}

func TestDecodeInstanceStreamFrameRejectsShortBuffer(t *testing.T) {
	if _, err := DecodeInstanceStreamFrame([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected short buffer to be rejected")
	}
}

func TestDecodeInstanceStreamFrameRejectsBadMagic(t *testing.T) {
	frame := InstanceStreamFrame{BatchID: "x", Revision: 1, Kind: InstanceStreamTransform, Count: 0}
	encoded, err := frame.Encode()
	if err != nil {
		t.Fatal(err)
	}
	encoded[0] = 'Z'
	if _, err := DecodeInstanceStreamFrame(encoded); err == nil {
		t.Fatal("expected bad magic to be rejected")
	}
}

func TestDecodeInstanceStreamFrameRejectsBadVersion(t *testing.T) {
	frame := InstanceStreamFrame{BatchID: "x", Revision: 1, Kind: InstanceStreamTransform, Count: 0}
	encoded, err := frame.Encode()
	if err != nil {
		t.Fatal(err)
	}
	encoded[4] = 2
	if _, err := DecodeInstanceStreamFrame(encoded); err == nil {
		t.Fatal("expected unsupported version to be rejected")
	}
}

func TestDecodeInstanceStreamFrameRejectsTruncatedPayload(t *testing.T) {
	frame := InstanceStreamFrame{
		BatchID: "x", Revision: 1, Kind: InstanceStreamTransform, Count: 2,
		Data: float32Slice(2, 16),
	}
	encoded, err := frame.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeInstanceStreamFrame(encoded[:len(encoded)-4]); err == nil {
		t.Fatal("expected truncated payload to be rejected")
	}
}

func TestInstanceStreamKindStride(t *testing.T) {
	if got := InstanceStreamTransform.Stride(); got != 16 {
		t.Fatalf("InstanceStreamTransform.Stride() = %d, want 16", got)
	}
	if got := InstanceStreamTransformColor.Stride(); got != 20 {
		t.Fatalf("InstanceStreamTransformColor.Stride() = %d, want 20", got)
	}
	if got := InstanceStreamSkinnedPose.Stride(); got != 18 {
		t.Fatalf("InstanceStreamSkinnedPose.Stride() = %d, want 18", got)
	}
	if got := InstanceStreamKind(99).Stride(); got != 0 {
		t.Fatalf("unknown kind Stride() = %d, want 0", got)
	}
}

func TestInstanceStreamKindString(t *testing.T) {
	for _, k := range []InstanceStreamKind{InstanceStreamTransform, InstanceStreamTransformColor, InstanceStreamSkinnedPose} {
		if k.String() == "" {
			t.Fatalf("kind %d has empty String()", k)
		}
	}
	if got := InstanceStreamKind(99).String(); got == "" {
		t.Fatal("unknown kind should still describe itself")
	}
}

func TestInstanceStreamTransformColorsPackOwnsAndReusesStorage(t *testing.T) {
	records := []InstanceTransformColor{{Transform: [16]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 2, 3, 4, 1}, Color: [4]float32{.25, .5, 1, 1}}}
	f := InstanceStreamFrame{BatchID: "tinted", Revision: 7}
	f.SetTransformColors(records)
	first := &f.Data[0]
	records[0].Color[1] = .75
	if f.Data[17] != .5 || f.Kind != InstanceStreamTransformColor || f.Count != 1 {
		t.Fatal("snapshot was not packed independently", f)
	}
	f.SetTransformColors(nil)
	if f.Count != 0 || len(f.Data) != 0 {
		t.Fatal("empty snapshot", f)
	}
	f.SetTransformColors(records)
	if &f.Data[0] != first || f.Data[17] != .75 || f.BatchID != "tinted" || f.Revision != 7 {
		t.Fatal("repack lost storage or metadata", f)
	}
	b, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeInstanceStreamFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, f) {
		t.Fatal("packed roundtrip mismatch", decoded, f)
	}
	for _, invalid := range []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))} {
		f.Data[19] = invalid
		dst := bytes.Repeat([]byte{0x7f}, 128)
		before := append([]byte(nil), dst...)
		if _, err := f.EncodeInto(dst); err == nil {
			t.Fatal("accepted non-finite color")
		}
		if !bytes.Equal(dst, before) {
			t.Fatal("invalid encode mutated destination")
		}
	}
}
