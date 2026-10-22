package scene

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

func TestNilOceanKeepsWireByteIdentical(t *testing.T) {
	props := Props{Environment: Environment{AmbientColor: "#ffffff", AmbientIntensity: 1}}
	wire, err := json.Marshal(props.SceneIR().Environment)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(wire), `{"ambientColor":"#ffffff","ambientIntensity":1}`; got != want {
		t.Fatalf("nil Ocean changed the existing environment wire: got %s, want %s", got, want)
	}
}

func TestOceanLowersTrimmedAndClampedWithoutApplyingDefaults(t *testing.T) {
	props := Props{Environment: Environment{Ocean: &Ocean{
		Level: 3.5, WindDirection: -725,
		WaveHeight: 9, WaveLength: 1, Choppiness: -1, Speed: 8,
		DeepColor: " #123456 ", ShallowColor: "  ", ScatterColor: " #abcdef ", FoamColor: " #eeeeee ",
		Clarity: 0.1, Roughness: 0.8, Foam: 2, Surf: -1, Extent: 25000,
		Bathymetry: &OceanBathymetry{Src: " bathy.png ", MinX: -10, MinZ: -20, MaxX: 10, MaxZ: 20, MinHeight: -5, MaxHeight: 2},
	}}}
	ocean := props.SceneIR().Environment.Ocean
	if ocean == nil {
		t.Fatal("expected Ocean after lowering")
	}
	if ocean.Level != 3.5 || ocean.WindDirection != 355 {
		t.Fatalf("level/wind direction = %v/%v", ocean.Level, ocean.WindDirection)
	}
	if ocean.WaveHeight != 6 || ocean.WaveLength != 2 || ocean.Choppiness != 0 || ocean.Speed != 4 ||
		ocean.Clarity != 0.5 || ocean.Roughness != 0.5 || ocean.Foam != 1 || ocean.Surf != 0 || ocean.Extent != 20000 {
		t.Fatalf("ocean parameters were not clamped: %#v", ocean)
	}
	if ocean.DeepColor != "#123456" || ocean.ShallowColor != "" || ocean.ScatterColor != "#abcdef" || ocean.FoamColor != "#eeeeee" {
		t.Fatalf("ocean colors were not trimmed: %#v", ocean)
	}
	if ocean.Bathymetry == nil || ocean.Bathymetry.Src != "bathy.png" {
		t.Fatalf("valid bathymetry did not survive trimming: %#v", ocean.Bathymetry)
	}

	defaults := props
	defaults.Environment.Ocean = &Ocean{}
	got := defaults.SceneIR().Environment.Ocean
	if got.WaveHeight != 0 || got.WaveLength != 0 || got.Speed != 0 || got.Clarity != 0 || got.Roughness != 0 || got.Extent != 0 {
		t.Fatalf("Go lowering must preserve zero default markers, got %#v", got)
	}
	canonical := props.CanonicalIR().Environment.Ocean
	if canonical == nil || canonical.WaveHeight != 6 || canonical.WindDirection != 355 {
		t.Fatalf("Ocean did not reach canonical IR: %#v", canonical)
	}
}

func TestOceanInvalidBathymetryIsDropped(t *testing.T) {
	invalid := []OceanBathymetry{
		{Src: " ", MinX: 0, MinZ: 0, MaxX: 1, MaxZ: 1, MinHeight: 0, MaxHeight: 1},
		{Src: "map", MinX: 1, MinZ: 0, MaxX: 1, MaxZ: 1, MinHeight: 0, MaxHeight: 1},
		{Src: "map", MinX: 0, MinZ: 1, MaxX: 1, MaxZ: 1, MinHeight: 0, MaxHeight: 1},
		{Src: "map", MinX: 0, MinZ: 0, MaxX: 1, MaxZ: 1, MinHeight: 2, MaxHeight: 1},
	}
	for i, bathymetry := range invalid {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			got := normalizeOcean(&Ocean{Bathymetry: &bathymetry})
			if got == nil || got.Bathymetry != nil {
				t.Fatalf("invalid bathymetry was not dropped: %#v", got)
			}
		})
	}
	if normalizeOcean(nil) != nil {
		t.Fatal("nil Ocean must normalize to nil")
	}
	if got := normalizeOcean(&Ocean{WindDirection: 360}); got.WindDirection != 0 || math.IsNaN(got.WindDirection) {
		t.Fatalf("360 degrees should wrap to zero, got %v", got.WindDirection)
	}
}

func TestOceanRaisesFeature(t *testing.T) {
	props := Props{Environment: Environment{Ocean: &Ocean{}}}
	if !featureSet(collectFeatures(props.SceneIR()))[capability.FeatureOcean] {
		t.Fatalf("authored ocean did not raise %s", capability.FeatureOcean)
	}
	if featureSet(collectFeatures((Props{}).SceneIR()))[capability.FeatureOcean] {
		t.Fatalf("an absent ocean raised %s", capability.FeatureOcean)
	}
}

func TestOceanBathymetryEncodingNormalizes(t *testing.T) {
	base := OceanBathymetry{Src: "h.png", MinX: -1, MinZ: -1, MaxX: 1, MaxZ: 1, MinHeight: -8, MaxHeight: 4}
	for input, want := range map[string]string{"": "", "linear": "", " Signed-Sqrt ": "signed-sqrt", "log": ""} {
		b := base
		b.Encoding = input
		if got := normalizeOcean(&Ocean{Bathymetry: &b}).Bathymetry.Encoding; got != want {
			t.Fatalf("encoding %q normalized to %q, want %q", input, got, want)
		}
	}
}

func TestOceanSpectrumNormalizesAndClamps(t *testing.T) {
	got := normalizeOcean(&Ocean{
		Masks: []string{" hull ", "", "dock", "hull"},
		Spectrum: &OceanSpectrum{
			WindSpeed: 90, Fetch: 0.1, PeakEnhancement: 9, Spread: 0.2, Swell: 3, SwellDirection: -90, Seed: 0,
		},
	})
	s := got.Spectrum
	if s == nil {
		t.Fatal("Spectrum was dropped")
	}
	if s.WindSpeed != 40 || s.Fetch != 1 || s.PeakEnhancement != 7 || s.Spread != 1 || s.Swell != 1 || s.SwellDirection != 270 {
		t.Fatalf("spectrum was not clamped: %#v", s)
	}
	if want := []string{"hull", "dock"}; len(got.Masks) != 2 || got.Masks[0] != want[0] || got.Masks[1] != want[1] {
		t.Fatalf("masks = %q, want %q", got.Masks, want)
	}
	zero := normalizeOcean(&Ocean{Spectrum: &OceanSpectrum{}}).Spectrum
	if zero.WindSpeed != 0 || zero.Fetch != 0 || zero.Cascades != nil {
		t.Fatalf("zero must stay the default marker: %#v", zero)
	}
	if normalizeOcean(&Ocean{}).Spectrum != nil {
		t.Fatal("an ocean without a spectrum must not gain one")
	}
}

func TestOceanSpectrumCascadesAreValidated(t *testing.T) {
	good := []OceanCascade{{Size: 1600, KMin: 0.0039, KMax: 0.12}, {Size: 250, KMin: 0.12, KMax: 9}}
	got := normalizeOcean(&Ocean{Spectrum: &OceanSpectrum{Cascades: good}}).Spectrum.Cascades
	if len(got) != 2 || got[1].KMax >= math.Pi*64/250 {
		t.Fatalf("KMax must sit below pi*64/Size: %#v", got)
	}
	bad := []OceanCascade{{Size: 0, KMin: 0, KMax: 1}}
	if c := normalizeOcean(&Ocean{Spectrum: &OceanSpectrum{Cascades: bad}}).Spectrum.Cascades; c != nil {
		t.Fatalf("an invalid cascade list must fall back to the defaults, got %#v", c)
	}
	if c := normalizeOcean(&Ocean{Spectrum: &OceanSpectrum{Cascades: make([]OceanCascade, 5)}}).Spectrum.Cascades; c != nil {
		t.Fatalf("more than four cascades must fall back to the defaults, got %#v", c)
	}
}

func TestOceanPresets(t *testing.T) {
	var last float64
	for _, name := range []string{"calm", "moderate", "rough", "storm"} {
		p := OceanPreset(name)
		if p.WindSpeed <= last || p.Fetch <= 0 || p.PeakEnhancement < 1 || p.Spread < 1 {
			t.Fatalf("preset %q: %#v", name, p)
		}
		last = p.WindSpeed
		if n := normalizeOcean(&Ocean{Spectrum: &p}).Spectrum; !reflect.DeepEqual(*n, p) {
			t.Fatalf("preset %q changes under normalization: %#v vs %#v", name, *n, p)
		}
	}
	if !reflect.DeepEqual(OceanPreset("Moderate"), OceanPreset("moderate")) || !reflect.DeepEqual(OceanPreset("nope"), OceanPreset("moderate")) {
		t.Fatal("names are case-insensitive and unknown names give moderate")
	}
}

func TestOceanSpectrumWire(t *testing.T) {
	spectrum := OceanPreset("calm")
	spectrum.Seed = 0
	wire, err := json.Marshal(Ocean{Spectrum: &spectrum, Masks: []string{"hull"}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"spectrum":{"windSpeed":3,"fetch":30,"peakEnhancement":3.3,"spread":16,"swell":0.1,"seed":0},"masks":["hull"]}`
	if string(wire) != want {
		t.Fatalf("wire = %s\nwant   %s", wire, want)
	}
}
