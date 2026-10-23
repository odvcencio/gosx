package oceanspec

import (
	"math"
	"testing"
)

func defaults() (Params, []Cascade) {
	return Params{WindSpeed: 9, FetchKm: 300, Gamma: 3.3, Spread: 8, WindDir: Direction(0), SwellDir: Direction(0), Seed: 1},
		[]Cascade{{1600, 2 * math.Pi / 1600, 0.12}, {250, 0.12, 0.80}}
}

// The PCG key is the signed wavenumber index, so +-ix keys differ and the hash
// matches Jarzynski and Olano's reference words.
func TestNormalsAreKeyedBySignedIndex(t *testing.T) {
	a0, a1 := normals(1, 0, 3, -4)
	b0, b1 := normals(1, 0, -3, 4)
	if a0 == b0 && a1 == b1 {
		t.Fatal("mirror bins share a key")
	}
	c0, c1 := normals(1, 0, 3, -4)
	if a0 != c0 || a1 != c1 {
		t.Fatal("same key gave different normals")
	}
	if z0, _ := normals(0, 0, 0, 0); math.IsNaN(z0) {
		t.Fatal("seed 0 failed")
	}
}

func TestBinsStayInBandAndOffNyquist(t *testing.T) {
	p, cs := defaults()
	for ci, c := range cs {
		bins := Bins(p, c, ci)
		if len(bins) == 0 {
			t.Fatalf("cascade %d has no bins", ci)
		}
		for _, w := range bins {
			if w.K < c.KMin || w.K >= c.KMax || w.IX == 32 || w.IZ == 32 || (w.IX == 0 && w.IZ == 0) {
				t.Fatalf("cascade %d bin (%d,%d) k=%g outside the band or on a zero line", ci, w.IX, w.IZ, w.K)
			}
			if math.Abs(math.Hypot(w.DirX, w.DirZ)-1) > 1e-15 {
				t.Fatalf("direction of (%d,%d) is not a unit vector", w.IX, w.IZ)
			}
			w0 := 2 * math.Pi / DefaultLoopPeriod
			if m := w.Omega / w0; math.Abs(m-math.Round(m)) > 1e-9 {
				t.Fatalf("omega %g is not a multiple of omega0", w.Omega)
			}
		}
	}
}

func TestStrongestIsPrefixStable(t *testing.T) {
	p, cs := defaults()
	a := Strongest(p, cs, 24)
	b := Strongest(p, cs, 8)
	if len(a) != 24 || len(b) != 8 {
		t.Fatalf("got %d and %d waves", len(a), len(b))
	}
	for i := range b {
		if a[i] != b[i] {
			t.Fatalf("wave %d differs between K=24 and K=8", i)
		}
	}
	for i := 1; i < len(a); i++ {
		if stronger(a[i], a[i-1]) {
			t.Fatalf("wave %d out of order", i)
		}
	}
	if Strongest(p, cs, 0) != nil {
		t.Fatal("K=0 must give no waves")
	}
}

func TestStrongestBreaksTiesBySmallerKThenIndex(t *testing.T) {
	x := Wave{Amplitude: 1, K: 2, IX: 5}
	if !stronger(Wave{Amplitude: 1, K: 1, IX: 9}, x) {
		t.Fatal("smaller |k| must win a tie")
	}
	if !stronger(Wave{Amplitude: 1, K: 2, IX: -3}, x) || stronger(Wave{Amplitude: 1, K: 2, IX: 7}, x) {
		t.Fatal("smaller ix must win when amplitude and |k| tie")
	}
	if !stronger(Wave{Amplitude: 1, K: 2, IX: 5, IZ: -1}, Wave{Amplitude: 1, K: 2, IX: 5, IZ: 1}) {
		t.Fatal("smaller iz must win next")
	}
}

// Direction is the angle theta of the travel vector (cos theta, sin theta) in
// (x, z): 0 degrees travels toward +Z, 90 degrees toward +X.
func TestDirectionIsTheTravelAngle(t *testing.T) {
	for _, c := range []struct{ deg, dx, dz float64 }{{0, 0, 1}, {90, 1, 0}, {180, 0, -1}, {270, -1, 0}, {45, math.Sqrt2 / 2, math.Sqrt2 / 2}} {
		th := Direction(c.deg)
		if math.Abs(math.Cos(th)-c.dx) > 1e-15 || math.Abs(math.Sin(th)-c.dz) > 1e-15 {
			t.Errorf("Direction(%g) = %g, vector (%g, %g), want (%g, %g)", c.deg, th, math.Cos(th), math.Sin(th), c.dx, c.dz)
		}
	}
}
