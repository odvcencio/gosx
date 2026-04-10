package field

import "testing"

func TestQuantizeDecompressRoundTrip(t *testing.T) {
	f := FromFunc([3]int{16, 16, 16}, 1,
		AABB{Min: [3]float32{0, 0, 0}, Max: [3]float32{1, 1, 1}},
		func(x, y, z float32) []float32 { return []float32{x} },
	)
	q := f.Quantize(QuantizeOptions{BitWidth: 8})
	if q.BitWidth != 8 {
		t.Errorf("bitwidth = %d, want 8", q.BitWidth)
	}
	if q.Resolution != f.Resolution {
		t.Errorf("resolution mismatch: %v vs %v", q.Resolution, f.Resolution)
	}
	if len(q.Mins) != 1 || len(q.Maxs) != 1 {
		t.Errorf("mins/maxs len: %d/%d, want 1/1", len(q.Mins), len(q.Maxs))
	}
	round := q.Decompress()
	if round.Resolution != f.Resolution {
		t.Fatalf("round-trip resolution mismatch")
	}
	for i := range round.Data {
		diff := round.Data[i] - f.Data[i]
		if diff < -0.01 || diff > 0.01 {
			t.Errorf("data[%d] err = %f", i, diff)
			break
		}
	}
}

func TestQuantizeWireSize(t *testing.T) {
	f := New([3]int{32, 32, 32}, 3, AABB{Max: [3]float32{1, 1, 1}})
	q := f.Quantize(QuantizeOptions{BitWidth: 6})
	want := 32 * 32 * 32 * 3 * 6 / 8
	if got := len(q.Packed); got != want {
		t.Errorf("packed size = %d, want %d", got, want)
	}
}

func TestDeltaEncodingRoundTrip(t *testing.T) {
	prev := FromFunc([3]int{8, 8, 8}, 1,
		AABB{Max: [3]float32{1, 1, 1}},
		func(x, y, z float32) []float32 { return []float32{x} },
	)
	curr := FromFunc([3]int{8, 8, 8}, 1,
		AABB{Max: [3]float32{1, 1, 1}},
		func(x, y, z float32) []float32 { return []float32{x + 0.01*y} },
	)
	q := curr.Quantize(QuantizeOptions{BitWidth: 8, DeltaAgainst: prev})
	if !q.IsDelta {
		t.Fatal("expected IsDelta == true")
	}
	round := ApplyDelta(prev, q)
	for i := range round.Data {
		diff := round.Data[i] - curr.Data[i]
		if diff < -0.01 || diff > 0.01 {
			t.Errorf("data[%d] err = %f", i, diff)
			break
		}
	}
}
