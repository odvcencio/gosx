package netsync

// Interpolator buffers authoritative values of remote entities by tick and
// samples them between ticks, so other players move smoothly even though
// snapshots arrive at the tick rate and with jitter. Render remote entities at
// a time a little behind the server, such as Client.ServerTick() minus two
// snapshot intervals, and the local player from the predicted simulation.
//
// Each entity has an ID and Width float32 values (a position, a rotation as
// Euler angles, a scale). Values interpolate linearly; an entity missing from
// either bracketing frame holds its known value. Begin, Add, Sample and Latest
// do not allocate once the frames have grown to fit the entity count.
//
//	ip := netsync.NewInterpolator(3, 32)
//	cfg.OnSnapshot = func(tick uint64, s sim.Simulation) {
//		ip.Begin(tick)
//		for _, e := range entitiesOf(s) {
//			ip.Add(e.ID, e.Pos[:])
//		}
//	}
//	// each render frame:
//	ip.Sample(client.ServerTick()-2, func(id uint32, v []float32) { place(id, v) })
type Interpolator struct {
	width  int
	frames []interpFrame
	head   int // index of the newest frame
	size   int
	out    []float32
	open   bool
}

type interpFrame struct {
	tick uint64
	ids  []uint32
	vals []float32
}

// NewInterpolator holds capacity frames of entities with width values each.
func NewInterpolator(width, capacity int) *Interpolator {
	width, capacity = max(width, 1), max(capacity, 2)
	return &Interpolator{width: width, frames: make([]interpFrame, capacity), head: -1, out: make([]float32, width)}
}

// Width returns the values per entity.
func (b *Interpolator) Width() int { return b.width }

// Begin starts the frame for tick. Ticks must increase; Begin ignores a tick
// at or before the newest frame, and the Add calls that follow it.
func (b *Interpolator) Begin(tick uint64) {
	if b.size > 0 && tick <= b.frames[b.head].tick {
		b.open = false
		return
	}
	b.head = (b.head + 1) % len(b.frames)
	if b.size < len(b.frames) {
		b.size++
	}
	f := &b.frames[b.head]
	f.tick, f.ids, f.vals = tick, f.ids[:0], f.vals[:0]
	b.open = true
}

// Add records entity id's values in the frame Begin started. values must have
// Width entries; extra ones are ignored and missing ones read as zero.
func (b *Interpolator) Add(id uint32, values []float32) {
	if !b.open {
		return
	}
	f := &b.frames[b.head]
	f.ids = append(f.ids, id)
	for i := 0; i < b.width; i++ {
		var v float32
		if i < len(values) {
			v = values[i]
		}
		f.vals = append(f.vals, v)
	}
}

// Latest returns the newest buffered tick, or false when empty.
func (b *Interpolator) Latest() (uint64, bool) {
	if b.size == 0 {
		return 0, false
	}
	return b.frames[b.head].tick, true
}

// frame returns the i-th oldest frame.
func (b *Interpolator) frame(i int) *interpFrame {
	return &b.frames[(b.head-b.size+1+i+len(b.frames))%len(b.frames)]
}

// Sample calls fn once per entity in the frames bracketing tick, with values
// interpolated to tick. Before the oldest frame it uses the oldest; after the
// newest it holds the newest (no extrapolation). The slice passed to fn is
// reused; copy it to keep it.
func (b *Interpolator) Sample(tick float64, fn func(id uint32, values []float32)) {
	if b.size == 0 {
		return
	}
	// Find the last frame at or before tick.
	lo := -1
	for i := 0; i < b.size; i++ {
		if float64(b.frame(i).tick) <= tick {
			lo = i
		} else {
			break
		}
	}
	if lo < 0 || lo == b.size-1 {
		f := b.frame(max(lo, 0)) // before the oldest or after the newest: hold
		for i, id := range f.ids {
			fn(id, f.vals[i*b.width:(i+1)*b.width])
		}
		return
	}
	a, c := b.frame(lo), b.frame(lo+1)
	t := float32((tick - float64(a.tick)) / float64(c.tick-a.tick))
	j := 0
	for i, id := range a.ids {
		av := a.vals[i*b.width : (i+1)*b.width]
		// IDs usually keep their order between frames; scan forward from the
		// last match, then wrap.
		k := -1
		for n := 0; n < len(c.ids); n++ {
			idx := (j + n) % len(c.ids)
			if c.ids[idx] == id {
				k = idx
				break
			}
		}
		if k < 0 {
			fn(id, av)
			continue
		}
		j = k + 1
		cv := c.vals[k*b.width : (k+1)*b.width]
		for w := 0; w < b.width; w++ {
			b.out[w] = av[w] + (cv[w]-av[w])*t
		}
		fn(id, b.out)
	}
}
