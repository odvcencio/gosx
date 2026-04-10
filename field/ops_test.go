package field

import (
	"math"
	"testing"
)

func TestAdvectConstantVelocity(t *testing.T) {
	v := FromFunc([3]int{8, 8, 8}, 3,
		AABB{Min: [3]float32{0, 0, 0}, Max: [3]float32{1, 1, 1}},
		func(x, y, z float32) []float32 { return []float32{1, 0, 0} },
	)
	particles := []float32{0.5, 0.5, 0.5}
	Advect(v, particles, 0.1)
	if particles[0] < 0.55 || particles[0] > 0.65 {
		t.Errorf("particle x = %f, want ~0.6", particles[0])
	}
	if math.Abs(float64(particles[1]-0.5)) > 0.01 {
		t.Errorf("particle y drifted: %f", particles[1])
	}
}

func TestGradientLinear(t *testing.T) {
	f := FromFunc([3]int{16, 16, 16}, 1,
		AABB{Min: [3]float32{0, 0, 0}, Max: [3]float32{1, 1, 1}},
		func(x, y, z float32) []float32 { return []float32{x} },
	)
	g := Gradient(f)
	if g.Components != 3 {
		t.Fatalf("gradient components = %d, want 3", g.Components)
	}
	got := g.SampleVec3(0.5, 0.5, 0.5)
	if got[0] < 0.9 || got[0] > 1.1 {
		t.Errorf("grad x = %f, want ~1.0", got[0])
	}
	if got[1] < -0.1 || got[1] > 0.1 {
		t.Errorf("grad y = %f, want ~0", got[1])
	}
	if got[2] < -0.1 || got[2] > 0.1 {
		t.Errorf("grad z = %f, want ~0", got[2])
	}
}

func TestDivergenceConstant(t *testing.T) {
	v := FromFunc([3]int{8, 8, 8}, 3,
		AABB{Min: [3]float32{0, 0, 0}, Max: [3]float32{1, 1, 1}},
		func(x, y, z float32) []float32 { return []float32{1, 0, 0} },
	)
	d := Divergence(v)
	if d.Components != 1 {
		t.Fatalf("divergence components = %d, want 1", d.Components)
	}
	got := d.SampleScalar(0.5, 0.5, 0.5)
	if got < -0.01 || got > 0.01 {
		t.Errorf("div = %f, want ~0", got)
	}
}

func TestDivergenceLinearExpansion(t *testing.T) {
	v := FromFunc([3]int{16, 16, 16}, 3,
		AABB{Min: [3]float32{0, 0, 0}, Max: [3]float32{1, 1, 1}},
		func(x, y, z float32) []float32 { return []float32{x, 0, 0} },
	)
	d := Divergence(v)
	got := d.SampleScalar(0.5, 0.5, 0.5)
	if got < 0.9 || got > 1.1 {
		t.Errorf("div = %f, want ~1", got)
	}
}

func TestCurlRotation(t *testing.T) {
	v := FromFunc([3]int{16, 16, 16}, 3,
		AABB{Min: [3]float32{-1, -1, -1}, Max: [3]float32{1, 1, 1}},
		func(x, y, z float32) []float32 { return []float32{-y, x, 0} },
	)
	c := Curl(v)
	if c.Components != 3 {
		t.Fatalf("curl components = %d, want 3", c.Components)
	}
	got := c.SampleVec3(0, 0, 0)
	if got[0] < -0.1 || got[0] > 0.1 {
		t.Errorf("curl.x = %f, want ~0", got[0])
	}
	if got[1] < -0.1 || got[1] > 0.1 {
		t.Errorf("curl.y = %f, want ~0", got[1])
	}
	if got[2] < 1.9 || got[2] > 2.1 {
		t.Errorf("curl.z = %f, want ~2", got[2])
	}
}

func TestBlurSmoothsImpulse(t *testing.T) {
	f := New([3]int{8, 8, 8}, 1, AABB{Min: [3]float32{0, 0, 0}, Max: [3]float32{1, 1, 1}})
	mid := ((4*8+4)*8 + 4) * 1
	f.Data[mid] = 1.0
	b := Blur(f, 1.5)
	centerBefore := f.Data[mid]
	centerAfter := b.Data[mid]
	if centerAfter >= centerBefore {
		t.Errorf("blur center: before=%f after=%f, expected dimmer", centerBefore, centerAfter)
	}
	neighbor := ((4*8+4)*8 + 5) * 1
	if b.Data[neighbor] <= 0 {
		t.Errorf("blur neighbor still 0: %f", b.Data[neighbor])
	}
}

func TestResampleUpDown(t *testing.T) {
	f := FromFunc([3]int{16, 16, 16}, 1,
		AABB{Min: [3]float32{0, 0, 0}, Max: [3]float32{1, 1, 1}},
		func(x, y, z float32) []float32 { return []float32{x} },
	)
	low := Resample(f, [3]int{4, 4, 4})
	if low.Resolution != [3]int{4, 4, 4} {
		t.Fatalf("resolution = %v, want [4 4 4]", low.Resolution)
	}
	high := Resample(low, [3]int{16, 16, 16})
	got := high.SampleScalar(0.5, 0.5, 0.5)
	if got < 0.4 || got > 0.6 {
		t.Errorf("resampled center = %f, want ~0.5", got)
	}
}
