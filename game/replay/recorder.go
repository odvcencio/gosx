package replay

import (
	"m31labs.dev/gosx/sim/simcore"
)

// Config sets how a Recorder stores play.
type Config struct {
	// Interval is the ticks between keyframes. Smaller values seek faster and
	// use more memory. Zero means 120.
	Interval int
	// MaxBytes bounds the memory of snapshots and inputs. When a new segment
	// would exceed it, the oldest segment is dropped, so the recording keeps
	// the most recent play. Zero means 64 MiB. The newest two segments are
	// always kept.
	MaxBytes int
	// StartTick numbers the first keyframe. Zero for a fresh simulation.
	StartTick uint64
	// TickRate is the simulation's ticks per second. It is stored with clips so
	// a player can run at real-time speed. Zero means unknown.
	TickRate float64
}

const (
	defaultInterval = 120
	defaultMaxBytes = 64 << 20
)

// Recorder wraps a simulation and records every tick. Call Tick in place of
// the simulation's Tick (a Recorder is a simcore.Simulation), or call Record
// just before you tick the simulation yourself.
//
// A Recorder is not safe for concurrent use.
type Recorder struct {
	sim     simcore.Simulation
	cfg     Config
	closed  []*segment
	cur     *segment
	tick    uint64
	bytes   int // bytes in closed segments
	dropped uint64
	markers []Marker
}

// NewRecorder starts recording sim from its current state, which becomes the
// first keyframe at cfg.StartTick.
func NewRecorder(sim simcore.Simulation, cfg Config) *Recorder {
	if cfg.Interval <= 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = defaultMaxBytes
	}
	r := &Recorder{sim: sim, cfg: cfg, tick: cfg.StartTick}
	r.openSegment()
	return r
}

func (r *Recorder) openSegment() {
	r.cur = &segment{start: r.tick, snapshot: append([]byte(nil), r.sim.Snapshot()...)}
}

// Tick implements simcore.Simulation: it records inputs, then ticks the
// wrapped simulation.
func (r *Recorder) Tick(inputs map[string]simcore.Input) {
	r.Record(inputs)
	r.sim.Tick(inputs)
}

// Snapshot, Restore and State pass through to the wrapped simulation.
func (r *Recorder) Snapshot() []byte { return r.sim.Snapshot() }

// Restore passes through. Restoring while recording breaks the tick count, so
// call it only before the first Tick.
func (r *Recorder) Restore(s []byte) { r.sim.Restore(s) }

// State passes through to the wrapped simulation.
func (r *Recorder) State() []byte { return r.sim.State() }

// Unwrap returns the wrapped simulation.
func (r *Recorder) Unwrap() simcore.Simulation { return r.sim }

// Record stores the inputs for the next tick. Call it before the simulation
// ticks. It takes the keyframe snapshot when the tick falls on an interval.
func (r *Recorder) Record(inputs map[string]simcore.Input) {
	if r.cur.ticks() >= r.cfg.Interval {
		r.closeCurrent()
		r.openSegment()
	}
	s := r.cur
	s.offsets = append(s.offsets, uint32(len(s.inputs)))
	s.inputs = appendInputs(s.inputs, inputs)
	r.tick++
}

func (r *Recorder) closeCurrent() {
	r.closed = append(r.closed, r.cur)
	r.bytes += r.cur.bytes()
	for r.bytes+r.cur.bytes() > r.cfg.MaxBytes && len(r.closed) > 1 {
		old := r.closed[0]
		r.closed[0] = nil
		r.closed = r.closed[1:]
		r.bytes -= old.bytes()
		r.dropped += uint64(old.ticks())
	}
}

// Tick returns the tick the simulation is at after the recorded inputs run.
// Between Record and the simulation's Tick it is one ahead of the simulation.
func (r *Recorder) TickCount() uint64 { return r.tick }

// Start is the oldest tick a Recording can show.
func (r *Recorder) Start() uint64 {
	if len(r.closed) > 0 {
		return r.closed[0].start
	}
	return r.cur.start
}

// Bytes is the memory the recorder holds for snapshots and inputs.
func (r *Recorder) Bytes() int { return r.bytes + r.cur.bytes() }

// Dropped is the number of ticks discarded to stay within MaxBytes.
func (r *Recorder) Dropped() uint64 { return r.dropped }

// Recording returns a view of everything recorded so far. It stays valid and
// unchanged as recording continues. The inputs recorded before the simulation
// has ticked are included, so call it between whole ticks.
func (r *Recorder) Recording() *Recording {
	rec := &Recording{tickHz: r.cfg.TickRate}
	rec.segs = make([]*segment, 0, len(r.closed)+1)
	rec.segs = append(rec.segs, r.closed...)
	rec.segs = append(rec.segs, r.cur.clone())
	rec.markers = append([]Marker(nil), r.markers...)
	return rec
}
