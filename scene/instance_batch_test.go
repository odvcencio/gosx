package scene

import (
	"math"
	"testing"
)

func TestPlaceInstanceTurnsTiltsAndScales(t *testing.T) {
	var m [16]float32
	PlaceInstance(m[:], Vector3{X: 1, Y: 2, Z: 3}, Vector3{X: 2, Y: 1, Z: 4}, math.Pi/2, 0)
	// A quarter turn about Y sends +X to -Z and +Z to +X.
	want := [16]float32{0, 0, -2, 0, 0, 1, 0, 0, 4, 0, 0, 0, 1, 2, 3, 1}
	for i := range m {
		if math.Abs(float64(m[i]-want[i])) > 1e-6 {
			t.Fatalf("matrix = %v, want %v", m, want)
		}
	}
}

func TestInstanceBatchGrowsTruncatesAndColors(t *testing.T) {
	b := NewInstanceBatch("arena", "marks", InstanceStreamTransformColor)
	b.Place(2, Vector3{}, Vector3{X: 1, Y: 1, Z: 1}, 0, 0)
	b.Color(2, [4]float64{1, .5, 0, 1})
	if b.Len() != 3 || b.data[2*20+16] != 1 || b.data[2*20+17] != .5 {
		t.Fatalf("len %d, color %v", b.Len(), b.data[2*20+16:2*20+20])
	}
	b.Truncate(1)
	if b.Len() != 1 {
		t.Fatalf("len after Truncate(1) = %d", b.Len())
	}
	if ok, _ := b.Send(); ok {
		t.Fatal("Send reported success with no renderer")
	}
}
