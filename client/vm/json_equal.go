package vm

import "math"

// EqualJSON compares the normalized JSON values held by two Values without
// expression-language coercions. It is intended for state snapshot writes,
// not event delivery. Maps and arrays are read by value: a shared payload may
// have been edited in place since the previous write. Cycles and excessive
// nesting decline equality, so they cannot suppress a later notification.
func EqualJSON(a, b Value) bool { return equalJSONAtDepth(a, b, 0) }

func equalJSONAtDepth(a, b Value, depth int) bool {
	// The tag contains kind, boolean payload and control bits. Compare it once
	// rather than copying Values through exported accessors at every level.
	// n carries array/string length or the low word of a wide integer.
	if depth > maxValueRecursionDepth || a.Type != b.Type || a.tag != b.tag || a.n != b.n {
		return false
	}
	if a.isList() {
		left, right := a.list(), b.list()
		if (left == nil) != (right == nil) {
			return false
		}
		for i := range left {
			if !equalJSONAtDepth(left[i], right[i], depth+1) {
				return false
			}
		}
		return true
	}
	if a.isMap() {
		left, right := a.dict(), b.dict()
		if (left == nil) != (right == nil) || len(left) != len(right) {
			return false
		}
		for key, value := range left {
			other, ok := right[key]
			if !ok || !equalJSONAtDepth(value, other, depth+1) {
				return false
			}
		}
		return true
	}
	if a.kind() == kindClosure {
		return false
	}
	bits := math.Float64bits(a.num)
	return a.text() == b.text() && bits == math.Float64bits(b.num) && bits&0x7ff0000000000000 != 0x7ff0000000000000
}
