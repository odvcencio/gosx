package replay

import (
	"encoding/binary"
	"errors"
	"sort"

	"m31labs.dev/gosx/sim/simcore"
)

// segment is one keyframe and the inputs of the ticks that follow it, up to
// the next keyframe. A closed segment never changes, so Recordings share them.
type segment struct {
	start    uint64   // tick of the keyframe
	snapshot []byte   // simulation state at start
	offsets  []uint32 // offsets[i] is where tick start+i's inputs begin in inputs
	inputs   []byte   // encoded inputs
}

func (s *segment) ticks() int { return len(s.offsets) }

func (s *segment) end() uint64 { return s.start + uint64(len(s.offsets)) }

func (s *segment) bytes() int {
	return len(s.snapshot) + len(s.inputs) + 4*len(s.offsets) + 64
}

func (s *segment) clone() *segment {
	return &segment{
		start:    s.start,
		snapshot: s.snapshot,
		offsets:  append([]uint32(nil), s.offsets...),
		inputs:   append([]byte(nil), s.inputs...),
	}
}

// appendInputs encodes one tick's inputs: uvarint count, then for each player
// in ascending ID order the uvarint ID length, the ID, the uvarint data length
// and the data. An empty tick costs one byte.
func appendInputs(dst []byte, inputs map[string]simcore.Input) []byte {
	n := 0
	for _, in := range inputs {
		_ = in
		n++
	}
	dst = binary.AppendUvarint(dst, uint64(n))
	if n == 0 {
		return dst
	}
	for _, id := range simcore.SortedPlayerIDs(inputs) {
		in := inputs[id]
		dst = binary.AppendUvarint(dst, uint64(len(id)))
		dst = append(dst, id...)
		dst = binary.AppendUvarint(dst, uint64(len(in.Data)))
		dst = append(dst, in.Data...)
	}
	return dst
}

var errCorrupt = errors.New("replay: corrupt input stream")

// decodeInputs fills dst (cleared first) from b. Input data aliases b.
func decodeInputs(b []byte, dst map[string]simcore.Input, ids *idCache) error {
	for k := range dst {
		delete(dst, k)
	}
	n, w := binary.Uvarint(b)
	if w <= 0 {
		return errCorrupt
	}
	b = b[w:]
	for i := uint64(0); i < n; i++ {
		l, w := binary.Uvarint(b)
		if w <= 0 || uint64(len(b)-w) < l {
			return errCorrupt
		}
		id := ids.get(b[w : w+int(l)])
		b = b[w+int(l):]
		l, w = binary.Uvarint(b)
		if w <= 0 || uint64(len(b)-w) < l {
			return errCorrupt
		}
		dst[id] = simcore.Input{Data: b[w : w+int(l) : w+int(l)]}
		b = b[w+int(l):]
	}
	return nil
}

// idCache interns player ID strings so decoding does not allocate per tick.
type idCache struct{ m map[string]string }

func (c *idCache) get(b []byte) string {
	if s, ok := c.m[string(b)]; ok { // the compiler avoids the allocation here
		return s
	}
	if c.m == nil {
		c.m = map[string]string{}
	}
	s := string(b)
	c.m[s] = s
	return s
}

// Recording is an immutable view of recorded play: keyframes and inputs for
// the ticks [Start, End]. A Recorder returns one from Recording, and Clip and
// Unmarshal produce them too. Give it to NewPlayer.
type Recording struct {
	segs    []*segment
	markers []Marker
	tickHz  float64
}

// Start is the first tick the recording can show.
func (r *Recording) Start() uint64 {
	if r == nil || len(r.segs) == 0 {
		return 0
	}
	return r.segs[0].start
}

// End is the last tick the recording can show: the state after the last
// recorded input.
func (r *Recording) End() uint64 {
	if r == nil || len(r.segs) == 0 {
		return 0
	}
	return r.segs[len(r.segs)-1].end()
}

// Keyframes returns the ticks that hold a snapshot.
func (r *Recording) Keyframes() []uint64 {
	if r == nil {
		return nil
	}
	out := make([]uint64, len(r.segs))
	for i, s := range r.segs {
		out[i] = s.start
	}
	return out
}

// Bytes is the memory the recording's snapshots and inputs hold.
func (r *Recording) Bytes() int {
	if r == nil {
		return 0
	}
	n := 0
	for _, s := range r.segs {
		n += s.bytes()
	}
	return n
}

// TickRate is the ticks per second recorded with the clip, or 0 if unknown.
func (r *Recording) TickRate() float64 {
	if r == nil {
		return 0
	}
	return r.tickHz
}

// segmentFor returns the index of the segment whose keyframe is the nearest at
// or before tick, or -1 if tick is before the recording.
func (r *Recording) segmentFor(tick uint64) int {
	i := sort.Search(len(r.segs), func(i int) bool { return r.segs[i].start > tick })
	return i - 1
}
