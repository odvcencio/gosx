package motion

// WriteBuf is a caller-owned flat float64 buffer for per-frame animation writes.
// Each write is packed as [targetID, propID, arity, v.F...].
// The buffer is reused every frame via Reset; pushing does not allocate when
// capacity is sufficient — zero heap alloc on the hot path is the design goal.
type WriteBuf struct {
	F []float64 // packed buffer
	n int       // write cursor (number of floats written)
}

// NewWriteBuf preallocates a buffer with room for capacity float64s.
func NewWriteBuf(capacity int) *WriteBuf {
	return &WriteBuf{F: make([]float64, capacity)}
}

// Reset rewinds the write cursor to zero without freeing the buffer (no alloc).
func (w *WriteBuf) Reset() {
	w.n = 0
}

// Push packs one write: [targetID, propID, arity, v.F...].
// Grows the backing buffer (one allocation) ONLY when capacity is insufficient;
// otherwise zero alloc.
func (w *WriteBuf) Push(targetID, propID int, v Value) {
	need := 3 + len(v.F)
	if w.n+need > len(w.F) {
		// Grow: double the current length (or use n+need if that's larger).
		newLen := len(w.F) * 2
		if newLen < w.n+need {
			newLen = w.n + need
		}
		grown := make([]float64, newLen)
		copy(grown, w.F[:w.n])
		w.F = grown
	}
	w.F[w.n] = float64(targetID)
	w.F[w.n+1] = float64(propID)
	w.F[w.n+2] = float64(v.Arity)
	copy(w.F[w.n+3:], v.F)
	w.n += need
}

// Writes returns the packed slice [0:n] as a view into the backing buffer (no copy).
func (w *WriteBuf) Writes() []float64 {
	return w.F[:w.n]
}

// Len returns the number of packed float64s written since the last Reset.
func (w *WriteBuf) Len() int {
	return w.n
}
