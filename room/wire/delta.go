package wire

import (
	"encoding/binary"
	"errors"
)

// Delta encoding
//
// A delta rebuilds a target byte string from a base (the keyframe state) and a
// list of operations:
//
//	uvarint targetLen
//	op*:   uvarint h
//	       h even: literal of h>>1 bytes, which follow
//	       h odd:  copy of (h>>1)+minMatch bytes from base at outPos+rel,
//	               where rel is the zigzag varint that follows
//
// outPos is the number of bytes produced so far. A snapshot whose layout does
// not move between ticks (fixed-size binary records) encodes each unchanged run
// as a copy with rel 0, a two- or three-byte op. A layout that shifts (JSON
// whose number widths change) still finds its runs through a hash index of the
// base, like LZ77 with the base as an external dictionary.
//
// Copies read only the base, never the output, so decoding needs no overlap
// rules and every op is bounds-checked against both buffers.

const (
	minMatch       = 4
	minTableBits   = 8
	maxTableBits   = 16
	maxMatchSearch = 1 << 20 // a match longer than this is split into ops
)

// ErrDelta reports a delta that does not decode against its base.
var ErrDelta = errors.New("wire: invalid delta")

// DeltaEncoder encodes deltas against one base. Index a base with SetBase,
// then call Append for each target. Reusing an encoder does not allocate once
// its index has grown to fit the largest base. Not safe for concurrent use.
type DeltaEncoder struct {
	base  []byte
	table []int32 // hash of 4 base bytes -> base position + 1
	shift uint
}

// SetBase indexes base. The encoder keeps base, so the caller must not change
// it until the next SetBase.
func (e *DeltaEncoder) SetBase(base []byte) {
	e.base = base
	bits := minTableBits
	for bits < maxTableBits && 1<<bits < len(base) {
		bits++
	}
	size := 1 << bits
	if cap(e.table) < size {
		e.table = make([]int32, size)
	} else {
		e.table = e.table[:size]
		clear(e.table)
	}
	e.shift = uint(32 - bits)
	// Index backwards so the earliest position of a repeated window wins; that
	// keeps copies of a stable layout close to rel 0.
	for i := len(base) - minMatch; i >= 0; i-- {
		e.table[hash4(base[i:], e.shift)] = int32(i + 1)
	}
}

// Base returns the indexed base.
func (e *DeltaEncoder) Base() []byte { return e.base }

func hash4(b []byte, shift uint) uint32 {
	return (binary.LittleEndian.Uint32(b) * 2654435761) >> shift
}

func matchLen(a, b []byte) int {
	n := min(len(a), len(b), maxMatchSearch)
	i := 0
	for i+8 <= n && binary.LittleEndian.Uint64(a[i:]) == binary.LittleEndian.Uint64(b[i:]) {
		i += 8
	}
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}

// Append encodes target as a delta against the indexed base and appends it to
// dst.
func (e *DeltaEncoder) Append(dst, target []byte) []byte {
	dst = binary.AppendUvarint(dst, uint64(len(target)))
	base := e.base
	rep := 0 // offset of the previous copy: base position minus output position
	lit := 0
	i := 0
	for i < len(target) {
		best, bestLen := 0, 0
		if p := i + rep; p >= 0 && p < len(base) {
			if l := matchLen(base[p:], target[i:]); l >= minMatch {
				best, bestLen = p, l
			}
		}
		if bestLen < 32 && i+minMatch <= len(target) && len(e.table) > 0 {
			if c := int(e.table[hash4(target[i:], e.shift)]) - 1; c >= 0 {
				if l := matchLen(base[c:], target[i:]); l > bestLen {
					best, bestLen = c, l
				}
			}
		}
		if bestLen < minMatch {
			i++
			continue
		}
		if lit < i {
			dst = appendLiteral(dst, target[lit:i])
		}
		rel := best - i
		dst = binary.AppendUvarint(dst, uint64(bestLen-minMatch)<<1|1)
		dst = binary.AppendVarint(dst, int64(rel))
		rep = rel
		i += bestLen
		lit = i
	}
	if lit < len(target) {
		dst = appendLiteral(dst, target[lit:])
	}
	return dst
}

func appendLiteral(dst, lit []byte) []byte {
	dst = binary.AppendUvarint(dst, uint64(len(lit))<<1)
	return append(dst, lit...)
}

// ApplyDelta rebuilds the target of delta against base into dst[:0] and
// returns it. It grows dst only when its capacity is short, so a caller that
// reuses dst decodes without allocating. maxLen bounds the target; zero means
// MaxStateSize.
func ApplyDelta(dst, base, delta []byte, maxLen int) ([]byte, error) {
	if maxLen <= 0 {
		maxLen = MaxStateSize
	}
	total, n := binary.Uvarint(delta)
	if n <= 0 || total > uint64(maxLen) {
		return dst[:0], ErrDelta
	}
	pos := n
	if cap(dst) < int(total) {
		dst = make([]byte, 0, total)
	}
	out := dst[:0]
	for len(out) < int(total) {
		h, n := binary.Uvarint(delta[pos:])
		if n <= 0 {
			return dst[:0], ErrDelta
		}
		pos += n
		room := uint64(int(total) - len(out))
		if h&1 == 0 {
			size := h >> 1
			if size == 0 || size > room || size > uint64(len(delta)-pos) {
				return dst[:0], ErrDelta
			}
			out = append(out, delta[pos:pos+int(size)]...)
			pos += int(size)
			continue
		}
		size := h>>1 + minMatch
		if size > room {
			return dst[:0], ErrDelta
		}
		rel, n := binary.Varint(delta[pos:])
		if n <= 0 {
			return dst[:0], ErrDelta
		}
		pos += n
		src := int64(len(out)) + rel
		if src < 0 || src > int64(len(base)) || uint64(int64(len(base))-src) < size {
			return dst[:0], ErrDelta
		}
		out = append(out, base[src:src+int64(size)]...)
	}
	if pos != len(delta) {
		return dst[:0], ErrDelta
	}
	return out, nil
}
