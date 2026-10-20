package wasmgen

import (
	"bytes"
	"encoding/hex"
	"math"
	"testing"
)

func TestUnsignedLEBBoundaries(t *testing.T) {
	for _, tc := range []struct {
		value uint32
		hex   string
	}{
		{0, "00"}, {127, "7f"}, {128, "8001"}, {255, "ff01"},
		{16383, "ff7f"}, {16384, "808001"}, {1 << 28, "8080808001"},
		{math.MaxUint32, "ffffffff0f"},
	} {
		want, _ := hex.DecodeString(tc.hex)
		got := AppendU32([]byte{0xaa}, tc.value)
		if !bytes.Equal(got, append([]byte{0xaa}, want...)) {
			t.Fatalf("%d: %x want %x", tc.value, got, want)
		}
	}
}

func TestSignedLEBBoundaries(t *testing.T) {
	for _, tc := range []struct {
		value int64
		hex   string
	}{
		{0, "00"}, {63, "3f"}, {64, "c000"}, {127, "ff00"}, {128, "8001"},
		{-1, "7f"}, {-64, "40"}, {-65, "bf7f"}, {-128, "807f"}, {-129, "ff7e"},
		{math.MinInt32, "8080808078"}, {math.MaxInt32, "ffffffff07"},
		{math.MinInt64, "8080808080808080807f"}, {math.MaxInt64, "ffffffffffffffffff00"},
	} {
		want, _ := hex.DecodeString(tc.hex)
		got := AppendI64([]byte{0xaa}, tc.value)
		if !bytes.Equal(got, append([]byte{0xaa}, want...)) {
			t.Fatalf("%d: %x want %x", tc.value, got, want)
		}
		if tc.value >= math.MinInt32 && tc.value <= math.MaxInt32 && !bytes.Equal(AppendI32(nil, int32(tc.value)), want) {
			t.Fatalf("i32 %d", tc.value)
		}
	}
}

func TestSignedLEBTransitionRoundTrips(t *testing.T) {
	for bit := 0; bit < 63; bit++ {
		boundary := int64(1) << bit
		for _, value := range []int64{boundary - 1, boundary, -boundary, -boundary - 1} {
			encoded := AppendI64(nil, value)
			var decoded uint64
			for i, b := range encoded {
				decoded |= uint64(b&0x7f) << (7 * i)
			}
			shift := uint(len(encoded) * 7)
			if shift < 64 && encoded[len(encoded)-1]&0x40 != 0 {
				decoded |= ^uint64(0) << shift
			}
			if int64(decoded) != value || len(encoded) > 10 {
				t.Fatalf("%d -> %x -> %d", value, encoded, int64(decoded))
			}
			if len(encoded) > 1 {
				last, previous := encoded[len(encoded)-1], encoded[len(encoded)-2]
				if last == 0 && previous&0x40 == 0 || last == 0x7f && previous&0x40 != 0 {
					t.Fatalf("nonminimal %x", encoded)
				}
			}
		}
	}
}
