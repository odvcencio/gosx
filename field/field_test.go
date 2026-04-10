package field

import (
	"testing"
)

func TestNewField(t *testing.T) {
	f := New([3]int{4, 4, 4}, 1, AABB{Min: [3]float32{0, 0, 0}, Max: [3]float32{1, 1, 1}})
	if f.Resolution != [3]int{4, 4, 4} {
		t.Errorf("resolution = %v, want [4 4 4]", f.Resolution)
	}
	if f.Components != 1 {
		t.Errorf("components = %d, want 1", f.Components)
	}
	if len(f.Data) != 4*4*4*1 {
		t.Errorf("data len = %d, want 64", len(f.Data))
	}
}

func TestFromFuncScalar(t *testing.T) {
	f := FromFunc([3]int{2, 2, 2}, 1,
		AABB{Min: [3]float32{0, 0, 0}, Max: [3]float32{1, 1, 1}},
		func(x, y, z float32) []float32 { return []float32{x + y + z} },
	)
	if len(f.Data) != 8 {
		t.Fatalf("data len = %d, want 8", len(f.Data))
	}
	// Voxel (0,0,0) center is at world (0.25, 0.25, 0.25) → 0.75
	if got := f.Data[0]; got < 0.74 || got > 0.76 {
		t.Errorf("data[0] = %f, want ~0.75", got)
	}
}
