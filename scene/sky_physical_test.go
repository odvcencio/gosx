package scene

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

func TestGradientSkyWireHasNoPhysicalKeys(t *testing.T) {
	props := Props{Environment: Environment{Sky: &Sky{Mode: "gradient", TopColor: "#123456"}}}
	wire, err := json.Marshal(props.SceneIR().Environment)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"sunDirection", "turbidity", "rayleigh", "mieCoefficient", "mieDirectionalG", "sunDiskRadius"} {
		if strings.Contains(string(wire), key) {
			t.Fatalf("gradient sky wire carries %q: %s", key, wire)
		}
	}
}

func TestPhysicalSkyLowersNormalizedAndFillsFallbackStops(t *testing.T) {
	props := Props{Environment: Environment{Sky: &Sky{
		Mode: " Physical ", SunDirection: Vector3{Y: 3, Z: -4}, Turbidity: 40, Rayleigh: -1, MieDirectionalG: 2, SunDiskRadius: 9,
	}}}
	ir := props.SceneIR()
	sky := ir.Environment.Sky
	if sky == nil || sky.Mode != "physical" {
		t.Fatalf("sky = %#v", sky)
	}
	if math.Abs(sky.SunDirection.Y-0.6) > 1e-12 || math.Abs(sky.SunDirection.Z+0.8) > 1e-12 {
		t.Fatalf("sun direction not normalized: %#v", sky.SunDirection)
	}
	if sky.Turbidity != 20 || sky.Rayleigh != 0 || sky.MieDirectionalG != 0.999 || sky.SunDiskRadius != 5 {
		t.Fatalf("parameters not clamped: %#v", sky)
	}
	for _, stop := range []string{sky.TopColor, sky.HorizonColor, sky.BottomColor} {
		if len(stop) != 7 || stop[0] != '#' {
			t.Fatalf("fallback stops not filled: %#v", sky)
		}
	}
	if !featureSet(collectFeatures(ir))[capability.FeatureSkyPhysical] {
		t.Fatalf("physical sky did not raise %s", capability.FeatureSkyPhysical)
	}
	authored := Props{Environment: Environment{Sky: &Sky{Mode: "physical", HorizonColor: "#010203"}}}
	if got := authored.SceneIR().Environment.Sky.HorizonColor; got != "#010203" {
		t.Fatalf("an authored stop was replaced: %s", got)
	}
}

func TestPhysicalSkyZeroSunDirectionUsesDefault(t *testing.T) {
	sky := normalizeSky(&Sky{Mode: "physical"})
	want := SunDirectionFromAngles(6, 0)
	if math.Abs(sky.SunDirection.Y-want.Y) > 1e-12 || math.Abs(sky.SunDirection.Z-want.Z) > 1e-12 {
		t.Fatalf("sun = %#v, want %#v", sky.SunDirection, want)
	}
}

func TestPhysicalSkyRadianceBehavesLikeDaylight(t *testing.T) {
	noon := Sky{Mode: "physical", SunDirection: SunDirectionFromAngles(60, 0)}
	r, _, b := noon.PhysicalRadiance(Vector3{Y: 1}, false)
	if !(b > r) {
		t.Fatalf("noon zenith should be blue: r=%v b=%v", r, b)
	}
	sunset := Sky{Mode: "physical", SunDirection: SunDirectionFromAngles(2, 0)}
	r, _, b = sunset.PhysicalRadiance(Vector3{Y: 0.03, Z: -1}, false)
	if !(r > b) {
		t.Fatalf("sunset horizon toward the sun should be red: r=%v b=%v", r, b)
	}
	awayR, _, _ := sunset.PhysicalRadiance(Vector3{Y: 0.03, Z: 1}, false)
	if !(r > awayR) {
		t.Fatalf("the horizon toward the sun should be brighter than away from it: %v <= %v", r, awayR)
	}
	sunDir := sunset.SunDirection
	withSun, _, _ := sunset.PhysicalRadiance(sunDir, true)
	without, _, _ := sunset.PhysicalRadiance(sunDir, false)
	if !(withSun > 3*without) {
		t.Fatalf("the sun disk should dominate: with=%v without=%v", withSun, without)
	}
	hidden := sunset
	hidden.SunDiskRadius = -1
	if got, _, _ := hidden.PhysicalRadiance(sunDir, true); got != without {
		t.Fatalf("a negative disk radius should hide the sun: %v vs %v", got, without)
	}
	for _, dir := range []Vector3{{Y: -1}, {X: 1}, {Z: -1, Y: -0.3}, {}} {
		r, g, b := noon.PhysicalRadiance(dir, true)
		for _, v := range []float64{r, g, b} {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				t.Fatalf("radiance along %#v is %v %v %v", dir, r, g, b)
			}
		}
	}
	bright := noon
	bright.Intensity = 2
	r1, _, _ := noon.PhysicalRadiance(Vector3{Y: 1}, false)
	r2, _, _ := bright.PhysicalRadiance(Vector3{Y: 1}, false)
	if math.Abs(r2-2*r1) > 1e-12 {
		t.Fatalf("intensity should scale linearly: %v vs %v", r2, 2*r1)
	}
}

// TestPhysicalSkyParamsGolden pins the per-frame parameter block that the
// browser packs (sceneSkyPhysicalParams in 16c-scene-shared-pbr.ts). The
// JavaScript test scene3d-sky-physical.test.js asserts the same numbers.
func TestPhysicalSkyParamsGolden(t *testing.T) {
	p := newPhysicalSkyParams(Sky{Mode: "physical", SunDirection: SunDirectionFromAngles(8, 20), Turbidity: 6, Rayleigh: 1.5, MieCoefficient: 0.004, MieDirectionalG: 0.85, SunDiskRadius: 0.6})
	got := []float64{p.betaR[0], p.betaR[1], p.betaR[2], p.sunE, p.betaM[0], p.betaM[1], p.betaM[2], p.sunFade, p.diskCos}
	want := []float64{8.706814e-06, 2.034437e-05, 4.539885e-05, 1.130223e+02, 3.833071e-06, 5.790884e-06, 8.497473e-06, 1, 9.999452e-01}
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-6*math.Abs(want[i]) {
			t.Fatalf("param %d = %.6e, want %.6e (all: %.6e)", i, got[i], want[i], got)
		}
	}
	c := p.radiance(normalizeSunDirection(Vector3{X: 0.2, Y: 0.1, Z: -1}))
	for _, v := range c {
		if !(v > 0) {
			t.Fatalf("radiance = %v", c)
		}
	}
}
