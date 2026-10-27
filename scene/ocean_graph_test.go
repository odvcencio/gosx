package scene

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"sort"
	"testing"
)

// graphValueParams are the value-source parameters of the Elio ocean graph
// (elio emit ts --set ocean --backend webgpu, Elio 7d88e46), excluding the
// vec4 array probePoints that the vessel fills at run time.
var graphValueParams = []string{
	"windSpeed", "fetch", "gamma", "spread", "swell", "windDirDeg", "swellDirDeg", "seed", "loopPeriod", "lambda",
	"timeScale", "driftX", "driftZ", "foamTau", "foamThreshold", "foamGain", "probeCount", "probeOriginX", "probeOriginZ",
	"wakeEnabled", "hullLength", "hullBeam",
}

func TestOceanGraphParamsNameExactlyTheGraphsValueParameters(t *testing.T) {
	ocean := normalizeOcean(&Ocean{Spectrum: &OceanSpectrum{}})
	var got []string
	for name := range ocean.GraphParams {
		got = append(got, name)
	}
	want := append([]string(nil), graphValueParams...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("graph params %v, want %v", got, want)
	}
	// Defaults of an empty spectrum are the graph's own defaults except lambda
	// (the Choppiness default 0.6) and the foam settings the table below owns.
	p := ocean.GraphParams
	for name, want := range map[string]float64{"windSpeed": 9, "fetch": 300, "gamma": 3.3, "spread": 8, "swell": 0, "windDirDeg": 0, "swellDirDeg": 0, "seed": 0,
		"loopPeriod": 1000, "lambda": 0.6, "timeScale": 1, "driftX": 0, "driftZ": 0, "probeCount": 0, "probeOriginX": 0, "probeOriginZ": 0} {
		if p[name] != want {
			t.Errorf("%s = %v, want %v", name, p[name], want)
		}
	}
	if ocean.GraphTween != 20 {
		t.Errorf("tween %v, want the graph's 20", ocean.GraphTween)
	}
}

func TestOceanGraphParamsAbsentWithoutASpectrum(t *testing.T) {
	ocean := normalizeOcean(&Ocean{Level: 2, GraphParams: map[string]float64{"windSpeed": 99}, GraphTween: 7})
	if ocean.GraphParams != nil || ocean.GraphTween != 0 {
		t.Fatalf("graph params without a spectrum: %v %v", ocean.GraphParams, ocean.GraphTween)
	}
	wire, _ := json.Marshal(ocean)
	if string(wire) != `{"level":2}` {
		t.Fatalf("wire changed: %s", wire)
	}
}

func TestOceanGraphParamsFollowThePresentationAndReplaceAuthoredValues(t *testing.T) {
	ocean := normalizeOcean(&Ocean{
		WindDirection: 30, Choppiness: 0.5,
		Spectrum:     &OceanSpectrum{WindSpeed: 12, Fetch: 100, PeakEnhancement: 2, Spread: 4, Swell: 0.3, SwellDirection: 400, Seed: 7},
		Presentation: &OceanPresentation{TimeScale: 2, WaterDriftX: 1.5, WaterDriftZ: -8, TransitionSeconds: 45},
		GraphParams:  map[string]float64{"windSpeed": 1},
	})
	p := ocean.GraphParams
	for name, want := range map[string]float64{"windSpeed": 12, "fetch": 100, "gamma": 2, "spread": 4, "swell": 0.3, "swellDirDeg": 40, "windDirDeg": 30, "seed": 7,
		"lambda": 0.5, "timeScale": 2, "driftX": 1.5, "driftZ": -8} {
		if math.Abs(p[name]-want) > 1e-9 {
			t.Errorf("%s = %v, want %v", name, p[name], want)
		}
	}
	if ocean.GraphTween != 45 {
		t.Errorf("tween %v, want 45", ocean.GraphTween)
	}
}

func TestOceanFoamThresholdRisesWithWindAndFollowsChoppiness(t *testing.T) {
	for _, u := range []float64{0.5, 3, 5} {
		if oceanFoamThreshold(u, 0.6) != 0 || oceanFoamThreshold(u, 1) != 0 {
			t.Errorf("%v m/s must carry no foam", u)
		}
	}
	if got := oceanFoamThreshold(40, 1); math.Abs(got-OceanFoam.MaxThreshold) > 1e-12 {
		t.Errorf("a gale at choppiness 1 = %v, want the maximum", got)
	}
	last := -1.0
	for u := 0.0; u <= 40; u += 0.5 {
		if got := oceanFoamThreshold(u, 1); got < last {
			t.Fatalf("threshold falls at %v m/s", u)
		} else {
			last = got
		}
	}
	// The same crest compression at a lower choppiness needs a higher threshold.
	if oceanFoamThreshold(15, 0.32) <= oceanFoamThreshold(15, 1) {
		t.Error("a low choppiness must raise the J threshold")
	}
}

func TestOceanGraphParamsGolden(t *testing.T) {
	type entry struct {
		Preset      string             `json:"preset"`
		GraphParams map[string]float64 `json:"graphParams"`
		GraphTween  float64            `json:"graphTween"`
	}
	var got []entry
	for _, name := range []string{"calm", "moderate", "rough", "storm"} {
		spectrum := OceanPreset(name)
		ocean := normalizeOcean(&Ocean{WindDirection: 20, Choppiness: 0.6, Spectrum: &spectrum, Presentation: &OceanPresentation{WaterDriftZ: 8.2}})
		got = append(got, entry{name, ocean.GraphParams, ocean.GraphTween})
	}
	wire, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	wire = append(wire, '\n')
	const path = "testdata/ocean-graph-params.json"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, wire, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(wire) {
		t.Fatalf("%s differs; run with UPDATE_GOLDEN=1 after reviewing the change\n%s", path, wire)
	}
}
