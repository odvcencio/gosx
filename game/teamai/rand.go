package teamai

// Mix hashes a seed and up to three integer keys into 64 random-looking bits.
// It is SplitMix64's finalizer applied to each key in turn, so the same
// arguments give the same bits on every platform, and a draw never depends on
// how many draws came before. Use it for decisions keyed by (seed, tick,
// agent): Mix(seed, tick, agent, decision).
func Mix(seed uint64, a, b, c uint64) uint64 {
	h := seed
	h = mix64(h ^ mix64(a+0x9E3779B97F4A7C15))
	h = mix64(h ^ mix64(b+0xBF58476D1CE4E5B9))
	h = mix64(h ^ mix64(c+0x94D049BB133111EB))
	return h
}

func mix64(z uint64) uint64 {
	z += 0x9E3779B97F4A7C15
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// Unit maps 64 random bits to a float64 in [0, 1) with 53 bits of precision.
func Unit(bits uint64) float64 {
	return float64(bits>>11) / (1 << 53)
}

// Rand is a SplitMix64 sequence: small, seedable, and the same on every
// platform. The zero value is a valid generator with seed 0.
type Rand struct {
	state uint64
}

// NewRand returns a generator with the given seed.
func NewRand(seed uint64) *Rand { return &Rand{state: seed} }

// Uint64 returns the next 64 random bits.
func (r *Rand) Uint64() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// Float64 returns a float64 in [0, 1).
func (r *Rand) Float64() float64 { return Unit(r.Uint64()) }

// Range returns a float64 in [lo, hi).
func (r *Rand) Range(lo, hi float64) float64 {
	return lo + float64((hi-lo)*r.Float64())
}
