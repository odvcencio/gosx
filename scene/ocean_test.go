package scene

import (
	"encoding/json"
	"math"
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
