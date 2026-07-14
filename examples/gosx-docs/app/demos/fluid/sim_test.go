package fluid

import (
	"math"
	"testing"
	"time"

	"m31labs.dev/gosx/field"
	"m31labs.dev/gosx/hub"
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

// TestQuantizeKeyframeRoundTripBounded locks in that 6-bit quantization at the
// current grid size still reconstructs values within one quantization step of
// the original, matching what the client's decodeQuantized does on the wire.
func TestQuantizeKeyframeRoundTripBounded(t *testing.T) {
	s := &Sim{hub: hub.New("fluid-test"), startTime: time.Now()}
	f := s.computeFrame(0.75)

	q, err := f.QuantizeChecked(field.QuantizeOptions{BitWidth: bitWidth})
	if err != nil {
		t.Fatalf("QuantizeChecked: %v", err)
	}
	if q.IsDelta {
		t.Fatal("expected a keyframe (IsDelta = false) when DeltaAgainst is nil")
	}
	decoded, err := q.DecompressChecked()
	if err != nil {
		t.Fatalf("DecompressChecked: %v", err)
	}
	if len(decoded.Data) != len(f.Data) {
		t.Fatalf("decoded length = %d; want %d", len(decoded.Data), len(f.Data))
	}

	// Values are bounded in [-2, 2] (see TestComputeFrameMagnitudeBounded), so
	// a single component's quantized range spans at most 4.0 across
	// (1<<bitWidth)-1 levels; max error is one bin width.
	levels := float64((1 << bitWidth) - 1)
	maxStep := 4.0 / levels
	for i, want := range f.Data {
		got := decoded.Data[i]
		diff := math.Abs(float64(got) - float64(want))
		if diff > maxStep {
			t.Errorf("Data[%d]: decoded %v, want ~%v (diff %v > maxStep %v)", i, got, want, diff, maxStep)
		}
	}
}

// TestQuantizeDeltaRoundTrip mirrors the client's delta-application path
// (ApplyDelta / the onHubEvent accumulation loop) for a single tick-to-tick
// step, and checks the reconstructed field matches the true next frame.
func TestQuantizeDeltaRoundTrip(t *testing.T) {
	s := &Sim{hub: hub.New("fluid-test"), startTime: time.Now()}
	prev := s.computeFrame(1.0)
	next := s.computeFrame(1.0 + 1.0/tickRateHz) // one tick later at tickRateHz

	q, err := next.QuantizeChecked(field.QuantizeOptions{BitWidth: bitWidth, DeltaAgainst: prev})
	if err != nil {
		t.Fatalf("QuantizeChecked delta: %v", err)
	}
	if !q.IsDelta {
		t.Fatal("expected IsDelta = true when DeltaAgainst is set")
	}

	reconstructed, err := field.ApplyDeltaChecked(prev, q)
	if err != nil {
		t.Fatalf("ApplyDeltaChecked: %v", err)
	}

	// Per-tick swirl deltas are small; the adaptive per-component min/max
	// means the fixed 6-bit budget concentrates on that small range, so error
	// should be much tighter than the full-range keyframe case above.
	const maxDeltaError = 0.05
	for i, want := range next.Data {
		got := reconstructed.Data[i]
		diff := math.Abs(float64(got) - float64(want))
		if diff > maxDeltaError {
			t.Errorf("Data[%d]: reconstructed %v, want ~%v (diff %v > %v)", i, got, want, diff, maxDeltaError)
		}
	}
}

// TestQuantizedWireSizeStaysTiny locks in the demo's core honesty claim: even
// as gridN grows, the packed wire payload stays a small fraction of the raw
// float32 field it represents.
func TestQuantizedWireSizeStaysTiny(t *testing.T) {
	s := &Sim{hub: hub.New("fluid-test"), startTime: time.Now()}
	f := s.computeFrame(0)

	q, err := f.QuantizeChecked(field.QuantizeOptions{BitWidth: bitWidth})
	if err != nil {
		t.Fatalf("QuantizeChecked: %v", err)
	}

	rawBytes := gridN * gridN * gridN * 3 * 4 // float32 per component, uncompressed
	wireBytes := q.WireSize()
	if wireBytes >= rawBytes {
		t.Fatalf("WireSize() = %d bytes; want < raw float32 size %d bytes", wireBytes, rawBytes)
	}
	// bitWidth=6 vs float32's 32 bits is a fixed ~5.3x ratio; assert a >=4x
	// floor so a future bitWidth/grid change that erodes the "tiny wire"
	// story fails loudly here instead of silently going stale in the HUD.
	ratio := float64(rawBytes) / float64(wireBytes)
	if ratio < 4.0 {
		t.Errorf("compression ratio = %.2fx; want >= 4x to match the 6-bit-vs-32-bit claim", ratio)
	}
}
