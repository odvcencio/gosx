package meshopt

import "fmt"

const indexHeader = 0xe1

type indexState struct {
	edgeA, edgeB [16]int64
	fifo         [16]int64
	edgeOffset   int
	fifoOffset   int
	next, last   int64
}

func newIndexState() *indexState {
	s := &indexState{}
	for i := range s.edgeA {
		s.edgeA[i], s.edgeB[i], s.fifo[i] = -1, -1, -1
	}
	return s
}

func (s *indexState) pushEdge(a, b int64) {
	s.edgeA[s.edgeOffset], s.edgeB[s.edgeOffset] = a, b
	s.edgeOffset = (s.edgeOffset + 1) & 15
}

func (s *indexState) pushVertex(v int64, advance bool) {
	s.fifo[s.fifoOffset] = v
	if advance {
		s.fifoOffset = (s.fifoOffset + 1) & 15
	}
}

func zigzag(d int64) uint32 { return uint32((d << 1) ^ (d >> 31)) }

func varint(dst []byte, v uint32) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

func varintLen(v uint32) int {
	n := 1
	for ; v >= 0x80; v >>= 7 {
		n++
	}
	return n
}

// EncodeIndexBuffer compresses a triangle list. vertexCount only bounds the
// check that every index names a vertex.
//
// A triangle that shares an edge with one of the last 16 triangles costs one
// code byte when its third vertex is new, recent, or next to the last explicit
// index. Any other triangle is written with explicit indices. The encoder may
// rotate a triangle, which keeps its winding.
func EncodeIndexBuffer(indices []uint32, vertexCount int) ([]byte, error) {
	if len(indices)%3 != 0 {
		return nil, fmt.Errorf("meshopt: %d indices is not a whole number of triangles", len(indices))
	}
	for _, v := range indices {
		if int(v) >= vertexCount {
			return nil, fmt.Errorf("meshopt: index %d is outside %d vertices", v, vertexCount)
		}
	}
	s := newIndexState()
	codes := make([]byte, 0, len(indices)/3)
	var data []byte
	for i := 0; i < len(indices); i += 3 {
		tri := [3]int64{int64(indices[i]), int64(indices[i+1]), int64(indices[i+2])}
		if code, extra, ok := s.encodeShared(tri); ok {
			codes = append(codes, code)
			data = append(data, extra...)
			continue
		}
		code, extra := s.encodeExplicit(tri)
		codes = append(codes, code)
		data = append(data, extra...)
	}
	out := append([]byte{indexHeader}, codes...)
	out = append(out, data...)
	return append(out, make([]byte, 16)...), nil // the aux table, unused by these codes
}

// encodeShared tries each rotation against the edge FIFO and keeps the
// cheapest. It applies the state change only when it succeeds.
func (s *indexState) encodeShared(tri [3]int64) (byte, []byte, bool) {
	bestCost := 1 << 30
	var best struct {
		fe, fec  int
		a, b, c  int64
		explicit bool
	}
	for rot := 0; rot < 3; rot++ {
		a, b, c := tri[rot], tri[(rot+1)%3], tri[(rot+2)%3]
		for fe := 0; fe < 15; fe++ { // 15 would collide with the 0xf0 codes
			slot := (s.edgeOffset - 1 - fe) & 15
			if s.edgeA[slot] != a || s.edgeB[slot] != b {
				continue
			}
			fec, cost, explicit := s.thirdVertex(c)
			if cost < bestCost {
				bestCost = cost
				best.fe, best.fec, best.a, best.b, best.c, best.explicit = fe, fec, a, b, c, explicit
			}
			break
		}
	}
	if bestCost == 1<<30 {
		return 0, nil, false
	}
	var extra []byte
	switch {
	case best.fec == 0:
		s.next++
		s.pushVertex(best.c, true)
	case best.fec <= 12:
	case best.fec == 13 || best.fec == 14:
		s.last = best.c
		s.pushVertex(best.c, true)
	default:
		extra = varint(extra, zigzag(best.c-s.last))
		s.last = best.c
		s.pushVertex(best.c, true)
	}
	s.pushEdge(best.c, best.b)
	s.pushEdge(best.a, best.c)
	return byte(best.fe<<4 | best.fec), extra, true
}

// thirdVertex chooses the 4 bit code for the vertex across a shared edge.
func (s *indexState) thirdVertex(c int64) (fec, cost int, explicit bool) {
	if c == s.next {
		return 0, 0, false
	}
	for k := 1; k <= 12; k++ {
		if s.fifo[(s.fifoOffset-1-k)&15] == c {
			return k, 0, false
		}
	}
	if c == s.last-1 {
		return 13, 0, false
	}
	if c == s.last+1 {
		return 14, 0, false
	}
	return 15, varintLen(zigzag(c - s.last)), true
}

// encodeExplicit writes a triangle that shares no edge with the FIFO. The code
// byte says whether the first vertex is new (0xfe) or explicit (0xff); a packed
// byte gives the second and third vertex as new (0), recent (1 to 14) or
// explicit (15).
func (s *indexState) encodeExplicit(tri [3]int64) (byte, []byte) {
	type plan struct {
		code   byte
		packed byte
		cost   int
		rot    int
	}
	var best plan
	best.cost = 1 << 30
	for rot := 0; rot < 3; rot++ {
		a, b, c := tri[rot], tri[(rot+1)%3], tri[(rot+2)%3]
		nx := s.next
		cost := 0
		code := byte(0xfe)
		if a == nx {
			nx++
		} else {
			code = 0xff
			cost += varintLen(zigzag(a - s.last))
		}
		pick := func(v int64) int {
			if v == nx {
				nx++
				return 0
			}
			for f := 1; f <= 14; f++ {
				if s.fifo[(s.fifoOffset-f)&15] == v {
					return f
				}
			}
			cost += 2
			return 15
		}
		f1 := pick(b)
		f2 := pick(c)
		if cost < best.cost {
			best = plan{code: code, packed: byte(f1<<4 | f2), cost: cost, rot: rot}
		}
	}
	a, b, c := tri[best.rot], tri[(best.rot+1)%3], tri[(best.rot+2)%3]
	f1, f2 := int(best.packed>>4), int(best.packed&15)
	extra := []byte{best.packed}
	nx := s.next
	if best.code == 0xfe {
		nx++
	} else {
		extra = varint(extra, zigzag(a-s.last))
		s.last = a
	}
	if f1 == 0 {
		nx++
	} else if f1 == 15 {
		extra = varint(extra, zigzag(b-s.last))
		s.last = b
	}
	if f2 == 0 {
		nx++
	} else if f2 == 15 {
		extra = varint(extra, zigzag(c-s.last))
		s.last = c
	}
	s.next = nx
	s.pushVertex(a, true)
	s.pushVertex(b, f1 == 0 || f1 == 15)
	s.pushVertex(c, f2 == 0 || f2 == 15)
	s.pushEdge(b, a)
	s.pushEdge(c, b)
	s.pushEdge(a, c)
	return best.code, extra
}

// DecodeIndexBuffer reverses EncodeIndexBuffer. It exists to verify output; it
// follows the same rules as the scene loader's decoder.
func DecodeIndexBuffer(src []byte, count int) ([]uint32, error) {
	if count%3 != 0 || len(src) < 1+count/3+16 || src[0]&0xf0 != 0xe0 || src[0]&0x0f > 1 {
		return nil, fmt.Errorf("meshopt: bad index buffer")
	}
	s := newIndexState()
	out := make([]uint32, 0, count)
	code, pos := 1, 1+count/3
	aux := len(src) - 16
	readVarint := func() (uint32, error) {
		var v uint32
		for shift := 0; shift < 35; shift += 7 {
			if pos >= len(src) {
				return 0, fmt.Errorf("meshopt: truncated index data")
			}
			b := src[pos]
			pos++
			v |= uint32(b&0x7f) << shift
			if b < 0x80 {
				return v, nil
			}
		}
		return 0, fmt.Errorf("meshopt: index varint is too long")
	}
	free := func() (int64, error) {
		v, err := readVarint()
		if err != nil {
			return 0, err
		}
		s.last = int64(uint32(s.last) + (v>>1 ^ -(v & 1)))
		return s.last, nil
	}
	for i := 0; i < count; i += 3 {
		tri := src[code]
		code++
		var a, b, c int64
		switch {
		case tri < 0xf0:
			slot := (s.edgeOffset - 1 - int(tri>>4)) & 15
			a, b = s.edgeA[slot], s.edgeB[slot]
			switch fec := int(tri & 15); {
			case fec == 0:
				c = s.next
				s.next++
				s.pushVertex(c, true)
			case fec <= 12:
				c = s.fifo[(s.fifoOffset-1-fec)&15]
			case fec == 13 || fec == 14:
				c = s.last - 1
				if fec == 14 {
					c = s.last + 1
				}
				s.last = c
				s.pushVertex(c, true)
			default:
				var err error
				if c, err = free(); err != nil {
					return nil, err
				}
				s.pushVertex(c, true)
			}
			s.pushEdge(c, b)
			s.pushEdge(a, c)
		case tri < 0xfe:
			t := src[aux+int(tri&15)]
			feb, fc := int(t>>4), int(t&15)
			a = s.next
			s.next++
			if feb == 0 {
				b = s.next
				s.next++
			} else {
				b = s.fifo[(s.fifoOffset-feb)&15]
			}
			if fc == 0 {
				c = s.next
				s.next++
			} else {
				c = s.fifo[(s.fifoOffset-fc)&15]
			}
			s.pushVertex(a, true)
			if feb == 0 {
				s.pushVertex(b, true)
			}
			if fc == 0 {
				s.pushVertex(c, true)
			}
			s.pushEdge(b, a)
			s.pushEdge(c, b)
			s.pushEdge(a, c)
		default:
			if pos >= len(src) {
				return nil, fmt.Errorf("meshopt: truncated index data")
			}
			packed := src[pos]
			pos++
			f0, f1, f2 := 0, int(packed>>4), int(packed&15)
			if tri == 0xff {
				f0 = 15
			}
			pickVertex := func(f int) int64 {
				switch f {
				case 0:
					v := s.next
					s.next++
					return v
				case 15:
					return 0
				}
				return s.fifo[(s.fifoOffset-f)&15]
			}
			a, b, c = pickVertex(f0), pickVertex(f1), pickVertex(f2)
			var err error
			for _, p := range []struct {
				f int
				v *int64
			}{{f0, &a}, {f1, &b}, {f2, &c}} {
				if p.f == 15 {
					if *p.v, err = free(); err != nil {
						return nil, err
					}
				}
			}
			s.pushVertex(a, true)
			s.pushVertex(b, f1 == 0 || f1 == 15)
			s.pushVertex(c, f2 == 0 || f2 == 15)
			s.pushEdge(b, a)
			s.pushEdge(c, b)
			s.pushEdge(a, c)
		}
		out = append(out, uint32(a), uint32(b), uint32(c))
	}
	return out, nil
}
