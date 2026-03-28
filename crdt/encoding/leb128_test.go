package encoding

import "testing"

func TestULEB128RoundTrip(t *testing.T) {
	values := []uint64{0, 1, 63, 127, 128, 255, 256, 16384, 1<<20 + 7, 1<<63 - 1}
	for _, value := range values {
		encoded := AppendULEB128(nil, value)
		decoded, n, err := ReadULEB128(encoded)
		if err != nil {
			t.Fatalf("read %d: %v", value, err)
		}
		if decoded != value {
			t.Fatalf("round trip mismatch for %d: got %d", value, decoded)
		}
		if n != len(encoded) {
			t.Fatalf("expected %d bytes consumed for %d, got %d", len(encoded), value, n)
		}
	}
}
