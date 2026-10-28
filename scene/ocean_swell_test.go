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

// rawSpectralOcean is an ocean an author could write that normalization
// changes: an out-of-range scalar, a direction outside [0, 360), a non-finite
// value or a cascade list that falls back or is clamped.
type rawSpectralOcean struct {
	name      string
	direction float64
	spectrum  OceanSpectrum
}

func rawSpectralOceans() []rawSpectralOcean {
	rough := OceanPreset("rough")
	with := func(edit func(*OceanSpectrum)) OceanSpectrum {
		s := rough
		edit(&s)
		return s
	}
	nan, inf := math.NaN(), math.Inf(1)
	return []rawSpectralOcean{
		{"in range", 40, rough},
		{"direction above 360", 372, rough},
		{"direction below 0", -348, rough},
		{"direction at 360", 360, rough},
		{"direction a hair below 0", -1e-20, rough},
		{"direction NaN", nan, rough},
		{"direction infinite", inf, rough},
		{"wind speed above range", 12, with(func(s *OceanSpectrum) { s.WindSpeed = 55 })},
		{"wind speed negative", 12, with(func(s *OceanSpectrum) { s.WindSpeed = -3 })},
		{"wind speed below range", 12, with(func(s *OceanSpectrum) { s.WindSpeed = 0.1 })},
		{"wind speed NaN", 12, with(func(s *OceanSpectrum) { s.WindSpeed = nan })},
		{"fetch above range", 12, with(func(s *OceanSpectrum) { s.Fetch = 5000 })},
		{"fetch below range", 12, with(func(s *OceanSpectrum) { s.Fetch = 0.01 })},
		{"fetch NaN", 12, with(func(s *OceanSpectrum) { s.Fetch = nan })},
		{"peak enhancement above range", 12, with(func(s *OceanSpectrum) { s.PeakEnhancement = 12 })},
		{"peak enhancement below range", 12, with(func(s *OceanSpectrum) { s.PeakEnhancement = 0.2 })},
		{"spread above range", 12, with(func(s *OceanSpectrum) { s.Spread = 100 })},
		{"spread below range", 12, with(func(s *OceanSpectrum) { s.Spread = 0.1 })},
		{"swell above range", 12, with(func(s *OceanSpectrum) { s.Swell = 3 })},
		{"swell negative", 12, with(func(s *OceanSpectrum) { s.Swell = -2 })},
		{"swell direction outside the range", 12, with(func(s *OceanSpectrum) { s.Swell, s.SwellDirection = 0.4, 725 })},
		{"swell direction negative", 12, with(func(s *OceanSpectrum) { s.Swell, s.SwellDirection = 0.4, -90 })},
		{"every scalar NaN", 12, OceanSpectrum{WindSpeed: nan, Fetch: nan, PeakEnhancement: nan, Spread: nan, Swell: nan, SwellDirection: nan}},
		{"cascade band past the Nyquist limit", 12, with(func(s *OceanSpectrum) {
			s.Cascades = []OceanCascade{{Size: 1600, KMin: 0.0039, KMax: 0.12}, {Size: 250, KMin: 0.12, KMax: 9}}
		})},
		{"cascade size out of range", 12, with(func(s *OceanSpectrum) {
			s.Cascades = []OceanCascade{{Size: 90000, KMin: 0.0001, KMax: 0.12}, {Size: 250, KMin: 0.12, KMax: 0.8}}
		})},
		{"invalid cascade list", 12, with(func(s *OceanSpectrum) { s.Cascades = []OceanCascade{{Size: 0, KMin: 0, KMax: 1}} })},
		{"more than four cascades", 12, with(func(s *OceanSpectrum) { s.Cascades = make([]OceanCascade, 5) })},
	}
}

func finiteWaves(waves []OceanWave) bool {
	for _, w := range waves {
		for _, v := range []float64{w.DirX, w.DirZ, w.K, w.Omega, w.Amplitude, w.Phase} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return false
			}
		}
	}
	return true
}

// TestOceanSwellWavesNormalizeTheirInputsLikeTheSceneIR pins the contract that
// OceanSwellWaves(spectrum, direction) returns the waves of the normalized Ocean
// for any input, not only for input that is already in range. A ship that reads
// OceanSwellWaves and a renderer that reads the SceneIR's SwellWaves then ride
// the same sea.
func TestOceanSwellWavesNormalizeTheirInputsLikeTheSceneIR(t *testing.T) {
	for _, c := range rawSpectralOceans() {
		t.Run(c.name, func(t *testing.T) {
			spectrum := c.spectrum
			props := Props{Environment: Environment{Ocean: &Ocean{WindDirection: c.direction, Spectrum: &spectrum}}}
			want := props.SceneIR().Environment.Ocean.SwellWaves
			got := OceanSwellWaves(spectrum, c.direction, DefaultOceanSwellCount)
			if len(got) != DefaultOceanSwellCount || !finiteWaves(got) {
				t.Fatalf("%d waves, finite %v", len(got), finiteWaves(got))
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("OceanSwellWaves differs from the normalized Ocean.SwellWaves\n got %+v\nwant %+v", got[0], want[0])
			}
			// A smaller k is still a prefix of the larger.
			if short := OceanSwellWaves(spectrum, c.direction, 3); !reflect.DeepEqual(short, want[:3]) {
				t.Fatal("a smaller k must be a prefix of the normalized waves")
			}
			// Normalization is idempotent, so the SceneIR's two normalizations
			// (SceneIR and CanonicalIR) ship the same waves.
			ocean := Ocean{WindDirection: c.direction, Spectrum: &spectrum}
			once := normalizeOcean(&ocean)
			if twice := normalizeOcean(once); !reflect.DeepEqual(once, twice) {
				t.Fatalf("normalizing twice changes the ocean\n once %+v\ntwice %+v", once, twice)
			}
		})
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

// presetHs are the significant wave heights Hs = 4 sqrt(m0), in metres, that
// the OceanPreset doc comment quotes for the realized four-cascade spectrum
// with seed 1, the default cascades and wind direction 0.
var presetHs = map[string]float64{"calm": 0.4, "moderate": 1.7, "rough": 3.6, "storm": 6.4}

// presetHsTolerance is half the one-decimal rounding of presetHs.
const presetHsTolerance = 0.05

// TestOceanSwellTruncationError reports, per preset, how much of the full
// four-cascade surface the K swell waves keep. Both are RMS heights in metres:
// sum A^2 / 2 over the waves. The error is the RMS of the omitted waves (they
// are uncorrelated), as a share of the full RMS. It also holds the Hs quoted in
// the OceanPreset doc comment to the measured one.
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
		if hs := 4 * fullRMS; math.Abs(hs-presetHs[name]) > presetHsTolerance {
			t.Errorf("%s: measured Hs %.3f m, the OceanPreset doc comment says %.1f m", name, hs, presetHs[name])
		}
	}
}
