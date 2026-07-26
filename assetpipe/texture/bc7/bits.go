package bc7

// BlockBytes is the size of one BC7 block. Every mode costs the same 16 bytes.
const BlockBytes = 16

// blockBits is the bit budget of one block.
const blockBits = BlockBytes * 8

// bitWriter fills one block least significant bit first, starting at byte 0
// bit 0. That order is what the BC7 decode rules assume, and it is also the
// order a little-endian load of the 128-bit block produces.
type bitWriter struct {
	buf [BlockBytes]byte
	pos int
}

// put appends the low n bits of value.
//
// The loop writes one bit at a time on purpose. A block costs 128 bits, so the
// whole encode spends well under one percent of its time here, and a bit at a
// time cannot straddle a word boundary wrongly.
func (w *bitWriter) put(value uint32, n int) {
	for i := 0; i < n; i++ {
		if value>>uint(i)&1 != 0 {
			p := w.pos + i
			w.buf[p>>3] |= 1 << uint(p&7)
		}
	}
	w.pos += n
}

// reset clears the block and rewinds to bit 0.
func (w *bitWriter) reset() {
	w.buf = [BlockBytes]byte{}
	w.pos = 0
}

// bitReader reads one block least significant bit first.
type bitReader struct {
	buf []byte
	pos int
}

// get returns the next n bits. It returns zero past the end of the block, so a
// truncated input cannot panic.
func (r *bitReader) get(n int) uint32 {
	var out uint32
	for i := 0; i < n; i++ {
		p := r.pos + i
		if p >= len(r.buf)*8 {
			break
		}
		if r.buf[p>>3]>>uint(p&7)&1 != 0 {
			out |= 1 << uint(i)
		}
	}
	r.pos += n
	return out
}
