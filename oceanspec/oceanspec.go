//gosx:deterministic

// Package oceanspec computes the swell waves of the Scene3D spectral ocean.
//
// It ports the spectrum model of Elio's ocean_spectrum kernel (stdlib/ocean/
// spectrum.elio): a PCG hash keyed by (seed, cascade, signed ix, iz) feeding a
// Box-Muller pair, a JONSWAP frequency spectrum with Hasselmann directional
// spreading and an optional swell component, band edges per cascade, zero
// Nyquist lines and k = 0, and the looping dispersion
// omega_q = omega0 floor(omega / omega0), omega0 = 2 pi / loopPeriod.
// The kernel runs on the GPU; this function runs the same arithmetic on the CPU
// with package det, so a ship's motion and the browser's first frames agree
// bit for bit on every platform. A golden file produced by Elio's interpreter
// (scene/testdata/ocean-swell-bins.json) holds the two together.
//
// Each bin k of the evolved field is the plane wave
//
//	y = A cos(k.x - omega_q t + phase),  A = 2 |h0(k)|,  phase = arg h0(k)
//
// where h0(k) is the kernel's (x, y) lane pair. The wave travels toward +k.
// Summed over every bin it equals the inverse FFT of the kernel's evolved
// spectrum h(k, t) = h0(k) e^{-i omega t} + conj h0(-k) e^{+i omega t} exactly.
package oceanspec

import "m31labs.dev/gosx/det"

const (
	gravity = 9.81
	twoPi   = 6.283185307179586

	// GridSize is the grid the bins are enumerated on. Band edges sit below the
	// Nyquist wavenumber of a 64-point cascade, so every authored bin has
	// |ix| and |iz| below GridSize/2 and a larger grid adds none.
	GridSize = 64

	// DefaultLoopPeriod is the dispersion loop period, in seconds, when
	// Params.LoopPeriod is not positive.
	DefaultLoopPeriod = 1000.0
)

// Params are the spectrum parameters in the kernel's units. Directions are in
// radians measured from +x toward +z, as the kernel's windDir and swellDir.
type Params struct {
	WindSpeed  float64 // m/s at 10 m
	FetchKm    float64 // km
	Gamma      float64 // JONSWAP peak enhancement
	Spread     float64 // directional sharpness
	Swell      float64 // 0-1 share of the swell component
	WindDir    float64 // radians
	SwellDir   float64 // radians
	LoopPeriod float64 // seconds; 0 means DefaultLoopPeriod
	Seed       uint32
}

// Cascade is one tile: its size in metres and its band [KMin, KMax) in rad/m.
type Cascade struct{ Size, KMin, KMax float64 }

// Wave is one plane wave. Dir is the unit direction of travel in x and z, K the
// wavenumber in rad/m, Omega the quantized angular frequency in rad/s, Amplitude
// in metres and Phase in radians.
type Wave struct {
	DirX, DirZ, K, Omega, Amplitude, Phase float64
	Cascade, IX, IZ                        int
}

// pcg is one step of the PCG output hash (Jarzynski and Olano 2020).
func pcg(v uint32) uint32 {
	state := v*747796405 + 2891336453
	word := ((state >> ((state >> 28) + 4)) ^ state) * 277803737
	return (word >> 22) ^ word
}

// normals returns the Box-Muller pair for the key (seed, cascade, ix, iz).
func normals(seed, cascade uint32, ix, iz int) (z0, z1 float64) {
	words := [6]uint32{seed, cascade, uint32(int32(ix)), uint32(int32(iz)), 2654435769, 2246822519}
	var h, b1, b2 uint32
	for r, w := range words {
		h = pcg(h ^ w)
		if r == 4 {
			b1 = h
		}
		if r == 5 {
			b2 = h
		}
	}
	u1 := (float64(b1>>8) + 1) / 16777216
	u2 := (float64(b2>>8) + 1) / 16777216
	rad := det.Sqrt(-2 * det.Log(u1))
	a := float64(twoPi * u2)
	return float64(rad * det.Cos(a)), float64(rad * det.Sin(a))
}

// lnSpreadNorm is ln N(s) = ln Gamma(s+1) - ln Gamma(s+1/2) - ln(2 sqrt(pi)),
// by Stirling's series at x + 6 as the kernel does.
func lnSpreadNorm(s float64) float64 {
	lnn := -1.2655121234846454
	for q := 0; q < 2; q++ {
		xq, sign := s+1, 1.0
		if q == 1 {
			xq, sign = s+0.5, -1.0
		}
		xs := xq + 6
		stirling := float64((xs-0.5)*det.Log(xs)) - xs + 0.9189385332046727 +
			1/float64(12*xs) - 1/float64(float64(float64(360*xs)*xs)*xs) +
			1/float64(float64(float64(float64(float64(1260*xs)*xs)*xs)*xs)*xs)
		prod := float64(float64(float64(float64(float64(xq*(xq+1))*(xq+2))*(xq+3))*(xq+4)) * (xq + 5))
		lnn += float64(sign * (stirling - det.Log(prod)))
	}
	return lnn
}

// density is the 2D wavenumber spectrum S(kx, kz) in m^4 of the kernel.
func density(p Params, wp, alpha, kx, kz float64) float64 {
	kk := det.Sqrt(float64(kx*kx) + float64(kz*kz))
	w := det.Sqrt(float64(gravity * kk))
	var dens float64
	for c := 0; c < 2; c++ {
		weight, wpk, alph, gam, sp, dir := 1-p.Swell, wp, alpha, p.Gamma, p.Spread, p.WindDir
		if c == 1 {
			weight, wpk, alph, gam = p.Swell, 0.5*wp, alpha*0.0625, 7.0
			sp, dir = det.Min(4*p.Spread, 64), p.SwellDir
		}
		if weight <= 0 {
			continue
		}
		sigma := 0.07
		if w > wpk {
			sigma = 0.09
		}
		d := w - wpk
		rpk := det.Exp(-float64(d*d) / float64(float64(float64(2*sigma)*sigma)*float64(wpk*wpk)))
		sw := float64(float64(float64(float64(alph*gravity)*gravity)/det.Pow(w, 5)) *
			det.Exp(float64(-1.25*det.Pow(wpk/w, 4))))
		sw = float64(sw * det.Pow(gam, rpk))
		s := float64(sp * det.Pow(w/wpk, 4.06))
		if w > wpk {
			s = float64(sp * det.Pow(w/wpk, -2.33))
		}
		cosT := (float64(kx*det.Cos(dir)) + float64(kz*det.Sin(dir))) / kk
		base := det.Max(0.5*(1+cosT), 0.00000001)
		dens += float64(float64(float64(weight*sw)*det.Exp(lnSpreadNorm(s)))*det.Pow(base, s)) * gravity / float64(float64(2*w)*kk)
	}
	return dens
}

// Bins returns every non-zero bin of one cascade as a plane wave, in row order
// (iz from -31, then ix from -31). cascade is the index that keys the hash.
func Bins(p Params, c Cascade, cascade int) []Wave {
	fetch := float64(p.FetchKm * 1000)
	wp := float64(22 * det.Pow(float64(gravity*gravity)/float64(p.WindSpeed*fetch), 0.3333333333333333))
	alpha := float64(0.076 * det.Pow(float64(p.WindSpeed*p.WindSpeed)/float64(fetch*gravity), 0.22))
	dk := twoPi / c.Size
	tloop := p.LoopPeriod
	if tloop <= 0 {
		tloop = DefaultLoopPeriod
	}
	omega0 := twoPi / tloop
	half := GridSize / 2
	var out []Wave
	for iz := -half + 1; iz < half; iz++ {
		for ix := -half + 1; ix < half; ix++ {
			if ix == 0 && iz == 0 {
				continue
			}
			n := det.Sqrt(float64(ix*ix + iz*iz))
			k0 := float64(dk * n)
			if k0 < c.KMin || k0 >= c.KMax {
				continue
			}
			// The (x, y) lanes: z0, z1 keyed by (ix, iz), density at +k.
			z0, z1 := normals(p.Seed, uint32(cascade), ix, iz)
			amp := float64(float64(0.5*det.Sqrt(density(p, wp, alpha, float64(dk*float64(ix)), float64(dk*float64(iz))))) * dk)
			hr, hi := float64(z0*amp), float64(z1*amp)
			a := 2 * det.Sqrt(float64(hr*hr)+float64(hi*hi))
			if a == 0 {
				continue
			}
			out = append(out, Wave{
				DirX: float64(ix) / n, DirZ: float64(iz) / n, K: k0,
				Omega:     float64(omega0 * det.Floor(det.Sqrt(float64(gravity*k0))/omega0)),
				Amplitude: a, Phase: det.Atan2(hi, hr),
				Cascade: cascade, IX: ix, IZ: iz,
			})
		}
	}
	return out
}

// stronger orders waves for selection: larger amplitude first, then smaller |k|,
// then smaller signed index (ix, then iz), then smaller cascade.
func stronger(a, b Wave) bool {
	switch {
	case a.Amplitude != b.Amplitude:
		return a.Amplitude > b.Amplitude
	case a.K != b.K:
		return a.K < b.K
	case a.IX != b.IX:
		return a.IX < b.IX
	case a.IZ != b.IZ:
		return a.IZ < b.IZ
	}
	return a.Cascade < b.Cascade
}

// Strongest returns the count strongest waves of the cascades given, in order
// of strength. It does not modify its argument's cascades.
func Strongest(p Params, cascades []Cascade, count int) []Wave {
	if count <= 0 {
		return nil
	}
	top := make([]Wave, 0, count+1)
	for ci, c := range cascades {
		for _, w := range Bins(p, c, ci) {
			pos := len(top)
			for pos > 0 && stronger(w, top[pos-1]) {
				pos--
			}
			if pos >= count {
				continue
			}
			top = append(top, Wave{})
			copy(top[pos+1:], top[pos:])
			top[pos] = w
			if len(top) > count {
				top = top[:count]
			}
		}
	}
	return top
}

// Direction converts a scene direction in degrees (0 travels toward +Z,
// positive toward +X, as scene.Ocean.WindDirection) to the kernel's windDir and
// swellDir uniform: the angle theta of the direction of travel, vector
// (cos theta, sin theta) in (x, z), so theta = pi/2 - degrees pi/180.
func Direction(degrees float64) float64 {
	return 1.5707963267948966 - float64(degrees*3.141592653589793)/180
}
