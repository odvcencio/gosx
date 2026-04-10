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

func TestSampleScalarLinear(t *testing.T) {
	// Field with f(x,y,z) = x. Sampling should reproduce x within trilinear precision.
	f := FromFunc([3]int{8, 8, 8}, 1,
		AABB{Min: [3]float32{0, 0, 0}, Max: [3]float32{1, 1, 1}},
		func(x, y, z float32) []float32 { return []float32{x} },
	)
	cases := []struct {
		x, y, z, want float32
	}{
		{0.5, 0.5, 0.5, 0.5},
		{0.25, 0.5, 0.5, 0.25},
		{0.75, 0.1, 0.9, 0.75},
	}
	for _, c := range cases {
		got := f.SampleScalar(c.x, c.y, c.z)
		if diff := got - c.want; diff < -0.05 || diff > 0.05 {
			t.Errorf("SampleScalar(%v,%v,%v) = %f, want ~%f", c.x, c.y, c.z, got, c.want)
		}
	}
}

func TestSampleScalarEdgeClamp(t *testing.T) {
	f := FromFunc([3]int{4, 4, 4}, 1,
		AABB{Min: [3]float32{0, 0, 0}, Max: [3]float32{1, 1, 1}},
		func(x, y, z float32) []float32 { return []float32{1.0} },
	)
	// Out-of-bounds samples should clamp, not panic, and return ~1.0.
	if got := f.SampleScalar(-5, -5, -5); got < 0.99 || got > 1.01 {
		t.Errorf("clamped low = %f, want ~1.0", got)
	}
	if got := f.SampleScalar(99, 99, 99); got < 0.99 || got > 1.01 {
		t.Errorf("clamped high = %f, want ~1.0", got)
	}
}
