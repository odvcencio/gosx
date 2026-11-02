package vm

import "math"

// valueHash is a 128-bit digest of everything an expression can observe in a
// Value: its static type, tag (kind, boolean payload, control bits), length,
// numeric payload, text, and the same for every element or field.
//
// Subtree reuse compares digests instead of holding a copy of a composite
// value. A digest taken when a subtree was evaluated describes the content that
// evaluation read. If the value is edited in place afterwards (OpFieldSet and
// OpIndexSet do that), the next digest differs, so the in-place edit is seen
// and the subtree is evaluated again. A stored reference could not promise
// that, because it would alias the edited storage.
//
// The digest is two independent 64-bit lanes, so two different values collide
// with probability about 2^-128 per comparison.
type valueHash struct{ a, b uint64 }

const (
	hashPrimeA = 0x100000001b3
	hashPrimeB = 0x9e3779b97f4a7c15
)

func mix64(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

func (h *valueHash) word(x uint64) {
	h.a = (h.a ^ x) * hashPrimeA
	h.a ^= h.a >> 29
	h.b = mix64(h.b + x*hashPrimeB + 0x632be59bd9b4e019)
}

func (h *valueHash) text(s string) {
	a, b := h.a, h.b
	for i := 0; i < len(s); i++ {
		c := uint64(s[i])
		a = (a ^ c) * hashPrimeA
		b = (b + c + 1) * hashPrimeB
		b ^= b >> 32
	}
	h.a, h.b = a, b
	h.word(uint64(len(s)))
}

// hashValue digests v. ok is false when v holds something that cannot be
// compared by content (a closure) or nests deeper than the evaluator allows;
// the caller then treats the input as changed.
func hashValue(v Value) (valueHash, bool) {
	var h valueHash
	h.a, h.b = 0xcbf29ce484222325, 0x84222325cbf29ce4
	ok := h.value(&v, 0)
	return h, ok
}

func (h *valueHash) value(v *Value, depth int) bool {
	if depth > maxValueRecursionDepth {
		return false
	}
	h.word(uint64(v.Type)<<8 | uint64(v.tag))
	h.word(uint64(int64(v.n)))
	h.word(math.Float64bits(v.num))
	switch v.kind() {
	case kindClosure:
		return false
	case kindString:
		h.text(v.text())
	case kindArray:
		items := v.list()
		h.word(uint64(len(items)))
		if items == nil {
			h.word(1)
		}
		for i := range items {
			if !h.value(&items[i], depth+1) {
				return false
			}
		}
	case kindMap:
		fields := v.dict()
		h.word(uint64(len(fields)))
		if fields == nil {
			h.word(1)
		}
		// Map order is random, so combine the entries with operations that do
		// not depend on order.
		var sumA, sumB uint64
		for key, field := range fields {
			e := valueHash{a: 0x1234567890abcdef, b: 0xfedcba0987654321}
			e.text(key)
			f := field
			if !e.value(&f, depth+1) {
				return false
			}
			sumA += mix64(e.a)
			sumB ^= mix64(e.b + 0x2545f4914f6cdd1d)
		}
		h.word(sumA)
		h.word(sumB)
	}
	return true
}
