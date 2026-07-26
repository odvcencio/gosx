package signal

import "reflect"

// defaultEqual returns the equality test that New installs when the caller
// supplies none. A signal over a comparable type must not notify subscribers
// when the value did not change.
//
// The function returns nil for types that Go cannot compare, such as slices,
// maps, and funcs. It also returns nil for interface types, because two
// interface values with an incomparable dynamic type panic on ==.
//
// Basic kinds get a pointer-based comparison. Converting a pointer to an
// interface never allocates, so Set stays allocation free.
func defaultEqual[T any]() func(a, b T) bool {
	typ := reflect.TypeOf((*T)(nil)).Elem()
	if typ.Kind() == reflect.Interface || !typ.Comparable() {
		return nil
	}

	var zero T
	switch any(&zero).(type) {
	case *bool:
		return func(a, b T) bool { return *any(&a).(*bool) == *any(&b).(*bool) }
	case *string:
		return func(a, b T) bool { return *any(&a).(*string) == *any(&b).(*string) }
	case *int:
		return func(a, b T) bool { return *any(&a).(*int) == *any(&b).(*int) }
	case *int8:
		return func(a, b T) bool { return *any(&a).(*int8) == *any(&b).(*int8) }
	case *int16:
		return func(a, b T) bool { return *any(&a).(*int16) == *any(&b).(*int16) }
	case *int32:
		return func(a, b T) bool { return *any(&a).(*int32) == *any(&b).(*int32) }
	case *int64:
		return func(a, b T) bool { return *any(&a).(*int64) == *any(&b).(*int64) }
	case *uint:
		return func(a, b T) bool { return *any(&a).(*uint) == *any(&b).(*uint) }
	case *uint8:
		return func(a, b T) bool { return *any(&a).(*uint8) == *any(&b).(*uint8) }
	case *uint16:
		return func(a, b T) bool { return *any(&a).(*uint16) == *any(&b).(*uint16) }
	case *uint32:
		return func(a, b T) bool { return *any(&a).(*uint32) == *any(&b).(*uint32) }
	case *uint64:
		return func(a, b T) bool { return *any(&a).(*uint64) == *any(&b).(*uint64) }
	case *uintptr:
		return func(a, b T) bool { return *any(&a).(*uintptr) == *any(&b).(*uintptr) }
	case *float32:
		return func(a, b T) bool { return *any(&a).(*float32) == *any(&b).(*float32) }
	case *float64:
		return func(a, b T) bool { return *any(&a).(*float64) == *any(&b).(*float64) }
	case *complex64:
		return func(a, b T) bool { return *any(&a).(*complex64) == *any(&b).(*complex64) }
	case *complex128:
		return func(a, b T) bool { return *any(&a).(*complex128) == *any(&b).(*complex128) }
	}

	// Structs, arrays, pointers, channels, and named basic types fall back to
	// interface comparison. Pointer-shaped types stay allocation free; larger
	// values pay one boxing allocation per Set.
	return func(a, b T) bool { return any(a) == any(b) }
}
