package replay

// Interpolator blends the previous and current replay state so rendering is
// smooth between ticks, whatever the playback speed. The game samples the
// values it draws (positions, angles) into a float slice after each Advance or
// Seek; the Interpolator keeps the last two samples and blends them by the
// Player's Alpha. Hand the result to the normal Scene3D pose stream.
//
// After a seek it shows the new state with no blend, because the sample before
// it is from a different moment.
type Interpolator struct {
	prev, cur []float64
	tick      uint64
	seeks     uint64
	blend     bool // prev is the sample one tick before cur
	have      bool
}

// Update samples the player's current state if its tick changed. sample
// appends the values to draw to dst and returns the result; it must append the
// same number of values every time. Call Update once per frame after the
// player moved.
func (i *Interpolator) Update(p *Player, sample func(dst []float64) []float64) {
	if i.have && i.tick == p.Tick() && i.seeks == p.Seeks() {
		return
	}
	continuous := i.have && i.seeks == p.Seeks() && p.Tick() == i.tick+1
	i.prev, i.cur = i.cur, i.prev
	i.cur = sample(i.cur[:0])
	i.blend = continuous && len(i.prev) == len(i.cur)
	i.tick, i.seeks, i.have = p.Tick(), p.Seeks(), true
}

// At writes the blended values for alpha in [0, 1] to dst and returns it. It
// returns the current sample unblended after a seek or a multi-tick jump.
func (i *Interpolator) At(alpha float64, dst []float64) []float64 {
	dst = dst[:0]
	if !i.blend {
		return append(dst, i.cur...)
	}
	if alpha < 0 {
		alpha = 0
	} else if alpha > 1 {
		alpha = 1
	}
	for k, c := range i.cur {
		dst = append(dst, i.prev[k]+(c-i.prev[k])*alpha)
	}
	return dst
}
