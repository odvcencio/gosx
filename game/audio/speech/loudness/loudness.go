// Package loudness measures and normalizes mono speech clips: ITU-R BS.1770-4
// loudness, 4x true peak, silence trimming, a peak-safe limiter, resampling
// and WAV reading and writing.
//
// The BS.1770 meter is a generalization of the one in the caribbean-dominoes
// audio bake (audio/internal/level), cut down to mono PCM and extended with a
// short-clip mode: a clip under 400 ms is measured as a single block, so
// one-word clips are not diluted by padding.
package loudness

import "math"

// Audio is mono PCM at a sample rate, in the range [-1, 1].
type Audio struct {
	Rate    int
	Samples []float64
}

// Seconds is the duration.
func (a Audio) Seconds() float64 {
	if a.Rate == 0 {
		return 0
	}
	return float64(len(a.Samples)) / float64(a.Rate)
}

// DB converts a linear amplitude to decibels, with a -200 dB floor.
func DB(v float64) float64 {
	if v <= 1e-10 {
		return -200
	}
	return 20 * math.Log10(v)
}

// FromDB converts decibels to a linear amplitude.
func FromDB(db float64) float64 { return math.Pow(10, db/20) }

// Peak is the largest absolute sample.
func Peak(a Audio) float64 {
	var p float64
	for _, v := range a.Samples {
		p = math.Max(p, math.Abs(v))
	}
	return p
}

// TruePeak estimates the inter-sample peak with 4x windowed-sinc
// oversampling, as BS.1770-4 Annex 2 recommends. It returns a linear value.
func TruePeak(a Audio) float64 {
	const taps = 12
	// Kernel weights for the three interpolated phases, computed once.
	var kern [4][2 * taps]float64
	for phase := 1; phase < 4; phase++ {
		x := float64(phase) / 4
		for k := -taps + 1; k <= taps; k++ {
			t := x - float64(k)
			w := 0.5 + 0.5*math.Cos(math.Pi*t/float64(taps))
			kern[phase][k+taps-1] = sinc(t) * w
		}
	}
	p := Peak(a)
	ch := a.Samples
	for i := range ch {
		for phase := 1; phase < 4; phase++ {
			var sum float64
			for k := -taps + 1; k <= taps; k++ {
				j := i + k
				if j < 0 || j >= len(ch) {
					continue
				}
				sum += ch[j] * kern[phase][k+taps-1]
			}
			p = math.Max(p, math.Abs(sum))
		}
	}
	return p
}

func sinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	return math.Sin(math.Pi*x) / (math.Pi * x)
}

type biquad struct{ b0, b1, b2, a1, a2, x1, x2, y1, y2 float64 }

func (f *biquad) run(x float64) float64 {
	y := f.b0*x + f.b1*f.x1 + f.b2*f.x2 - f.a1*f.y1 - f.a2*f.y2
	f.x2, f.x1, f.y2, f.y1 = f.x1, x, f.y1, y
	return y
}

// kWeighting returns the BS.1770-4 pre-filter (high shelf) and RLB highpass.
func kWeighting(rate float64) (biquad, biquad) {
	f0, g, q := 1681.974450955533, 3.999843853973347, 0.7071752369554196
	k := math.Tan(math.Pi * f0 / rate)
	vh := math.Pow(10, g/20)
	vb := math.Pow(vh, 0.4996667741545416)
	a0 := 1 + k/q + k*k
	shelf := biquad{
		b0: (vh + vb*k/q + k*k) / a0, b1: 2 * (k*k - vh) / a0, b2: (vh - vb*k/q + k*k) / a0,
		a1: 2 * (k*k - 1) / a0, a2: (1 - k/q + k*k) / a0,
	}
	f0, q = 38.13547087602444, 0.5003270373238773
	k = math.Tan(math.Pi * f0 / rate)
	a0 = 1 + k/q + k*k
	high := biquad{b0: 1, b1: -2, b2: 1, a1: 2 * (k*k - 1) / a0, a2: (1 - k/q + k*k) / a0}
	return shelf, high
}

func weighted(a Audio) []float64 {
	shelf, high := kWeighting(float64(a.Rate))
	out := make([]float64, len(a.Samples))
	for i, v := range a.Samples {
		out[i] = high.run(shelf.run(v))
	}
	return out
}

func lufs(power float64) float64 {
	if power <= 0 {
		return -200
	}
	return -0.691 + 10*math.Log10(power)
}

// blockPowers returns the mean K-weighted power of 400 ms blocks with 75%
// overlap. A clip shorter than one block yields a single block over the
// whole clip.
func blockPowers(a Audio) []float64 {
	w := weighted(a)
	size := int(0.4 * float64(a.Rate))
	if len(w) == 0 {
		return nil
	}
	meanSq := func(s []float64) float64 {
		var sum float64
		for _, v := range s {
			sum += v * v
		}
		return sum / float64(len(s))
	}
	if len(w) < size {
		return []float64{meanSq(w)}
	}
	step := size / 4
	var out []float64
	for start := 0; start+size <= len(w); start += step {
		out = append(out, meanSq(w[start:start+size]))
	}
	return out
}

// Integrated is the BS.1770-4 gated integrated loudness in LUFS (absolute
// gate -70 LUFS, relative gate -10 LU). It returns -200 for silence.
func Integrated(a Audio) float64 {
	var kept []float64
	for _, p := range blockPowers(a) {
		if lufs(p) > -70 {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return -200
	}
	mean := func(ps []float64) float64 {
		var s float64
		for _, p := range ps {
			s += p
		}
		return s / float64(len(ps))
	}
	gate := lufs(mean(kept)) - 10
	var final []float64
	for _, p := range kept {
		if lufs(p) > gate {
			final = append(final, p)
		}
	}
	if len(final) == 0 {
		return -200
	}
	return lufs(mean(final))
}

// Gain returns a copy of a scaled by db decibels.
func Gain(a Audio, db float64) Audio {
	g := FromDB(db)
	out := Audio{Rate: a.Rate, Samples: make([]float64, len(a.Samples))}
	for i, v := range a.Samples {
		out.Samples[i] = v * g
	}
	return out
}

// Limit returns a copy of a whose sample peaks do not exceed ceiling (a
// linear value). The gain curve is non-causal: a minimum filter over
// +-lookahead samples, then a moving average of the same width, which keeps
// the curve at or below the needed reduction at every sample and avoids
// clicks. lookahead is in samples.
func Limit(a Audio, ceiling float64, lookahead int) Audio {
	n := len(a.Samples)
	out := Audio{Rate: a.Rate, Samples: make([]float64, n)}
	if lookahead < 1 {
		lookahead = 1
	}
	need := make([]float64, n)
	for i, v := range a.Samples {
		need[i] = 1
		if av := math.Abs(v); av > ceiling {
			need[i] = ceiling / av
		}
	}
	mn := make([]float64, n)
	for i := range mn {
		m := 1.0
		for j := max(0, i-lookahead); j <= min(n-1, i+lookahead); j++ {
			m = math.Min(m, need[j])
		}
		mn[i] = m
	}
	// Out-of-range window points repeat the edge value, which keeps the curve
	// at or below the needed reduction at the first and last samples too.
	for i, v := range a.Samples {
		var sum float64
		for j := i - lookahead; j <= i+lookahead; j++ {
			sum += mn[min(max(j, 0), n-1)]
		}
		out.Samples[i] = v * sum / float64(2*lookahead+1)
	}
	return out
}

// Trim removes leading and trailing samples quieter than thresholdDB (dBFS,
// absolute) and keeps padMs of the quiet signal on each side. A clip that is
// entirely below the threshold is returned empty.
func Trim(a Audio, thresholdDB, padMs float64) Audio {
	th := FromDB(thresholdDB)
	lo, hi := -1, -1
	for i, v := range a.Samples {
		if math.Abs(v) >= th {
			if lo < 0 {
				lo = i
			}
			hi = i
		}
	}
	if lo < 0 {
		return Audio{Rate: a.Rate}
	}
	pad := int(padMs * float64(a.Rate) / 1000)
	lo = max(0, lo-pad)
	hi = min(len(a.Samples)-1, hi+pad)
	return Audio{Rate: a.Rate, Samples: append([]float64(nil), a.Samples[lo:hi+1]...)}
}

// Fade applies a linear fade over the first and last ms of the clip, so a
// trimmed clip never starts or ends on a step.
func Fade(a Audio, ms float64) Audio {
	n := int(ms * float64(a.Rate) / 1000)
	out := Audio{Rate: a.Rate, Samples: append([]float64(nil), a.Samples...)}
	n = min(n, len(out.Samples)/2)
	for i := 0; i < n; i++ {
		g := float64(i) / float64(n)
		out.Samples[i] *= g
		out.Samples[len(out.Samples)-1-i] *= g
	}
	return out
}

// Resample converts a to rate with a Hann-windowed sinc filter (16 zero
// crossings each side). Equal rates return a copy.
func Resample(a Audio, rate int) Audio {
	if a.Rate == rate || len(a.Samples) == 0 {
		return Audio{Rate: rate, Samples: append([]float64(nil), a.Samples...)}
	}
	ratio := float64(rate) / float64(a.Rate)
	n := int(math.Round(float64(len(a.Samples)) * ratio))
	cutoff := math.Min(1, ratio) // lowpass at the lower Nyquist
	const zc = 16
	half := float64(zc) / cutoff
	out := Audio{Rate: rate, Samples: make([]float64, n)}
	for i := range out.Samples {
		pos := float64(i) / ratio
		lo, hi := int(math.Ceil(pos-half)), int(math.Floor(pos+half))
		var sum float64
		for j := max(0, lo); j <= min(len(a.Samples)-1, hi); j++ {
			d := pos - float64(j)
			w := 0.5 + 0.5*math.Cos(math.Pi*d/half)
			k := cutoff * sinc(d*cutoff) * w
			sum += a.Samples[j] * k
		}
		out.Samples[i] = sum
	}
	return out
}

// Report is a clip's measurement.
type Report struct {
	LUFS       float64
	PeakDBFS   float64
	TruePeakDB float64 // dBTP
}

// Measure reports loudness, sample peak and true peak.
func Measure(a Audio) Report {
	return Report{LUFS: Integrated(a), PeakDBFS: DB(Peak(a)), TruePeakDB: DB(TruePeak(a))}
}

// Normalize gains a toward targetLUFS and then holds the true peak at or
// below ceilingDBTP. It trims nothing. The limiter engages when the target
// would push the peak past the ceiling; if the limiter cannot hold the true
// peak (inter-sample overshoot), it lowers the gain until it does. The
// returned report is measured on the result, so a clip the limiter held below
// target says so.
func Normalize(a Audio, targetLUFS, ceilingDBTP float64) (Audio, Report) {
	cur := Integrated(a)
	if cur <= -100 {
		return a, Measure(a)
	}
	out := Gain(a, targetLUFS-cur)
	ceil := FromDB(ceilingDBTP)
	look := max(1, a.Rate*3/2000) // 1.5 ms
	for try := 0; try < 8; try++ {
		if Peak(out) > ceil {
			out = Limit(out, ceil*0.999, look)
		}
		tp := TruePeak(out)
		if tp <= ceil*1.0005 {
			break
		}
		out = Gain(out, DB(ceil/tp)-0.05)
	}
	return out, Measure(out)
}
