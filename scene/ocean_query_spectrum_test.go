package scene

import (
	"math"
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
