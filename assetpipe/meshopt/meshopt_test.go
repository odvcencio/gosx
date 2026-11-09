package meshopt

import (
	"bytes"
	"math"
	"math/rand"
	"testing"
)

func TestVertexRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for _, stride := range []int{4, 8, 12, 16, 32, 36, 64, 256} {
		for _, count := range []int{0, 1, 15, 16, 17, 255, 256, 257, 1000, 3333} {
			data := make([]byte, count*stride)
			for i := range data {
				// A smooth ramp with noise and occasional jumps: every group form appears.
				data[i] = byte(i/stride*3 + rng.Intn(4))
				if rng.Intn(50) == 0 {
					data[i] = byte(rng.Intn(256))
				}
			}
			enc, err := EncodeVertexBuffer(data, count, stride)
			if err != nil {
				t.Fatal(err)
			}
			got, err := DecodeVertexBuffer(enc, count, stride)
			if err != nil {
				t.Fatalf("stride %d count %d: %v", stride, count, err)
			}
			if !bytes.Equal(got, data) {
				t.Fatalf("stride %d count %d: round trip differs", stride, count)
			}
		}
	}
}

func TestVertexCompressesSmoothData(t *testing.T) {
	const count, stride = 2000, 12
	data := make([]byte, count*stride)
	for v := 0; v < count; v++ {
		for k := 0; k < stride; k++ {
			data[v*stride+k] = byte(v / (k + 1))
		}
	}
	enc, err := EncodeVertexBuffer(data, count, stride)
	if err != nil {
		t.Fatal(err)
	}
	if len(enc)*3 > len(data) {
		t.Fatalf("smooth data compressed to %d of %d bytes, want under a third", len(enc), len(data))
	}
}

func TestVertexRejectsBadInput(t *testing.T) {
	if _, err := EncodeVertexBuffer(make([]byte, 18), 3, 6); err == nil {
		t.Fatal("a stride of 6 must fail")
	}
	if _, err := EncodeVertexBuffer(make([]byte, 10), 3, 4); err == nil {
		t.Fatal("a size mismatch must fail")
	}
	if _, err := DecodeVertexBuffer([]byte{0xa1, 0, 0, 0, 0}, 1, 4); err == nil {
		t.Fatal("a bad header must fail")
	}
}

func rotated(tri [3]uint32) [3]uint32 {
	m := 0
	for i := 1; i < 3; i++ {
		if tri[i] < tri[m] {
			m = i
		}
	}
	return [3]uint32{tri[m], tri[(m+1)%3], tri[(m+2)%3]}
}

func checkIndexRoundTrip(t *testing.T, name string, indices []uint32, vertices int) []byte {
	t.Helper()
	enc, err := EncodeIndexBuffer(indices, vertices)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	got, err := DecodeIndexBuffer(enc, len(indices))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(got) != len(indices) {
		t.Fatalf("%s: %d indices back, want %d", name, len(got), len(indices))
	}
	for i := 0; i < len(indices); i += 3 {
		want := rotated([3]uint32{indices[i], indices[i+1], indices[i+2]})
		have := rotated([3]uint32{got[i], got[i+1], got[i+2]})
		if want != have {
			t.Fatalf("%s: triangle %d is %v, want %v (up to rotation)", name, i/3, have, want)
		}
	}
	return enc
}

// grid builds a connected triangle list in row order, the layout most exports have.
func grid(n int) ([]uint32, int) {
	var out []uint32
	w := n + 1
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			a, b := uint32(y*w+x), uint32((y+1)*w+x)
			out = append(out, a, b, a+1, a+1, b, b+1)
		}
	}
	return out, w * w
}

func TestIndexRoundTripGrid(t *testing.T) {
	indices, vertices := grid(64)
	enc := checkIndexRoundTrip(t, "grid", indices, vertices)
	if raw := len(indices) * 2; len(enc)*2 > raw {
		t.Fatalf("grid compressed to %d of %d bytes, want at most half", len(enc), raw)
	}
}

func TestIndexRoundTripSoup(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	const vertices = 400
	var indices []uint32
	for i := 0; i < 600; i++ {
		a, b, c := rng.Intn(vertices), rng.Intn(vertices), rng.Intn(vertices)
		indices = append(indices, uint32(a), uint32(b), uint32(c))
	}
	checkIndexRoundTrip(t, "soup", indices, vertices)
}

func TestIndexRoundTripShuffledGrid(t *testing.T) {
	indices, vertices := grid(30)
	rng := rand.New(rand.NewSource(5))
	tris := len(indices) / 3
	rng.Shuffle(tris, func(i, j int) {
		for k := 0; k < 3; k++ {
			indices[i*3+k], indices[j*3+k] = indices[j*3+k], indices[i*3+k]
		}
	})
	checkIndexRoundTrip(t, "shuffled", indices, vertices)
}

func TestIndexRoundTripLargeIndices(t *testing.T) {
	indices := []uint32{0, 1, 2, 100000, 2, 1, 4000000, 100000, 1, 0, 4000000, 2}
	checkIndexRoundTrip(t, "large", indices, 4000001)
}

func TestIndexKeepsWinding(t *testing.T) {
	// Rotation keeps the order of the corners around the triangle; a swap would flip it.
	indices, vertices := grid(8)
	enc, _ := EncodeIndexBuffer(indices, vertices)
	got, _ := DecodeIndexBuffer(enc, len(indices))
	for i := 0; i < len(indices); i += 3 {
		want := [3]uint32{indices[i], indices[i+1], indices[i+2]}
		have := [3]uint32{got[i], got[i+1], got[i+2]}
		if rotated(want) != rotated(have) {
			t.Fatalf("triangle %d changed winding: %v vs %v", i/3, have, want)
		}
	}
}

func TestIndexRejectsBadInput(t *testing.T) {
	if _, err := EncodeIndexBuffer([]uint32{0, 1}, 2); err == nil {
		t.Fatal("a partial triangle must fail")
	}
	if _, err := EncodeIndexBuffer([]uint32{0, 1, 5}, 3); err == nil {
		t.Fatal("an index past the vertex count must fail")
	}
	if _, err := DecodeIndexBuffer([]byte{0xd0}, 3); err == nil {
		t.Fatal("a bad header must fail")
	}
}

func TestFloatPositionsSurviveExactly(t *testing.T) {
	const count = 500
	data := make([]byte, count*12)
	for v := 0; v < count; v++ {
		for k, f := range []float64{math.Sin(float64(v) / 30), math.Cos(float64(v) / 40), float64(v) / 100} {
			bits := math.Float32bits(float32(f))
			data[v*12+k*4], data[v*12+k*4+1], data[v*12+k*4+2], data[v*12+k*4+3] = byte(bits), byte(bits>>8), byte(bits>>16), byte(bits>>24)
		}
	}
	enc, err := EncodeVertexBuffer(data, count, 12)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeVertexBuffer(enc, count, 12)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("float bytes differ: %v", err)
	}
}
