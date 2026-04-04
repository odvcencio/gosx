package quant

import "testing"

func TestPackUnpackIndices1Bit(t *testing.T) {
	indices := []int{0, 1, 1, 0, 1, 0, 0, 1}
	dst := make([]byte, packedSize(len(indices), 1))
	packIndices(dst, indices, 1)
	got := make([]int, len(indices))
	unpackIndices(got, dst, len(indices), 1)
	for i := range indices {
		if got[i] != indices[i] {
			t.Fatalf("index %d: got %d want %d", i, got[i], indices[i])
		}
	}
}

func TestPackUnpackIndices2Bit(t *testing.T) {
	indices := []int{0, 1, 2, 3, 3, 2, 1, 0}
	dst := make([]byte, packedSize(len(indices), 2))
	packIndices(dst, indices, 2)
	got := make([]int, len(indices))
	unpackIndices(got, dst, len(indices), 2)
	for i := range indices {
		if got[i] != indices[i] {
			t.Fatalf("index %d: got %d want %d", i, got[i], indices[i])
		}
	}
}

func TestPackUnpackIndices3Bit(t *testing.T) {
	indices := []int{0, 1, 2, 3, 4, 5, 6, 7, 7, 0}
	dst := make([]byte, packedSize(len(indices), 3))
	packIndices(dst, indices, 3)
	got := make([]int, len(indices))
	unpackIndices(got, dst, len(indices), 3)
	for i := range indices {
		if got[i] != indices[i] {
			t.Fatalf("index %d: got %d want %d", i, got[i], indices[i])
		}
	}
}

func TestPackUnpackIndices4Bit(t *testing.T) {
	indices := make([]int, 100)
	for i := range indices {
		indices[i] = i % 16
	}
	dst := make([]byte, packedSize(len(indices), 4))
	packIndices(dst, indices, 4)
	got := make([]int, len(indices))
	unpackIndices(got, dst, len(indices), 4)
	for i := range indices {
		if got[i] != indices[i] {
			t.Fatalf("index %d: got %d want %d", i, got[i], indices[i])
		}
	}
}

func TestPackUnpackSigns(t *testing.T) {
	signs := []bool{true, false, true, true, false, false, true, false, true}
	dst := make([]byte, (len(signs)+7)/8)
	packSigns(dst, signs)
	got := make([]bool, len(signs))
	unpackSigns(got, dst, len(signs))
	for i := range signs {
		if got[i] != signs[i] {
			t.Fatalf("sign %d: got %v want %v", i, got[i], signs[i])
		}
	}
}

func TestPackedSize(t *testing.T) {
	cases := []struct{ count, bits, want int }{
		{8, 1, 1}, {9, 1, 2}, {8, 2, 2}, {8, 4, 4}, {10, 3, 4}, {384, 2, 96},
	}
	for _, c := range cases {
		got := packedSize(c.count, c.bits)
		if got != c.want {
			t.Errorf("packedSize(%d, %d) = %d, want %d", c.count, c.bits, got, c.want)
		}
	}
}
