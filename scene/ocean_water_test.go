package scene

import "testing"

func TestOceanWaterAdvance(t *testing.T) {
	f := OceanWaterFrame{X: 1, Z: 2, Time: 3, TimeScale: 2, WaterDriftX: 4, WaterDriftZ: -8, Spectral: true, Seq: 9}
	got := f.Advance(0.5)
	if got.X != 3 || got.Z != -2 || got.Time != 4 || got.Seq != 9 || !got.Spectral {
		t.Fatalf("advance: %+v", got)
	}
	for _, age := range []float64{0, -1} {
		if f.Advance(age) != f {
			t.Fatalf("an age of %v changed the frame", age)
		}
	}
	if nan := zeroOverZero(); f.Advance(nan) != f {
		t.Fatal("a NaN age changed the frame")
	}
	wx, wz := f.ToWater(10, 20)
	if x, z := f.FromWater(wx, wz); wx != 9 || wz != 18 || x != 10 || z != 20 {
		t.Fatalf("round trip: %v %v -> %v %v", wx, wz, x, z)
	}
}

func zeroOverZero() float64 {
	var z float64
	return z / z
}

// A frame with an offset and a time makes SampleWater answer from the water the shader draws: the same
// numbers as a query at the water coordinates and the wave time, moved back to world coordinates. Without
// the frame the sample is elsewhere in the waves, which is the bug this API closes.
func TestOceanQuerySampleWaterFollowsTheFrame(t *testing.T) {
	spectrum := OceanPreset("rough")
	q := NewOceanQuery(Ocean{Level: 0, Spectrum: &spectrum}, "high", nil)
	f := OceanWaterFrame{X: 31.5, Z: -12.25, Time: 17.75, Spectral: true}
	x, z := 120.0, -40.0
	want := q.Sample(x-f.X, z-f.Z, f.Time)
	got := q.SampleWater(f, x, z)
	if got.Position.Y != want.Position.Y || got.Normal != want.Normal || got.Velocity != want.Velocity {
		t.Fatalf("height or normal differ from the water-frame query: %+v vs %+v", got, want)
	}
	if got.Position.X != want.Position.X+f.X || got.Position.Z != want.Position.Z+f.Z {
		t.Fatalf("position not moved back to the world frame: %+v", got.Position)
	}
	if got.Parameter.X != want.Parameter.X+f.X || got.Parameter.Z != want.Parameter.Z+f.Z {
		t.Fatalf("parameter not moved back: %+v", got.Parameter)
	}
	naive := q.Sample(x, z, 0)
	if naive.Position.Y == got.Position.Y {
		t.Fatal("the unframed sample equals the framed one: the test does not tell them apart")
	}
	zero := q.SampleWater(OceanWaterFrame{Time: 5}, x, z)
	if zero != q.Sample(x, z, 5) {
		t.Fatal("a frame with no offset must equal the plain query")
	}
}

func TestOceanQuerySampleWaterDoesNotAllocate(t *testing.T) {
	spectrum := OceanPreset("rough")
	q := NewOceanQuery(Ocean{Spectrum: &spectrum}, "high", nil)
	f := OceanWaterFrame{X: 3, Z: 4, Time: 5}
	var sink OceanSample
	if n := testing.AllocsPerRun(50, func() { sink = q.SampleWater(f.Advance(0.016), 10, 20) }); n != 0 {
		t.Fatalf("SampleWater allocates %v times per call", n)
	}
	_ = sink
}
