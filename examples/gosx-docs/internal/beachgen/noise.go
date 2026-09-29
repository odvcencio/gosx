package beachgen

import "math"

type noiseField struct{ seed uint64 }

func newNoise(seed int64) noiseField { return noiseField{seed: uint64(seed)} }

func (n noiseField) value(x, y float64) float64 {
	x0, y0 := int64(math.Floor(x)), int64(math.Floor(y))
	tx, ty := x-float64(x0), y-float64(y0)
	tx, ty = fade(tx), fade(ty)
	a := n.lattice(x0, y0)
	b := n.lattice(x0+1, y0)
	c := n.lattice(x0, y0+1)
	d := n.lattice(x0+1, y0+1)
	return lerp(lerp(a, b, tx), lerp(c, d, tx), ty)
}

func (n noiseField) fbm(x, y float64, octaves int) float64 {
	amplitude, frequency, total, weight := 1.0, 1.0, 0.0, 0.0
	for octave := 0; octave < octaves; octave++ {
		total += amplitude * n.value(x*frequency, y*frequency)
		weight += amplitude
		amplitude *= 0.5
		frequency *= 2
	}
	if weight == 0 {
		return 0
	}
	return total / weight
}

func (n noiseField) lattice(x, y int64) float64 {
	h := n.seed ^ uint64(x)*0x9e3779b97f4a7c15 ^ uint64(y)*0xbf58476d1ce4e5b9
	h += 0x9e3779b97f4a7c15
	h = (h ^ (h >> 30)) * 0xbf58476d1ce4e5b9
	h = (h ^ (h >> 27)) * 0x94d049bb133111eb
	h ^= h >> 31
	return float64(h>>11)/float64(uint64(1)<<53)*2 - 1
}

type randomSource struct{ state uint64 }

func newRandom(seed int64) randomSource {
	return randomSource{state: uint64(seed) + 0x9e3779b97f4a7c15}
}

func (r *randomSource) uint64() uint64 {
	r.state += 0x9e3779b97f4a7c15
	z := r.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func (r *randomSource) float64() float64 {
	return float64(r.uint64()>>11) / float64(uint64(1)<<53)
}

func (r *randomSource) intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.uint64() % uint64(n))
}

func fade(t float64) float64 { return t * t * (3 - 2*t) }
func smoothstep(a, b, x float64) float64 {
	if a == b {
		if x < a {
			return 0
		}
		return 1
	}
	t := clamp((x-a)/(b-a), 0, 1)
	return t * t * (3 - 2*t)
}
func lerp(a, b, t float64) float64 { return a + (b-a)*t }
func clamp(x, low, high float64) float64 {
	if x < low {
		return low
	}
	if x > high {
		return high
	}
	return x
}
