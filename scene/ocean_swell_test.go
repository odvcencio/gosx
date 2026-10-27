package scene

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"
)

type swellGoldenBin struct {
	Cascade, IX, IZ            int
	K, Omega, Amplitude, Phase float64
}

type swellGoldenCase struct {
	Preset struct {
		Name            string
		WindSpeed       float64
		Fetch           float64
		PeakEnhancement float64
		Spread, Swell   float64
		SwellDirection  float64
		Seed            uint32
		WindDirection   float64
	}
	Bins []swellGoldenBin
}

func loadSwellGolden(t *testing.T) []swellGoldenCase {
	t.Helper()
	data, err := os.ReadFile("testdata/ocean-swell-bins.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []swellGoldenCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 4 {
		t.Fatalf("golden has %d presets, want 4", len(cases))
	}
	return cases
}

func relErr(got, want float64) float64 {
	return math.Abs(got-want) / math.Max(math.Abs(want), 1e-300)
}

// TestOceanSwellWavesMatchElio compares OceanSwellWaves with the bins Elio's
// interpreter produced from ocean_spectrum (swell-bins.json, written by Elio's
// TestOceanSwellBinsGolden). Same bins, same order, relative error at most 1e-9.
func TestOceanSwellWavesMatchElio(t *testing.T) {
	var worst float64
	for _, c := range loadSwellGolden(t) {
		p := c.Preset
		want := OceanPreset(p.Name)
		want.SwellDirection = p.SwellDirection
		spectrum := OceanSpectrum{
			WindSpeed: p.WindSpeed, Fetch: p.Fetch, PeakEnhancement: p.PeakEnhancement, Spread: p.Spread,
			Swell: p.Swell, SwellDirection: p.SwellDirection, Seed: p.Seed,
		}
		if !reflect.DeepEqual(spectrum, want) {
			t.Fatalf("preset %q = %#v differs from the values Elio's golden hard-codes, %#v", p.Name, want, spectrum)
		}
		waves := OceanSwellWaves(spectrum, p.WindDirection, DefaultOceanSwellCount)
		if len(waves) != len(c.Bins) {
			t.Fatalf("%s: %d waves, golden has %d", p.Name, len(waves), len(c.Bins))
		}
		for i, b := range c.Bins {
			w := waves[i]
			n := math.Hypot(float64(b.IX), float64(b.IZ))
			if math.Abs(w.DirX-float64(b.IX)/n) > 1e-15 || math.Abs(w.DirZ-float64(b.IZ)/n) > 1e-15 {
				t.Fatalf("%s wave %d: direction (%g, %g), golden bin (%d, %d)", p.Name, i, w.DirX, w.DirZ, b.IX, b.IZ)
			}
			dphi := math.Remainder(w.Phase-b.Phase, 2*math.Pi)
			errs := []float64{relErr(w.K, b.K), relErr(w.Omega, b.Omega), relErr(w.Amplitude, b.Amplitude), math.Abs(dphi) / math.Max(1, math.Abs(b.Phase))}
			for _, e := range errs {
				worst = math.Max(worst, e)
				if e > 1e-9 {
					t.Fatalf("%s wave %d (cascade %d bin %d,%d): k/omega/amplitude/phase relative errors %v exceed 1e-9\n got %+v\nwant %+v", p.Name, i, b.Cascade, b.IX, b.IZ, errs, w, b)
				}
			}
		}
	}
	t.Logf("worst relative error against Elio over four presets: %.3g", worst)
}

func TestOceanSwellWavesAreDeterministicAndOrdered(t *testing.T) {
	s := OceanPreset("rough")
	a := OceanSwellWaves(s, 123, 0)
	b := OceanSwellWaves(s, 123, DefaultOceanSwellCount)
	if len(a) != DefaultOceanSwellCount || !reflect.DeepEqual(a, b) {
		t.Fatalf("default count %d differs from explicit %d", len(a), len(b))
	}
	for i := 1; i < len(a); i++ {
		if a[i].Amplitude > a[i-1].Amplitude {
			t.Fatalf("wave %d is stronger than wave %d", i, i-1)
		}
	}
	if got := OceanSwellWaves(s, 123, 5); !reflect.DeepEqual(got, a[:5]) {
		t.Fatal("a smaller k must be a prefix of the larger")
	}
	s.Seed = 2
	if reflect.DeepEqual(OceanSwellWaves(s, 123, 5), a[:5]) {
		t.Fatal("a different seed gave the same waves")
	}
}

func TestOceanSwellWavesTravelTowardWindDirection(t *testing.T) {
	s := OceanPreset("moderate")
	s.Swell = 0
	for _, deg := range []float64{0, 90, 215} {
		wx, wz := math.Sin(deg*math.Pi/180), math.Cos(deg*math.Pi/180)
		var dot, total float64
		for _, w := range OceanSwellWaves(s, deg, 0) {
			dot += w.Amplitude * w.Amplitude * (w.DirX*wx + w.DirZ*wz)
			total += w.Amplitude * w.Amplitude
		}
		if dot/total < 0.5 {
			t.Errorf("wind %g: energy-weighted cosine to the wind direction is %.3f", deg, dot/total)
		}
	}
}

func TestNormalizedOceanCarriesSwellWaves(t *testing.T) {
	s := OceanPreset("calm")
	ocean := normalizeOcean(&Ocean{WindDirection: 40, Spectrum: &s, SwellWaves: []OceanWave{{K: 99}}})
	if want := OceanSwellWaves(s, 40, DefaultOceanSwellCount); !reflect.DeepEqual(ocean.SwellWaves, want) {
		t.Fatal("normalization must replace authored swell waves with the computed ones")
	}
	if got := normalizeOcean(&Ocean{SwellWaves: []OceanWave{{K: 1}}}); got.SwellWaves != nil {
		t.Fatal("swell waves without a spectrum must be dropped")
	}
	props := Props{Environment: Environment{Ocean: &Ocean{Spectrum: &s}}}
	if got := props.SceneIR().Environment.Ocean; got == nil || len(got.SwellWaves) != DefaultOceanSwellCount {
		t.Fatalf("SceneIR ocean has no swell waves: %#v", got)
	}
}

// TestOceanSwellTruncationError reports, per preset, how much of the full
// four-cascade surface the K swell waves keep. Both are RMS heights in metres:
// sum A^2 / 2 over the waves. The error is the RMS of the omitted waves (they
// are uncorrelated), as a share of the full RMS.
func TestOceanSwellTruncationError(t *testing.T) {
	for _, name := range []string{"calm", "moderate", "rough", "storm"} {
		s := OceanPreset(name)
		params := oceanSpectrumParams(s, 0)
		var full float64
		for ci, c := range oceanCascades(s) {
			for _, w := range oceanspecBins(params, c, ci) {
				full += w * w / 2
			}
		}
		var kept float64
		for _, w := range OceanSwellWaves(s, 0, DefaultOceanSwellCount) {
			kept += w.Amplitude * w.Amplitude / 2
		}
		fullRMS, keptRMS := math.Sqrt(full), math.Sqrt(kept)
		errRMS := math.Sqrt(math.Max(full-kept, 0))
		t.Logf("%-8s full RMS %.3f m (Hs %.2f m), %d waves RMS %.3f m, truncation error %.3f m (%.0f%% of full RMS)",
			name, fullRMS, 4*fullRMS, DefaultOceanSwellCount, keptRMS, errRMS, 100*errRMS/fullRMS)
		if keptRMS > fullRMS {
			t.Fatalf("%s: kept more than the full spectrum", name)
		}
	}
}
