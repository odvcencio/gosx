package scene

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestSkyNightWireAndNormalization(t *testing.T) {
	plain := normalizeSky(&Sky{Mode: "physical"})
	wire, _ := json.Marshal(plain)
	if strings.Contains(string(wire), "night") {
		t.Fatalf("a sky without Night changed its wire: %s", wire)
	}
	sky := normalizeSky(&Sky{Mode: "physical", SunDirection: SunDirectionFromAngles(-30, 90), Night: &SkyNight{
		StarDensity: 3, StarBrightness: 9, StarSeed: 7, MoonPhase: 1.25, MoonRadius: 40, MoonIntensity: -1,
	}})
	n := sky.Night
	if n == nil || n.StarDensity != 1 || n.StarBrightness != 4 || n.StarSeed != 7 || math.Abs(n.MoonPhase-0.25) > 1e-12 || n.MoonRadius != 5 || n.MoonIntensity != -1 {
		t.Fatalf("clamps: %#v", n)
	}
	// Unset direction: 40 degrees up, opposite the sun's bearing (sun due east, so the moon is due west).
	d := n.MoonDirection
	if math.Abs(d.Y-math.Sin(40*math.Pi/180)) > 1e-9 || d.X >= 0 || math.Abs(math.Hypot(d.X, d.Z)-math.Cos(40*math.Pi/180)) > 1e-9 {
		t.Fatalf("default moon direction: %#v", d)
	}
	wire, _ = json.Marshal(sky)
	if !strings.Contains(string(wire), `"night":{`) || !strings.Contains(string(wire), `"moonDirection":{`) {
		t.Fatalf("wire: %s", wire)
	}
	custom := normalizeSky(&Sky{Mode: "physical", Night: &SkyNight{MoonDirection: Vector3{X: 0, Y: 3, Z: -4}, MoonRadius: -1}}).Night
	if math.Abs(custom.MoonDirection.Y-0.6) > 1e-12 || math.Abs(custom.MoonDirection.Z+0.8) > 1e-12 || custom.MoonRadius != -1 {
		t.Fatalf("custom: %#v", custom)
	}
	if normalizeSky(&Sky{Mode: "gradient", TopColor: "#123456", Night: &SkyNight{}}).Night != nil {
		t.Fatal("night layer survived a gradient sky")
	}
}

func TestSkyNightIllumination(t *testing.T) {
	for phase, want := range map[float64]float64{0: 0, 0.25: 0.5, 0.5: 1, 0.75: 0.5, 1: 0, -0.5: 1} {
		if got := (SkyNight{MoonPhase: phase}).MoonIllumination(); math.Abs(got-want) > 1e-12 {
			t.Errorf("phase %v: illumination %v, want %v", phase, got, want)
		}
	}
	if got := (SkyNight{MoonPhase: math.NaN()}).MoonIllumination(); got != 0 {
		t.Errorf("NaN phase: %v", got)
	}
}
