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
