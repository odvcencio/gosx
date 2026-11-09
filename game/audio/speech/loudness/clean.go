package loudness

import (
	"math"
	"sort"
)

// RemoveDC subtracts the clip's mean, which a microphone preamp often leaves
// and which makes the cut at a clip edge click.
func RemoveDC(a Audio) Audio {
	if len(a.Samples) == 0 {
		return a
	}
	var sum float64
	for _, v := range a.Samples {
		sum += v
	}
	mean := sum / float64(len(a.Samples))
	out := Audio{Rate: a.Rate, Samples: make([]float64, len(a.Samples))}
	for i, v := range a.Samples {
		out.Samples[i] = v - mean
	}
	return out
}

// DeClick repairs isolated one-sample pops (a dropped buffer, a mouse click)
// by interpolating across them. A sample is a pop when it departs from the
// midpoint of its neighbours by far more than the surrounding 512-sample
// block normally does, and by at least 1.5 times as much as either
// neighbour's own departure (a spike shows as one big value flanked by two
// half-size opposite ones). It returns the repaired clip and the number of
// samples it changed. Edges are handled by Fade; DeClick leaves them alone.
func DeClick(a Audio) (Audio, int) {
	n := len(a.Samples)
	out := Audio{Rate: a.Rate, Samples: append([]float64(nil), a.Samples...)}
	if n < 5 {
		return out, 0
	}
	d := make([]float64, n)
	for i := 1; i < n-1; i++ {
		d[i] = math.Abs(a.Samples[i] - (a.Samples[i-1]+a.Samples[i+1])/2)
	}
	const block = 512
	fixed := 0
	for start := 0; start < n; start += block {
		end := min(start+block, n)
		tmp := append([]float64(nil), d[start:end]...)
		sort.Float64s(tmp)
		thr := math.Max(12*tmp[len(tmp)/2], 0.02)
		for i := max(start, 2); i < end && i < n-2; i++ {
			if d[i] > thr && d[i] > 1.5*math.Max(d[i-1], d[i+1]) {
				out.Samples[i] = (a.Samples[i-1] + a.Samples[i+1]) / 2
				fixed++
			}
		}
	}
	return out, fixed
}

// Span is a half-open range of sample indexes.
type Span struct{ From, To int }

// SliceBySilence splits a long recording at pauses. A pause is a run of at
// least minGapMs whose 5 ms RMS stays under thresholdDB (dBFS); a span
// shorter than minLenMs is dropped as a noise blip. Spans keep the quiet
// padding up to half the gap on each side, so a later Trim decides the final
// edges.
func SliceBySilence(a Audio, thresholdDB, minGapMs, minLenMs float64) []Span {
	win := max(1, int(0.005*float64(a.Rate)))
	th := FromDB(thresholdDB)
	n := len(a.Samples)
	loud := make([]bool, (n+win-1)/win)
	for w := range loud {
		lo, hi := w*win, min((w+1)*win, n)
		var s float64
		for _, v := range a.Samples[lo:hi] {
			s += v * v
		}
		loud[w] = math.Sqrt(s/float64(hi-lo)) >= th
	}
	gapWins := max(1, int(minGapMs/5))
	var spans []Span
	start, lastLoud := -1, -1
	for w, l := range loud {
		if l {
			if start < 0 {
				start = w
			}
			lastLoud = w
			continue
		}
		if start >= 0 && w-lastLoud >= gapWins {
			spans = append(spans, Span{start * win, (lastLoud + 1) * win})
			start = -1
		}
	}
	if start >= 0 {
		spans = append(spans, Span{start * win, min((lastLoud+1)*win, n)})
	}
	padHalf := int(minGapMs / 2 * float64(a.Rate) / 1000)
	minLen := int(minLenMs * float64(a.Rate) / 1000)
	var out []Span
	for _, s := range spans {
		if s.To-s.From < minLen {
			continue
		}
		out = append(out, Span{max(0, s.From-padHalf), min(n, s.To+padHalf)})
	}
	// Padding must not make neighbours overlap.
	for i := 1; i < len(out); i++ {
		if out[i].From < out[i-1].To {
			mid := (out[i].From + out[i-1].To) / 2
			out[i-1].To, out[i].From = mid, mid
		}
	}
	return out
}

// SliceByClaps splits a long recording at hand claps. A clap is a sample at
// or above clapDB (dBFS) with nothing louder within 300 ms; speech should sit
// well under it. Each take runs from 120 ms after one clap to 20 ms before
// the next, and the last runs to the end. Takes shorter than minLenMs are
// dropped.
func SliceByClaps(a Audio, clapDB, minLenMs float64) []Span {
	th := FromDB(clapDB)
	refractory := int(0.3 * float64(a.Rate))
	var claps []int // index of each clap's last loud sample
	last := -refractory * 2
	for i, v := range a.Samples {
		if math.Abs(v) >= th {
			if i-last > refractory {
				claps = append(claps, i)
			} else {
				claps[len(claps)-1] = i
			}
			last = i
		}
	}
	var out []Span
	minLen := int(minLenMs * float64(a.Rate) / 1000)
	after := int(0.12 * float64(a.Rate))
	before := int(0.02 * float64(a.Rate))
	for i, c := range claps {
		from := c + after
		to := len(a.Samples)
		if i+1 < len(claps) {
			// The next clap's first loud sample, found by walking back.
			first := claps[i+1]
			for first > from && math.Abs(a.Samples[first-1]) >= th*0.5 {
				first--
			}
			to = first - before
		}
		if to-from >= minLen {
			out = append(out, Span{from, to})
		}
	}
	return out
}

// Cut returns the samples of a in s as a new clip.
func Cut(a Audio, s Span) Audio {
	return Audio{Rate: a.Rate, Samples: append([]float64(nil), a.Samples[max(0, s.From):min(len(a.Samples), s.To)]...)}
}
