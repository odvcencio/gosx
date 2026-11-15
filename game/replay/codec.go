package replay

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Clip file format, version 1 (little endian):
//
//	8  bytes  magic "GXRPLY\x00\x00"
//	2         version (1)
//	2         flags (0)
//	8         tick rate (float64 bits)
//	8         first tick the clip shows
//	8         last tick the clip shows
//	4         segment count
//	per segment:
//	  8       keyframe tick
//	  4       snapshot length, then the snapshot
//	  4       tick count
//	  4       input stream length, then the stream (see appendInputs)
//	4         marker count
//	per marker: 8 tick, 2 label length, label
//	8         FNV-1a 64 of every byte above
const (
	clipMagic   = "GXRPLY\x00\x00"
	clipVersion = 1
)

// Errors from Unmarshal. Test with errors.Is.
var (
	ErrFormat  = errors.New("replay: malformed clip")
	ErrVersion = errors.New("replay: unsupported clip version")
)

// Marshal encodes the recording in the versioned clip format.
func (r *Recording) Marshal() []byte {
	n := len(clipMagic) + 40 + r.Bytes()
	b := make([]byte, 0, n)
	b = append(b, clipMagic...)
	b = binary.LittleEndian.AppendUint16(b, clipVersion)
	b = binary.LittleEndian.AppendUint16(b, 0)
	b = binary.LittleEndian.AppendUint64(b, math.Float64bits(r.tickHz))
	b = binary.LittleEndian.AppendUint64(b, r.Start())
	b = binary.LittleEndian.AppendUint64(b, r.End())
	b = binary.LittleEndian.AppendUint32(b, uint32(len(r.segs)))
	for _, s := range r.segs {
		b = binary.LittleEndian.AppendUint64(b, s.start)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(s.snapshot)))
		b = append(b, s.snapshot...)
		b = binary.LittleEndian.AppendUint32(b, uint32(s.ticks()))
		b = binary.LittleEndian.AppendUint32(b, uint32(len(s.inputs)))
		b = append(b, s.inputs...)
	}
	b = binary.LittleEndian.AppendUint32(b, uint32(len(r.markers)))
	for _, m := range r.markers {
		label := m.Label
		if len(label) > math.MaxUint16 {
			label = label[:math.MaxUint16]
		}
		b = binary.LittleEndian.AppendUint64(b, m.Tick)
		b = binary.LittleEndian.AppendUint16(b, uint16(len(label)))
		b = append(b, label...)
	}
	return binary.LittleEndian.AppendUint64(b, HashBytes(b))
}

type reader struct {
	b   []byte
	bad bool
}

func (r *reader) take(n uint64) []byte {
	if r.bad || n > uint64(len(r.b)) {
		r.bad = true
		return nil
	}
	out := r.b[:n]
	r.b = r.b[n:]
	return out
}
func (r *reader) u16() uint16 {
	if b := r.take(2); b != nil {
		return binary.LittleEndian.Uint16(b)
	}
	return 0
}
func (r *reader) u32() uint32 {
	if b := r.take(4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}
func (r *reader) u64() uint64 {
	if b := r.take(8); b != nil {
		return binary.LittleEndian.Uint64(b)
	}
	return 0
}

// Unmarshal decodes a clip written by Marshal. It checks the hash, the
// per-tick input encoding and the tick ranges, and copies what it keeps.
func Unmarshal(data []byte) (*Recording, error) {
	if len(data) < len(clipMagic)+4+8 || string(data[:len(clipMagic)]) != clipMagic {
		return nil, ErrFormat
	}
	body, sum := data[:len(data)-8], binary.LittleEndian.Uint64(data[len(data)-8:])
	if HashBytes(body) != sum {
		return nil, fmt.Errorf("%w: checksum mismatch", ErrFormat)
	}
	r := &reader{b: body[len(clipMagic):]}
	if v := r.u16(); v != clipVersion {
		if r.bad {
			return nil, ErrFormat
		}
		return nil, fmt.Errorf("%w: %d", ErrVersion, v)
	}
	r.u16()
	rec := &Recording{tickHz: math.Float64frombits(r.u64())}
	rec.lo, rec.hi = r.u64(), r.u64()
	n := r.u32()
	if r.bad || n == 0 || uint64(n) > uint64(len(r.b)) {
		return nil, ErrFormat
	}
	for i := uint32(0); i < n; i++ {
		s := &segment{start: r.u64()}
		s.snapshot = append([]byte(nil), r.take(uint64(r.u32()))...)
		ticks := r.u32()
		s.inputs = append([]byte(nil), r.take(uint64(r.u32()))...)
		if r.bad || uint64(ticks) > uint64(len(s.inputs)) {
			return nil, ErrFormat
		}
		offs, err := scanOffsets(s.inputs, int(ticks))
		if err != nil {
			return nil, err
		}
		s.offsets = offs
		if i > 0 && s.start != rec.segs[i-1].end() {
			return nil, fmt.Errorf("%w: segments are not contiguous", ErrFormat)
		}
		rec.segs = append(rec.segs, s)
	}
	m := r.u32()
	if r.bad || uint64(m) > uint64(len(r.b)) {
		return nil, ErrFormat
	}
	for i := uint32(0); i < m; i++ {
		tick := r.u64()
		label := r.take(uint64(r.u16()))
		if r.bad {
			return nil, ErrFormat
		}
		rec.markers = append(rec.markers, Marker{Tick: tick, Label: string(label)})
	}
	if r.bad || len(r.b) != 0 {
		return nil, ErrFormat
	}
	first, last := rec.segs[0], rec.segs[len(rec.segs)-1]
	if rec.lo < first.start || rec.hi > last.end() || rec.lo > rec.hi {
		return nil, fmt.Errorf("%w: tick range", ErrFormat)
	}
	return rec, nil
}

// scanOffsets walks an input stream of ticks entries and returns where each
// starts. It fails if the stream does not hold exactly that many.
func scanOffsets(b []byte, ticks int) ([]uint32, error) {
	offs := make([]uint32, 0, ticks)
	pos := 0
	for t := 0; t < ticks; t++ {
		offs = append(offs, uint32(pos))
		n, w := binary.Uvarint(b[pos:])
		if w <= 0 {
			return nil, ErrFormat
		}
		pos += w
		for i := uint64(0); i < n; i++ {
			for k := 0; k < 2; k++ { // id, then data
				l, w := binary.Uvarint(b[pos:])
				if w <= 0 || uint64(len(b)-pos-w) < l {
					return nil, ErrFormat
				}
				pos += w + int(l)
			}
		}
	}
	if pos != len(b) {
		return nil, ErrFormat
	}
	return offs, nil
}
