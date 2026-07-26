package field

import (
	"encoding/json"
	"testing"
)

func quantizedForTest(t *testing.T, res, components, bits, preview int) *Quantized {
	t.Helper()
	f := testField(res, components)
	q, err := f.QuantizeChecked(QuantizeOptions{BitWidth: bits, PreviewBits: preview})
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestQuantizedBinaryRoundTrip(t *testing.T) {
	cases := []struct {
		name       string
		res        int
		components int
		bits       int
		preview    int
	}{
		{"scalar", 8, 1, 6, 0},
		{"vec2", 6, 2, 4, 0},
		{"vec3", 7, 3, 8, 0},
		{"vec4preview", 5, 4, 6, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := quantizedForTest(t, tc.res, tc.components, tc.bits, tc.preview)
			data, err := want.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			if len(data) != want.BinarySize() {
				t.Fatalf("BinarySize = %d, encoded %d bytes", want.BinarySize(), len(data))
			}
			got, err := DecodeQuantized(data)
			if err != nil {
				t.Fatal(err)
			}
			if got.Resolution != want.Resolution || got.Components != want.Components {
				t.Fatalf("shape = %v/%d, want %v/%d",
					got.Resolution, got.Components, want.Resolution, want.Components)
			}
			if got.Bounds != want.Bounds {
				t.Fatalf("bounds = %v, want %v", got.Bounds, want.Bounds)
			}
			if got.BitWidth != want.BitWidth || got.PreviewBits != want.PreviewBits {
				t.Fatalf("bits = %d/%d, want %d/%d",
					got.BitWidth, got.PreviewBits, want.BitWidth, want.PreviewBits)
			}
			if got.IsDelta != want.IsDelta {
				t.Fatalf("IsDelta = %v, want %v", got.IsDelta, want.IsDelta)
			}
			sameData(t, "Mins", got.Mins, want.Mins)
			sameData(t, "Maxs", got.Maxs, want.Maxs)
			if string(got.Packed) != string(want.Packed) {
				t.Fatal("packed payload changed")
			}
			if string(got.Preview) != string(want.Preview) {
				t.Fatal("preview payload changed")
			}

			// The decoded form must reconstruct the same field.
			wantField, err := want.DecompressChecked()
			if err != nil {
				t.Fatal(err)
			}
			gotField, err := got.DecompressChecked()
			if err != nil {
				t.Fatal(err)
			}
			sameData(t, "decompressed", gotField.Data, wantField.Data)
		})
	}
}

func TestQuantizedBinaryCarriesDeltaFlag(t *testing.T) {
	base := testField(6, 3)
	next := testField(6, 3)
	next.Data[0] += 0.5
	q, err := next.QuantizeChecked(QuantizeOptions{BitWidth: 6, DeltaAgainst: base})
	if err != nil {
		t.Fatal(err)
	}
	if !q.IsDelta {
		t.Fatal("expected a delta payload")
	}
	data, err := q.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeQuantized(data)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsDelta {
		t.Fatal("delta flag lost in the binary form")
	}
	if _, err := ApplyDeltaChecked(base, got); err != nil {
		t.Fatal(err)
	}
}

func TestQuantizedBinaryRejectsBadPayloads(t *testing.T) {
	q := quantizedForTest(t, 6, 3, 6, 0)
	data, err := q.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := DecodeQuantized(nil); err == nil {
		t.Fatal("decoder accepted an empty payload")
	}
	bad := append([]byte(nil), data...)
	bad[0] = 0x7f
	if _, err := DecodeQuantized(bad); err == nil {
		t.Fatal("decoder accepted an unknown version")
	}
	for cut := 1; cut < len(data); cut += 7 {
		if _, err := DecodeQuantized(data[:cut]); err == nil {
			t.Fatalf("decoder accepted a payload truncated at %d bytes", cut)
		}
	}
}

// TestQuantizedBinaryNeverPanicsOnJunk checks the decoder against damaged bytes.
func TestQuantizedBinaryNeverPanicsOnJunk(t *testing.T) {
	q := quantizedForTest(t, 5, 3, 6, 4)
	data, err := q.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(data); i++ {
		for _, flip := range []byte{0x00, 0xff, 0x80} {
			bad := append([]byte(nil), data...)
			bad[i] = flip
			// The only requirement is that the decoder returns instead of
			// panicking. A damaged payload may still decode.
			_, _ = DecodeQuantized(bad)
		}
	}
}

// TestBinaryWireIsSmallerThanJSON reports the size of both transports.
func TestBinaryWireIsSmallerThanJSON(t *testing.T) {
	cases := []struct {
		name       string
		resolution int
		components int
		bits       int
		preview    int
	}{
		{"32cube/scalar/6bit", 32, 1, 6, 0},
		{"32cube/vec3/6bit", 32, 3, 6, 0},
		{"64cube/vec3/6bit", 64, 3, 6, 0},
		{"64cube/vec3/6bit+preview4", 64, 3, 6, 4},
	}
	for _, tc := range cases {
		q := quantizedForTest(t, tc.resolution, tc.components, tc.bits, tc.preview)
		jsonBytes, err := json.Marshal(q)
		if err != nil {
			t.Fatal(err)
		}
		binaryBytes, err := q.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s WireSize=%d binary=%d json=%d binaryOverhead=%.2f%% jsonOverhead=%.1f%% saved=%.1f%%",
			tc.name, q.WireSize(), len(binaryBytes), len(jsonBytes),
			100*float64(len(binaryBytes)-q.WireSize())/float64(q.WireSize()),
			100*float64(len(jsonBytes)-q.WireSize())/float64(q.WireSize()),
			100*float64(len(jsonBytes)-len(binaryBytes))/float64(len(jsonBytes)))
		if len(binaryBytes) >= len(jsonBytes) {
			t.Fatalf("%s: binary %d bytes is not smaller than json %d bytes",
				tc.name, len(binaryBytes), len(jsonBytes))
		}
		if len(binaryBytes) > q.WireSize()+64 {
			t.Fatalf("%s: binary header is %d bytes, want at most 64",
				tc.name, len(binaryBytes)-q.WireSize())
		}
	}
}
