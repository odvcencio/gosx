package replay

import (
	"errors"
	"fmt"
)

// Marker labels a tick of interest. Game code adds them with Recorder.Mark.
type Marker struct {
	Tick  uint64
	Label string
}

// Markers returns the recording's markers in the order they were made.
func (r *Recording) Markers() []Marker {
	if r == nil {
		return nil
	}
	return append([]Marker(nil), r.markers...)
}

// Clip returns a self-contained recording of the ticks [from, to], clamped to
// the ticks r holds. It keeps the keyframe at or before from and the inputs
// through to, so a Player can seek anywhere in the range and nothing else is
// needed. Markers inside the range come along. Marshal turns it into bytes to
// save or share.
func (r *Recording) Clip(from, to uint64) (*Recording, error) {
	if r == nil || len(r.segs) == 0 {
		return nil, errors.New("replay: empty recording")
	}
	if from < r.lo {
		from = r.lo
	}
	if to > r.hi {
		to = r.hi
	}
	if from > to {
		return nil, fmt.Errorf("%w: empty range", ErrOutOfRange)
	}
	i, j := r.segmentFor(from), r.segmentFor(to)
	segs := append([]*segment(nil), r.segs[i:j+1]...)
	if last := segs[len(segs)-1]; len(segs) > 1 && last.start == to {
		segs = segs[:len(segs)-1] // the previous segment already reaches `to`
	}
	last := segs[len(segs)-1]
	if need := int(to - last.start); need < last.ticks() {
		segs[len(segs)-1] = last.truncated(need)
	}
	c := newRecording(segs, nil, r.tickHz)
	c.lo, c.hi = from, to
	for _, m := range r.markers {
		if m.Tick >= from && m.Tick <= to {
			c.markers = append(c.markers, m)
		}
	}
	return c, nil
}

// Highlight returns a clip around a marker: before ticks ahead of it and after
// ticks behind, clamped to the recording.
func (r *Recording) Highlight(m Marker, before, after uint64) (*Recording, error) {
	from := uint64(0)
	if m.Tick > before {
		from = m.Tick - before
	}
	return r.Clip(from, m.Tick+after)
}
