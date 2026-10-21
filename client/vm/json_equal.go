package vm

import "math"

// EqualJSON compares the normalized JSON values held by two Values without
// expression-language coercions. It is intended for state snapshot writes,
// not event delivery. Maps and arrays are read by value: a shared payload may
// have been edited in place since the previous write. Cycles and excessive
// nesting decline equality, so they cannot suppress a later notification.
func EqualJSON(a, b Value) bool { return equalJSONAtDepth(a, b, 0) }

func equalJSONAtDepth(a, b Value, depth int) bool {
	if depth > maxValueRecursionDepth || a.Type != b.Type || a.kind() != b.kind() || a.Control() != b.Control() {
		return false
	}
	if a.isList() {
		left, right := a.list(), b.list()
		if (left == nil) != (right == nil) || len(left) != len(right) {
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
	return a.text() == b.text() && a.truth() == b.truth() &&
		!math.IsNaN(a.num) && !math.IsInf(a.num, 0) && math.Float64bits(a.num) == math.Float64bits(b.num)
}
