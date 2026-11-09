package scene

import (
	"math"
	"strings"
	"testing"
)

func TestWindDefaultsAndDirection(t *testing.T) {
	if (*Wind)(nil).SceneIR() != nil {
		t.Fatal("nil wind lowered to a record")
	}
	ir := (&Wind{}).SceneIR()
	if ir.Speed != 4 || ir.Gust != 0.35 || ir.GustScale != 24 || ir.GustSpeed != 4 || ir.Frequency != 1.3 {
		t.Fatalf("defaults = %+v", ir)
	}
	if math.Abs(ir.DirectionX) > 1e-12 || math.Abs(ir.DirectionZ-1) > 1e-12 {
		t.Fatalf("direction 0 should blow toward +Z, got (%v, %v)", ir.DirectionX, ir.DirectionZ)
	}
	east := (&Wind{Direction: 90, Speed: 50}).SceneIR()
	if math.Abs(east.DirectionX-1) > 1e-12 || math.Abs(east.DirectionZ) > 1e-12 || east.Speed != 40 {
		t.Fatalf("direction 90 at speed 50 = %+v, want +X at the 40 m/s cap", east)
	}
}

func TestWindSampleGustsTravelDownwind(t *testing.T) {
	w := &Wind{Direction: 30, Speed: 6, Gust: 0.4, GustScale: 20, GustSpeed: 5}
	ir := w.SceneIR()
	low, high := math.Inf(1), math.Inf(-1)
	for i := 0; i < 400; i++ {
		x, z := float64(i%20)*3.1, float64(i/20)*2.7
		v := w.Sample(x, z, 1.5)
		speed := math.Hypot(v.X, v.Z)
		low, high = math.Min(low, speed), math.Max(high, speed)
		if math.Abs(v.X*ir.DirectionZ-v.Z*ir.DirectionX) > 1e-9 {
			t.Fatalf("wind at (%v, %v) is not along the direction: %+v", x, z, v)
		}
	}
	if low < 6*0.6-1e-9 || high > 6*1.4+1e-9 || high-low < 2 {
		t.Fatalf("gusts span %v..%v m/s, want inside 3.6..8.4 and visibly varying", low, high)
	}
	// A front seen at t is seen dt later GustSpeed*dt further downwind.
	a := w.Sample(10, 4, 2)
	b := w.Sample(10+5*0.5*ir.DirectionX, 4+5*0.5*ir.DirectionZ, 2.5)
	if math.Abs(math.Hypot(a.X, a.Z)-math.Hypot(b.X, b.Z)) > 0.05 {
		t.Fatalf("front did not travel downwind: %v vs %v", a, b)
	}
}

func TestWindLowersIntoEnvironment(t *testing.T) {
	props := Props{Environment: Environment{Wind: &Wind{Speed: 8}}}
	ir := props.SceneIR()
	if ir.Environment.Wind == nil || ir.Environment.IsZero() {
		t.Fatal("wind did not lower")
	}
	data, err := ir.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"wind":{"directionX":0,"directionZ":1,"speed":8`) {
		t.Fatalf("wind wire missing: %s", data)
	}
}
