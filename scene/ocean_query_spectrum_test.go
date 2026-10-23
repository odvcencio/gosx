package scene

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func spectrumOcean() Ocean {
	s := OceanPreset("rough")
	return normalizeOcean(&Ocean{Level: 2, WindDirection: 35, Choppiness: 0.5, Spectrum: &s}).copyValue()
}

func (o *Ocean) copyValue() Ocean { return *o }

func TestOceanQuerySpectrumVerticalIsTheSwellSum(t *testing.T) {
	o := spectrumOcean()
	q := NewOceanQuery(o, "high", nil)
	for i := 0; i < 50; i++ {
		x, z, tm := float64(i)*37.1-900, float64(i)*-21.7+300, float64(i)*1.9
		want := o.Level
		for _, w := range o.SwellWaves {
			want += w.Amplitude * math.Cos(w.K*(w.DirX*x+w.DirZ*z)-w.Omega*tm+w.Phase)
		}
		if got := q.Evaluate(x, z, tm).Position.Y; math.Abs(got-want) > 1e-9 {
			t.Fatalf("sample %d: height %g, want %g", i, got, want)
		}
	}
}

func TestOceanQuerySpectrumIgnoresGerstnerSettings(t *testing.T) {
	a, b := spectrumOcean(), spectrumOcean()
	b.WaveHeight, b.WaveLength, b.Speed = 5, 90, 3
	pa := NewOceanQuery(a, "high", nil).Sample(10, 20, 30)
	pb := NewOceanQuery(b, "low", nil).Sample(10, 20, 30)
	if pa != pb {
		t.Fatal("WaveHeight, WaveLength, Speed and quality must not change a spectral ocean")
	}
}

func TestOceanQuerySpectrumDerivativesAreConsistent(t *testing.T) {
	o := spectrumOcean()
	q := NewOceanQuery(o, "high", nil)
	const h = 1e-4
	for i := 0; i < 20; i++ {
		x, z, tm := float64(i)*53.3-400, float64(i)*29.1-200, float64(i)*2.3
		s := q.Evaluate(x, z, tm)
		px, mx := q.Evaluate(x+h, z, tm).Position, q.Evaluate(x-h, z, tm).Position
		pz, mz := q.Evaluate(x, z+h, tm).Position, q.Evaluate(x, z-h, tm).Position
		pt, mt := q.Evaluate(x, z, tm+h).Position, q.Evaluate(x, z, tm-h).Position
		cmp := func(name string, got, want Vector3) {
			if math.Abs(got.X-want.X) > 1e-5 || math.Abs(got.Y-want.Y) > 1e-5 || math.Abs(got.Z-want.Z) > 1e-5 {
				t.Fatalf("sample %d %s = %+v, finite difference %+v", i, name, got, want)
			}
		}
		cmp("tangentX", s.TangentX, Vector3{X: (px.X - mx.X) / (2 * h), Y: (px.Y - mx.Y) / (2 * h), Z: (px.Z - mx.Z) / (2 * h)})
		cmp("tangentZ", s.TangentZ, Vector3{X: (pz.X - mz.X) / (2 * h), Y: (pz.Y - mz.Y) / (2 * h), Z: (pz.Z - mz.Z) / (2 * h)})
		cmp("velocity", s.Velocity, Vector3{X: (pt.X - mt.X) / (2 * h), Y: (pt.Y - mt.Y) / (2 * h), Z: (pt.Z - mt.Z) / (2 * h)})
	}
}

func TestOceanQuerySpectrumSampleFindsTheWaterAbove(t *testing.T) {
	q := NewOceanQuery(spectrumOcean(), "high", nil)
	for i := 0; i < 20; i++ {
		x, z, tm := float64(i)*41.3-300, float64(i)*-17.9+150, float64(i)*3.1
		s := q.Sample(x, z, tm)
		if math.Abs(s.Position.X-x) > 1e-6 || math.Abs(s.Position.Z-z) > 1e-6 {
			t.Fatalf("sample %d is above (%g, %g), want (%g, %g)", i, s.Position.X, s.Position.Z, x, z)
		}
		if math.Abs(math.Sqrt(s.Normal.X*s.Normal.X+s.Normal.Y*s.Normal.Y+s.Normal.Z*s.Normal.Z)-1) > 1e-12 || s.Normal.Y <= 0 {
			t.Fatalf("sample %d normal %+v", i, s.Normal)
		}
	}
}

func TestOceanQuerySpectrumShoalsOverShallowWater(t *testing.T) {
	o := spectrumOcean()
	o.Surf = 0.001 // the shore run-up is a separate term
	deep := NewOceanQuery(o, "high", nil)
	shallow := NewOceanQuery(o, "high", func(x, z float64) float64 { return o.Level - 0.05 })
	var d, s float64
	for i := 0; i < 40; i++ {
		x, tm := float64(i)*13.7, float64(i)*0.9
		d += math.Abs(deep.Evaluate(x, 0, tm).Position.Y - o.Level)
		s += math.Abs(shallow.Evaluate(x, 0, tm).Position.Y - o.Level)
	}
	if s >= d/4 {
		t.Fatalf("shallow water kept %.3f of the deep-water motion %.3f", s, d)
	}
}

func TestOceanQuerySpectrumComputesWavesWhenNotNormalized(t *testing.T) {
	s := OceanPreset("calm")
	raw := Ocean{WindDirection: 12, Spectrum: &s}
	n := normalizeOcean(&raw)
	a, b := NewOceanQuery(raw, "high", nil), NewOceanQuery(*n, "high", nil)
	if a.Evaluate(5, 6, 7) != b.Evaluate(5, 6, 7) {
		t.Fatal("an un-normalized spectral Ocean must compute its waves")
	}
}

// TestOceanQuerySpectrumParityGolden keeps the browser's ocean-query.ts and
// OceanQuery in step: it holds normalized spectral oceans (with their swell
// waves, as the SceneIR carries them) and Sample results, and
// client/js/scene3d-ocean-query.test.js evaluates the same file. UPDATE_GOLDEN=1
// rewrites testdata/ocean-spectrum-query-parity.json.
func TestOceanQuerySpectrumParityGolden(t *testing.T) {
	type record struct {
		Ocean       Ocean   `json:"ocean"`
		FloorOffset float64 `json:"floorOffset"` // floor(x, z) = level - floorOffset - floorSlope*x
		FloorSlope  float64 `json:"floorSlope"`
		X           float64 `json:"x"`
		Z           float64 `json:"z"`
		Time        float64 `json:"time"`
		Position    Vector3 `json:"position"`
		Normal      Vector3 `json:"normal"`
		Velocity    Vector3 `json:"velocity"`
	}
	var records []record
	for i, name := range []string{"calm", "moderate", "rough", "storm"} {
		s := OceanPreset(name)
		ocean := *normalizeOcean(&Ocean{Level: float64(i) - 1, WindDirection: float64(70*i + 10), Choppiness: 0.3 + 0.2*float64(i), Surf: 0.4, Spectrum: &s})
		for _, f := range [][2]float64{{1e4, 0}, {3, 0.01}} {
			f := f
			q := NewOceanQuery(ocean, "high", func(x, z float64) float64 { return ocean.Level - f[0] - f[1]*x })
			for _, p := range [][3]float64{{0, 0, 0}, {12.3, -40.1, 7.7}, {-250.5, 99.9, 63}, {800, -640, 1234.5}} {
				v := q.Sample(p[0], p[1], p[2])
				records = append(records, record{Ocean: ocean, FloorOffset: f[0], FloorSlope: f[1], X: p[0], Z: p[1], Time: p[2], Position: v.Position, Normal: v.Normal, Velocity: v.Velocity})
			}
		}
	}
	const path = "testdata/ocean-spectrum-query-parity.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		data, err := json.MarshalIndent(records, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with UPDATE_GOLDEN=1)", err)
	}
	var want []record
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if len(want) != len(records) {
		t.Fatalf("golden has %d records, this run %d: run with UPDATE_GOLDEN=1", len(want), len(records))
	}
	for i, w := range want {
		g := records[i]
		for _, d := range []float64{g.Position.X - w.Position.X, g.Position.Y - w.Position.Y, g.Position.Z - w.Position.Z,
			g.Normal.X - w.Normal.X, g.Normal.Y - w.Normal.Y, g.Normal.Z - w.Normal.Z,
			g.Velocity.X - w.Velocity.X, g.Velocity.Y - w.Velocity.Y, g.Velocity.Z - w.Velocity.Z} {
			if math.Abs(d) > 1e-7 {
				t.Fatalf("record %d differs by %g: got %+v, golden %+v", i, d, g, w)
			}
		}
	}
}
