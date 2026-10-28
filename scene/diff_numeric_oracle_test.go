package scene

import (
	"bytes"
	"math"
	"reflect"
)

// This file keeps the reflection walk exactly as it stood before provenJSONEqual
// gained typed integer loops, the shared-memory shortcut and the typed walk of
// map[string]any. It is the oracle: the new walk must give its answer for every
// input, so the tests in diff_numeric_test.go compare the two. Do not edit it to
// follow a change in diff.go; that would let a wrong change agree with itself.
// Only the names differ from the original: each has a reference prefix, and each
// refers to the other reference functions, so the oracle never calls new code.

// referenceProvenJSONEqual is the original provenJSONEqual, with its comment.
//
// provenJSONEqual reports whether two values of the same type are certain to
// encode to identical JSON. It returns false whenever it cannot prove that, so a
// false answer means "ask the encoder", not "the values differ". Every rule below
// exists to keep that guarantee, because a wrong true is a lost edit:
//
//   - A float must match bit for bit. Negative zero and positive zero compare
//     equal under ==, and encode as "-0" and "0", so bit equality is the test. A
//     NaN or an infinity is never proven equal, because the encoder rejects both
//     and sceneRecordMarshalEqual then reports unequal.
//   - A nil slice and an empty slice must not compare equal. They encode as null
//     and [] unless the field carries omitempty, so only the encoder knows.
//   - An interface must hold the same dynamic type on both sides. Two different
//     types can hold equal fields and still write different keys.
//   - An unexported field is compared too. The encoder ignores it, so comparing
//     it can only make this function stricter, never wrong. The walk never calls
//     Interface(), which is what an unexported field would reject.
//   - A kind this walk does not model, a channel or a function for example,
//     returns false.
func referenceProvenJSONEqual(a, b reflect.Value) bool {
	if a.Kind() != b.Kind() || a.Type() != b.Type() {
		return false
	}
	switch a.Kind() {
	case reflect.Bool:
		return a.Bool() == b.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return a.Int() == b.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return a.Uint() == b.Uint()
	case reflect.Float32, reflect.Float64:
		left, right := a.Float(), b.Float()
		if math.IsNaN(left) || math.IsNaN(right) || math.IsInf(left, 0) || math.IsInf(right, 0) {
			return false
		}
		return math.Float64bits(left) == math.Float64bits(right)
	case reflect.String:
		return a.String() == b.String()
	case reflect.Struct:
		for index := range a.NumField() {
			if !referenceProvenJSONEqual(a.Field(index), b.Field(index)) {
				return false
			}
		}
		return true
	case reflect.Slice:
		if a.IsNil() != b.IsNil() {
			return false
		}
		if a.Len() != b.Len() {
			return false
		}
		if a.Len() == 0 {
			return true
		}
		if equal, decided := referenceProvenNumericSliceEqual(a, b); decided {
			return equal
		}
		for index := range a.Len() {
			if !referenceProvenJSONEqual(a.Index(index), b.Index(index)) {
				return false
			}
		}
		return true
	case reflect.Array:
		for index := range a.Len() {
			if !referenceProvenJSONEqual(a.Index(index), b.Index(index)) {
				return false
			}
		}
		return true
	case reflect.Map:
		if a.IsNil() != b.IsNil() {
			return false
		}
		if a.Len() != b.Len() {
			return false
		}
		entries := a.MapRange()
		for entries.Next() {
			other := b.MapIndex(entries.Key())
			if !other.IsValid() {
				return false
			}
			if !referenceProvenJSONEqual(entries.Value(), other) {
				return false
			}
		}
		return true
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() && b.IsNil()
		}
		return referenceProvenJSONEqual(a.Elem(), b.Elem())
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() && b.IsNil()
		}
		if a.Elem().Type() != b.Elem().Type() {
			return false
		}
		return referenceProvenJSONEqual(a.Elem(), b.Elem())
	default:
		return false
	}
}

// referenceProvenNumericSliceEqual is the original provenNumericSliceEqual. It
// handled []byte, []float64 and []float32 and declined every other slice.
func referenceProvenNumericSliceEqual(a, b reflect.Value) (equal, decided bool) {
	// Check the element kind before reading the slice as a Go value. Reading it
	// boxes the slice header into an interface, which allocates, and most slice
	// fields on a lowered record hold structs this function would decline anyway.
	switch a.Type().Elem().Kind() {
	case reflect.Uint8, reflect.Float32, reflect.Float64:
	default:
		return false, false
	}
	if !a.CanInterface() || !b.CanInterface() {
		return false, false
	}
	switch left := a.Interface().(type) {
	case []byte:
		return bytes.Equal(left, b.Interface().([]byte)), true
	case []float64:
		right := b.Interface().([]float64)
		for index, value := range left {
			if !referenceProvenFloatEqual(value, right[index]) {
				return false, true
			}
		}
		return true, true
	case []float32:
		right := b.Interface().([]float32)
		for index, value := range left {
			if !referenceProvenFloatEqual(float64(value), float64(right[index])) {
				return false, true
			}
		}
		return true, true
	default:
		return false, false
	}
}

// referenceProvenFloatEqual is the original provenFloatEqual: it reports bit
// equality for two finite floats.
func referenceProvenFloatEqual(left, right float64) bool {
	if math.IsNaN(left) || math.IsNaN(right) || math.IsInf(left, 0) || math.IsInf(right, 0) {
		return false
	}
	return math.Float64bits(left) == math.Float64bits(right)
}
