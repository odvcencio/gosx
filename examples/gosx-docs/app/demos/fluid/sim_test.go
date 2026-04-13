package fluid

import (
	"math"
	"testing"
	"time"

	"github.com/odvcencio/gosx/field"
	"github.com/odvcencio/gosx/hub"
)

func TestComputeFrameShape(t *testing.T) {
	s := &Sim{hub: hub.New("fluid-test"), startTime: time.Now()}
	f := s.computeFrame(0)

	if f.Resolution != [3]int{gridN, gridN, gridN} {
		t.Errorf("Resolution = %v; want [%d %d %d]", f.Resolution, gridN, gridN, gridN)
	}
	if f.Components != 3 {
		t.Errorf("Components = %d; want 3", f.Components)
	}
	wantBounds := field.AABB{
		Min: [3]float32{boundMin, boundMin, boundMin},
		Max: [3]float32{boundMax, boundMax, boundMax},
	}
	if f.Bounds != wantBounds {
		t.Errorf("Bounds = %v; want %v", f.Bounds, wantBounds)
	}
	wantLen := gridN * gridN * gridN * 3
	if len(f.Data) != wantLen {
		t.Errorf("len(Data) = %d; want %d", len(f.Data), wantLen)
	}
}

func TestComputeFrameDeterministicAtT0(t *testing.T) {
	s := &Sim{hub: hub.New("fluid-test"), startTime: time.Now()}
	f1 := s.computeFrame(0)
	f2 := s.computeFrame(0)

	if len(f1.Data) != len(f2.Data) {
		t.Fatalf("Data lengths differ: %d vs %d", len(f1.Data), len(f2.Data))
	}
	for i, v := range f1.Data {
		if v != f2.Data[i] {
			t.Fatalf("Data[%d] differs: %v vs %v (not deterministic)", i, v, f2.Data[i])
		}
	}
}

func TestComputeFrameIsSmooth(t *testing.T) {
	s := &Sim{hub: hub.New("fluid-test"), startTime: time.Now()}
	f := s.computeFrame(1.5)

	// For neighboring voxels (i, j, k) and (i+1, j, k), the analytical function
	// is smooth so component differences should be much less than 2.0.
	const epsilon = 1.5
	res := gridN
	comp := 3
	for k := 0; k < res; k++ {
		for j := 0; j < res; j++ {
			for i := 0; i < res-1; i++ {
				idx0 := (k*res*res + j*res + i) * comp
				idx1 := (k*res*res + j*res + (i + 1)) * comp
				for c := 0; c < comp; c++ {
					diff := math.Abs(float64(f.Data[idx1+c] - f.Data[idx0+c]))
					if diff > epsilon {
						t.Errorf("voxel (%d,%d,%d) comp %d differs from neighbor by %f (want < %f)",
							i, j, k, c, diff, epsilon)
					}
				}
			}
		}
	}
}

func TestComputeFrameMagnitudeBounded(t *testing.T) {
	s := &Sim{hub: hub.New("fluid-test"), startTime: time.Now()}

	// Check at several time points to be thorough.
	for _, tt := range []float32{0, 0.5, 1.0, 3.14} {
		f := s.computeFrame(tt)
		for i, v := range f.Data {
			if v < -2.0 || v > 2.0 {
				t.Errorf("t=%v Data[%d] = %f; want in [-2, 2] (sin*cos product is bounded)", tt, i, v)
			}
		}
	}
}
