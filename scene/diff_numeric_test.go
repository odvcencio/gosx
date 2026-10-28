package scene

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"sync"
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

// The tests in this file hold the live equality walk to the reflection walk it
// replaced. diff_numeric_oracle_test.go keeps that walk unchanged. Every test
// asks the same question: for this pair, does provenJSONEqual answer what
// referenceProvenJSONEqual answers? A faster walk may do less work. It may not
// change one answer, because a wrong "equal" is a lost edit and a wrong "unequal"
// is a wasted encode.

// sliceLengths covers the empty slice, every length up to nine, the sizes around
// the powers of two where a loop might unroll, and a few long slices.
var sliceLengths = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 15, 16, 17, 31, 32, 33, 64, 100, 257, 1000}

// sliceFieldValue returns s as an addressable slice field, the way a record reached
// through a pointer presents its slices to the walk.
func sliceFieldValue[E any](s []E) reflect.Value {
	holder := &struct{ S []E }{S: s}
	return reflect.ValueOf(holder).Elem().Field(0)
}

// specialValue is an element that needs care, with the answer for a slice that
// holds it and is compared with itself or with a copy.
type specialValue[E any] struct {
	name      string
	value     E
	selfEqual bool
}

// elemOps describes one element type under test.
type elemOps[E comparable] struct {
	name string
	// value returns element i of the base slice. Values differ across i, so a
	// loop that compares the wrong offsets fails.
	value func(i int) E
	// changed returns a value that differs from its argument in the smallest way.
	changed func(E) E
	special []specialValue[E]
}

// exerciseSliceKind compares the live walk with the reference walk on slices of
// one element type: copies, shared slices, one changed element at each position
// that matters, slices of different length that share their data, a nil slice
// against an empty one, and the special values.
func exerciseSliceKind[E comparable](t *testing.T, ops elemOps[E]) {
	t.Helper()
	trueValue, falseValue := true, false

	check := func(label string, a, b []E, want *bool) {
		t.Helper()
		av, bv := sliceFieldValue(a), sliceFieldValue(b)
		reference := referenceProvenJSONEqual(av, bv)
		if got := provenJSONEqual(av, bv); got != reference {
			t.Errorf("%s %s: provenJSONEqual = %t, the reference walk says %t", ops.name, label, got, reference)
		}
		if want != nil && reference != *want {
			t.Errorf("%s %s: the reference walk says %t, the test expected %t", ops.name, label, reference, *want)
		}
		// The walk is symmetric. diffSceneRecords compares in one direction, so an
		// asymmetric walk would make the commands depend on which scene came first.
		if back := provenJSONEqual(bv, av); back != reference {
			t.Errorf("%s %s: reversed, provenJSONEqual = %t, the reference walk says %t", ops.name, label, back, reference)
		}
		// A value that is not addressable reaches the same answer.
		if plain := provenJSONEqual(reflect.ValueOf(a), reflect.ValueOf(b)); plain != reference {
			t.Errorf("%s %s: unaddressable, provenJSONEqual = %t, the reference walk says %t", ops.name, label, plain, reference)
		}
		// The typed path must take every non-empty slice of these types. A
		// decline would still give the right answer, through the slow walk, and
		// would hide that the speedup is gone.
		if len(a) > 0 && len(a) == len(b) {
			equal, decided := provenNumericSliceEqual(av, bv)
			if !decided {
				t.Errorf("%s %s: provenNumericSliceEqual declined a slice of its own element type", ops.name, label)
			} else if equal != reference {
				t.Errorf("%s %s: provenNumericSliceEqual = %t, the reference walk says %t", ops.name, label, equal, reference)
			}
		}
	}

	positions := func(length int) []int {
		if length <= 9 {
			all := make([]int, length)
			for index := range all {
				all[index] = index
			}
			return all
		}
		return []int{0, 1, length / 2, length - 2, length - 1}
	}

	for _, length := range sliceLengths {
		base := make([]E, length)
		for index := range base {
			base[index] = ops.value(index)
		}
		check(fmt.Sprintf("length %d, copy", length), base, slices.Clone(base), &trueValue)
		check(fmt.Sprintf("length %d, shared", length), base, base, &trueValue)
		if length > 0 {
			check(fmt.Sprintf("length %d, same data, one shorter", length), base, base[:length-1], &falseValue)
			check(fmt.Sprintf("length %d, one shorter, same data", length), base[:length-1], base, &falseValue)
		}
		if length > 1 {
			check(fmt.Sprintf("length %d, offset by one", length), base[1:], base[:length-1], nil)
		}
		for _, position := range positions(length) {
			other := slices.Clone(base)
			other[position] = ops.changed(base[position])
			check(fmt.Sprintf("length %d, element %d differs", length, position), base, other, &falseValue)
			check(fmt.Sprintf("length %d, element %d differs, reversed", length, position), other, base, &falseValue)
		}
		if length == 0 {
			continue
		}
		for _, special := range ops.special {
			for _, position := range []int{0, length / 2, length - 1} {
				withSpecial := slices.Clone(base)
				withSpecial[position] = special.value
				label := fmt.Sprintf("length %d, %s at %d", length, special.name, position)
				want := special.selfEqual
				// The same slice twice is the case a shortcut gets wrong: a NaN
				// is not equal to itself under this walk, shared or not.
				check(label+", shared", withSpecial, withSpecial, &want)
				check(label+", copy", withSpecial, slices.Clone(withSpecial), &want)
				check(label+", against the base", base, withSpecial, &falseValue)
			}
		}
	}

	var nilSlice []E
	emptySlice := []E{}
	check("nil against nil", nilSlice, nilSlice, &trueValue)
	check("empty against empty", emptySlice, []E{}, &trueValue)
	check("nil against empty", nilSlice, emptySlice, &falseValue)
	check("empty against nil", emptySlice, nilSlice, &falseValue)
}

func float64Specials() []specialValue[float64] {
	return []specialValue[float64]{
		{"quiet NaN", math.NaN(), false},
		{"NaN with a payload", math.Float64frombits(0x7ff8_0000_0000_0001), false},
		{"signaling NaN pattern", math.Float64frombits(0x7ff0_0000_0000_0001), false},
		{"negative NaN", math.Float64frombits(0xfff8_0000_0000_0000), false},
		{"positive infinity", math.Inf(1), false},
		{"negative infinity", math.Inf(-1), false},
		{"negative zero", negativeZero, true},
		{"largest finite", math.MaxFloat64, true},
		{"smallest subnormal", math.SmallestNonzeroFloat64, true},
	}
}

func float32Specials() []specialValue[float32] {
	return []specialValue[float32]{
		{"quiet NaN", float32(math.NaN()), false},
		{"NaN with a payload", math.Float32frombits(0x7fc0_0001), false},
		{"signaling NaN pattern", math.Float32frombits(0x7f80_0001), false},
		{"negative NaN", math.Float32frombits(0xffc0_0000), false},
		{"positive infinity", float32(math.Inf(1)), false},
		{"negative infinity", float32(math.Inf(-1)), false},
		{"negative zero", float32(negativeZero), true},
		{"largest finite", math.MaxFloat32, true},
		{"smallest subnormal", math.SmallestNonzeroFloat32, true},
	}
}

// TestProvenNumericSlicesMatchReflection is the table test for the typed loops.
// Each row is one element type the lowered scene uses for an index list or an
// attribute stream. Every row compares the live walk with the reference walk.
func TestProvenNumericSlicesMatchReflection(t *testing.T) {
	t.Run("uint8", func(t *testing.T) {
		exerciseSliceKind(t, elemOps[uint8]{name: "[]uint8", value: func(i int) uint8 { return uint8(i*7 + 1) }, changed: func(v uint8) uint8 { return v + 1 }})
	})
	t.Run("uint16", func(t *testing.T) {
		exerciseSliceKind(t, elemOps[uint16]{name: "[]uint16", value: func(i int) uint16 { return uint16(i*7 + 1) }, changed: func(v uint16) uint16 { return v + 1 }})
	})
	t.Run("uint32", func(t *testing.T) {
		exerciseSliceKind(t, elemOps[uint32]{name: "[]uint32", value: func(i int) uint32 { return uint32(i*7 + 1) }, changed: func(v uint32) uint32 { return v + 1 }})
	})
	t.Run("uint32 at the edges", func(t *testing.T) {
		exerciseSliceKind(t, elemOps[uint32]{name: "[]uint32 edge", value: func(i int) uint32 { return math.MaxUint32 - uint32(i) }, changed: func(v uint32) uint32 { return v ^ 1 }})
	})
	t.Run("uint64", func(t *testing.T) {
		exerciseSliceKind(t, elemOps[uint64]{name: "[]uint64", value: func(i int) uint64 { return uint64(i)<<33 + 1 }, changed: func(v uint64) uint64 { return v + 1<<32 }})
	})
	t.Run("uint", func(t *testing.T) {
		exerciseSliceKind(t, elemOps[uint]{name: "[]uint", value: func(i int) uint { return uint(i*7 + 1) }, changed: func(v uint) uint { return v + 1 }})
	})
	t.Run("int8", func(t *testing.T) {
		exerciseSliceKind(t, elemOps[int8]{name: "[]int8", value: func(i int) int8 { return int8(i*7 - 60) }, changed: func(v int8) int8 { return v + 1 }})
	})
	t.Run("int16", func(t *testing.T) {
		exerciseSliceKind(t, elemOps[int16]{name: "[]int16", value: func(i int) int16 { return int16(i*7 - 60) }, changed: func(v int16) int16 { return v + 1 }})
	})
	t.Run("int32", func(t *testing.T) {
		exerciseSliceKind(t, elemOps[int32]{name: "[]int32", value: func(i int) int32 { return int32(i*7 - 60) }, changed: func(v int32) int32 { return v + 1 }})
	})
	t.Run("int32 at the edges", func(t *testing.T) {
		exerciseSliceKind(t, elemOps[int32]{name: "[]int32 edge", value: func(i int) int32 { return math.MinInt32 + int32(i) }, changed: func(v int32) int32 { return -v - 1 }})
	})
	t.Run("int64", func(t *testing.T) {
		exerciseSliceKind(t, elemOps[int64]{name: "[]int64", value: func(i int) int64 { return int64(i)<<33 - 60 }, changed: func(v int64) int64 { return v + 1<<32 }})
	})
	t.Run("int", func(t *testing.T) {
		exerciseSliceKind(t, elemOps[int]{name: "[]int", value: func(i int) int { return i*7 - 60 }, changed: func(v int) int { return v + 1 }})
	})
	t.Run("string", func(t *testing.T) {
		exerciseSliceKind(t, elemOps[string]{name: "[]string", value: func(i int) string { return fmt.Sprintf("#%06x", i*977) }, changed: func(v string) string { return v + "0" }})
	})
	t.Run("index pair", func(t *testing.T) {
		exerciseSliceKind(t, elemOps[[2]int]{name: "[][2]int", value: func(i int) [2]int { return [2]int{i, i * 2} }, changed: func(v [2]int) [2]int { v[1]++; return v }})
	})
	t.Run("float64", func(t *testing.T) {
		exerciseSliceKind(t, elemOps[float64]{
			name:    "[]float64",
			value:   func(i int) float64 { return float64(i)*0.25 - 3 },
			changed: func(v float64) float64 { return math.Nextafter(v, math.Inf(1)) },
			special: float64Specials(),
		})
	})
	t.Run("float32", func(t *testing.T) {
		exerciseSliceKind(t, elemOps[float32]{
			name:    "[]float32",
			value:   func(i int) float32 { return float32(i)*0.25 - 3 },
			changed: func(v float32) float32 { return math.Nextafter32(v, float32(math.Inf(1))) },
			special: float32Specials(),
		})
	})
}

// TestProvenFloatSignedZeroAndNeighbours pins the bit-equality rule where a float
// slice and the reflection walk are easiest to confuse: zero with a sign, and
// values one step apart.
func TestProvenFloatSignedZeroAndNeighbours(t *testing.T) {
	cases := []struct {
		name string
		a, b []float64
		want bool
	}{
		{"positive zero against positive zero", []float64{0, 1}, []float64{0, 1}, true},
		{"positive zero against negative zero", []float64{0, 1}, []float64{negativeZero, 1}, false},
		{"negative zero against positive zero", []float64{negativeZero, 1}, []float64{0, 1}, false},
		{"negative zero against negative zero", []float64{negativeZero, 1}, []float64{negativeZero, 1}, true},
		{"neighbouring values", []float64{1}, []float64{math.Nextafter(1, 2)}, false},
		{"zero only in the last element", []float64{1, 2, 3, 0}, []float64{1, 2, 3, negativeZero}, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			av, bv := sliceFieldValue(testCase.a), sliceFieldValue(testCase.b)
			if got := provenJSONEqual(av, bv); got != testCase.want {
				t.Errorf("provenJSONEqual = %t, want %t", got, testCase.want)
			}
			if got := referenceProvenJSONEqual(av, bv); got != testCase.want {
				t.Errorf("reference walk = %t, want %t: the case is wrong", got, testCase.want)
			}
		})
	}
}

// TestProvenFloatEqualMatchesReference compares the exponent test with the
// original six-way test over bit patterns chosen to break it.
func TestProvenFloatEqualMatchesReference(t *testing.T) {
	patterns := []uint64{
		0, 1, 2, 0x000f_ffff_ffff_ffff, 0x0010_0000_0000_0000, 0x3ff0_0000_0000_0000, 0x7fef_ffff_ffff_ffff,
		0x7ff0_0000_0000_0000, 0x7ff0_0000_0000_0001, 0x7ff7_ffff_ffff_ffff, 0x7ff8_0000_0000_0000,
		0x7ff8_0000_0000_0001, 0x7fff_ffff_ffff_ffff,
	}
	var values []float64
	for _, pattern := range patterns {
		values = append(values, math.Float64frombits(pattern), math.Float64frombits(pattern|1<<63))
	}
	rng := rand.New(rand.NewPCG(7, 11))
	for range 200 {
		values = append(values, math.Float64frombits(rng.Uint64()))
	}
	for _, left := range values {
		for _, right := range values {
			if got, want := provenFloatEqual(left, right), referenceProvenFloatEqual(left, right); got != want {
				t.Fatalf("provenFloatEqual(%#x, %#x) = %t, the reference test says %t", math.Float64bits(left), math.Float64bits(right), got, want)
			}
		}
	}
}

// TestProvenFloat32sEqualMatchesReference does the same for the float32 loop,
// which tests the float32 exponent field instead of widening each element.
func TestProvenFloat32sEqualMatchesReference(t *testing.T) {
	patterns := []uint32{
		0, 1, 2, 0x007f_ffff, 0x0080_0000, 0x3f80_0000, 0x7f7f_ffff, 0x7f80_0000, 0x7f80_0001, 0x7fbf_ffff, 0x7fc0_0000,
		0x7fc0_0001, 0x7fff_ffff,
	}
	var values []float32
	for _, pattern := range patterns {
		values = append(values, math.Float32frombits(pattern), math.Float32frombits(pattern|1<<31))
	}
	rng := rand.New(rand.NewPCG(13, 17))
	for range 200 {
		values = append(values, math.Float32frombits(rng.Uint32()))
	}
	for _, left := range values {
		for _, right := range values {
			got := provenFloat32sEqual([]float32{left}, []float32{right})
			want := referenceProvenFloatEqual(float64(left), float64(right))
			if got != want {
				t.Fatalf("provenFloat32sEqual(%#x, %#x) = %t, the reference test says %t", math.Float32bits(left), math.Float32bits(right), got, want)
			}
		}
	}
}

// Named types, for the cases the typed path declines and the walk must still
// answer the way the reference does.
type (
	indexList   []uint32
	vertexIndex uint32
	packedBytes []byte
	anyObject   map[string]any
)

// TestProvenJSONEqualMixedKinds covers values that differ in kind: slices of
// different element types behind an interface, named types, an untyped nil
// against a typed nil, and unexported fields. The typed paths decline some of
// these, and the answer must not change when they do.
func TestProvenJSONEqualMixedKinds(t *testing.T) {
	type wrapped struct{ Value any }
	type hidden struct {
		indices []uint32
		floats  []float64
		layout  map[string]any
		names   map[string]string
	}
	cases := []struct {
		name string
		a, b any
	}{
		{"same uint32 slices", []uint32{1, 2, 3}, []uint32{1, 2, 3}},
		{"uint32 against int32 with the same values", []uint32{1, 2, 3}, []int32{1, 2, 3}},
		{"uint32 against uint16 with the same values", []uint32{1, 2, 3}, []uint16{1, 2, 3}},
		{"int against int64 with the same values", []int{1, 2}, []int64{1, 2}},
		{"float64 against float32 with the same values", []float64{1, 2}, []float32{1, 2}},
		{"bytes against uint8", []byte{1, 2}, []uint8{1, 2}},
		{"bytes against string", []byte("ab"), "ab"},
		{"typed nil slice against an untyped nil", []uint32(nil), nil},
		{"untyped nil against a typed nil slice", nil, []uint32(nil)},
		{"typed nil against an empty slice", []uint32(nil), []uint32{}},
		{"two typed nils", []uint32(nil), []uint32(nil)},
		{"named slice against itself", indexList{1, 2}, indexList{1, 2}},
		{"named slice against a different value", indexList{1, 2}, indexList{1, 3}},
		{"named slice against the unnamed type", indexList{1, 2}, []uint32{1, 2}},
		{"named element type", []vertexIndex{1, 2}, []vertexIndex{1, 2}},
		{"named element type, different", []vertexIndex{1, 2}, []vertexIndex{1, 4}},
		{"named bytes", packedBytes{1, 2}, packedBytes{1, 2}},
		{"named bytes, different", packedBytes{1, 2}, packedBytes{1, 3}},
		{"named map against the unnamed type", anyObject{"k": 1.0}, map[string]any{"k": 1.0}},
		{"named map against itself", anyObject{"k": 1.0}, anyObject{"k": 1.0}},
		{"float64 against int", 1.0, 1},
		{"float64 against float32", 1.0, float32(1)},
		{"string against bytes", "a", []byte("a")},
		{"nil against a map", nil, map[string]any{}},
		{"nested slices", []any{[]uint32{1}, "x", []float64{1, 2}}, []any{[]uint32{1}, "x", []float64{1, 2}}},
		{"nested slices, deep difference", []any{[]uint32{1}, "x", []float64{1, 2}}, []any{[]uint32{1}, "x", []float64{1, 3}}},
		{"nested slices, kind difference", []any{[]uint32{1}}, []any{[]int32{1}}},
		{"map with a nil value", map[string]any{"k": nil}, map[string]any{"k": nil}},
		{"map with a nil value against a typed nil", map[string]any{"k": nil}, map[string]any{"k": []uint32(nil)}},
		{"map with an int against a float", map[string]any{"k": 1}, map[string]any{"k": 1.0}},
		{"map of every typed scalar, equal", map[string]any{"b": true, "s": "ab", "i": 7, "f": 1.5, "v": []float64{1, 2}, "n": nil},
			map[string]any{"b": true, "s": "ab", "i": 7, "f": 1.5, "v": []float64{1, 2}, "n": nil}},
		{"map with different bools", map[string]any{"k": true}, map[string]any{"k": false}},
		{"map with different ints", map[string]any{"k": 1}, map[string]any{"k": 2}},
		{"map with different strings of one length", map[string]any{"k": "ab"}, map[string]any{"k": "ac"}},
		{"map with different floats", map[string]any{"k": 1.0}, map[string]any{"k": 2.0}},
		{"map with different float slices", map[string]any{"k": []float64{1, 2}}, map[string]any{"k": []float64{1, 3}}},
		{"map with float slices of different length", map[string]any{"k": []float64{1, 2}}, map[string]any{"k": []float64{1}}},
		{"list with different ints", []any{1}, []any{2}},
		{"list with different strings of one length", []any{"ab"}, []any{"ac"}},
		{"list with different bools", []any{true}, []any{false}},
		{"nested nil map against an empty map", map[string]any{"k": map[string]any(nil)}, map[string]any{"k": map[string]any{}}},
		{"nested empty map against a nil map", map[string]any{"k": map[string]any{}}, map[string]any{"k": map[string]any(nil)}},
		{"nested nil map against a nil map", map[string]any{"k": map[string]any(nil)}, map[string]any{"k": map[string]any(nil)}},
		{"nested nil list against an empty list", map[string]any{"k": []any(nil)}, map[string]any{"k": []any{}}},
		{"nested empty list against a nil list", map[string]any{"k": []any{}}, map[string]any{"k": []any(nil)}},
		{"nil list against an empty list", []any(nil), []any{}},
		{"empty list against a nil list", []any{}, []any(nil)},
		{"list of a nil map against a list of an empty map", []any{map[string]any(nil)}, []any{map[string]any{}}},
		{"list of a nil list against a list of an empty list", []any{[]any(nil)}, []any{[]any{}}},
		{"list of equal empty containers", []any{map[string]any{}, []any{}, []float64{}}, []any{map[string]any{}, []any{}, []float64{}}},
		{"list of nil float slice against an empty one", []any{[]float64(nil)}, []any{[]float64{}}},
		{"list of different lengths", []any{1.0, 2.0}, []any{1.0}},
		{"list holding a nil against a value", []any{nil}, []any{1.0}},
		{"list holding a value against a nil", []any{1.0}, []any{nil}},
		{"map with a missing key", map[string]any{"k": 1.0, "j": 2.0}, map[string]any{"k": 1.0, "z": 2.0}},
		{"map with a longer second map", map[string]any{"k": 1.0}, map[string]any{"k": 1.0, "j": 2.0}},
		{"map with a NaN", map[string]any{"k": math.NaN()}, map[string]any{"k": math.NaN()}},
		{"map with an infinity", map[string]any{"k": math.Inf(1)}, map[string]any{"k": math.Inf(1)}},
		{"map with a float slice and a NaN", map[string]any{"k": []float64{1, math.NaN()}}, map[string]any{"k": []float64{1, math.NaN()}}},
		{"map with signed zeros", map[string]any{"k": 0.0}, map[string]any{"k": negativeZero}},
		{"map with a nil float slice against an empty one", map[string]any{"k": []float64(nil)}, map[string]any{"k": []float64{}}},
		{"map with a struct", map[string]any{"k": struct{ A int }{1}}, map[string]any{"k": struct{ A int }{1}}},
		{"map with a struct, different", map[string]any{"k": struct{ A int }{1}}, map[string]any{"k": struct{ A int }{2}}},
		{"map with a pointer", map[string]any{"k": new(float64)}, map[string]any{"k": new(float64)}},
		{"map with a channel", map[string]any{"k": make(chan int)}, map[string]any{"k": make(chan int)}},
		{"unexported fields, equal", hidden{indices: []uint32{1, 2}, floats: []float64{1}, layout: map[string]any{"k": 1.0}, names: map[string]string{"a": "b"}},
			hidden{indices: []uint32{1, 2}, floats: []float64{1}, layout: map[string]any{"k": 1.0}, names: map[string]string{"a": "b"}}},
		{"unexported index list, different", hidden{indices: []uint32{1, 2}}, hidden{indices: []uint32{1, 3}}},
		{"unexported floats, NaN", hidden{floats: []float64{math.NaN()}}, hidden{floats: []float64{math.NaN()}}},
		{"unexported layout, different", hidden{layout: map[string]any{"k": 1.0}}, hidden{layout: map[string]any{"k": 2.0}}},
		{"unexported names, different", hidden{names: map[string]string{"a": "b"}}, hidden{names: map[string]string{"a": "c"}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// Behind a struct field of type any, the way a record holds one.
			a, b := wrapped{testCase.a}, wrapped{testCase.b}
			av, bv := reflect.ValueOf(&a).Elem(), reflect.ValueOf(&b).Elem()
			want := referenceProvenJSONEqual(av, bv)
			if got := provenJSONEqual(av, bv); got != want {
				t.Errorf("inside a struct: provenJSONEqual = %t, the reference walk says %t", got, want)
			}
			if got := provenJSONEqual(bv, av); got != want {
				t.Errorf("inside a struct, reversed: provenJSONEqual = %t, the reference walk says %t", got, want)
			}
			// As the bare interface values, the entry the typed walk of
			// map[string]any uses for its members.
			x, y := testCase.a, testCase.b
			xv, yv := reflect.ValueOf(&x).Elem(), reflect.ValueOf(&y).Elem()
			bare := referenceProvenJSONEqual(xv, yv)
			if got := provenAnyEqual(x, y); got != bare {
				t.Errorf("provenAnyEqual = %t, the reference walk says %t", got, bare)
			}
			if got := provenAnyEqual(y, x); got != bare {
				t.Errorf("provenAnyEqual reversed = %t, the reference walk says %t", got, bare)
			}
			if got := provenJSONEqual(xv, yv); got != bare {
				t.Errorf("bare interface: provenJSONEqual = %t, the reference walk says %t", got, bare)
			}
		})
	}
}

// numericGenerator fills a value of any type with a random one, for the
// differential tests. A seed fixes the value, so two generators with one seed
// build equal values that share no memory.
type numericGenerator struct {
	rng *rand.Rand
	// nonFinite lets a NaN and an infinity appear among the floats.
	nonFinite bool
}

var generatedLengths = []int{0, 1, 2, 3, 4, 5, 8, 9, 17}

func (g *numericGenerator) float() float64 {
	if g.nonFinite && g.rng.IntN(12) == 0 {
		return []float64{math.NaN(), math.Inf(1), math.Inf(-1), math.Float64frombits(0x7ff0_0000_0000_0001)}[g.rng.IntN(4)]
	}
	return []float64{0, negativeZero, 1, -1, 0.5, 1e-300, 1e300, float64(g.rng.IntN(7))}[g.rng.IntN(8)]
}

func (g *numericGenerator) fill(value reflect.Value, depth int) {
	if depth > 5 || !value.CanSet() {
		return
	}
	switch value.Kind() {
	case reflect.Bool:
		value.SetBool(g.rng.IntN(2) == 0)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(int64(g.rng.IntN(5)) - 1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(uint64(g.rng.IntN(4)))
	case reflect.Float32, reflect.Float64:
		value.SetFloat(g.float())
	case reflect.String:
		value.SetString([]string{"", "a", "b", "#0f0f0fff", "gles"}[g.rng.IntN(5)])
	case reflect.Pointer:
		if g.rng.IntN(3) == 0 {
			value.SetZero()
			return
		}
		created := reflect.New(value.Type().Elem())
		g.fill(created.Elem(), depth+1)
		value.Set(created)
	case reflect.Slice:
		length := generatedLengths[g.rng.IntN(len(generatedLengths))]
		if length == 0 {
			if g.rng.IntN(2) == 0 {
				value.SetZero()
			} else {
				value.Set(reflect.MakeSlice(value.Type(), 0, 0))
			}
			return
		}
		created := reflect.MakeSlice(value.Type(), length, length)
		for index := range length {
			g.fill(created.Index(index), depth+1)
		}
		value.Set(created)
	case reflect.Array:
		for index := range value.Len() {
			g.fill(value.Index(index), depth+1)
		}
	case reflect.Map:
		switch g.rng.IntN(4) {
		case 0:
			value.SetZero()
		case 1:
			value.Set(reflect.MakeMap(value.Type()))
		default:
			created := reflect.MakeMap(value.Type())
			for range 1 + g.rng.IntN(4) {
				key := reflect.New(value.Type().Key()).Elem()
				g.fill(key, depth+1)
				entry := reflect.New(value.Type().Elem()).Elem()
				g.fill(entry, depth+1)
				created.SetMapIndex(key, entry)
			}
			value.Set(created)
		}
	case reflect.Struct:
		for index := range value.NumField() {
			g.fill(value.Field(index), depth+1)
		}
	case reflect.Interface:
		if value.Type().NumMethod() != 0 {
			return
		}
		if generated := g.anyValue(depth); generated != nil {
			value.Set(reflect.ValueOf(generated))
		}
	}
}

// fillNumeric returns a freshly generated value of type T.
func fillNumeric[T any](g *numericGenerator, depth int) T {
	var value T
	g.fill(reflect.ValueOf(&value).Elem(), depth)
	return value
}

// anyValue builds a dynamic value for an interface field: the types a decoded
// JSON tree holds, the types a uniform table holds, and types that only the
// reflection walk can read.
func (g *numericGenerator) anyValue(depth int) any {
	switch g.rng.IntN(20) {
	case 0:
		return nil
	case 1:
		return g.rng.IntN(2) == 0
	case 2:
		return []string{"", "a", "b", "gles"}[g.rng.IntN(4)]
	case 3, 4:
		return g.float()
	case 5:
		return g.rng.IntN(4)
	case 6:
		return float32(g.float())
	case 7:
		return int64(g.rng.IntN(3))
	case 8:
		return fillNumeric[[]float64](g, depth+1)
	case 9:
		return fillNumeric[[]string](g, depth+1)
	case 10:
		return fillNumeric[[]uint32](g, depth+1)
	case 11, 12:
		if depth < 3 {
			return fillNumeric[[]any](g, depth+1)
		}
		return nil
	case 13, 14:
		if depth < 3 {
			return fillNumeric[map[string]any](g, depth+1)
		}
		return nil
	case 15:
		return fillNumeric[struct {
			A int
			B string
			C []float64
		}](g, depth+1)
	case 16:
		return fillNumeric[*float64](g, depth+1)
	case 17:
		return fillNumeric[[3]float64](g, depth+1)
	case 18:
		if depth < 3 {
			return fillNumeric[anyObject](g, depth+1)
		}
		return nil
	default:
		return fillNumeric[[]byte](g, depth+1)
	}
}

// generateNumeric returns the value of type T that a seed fixes.
func generateNumeric[T any](seed uint64, nonFinite bool) T {
	g := &numericGenerator{rng: rand.New(rand.NewPCG(seed, seed^0x9e37_79b9_7f4a_7c15)), nonFinite: nonFinite}
	return fillNumeric[T](g, 0)
}

// numericProbe holds every slice and map type the typed paths handle, next to the
// kinds they decline, so one generated value exercises each path.
type numericProbe struct {
	Bytes   []byte
	U16     []uint16
	U32     []uint32
	U64     []uint64
	Uint    []uint
	I8      []int8
	I16     []int16
	I32     []int32
	I64     []int64
	Int     []int
	F32     []float32
	F64     []float64
	Strings []string
	Pairs   [][2]int
	Triples [][3]float64
	Names   map[string]string
	Counts  map[string]int
	Series  map[string][]float64
	Layout  map[string]any
	List    []any
	Records []struct {
		A int
		B string
		C []uint32
	}
	Floaty []struct {
		A int
		F float64
	}
	Mixed     any
	Ptr       *[]uint32
	Named     indexList
	NamedElem []vertexIndex
	Features  map[capability.Backend][]capability.Feature
	hidden    []uint32
}

// assertGeneratedMatchesReference builds pairs of T and compares the live walk
// with the reference walk. The pairs are an exact copy that shares nothing, a
// shallow copy that shares every slice, map and pointer, a copy with one field
// built again, and an unrelated value. The shallow copy is the case the
// shared-memory shortcut changes, and the generator with non-finite floats is
// the case the shortcut must not turn into a match.
func assertGeneratedMatchesReference[T any](t *testing.T, iterations int) {
	t.Helper()
	var equalPairs, unequalPairs, sharedPairs int
	compare := func(label string, iteration int, nonFinite bool, a, b T) {
		t.Helper()
		av, bv := reflect.ValueOf(&a).Elem(), reflect.ValueOf(&b).Elem()
		want := referenceProvenJSONEqual(av, bv)
		if want {
			equalPairs++
		} else {
			unequalPairs++
		}
		if got := provenJSONEqual(av, bv); got != want {
			t.Errorf("%s, iteration %d, nonFinite %t: provenJSONEqual = %t, the reference walk says %t", label, iteration, nonFinite, got, want)
		}
		if got := provenJSONEqual(bv, av); got != want {
			t.Errorf("%s, iteration %d, nonFinite %t, reversed: provenJSONEqual = %t, the reference walk says %t", label, iteration, nonFinite, got, want)
		}
	}
	for iteration := range iterations {
		for _, nonFinite := range []bool{false, true} {
			seed := uint64(iteration)*2654435761 + 17
			a := generateNumeric[T](seed, nonFinite)
			compare("exact copy", iteration, nonFinite, a, generateNumeric[T](seed, nonFinite))
			shared := a
			compare("shared", iteration, nonFinite, a, shared)
			if want := referenceProvenJSONEqual(reflect.ValueOf(&a).Elem(), reflect.ValueOf(&shared).Elem()); want {
				sharedPairs++
			}
			compare("unrelated", iteration, nonFinite, a, generateNumeric[T](seed+1, nonFinite))
			if reflect.TypeFor[T]().Kind() == reflect.Struct {
				perturbed := generateNumeric[T](seed, nonFinite)
				rng := rand.New(rand.NewPCG(seed, 99))
				field := reflect.ValueOf(&perturbed).Elem().Field(rng.IntN(reflect.TypeFor[T]().NumField()))
				(&numericGenerator{rng: rng, nonFinite: nonFinite}).fill(field, 1)
				compare("one field built again", iteration, nonFinite, a, perturbed)
			}
		}
	}
	if equalPairs == 0 || unequalPairs == 0 {
		t.Errorf("the generated pairs gave %d equal and %d unequal answers; the test needs both", equalPairs, unequalPairs)
	}
	if sharedPairs == 0 {
		t.Error("no shared pair was proven equal; the shortcut went untested")
	}
}

// TestProvenJSONEqualMatchesReferenceOnGeneratedValues compares the live walk
// with the reference walk over generated records of every IR type that holds an
// index list, a vertex stream, a layout map or a uniform table.
func TestProvenJSONEqualMatchesReferenceOnGeneratedValues(t *testing.T) {
	t.Run("numericProbe", func(t *testing.T) { assertGeneratedMatchesReference[numericProbe](t, 400) })
	t.Run("MeshVertices", func(t *testing.T) { assertGeneratedMatchesReference[MeshVertices](t, 300) })
	t.Run("ObjectIR", func(t *testing.T) { assertGeneratedMatchesReference[ObjectIR](t, 150) })
	t.Run("ModelIR", func(t *testing.T) { assertGeneratedMatchesReference[ModelIR](t, 100) })
	t.Run("PointsIR", func(t *testing.T) { assertGeneratedMatchesReference[PointsIR](t, 200) })
	t.Run("InstancedMeshIR", func(t *testing.T) { assertGeneratedMatchesReference[InstancedMeshIR](t, 200) })
	t.Run("InstancedGLBMeshIR", func(t *testing.T) { assertGeneratedMatchesReference[InstancedGLBMeshIR](t, 150) })
	t.Run("ComputeParticlesIR", func(t *testing.T) { assertGeneratedMatchesReference[ComputeParticlesIR](t, 150) })
	t.Run("AnimationClipIR", func(t *testing.T) { assertGeneratedMatchesReference[AnimationClipIR](t, 150) })
	t.Run("LightIR", func(t *testing.T) { assertGeneratedMatchesReference[LightIR](t, 150) })
	t.Run("LabelIR", func(t *testing.T) { assertGeneratedMatchesReference[LabelIR](t, 150) })
	t.Run("CompressedArray", func(t *testing.T) { assertGeneratedMatchesReference[CompressedArray](t, 200) })
	t.Run("IRMaterial", func(t *testing.T) { assertGeneratedMatchesReference[IRMaterial](t, 150) })
	t.Run("[]ObjectIR", func(t *testing.T) { assertGeneratedMatchesReference[[]ObjectIR](t, 80) })
	t.Run("map[string]any", func(t *testing.T) { assertGeneratedMatchesReference[map[string]any](t, 400) })
	t.Run("[]any", func(t *testing.T) { assertGeneratedMatchesReference[[]any](t, 400) })
}

// TestProvenSelfEqualType pins the classification the shared-memory shortcut
// relies on, and proves it sound: for a type classified self-equal, no generated
// value, including one with a NaN where the type could hold one, may fail the
// reference walk when compared with itself.
func TestProvenSelfEqualType(t *testing.T) {
	type tree struct {
		Kids []tree
		N    int
	}
	type scalars struct {
		A int
		B string
		C []uint32
		D map[string]bool
	}
	cases := []struct {
		name string
		typ  reflect.Type
		want bool
	}{
		{"bool", reflect.TypeFor[bool](), true},
		{"int", reflect.TypeFor[int](), true},
		{"uint32", reflect.TypeFor[uint32](), true},
		{"string", reflect.TypeFor[string](), true},
		{"[]uint32", reflect.TypeFor[[]uint32](), true},
		{"[]uint16", reflect.TypeFor[[]uint16](), true},
		{"[]int32", reflect.TypeFor[[]int32](), true},
		{"[]int", reflect.TypeFor[[]int](), true},
		{"[]byte", reflect.TypeFor[[]byte](), true},
		{"[]string", reflect.TypeFor[[]string](), true},
		{"[][2]int", reflect.TypeFor[[][2]int](), true},
		{"[][]uint32", reflect.TypeFor[[][]uint32](), true},
		{"map[string]string", reflect.TypeFor[map[string]string](), true},
		{"map[string]int", reflect.TypeFor[map[string]int](), true},
		{"map[int]string", reflect.TypeFor[map[int]string](), true},
		{"map[string][]string", reflect.TypeFor[map[string][]string](), true},
		{"map[Backend][]Feature", reflect.TypeFor[map[capability.Backend][]capability.Feature](), true},
		{"[]scalars", reflect.TypeFor[[]scalars](), true},
		{"[]indexList", reflect.TypeFor[[]indexList](), true},
		{"[]vertexIndex", reflect.TypeFor[[]vertexIndex](), true},
		{"[]struct{}", reflect.TypeFor[[]struct{}](), true},

		{"float64", reflect.TypeFor[float64](), false},
		{"float32", reflect.TypeFor[float32](), false},
		{"[]float64", reflect.TypeFor[[]float64](), false},
		{"[]float32", reflect.TypeFor[[]float32](), false},
		{"[2]float64", reflect.TypeFor[[2]float64](), false},
		{"[][3]float64", reflect.TypeFor[[][3]float64](), false},
		{"map[string]float64", reflect.TypeFor[map[string]float64](), false},
		{"map[string][]float64", reflect.TypeFor[map[string][]float64](), false},
		{"map[float64]int", reflect.TypeFor[map[float64]int](), false},
		{"map[any]int", reflect.TypeFor[map[any]int](), false},
		{"map[string]any", reflect.TypeFor[map[string]any](), false},
		{"[]any", reflect.TypeFor[[]any](), false},
		{"any", reflect.TypeFor[any](), false},
		{"[]*int", reflect.TypeFor[[]*int](), false},
		{"[]Vector3", reflect.TypeFor[[]Vector3](), false},
		{"[]struct{A float64}", reflect.TypeFor[[]struct{ A float64 }](), false},
		{"[]chan int", reflect.TypeFor[[]chan int](), false},
		{"[]func()", reflect.TypeFor[[]func()](), false},
		{"[]complex128", reflect.TypeFor[[]complex128](), false},
		{"[]tree", reflect.TypeFor[[]tree](), false},
		{"[]MeshInstanceIR", reflect.TypeFor[[]MeshInstanceIR](), false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := provenSelfEqualType(testCase.typ); got != testCase.want {
				t.Errorf("provenSelfEqualType(%s) = %t, want %t", testCase.typ, got, testCase.want)
			}
			// A second call reads the cache and must agree.
			if got := provenSelfEqualType(testCase.typ); got != testCase.want {
				t.Errorf("provenSelfEqualType(%s), cached = %t, want %t", testCase.typ, got, testCase.want)
			}
		})
	}
}

// assertSelfEqualTypeIsSound generates values of T and requires the reference
// walk to prove each equal to itself, which is what a self-equal type promises.
func assertSelfEqualTypeIsSound[T any](t *testing.T) {
	t.Helper()
	if !provenSelfEqualType(reflect.TypeFor[T]()) {
		t.Fatalf("%s is not classified self-equal; the soundness check does not apply", reflect.TypeFor[T]())
	}
	for iteration := range 300 {
		for _, nonFinite := range []bool{false, true} {
			value := generateNumeric[T](uint64(iteration)+1000, nonFinite)
			v := reflect.ValueOf(&value).Elem()
			if !referenceProvenJSONEqual(v, v) {
				t.Fatalf("%s value %d is not equal to itself under the reference walk: the classification is unsound", reflect.TypeFor[T](), iteration)
			}
		}
	}
}

func TestProvenSelfEqualTypeIsSound(t *testing.T) {
	type scalars struct {
		A int
		B string
		C []uint32
		D map[string]bool
		E [2]uint8
		F [][2]int
	}
	t.Run("[]uint32", func(t *testing.T) { assertSelfEqualTypeIsSound[[]uint32](t) })
	t.Run("[]int", func(t *testing.T) { assertSelfEqualTypeIsSound[[]int](t) })
	t.Run("[]string", func(t *testing.T) { assertSelfEqualTypeIsSound[[]string](t) })
	t.Run("[]byte", func(t *testing.T) { assertSelfEqualTypeIsSound[[]byte](t) })
	t.Run("[][2]int", func(t *testing.T) { assertSelfEqualTypeIsSound[[][2]int](t) })
	t.Run("map[string]string", func(t *testing.T) { assertSelfEqualTypeIsSound[map[string]string](t) })
	t.Run("map[string][]string", func(t *testing.T) { assertSelfEqualTypeIsSound[map[string][]string](t) })
	t.Run("map[int][]uint16", func(t *testing.T) { assertSelfEqualTypeIsSound[map[int][]uint16](t) })
	t.Run("map[Backend][]Feature", func(t *testing.T) {
		assertSelfEqualTypeIsSound[map[capability.Backend][]capability.Feature](t)
	})
	t.Run("[]scalars", func(t *testing.T) { assertSelfEqualTypeIsSound[[]scalars](t) })
	t.Run("map[string]scalars", func(t *testing.T) { assertSelfEqualTypeIsSound[map[string]scalars](t) })
	t.Run("[]vertexIndex", func(t *testing.T) { assertSelfEqualTypeIsSound[[]vertexIndex](t) })
}

// TestProvenSharedEqualSkipsTheWalk proves the shortcut fires and stays cheap: a
// slice or map that two records share is settled without reading a value, which
// takes no allocation. The typed loops box each slice into an interface first, so
// an allocation here would mean the walk ran.
func TestProvenSharedEqualSkipsTheWalk(t *testing.T) {
	type record struct {
		Indices []uint32
		Wide    []int
		Names   map[string]string
		Colors  []string
		Pairs   [][2]int
	}
	one := &record{
		Indices: make([]uint32, 1<<16), Wide: make([]int, 1<<10), Names: map[string]string{"a": "b"},
		Colors: []string{"#fff"}, Pairs: [][2]int{{0, 1}},
	}
	two := &record{Indices: one.Indices, Wide: one.Wide, Names: one.Names, Colors: one.Colors, Pairs: one.Pairs}
	a, b := reflect.ValueOf(one).Elem(), reflect.ValueOf(two).Elem()
	if !provenJSONEqual(a, b) {
		t.Fatal("two records that share every slice and map are not proven equal")
	}
	if allocs := testing.AllocsPerRun(100, func() { provenJSONEqual(a, b) }); allocs != 0 {
		t.Errorf("comparing records that share their slices and maps allocated %v times per run; the walk ran", allocs)
	}
	for index := range a.NumField() {
		if !provenSharedEqual(a.Field(index), b.Field(index)) {
			t.Errorf("field %s: provenSharedEqual = false for a shared value", a.Type().Field(index).Name)
		}
	}

	// A copy shares nothing, so the shortcut must not fire, and the answer stays
	// right in both directions.
	three := &record{
		Indices: append([]uint32(nil), one.Indices...), Wide: append([]int(nil), one.Wide...),
		Names: map[string]string{"a": "b"}, Colors: []string{"#fff"}, Pairs: [][2]int{{0, 1}},
	}
	c := reflect.ValueOf(three).Elem()
	for index := range a.NumField() {
		if provenSharedEqual(a.Field(index), c.Field(index)) {
			t.Errorf("field %s: provenSharedEqual = true for a copy", a.Type().Field(index).Name)
		}
	}
	if !provenJSONEqual(a, c) {
		t.Error("a record and its copy are not proven equal")
	}
	three.Indices[len(three.Indices)-1] = 1
	if provenJSONEqual(a, c) {
		t.Error("a copy that differs in its last index is proven equal")
	}

	// Slices of the same data and different length are not the same slice.
	four := &record{Indices: one.Indices[:len(one.Indices)-1]}
	d := reflect.ValueOf(four).Elem()
	if provenJSONEqual(a, d) {
		t.Error("a slice and its prefix are proven equal")
	}
}

// TestProvenAnyMapWalkDoesNotAllocate proves the typed walk of map[string]any
// runs. The reflection walk copies every key, value and lookup result into a new
// heap value, so thousands of allocations per layout mean the typed walk is off.
func TestProvenAnyMapWalkDoesNotAllocate(t *testing.T) {
	decode := func() map[string]any {
		var layout map[string]any
		if err := json.Unmarshal([]byte(benchShaderLayoutJSON), &layout); err != nil {
			t.Fatal(err)
		}
		return layout
	}
	uniforms := func() map[string]any {
		return map[string]any{
			"sunColor": []float64{1, 0.96, 0.9}, "coat": 0.18, "environmentRadiance": "gosx:environment:radiance",
			"variant": 1.0, "enabled": true, "count": 3, "list": []any{1.0, "a", nil},
		}
	}
	type record struct{ ShaderLayout, CustomUniforms map[string]any }
	a, b := &record{decode(), uniforms()}, &record{decode(), uniforms()}
	av, bv := reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem()
	if !provenJSONEqual(av, bv) {
		t.Fatal("two equal layouts and uniform tables are not proven equal")
	}
	if allocs := testing.AllocsPerRun(50, func() { provenJSONEqual(av, bv) }); allocs != 0 {
		t.Errorf("comparing two equal layouts allocated %v times per run; the walk went through reflection", allocs)
	}
	b.ShaderLayout["uniformBlock"].(map[string]any)["size"] = 512.0
	if provenJSONEqual(av, bv) {
		t.Error("layouts that differ in one nested value are proven equal")
	}
}

// TestProvenSharedFloatsAreNotShortcut pins the rule the shortcut must keep: the
// walk rejects a NaN and an infinity even when they are the same memory, so a
// shared float slice, a shared map of interfaces and a shared record holding
// either must still answer false.
func TestProvenSharedFloatsAreNotShortcut(t *testing.T) {
	type record struct {
		Floats  []float64
		Floats3 []float32
		Layout  map[string]any
		Series  map[string][]float64
		Points  []Vector3
		Any     any
	}
	for _, special := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		one := &record{
			Floats:  []float64{1, special},
			Floats3: []float32{1, float32(special)},
			Layout:  map[string]any{"k": []any{1.0, special}},
			Series:  map[string][]float64{"k": {1, special}},
			Points:  []Vector3{{X: special}},
			Any:     []float64{special},
		}
		two := &record{Floats: one.Floats, Floats3: one.Floats3, Layout: one.Layout, Series: one.Series, Points: one.Points, Any: one.Any}
		a, b := reflect.ValueOf(one).Elem(), reflect.ValueOf(two).Elem()
		for index := range a.NumField() {
			name := a.Type().Field(index).Name
			if got, want := provenJSONEqual(a.Field(index), b.Field(index)), referenceProvenJSONEqual(a.Field(index), b.Field(index)); got != want || got {
				t.Errorf("%v in shared %s: provenJSONEqual = %t, reference = %t, want false from both", special, name, got, want)
			}
		}
		if provenJSONEqual(a, b) {
			t.Errorf("%v in a shared record: proven equal", special)
		}
	}
}

// singleFieldRecord returns a pointer to a struct that holds value in its one
// exported field, so a test can copy the struct and share the value.
func singleFieldRecord[T any](value T) any {
	return &struct{ V T }{V: value}
}

// TestProvenSharedUnencodableValuesAreWalked pins the whole rule behind the
// shortcut, not only its NaN half. The JSON encoder rejects a NaN, an infinity, a
// complex number, a channel and a function, and the walk answers false for each of
// them, even when both sides are the same memory. A container that can hold one
// of them, a list or map of interfaces, a list of pointers, a map keyed by
// floats, must be read on every comparison. Skipping a shared one would call a
// record the encoder cannot write unchanged: DiffCommands would stop resending
// it, and MarshalCommands would stop failing on it.
//
// Each case copies the record, so the copy shares the slice or map with the
// original. The controls hold finite values only, to show the test does not pass
// because every answer is false.
func TestProvenSharedUnencodableValuesAreWalked(t *testing.T) {
	nan, posInf, negInf := math.NaN(), math.Inf(1), math.Inf(-1)
	finite := 1.5
	cases := []struct {
		name   string
		record any
		// want is the answer of both walks for the record and its copy. A false
		// answer here also means the encoder rejects the record.
		want bool
		// shortcut is whether provenSharedEqual may skip the shared value.
		shortcut bool
	}{
		{name: "NaN in []float64", record: singleFieldRecord([]float64{1, nan})},
		{name: "+Inf in []float64", record: singleFieldRecord([]float64{1, posInf})},
		{name: "-Inf in []float64", record: singleFieldRecord([]float64{1, negInf})},
		{name: "NaN in []float32", record: singleFieldRecord([]float32{1, float32(nan)})},
		{name: "+Inf in []float32", record: singleFieldRecord([]float32{1, float32(posInf)})},
		{name: "NaN in [][]float64", record: singleFieldRecord([][]float64{{1, nan}})},
		{name: "NaN in map[string][]float64", record: singleFieldRecord(map[string][]float64{"k": {nan}})},
		{name: "NaN in []any", record: singleFieldRecord([]any{"a", nan})},
		{name: "+Inf in []any", record: singleFieldRecord([]any{"a", posInf})},
		{name: "complex128 in []any", record: singleFieldRecord([]any{"a", complex(1, 2)})},
		{name: "complex64 in []any", record: singleFieldRecord([]any{"a", complex64(complex(1, 2))})},
		{name: "channel in []any", record: singleFieldRecord([]any{"a", make(chan int)})},
		{name: "function in []any", record: singleFieldRecord([]any{"a", func() {}})},
		{name: "NaN in map[string]any", record: singleFieldRecord(map[string]any{"k": nan})},
		{name: "+Inf in map[string]any", record: singleFieldRecord(map[string]any{"k": posInf})},
		{name: "complex128 in map[string]any", record: singleFieldRecord(map[string]any{"k": complex(1, 2)})},
		{name: "channel in map[string]any", record: singleFieldRecord(map[string]any{"k": make(chan int)})},
		{name: "function in map[string]any", record: singleFieldRecord(map[string]any{"k": func() {}})},
		{name: "NaN in map[string][]any", record: singleFieldRecord(map[string][]any{"k": {1.0, nan}})},
		{name: "function in a nested map[string]any", record: singleFieldRecord(map[string]any{"a": map[string]any{"b": func() {}}})},
		{name: "NaN behind []*float64", record: singleFieldRecord([]*float64{&nan})},
		{name: "NaN key in map[float64]int", record: singleFieldRecord(map[float64]int{nan: 1})},
		{name: "complex128 in []complex128", record: singleFieldRecord([]complex128{complex(1, 2)})},
		{name: "channel in []chan int", record: singleFieldRecord([]chan int{make(chan int)})},
		{name: "function in []func()", record: singleFieldRecord([]func(){func() {}})},

		{name: "control: finite []float64", record: singleFieldRecord([]float64{1, 2}), want: true},
		{name: "control: finite []any", record: singleFieldRecord([]any{"a", 1.5, true, nil}), want: true},
		{name: "control: finite map[string]any", record: singleFieldRecord(map[string]any{"k": 1.5, "s": "x"}), want: true},
		{name: "control: finite []*float64", record: singleFieldRecord([]*float64{&finite}), want: true},
		{name: "control: finite map[float64]int", record: singleFieldRecord(map[float64]int{1.5: 1}), want: true},
		{name: "control: []uint32 takes the shortcut", record: singleFieldRecord([]uint32{1, 2, 3}), want: true, shortcut: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			original := reflect.ValueOf(testCase.record).Elem()
			copyPointer := reflect.New(original.Type())
			copyPointer.Elem().Set(original) // a shallow copy: the slice or map is shared
			shared := copyPointer.Elem()
			if got := provenJSONEqual(original, shared); got != testCase.want {
				t.Errorf("provenJSONEqual = %t, want %t", got, testCase.want)
			}
			if got := referenceProvenJSONEqual(original, shared); got != testCase.want {
				t.Errorf("the reference walk says %t, the test expected %t", got, testCase.want)
			}
			if got := provenSharedEqual(original.Field(0), shared.Field(0)); got != testCase.shortcut {
				t.Errorf("provenSharedEqual = %t, want %t", got, testCase.shortcut)
			}
			if !testCase.want && sceneRecordMarshalEqual(original.Interface(), shared.Interface()) {
				t.Error("the encoder writes this record, so the case holds no value the encoder rejects")
			}
		})
	}
}

// TestProvenNumericSliceKindsInTheSceneIRAreTyped walks every type the scene IR
// can reach and requires the typed path to take each unnamed numeric slice. A new
// field of an unhandled numeric type would silently fall back to the reflection
// walk; this test is the place that notices.
func TestProvenNumericSliceKindsInTheSceneIRAreTyped(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var found []reflect.Type
	var walk func(reflect.Type)
	walk = func(typ reflect.Type) {
		switch typ.Kind() {
		case reflect.Pointer, reflect.Array:
			walk(typ.Elem())
		case reflect.Map:
			walk(typ.Elem())
		case reflect.Slice:
			elem := typ.Elem()
			if typ.Name() == "" && elem.PkgPath() == "" {
				switch elem.Kind() {
				case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint,
					reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int,
					reflect.Float32, reflect.Float64, reflect.String:
					if !seen[typ] {
						seen[typ] = true
						found = append(found, typ)
					}
				case reflect.Array:
					if elem == indexPairType && !seen[typ] {
						seen[typ] = true
						found = append(found, typ)
					}
				}
			}
			walk(elem)
		case reflect.Struct:
			if seen[typ] {
				return
			}
			seen[typ] = true
			for index := range typ.NumField() {
				walk(typ.Field(index).Type)
			}
		}
	}
	for _, root := range []reflect.Type{reflect.TypeFor[SceneIR](), reflect.TypeFor[IR](), reflect.TypeFor[Props]()} {
		walk(root)
	}
	if len(found) == 0 {
		t.Fatal("found no numeric slice type in the scene IR; the walk is broken")
	}
	wanted := map[reflect.Type]bool{}
	for _, typ := range []reflect.Type{
		reflect.TypeFor[[]uint32](), reflect.TypeFor[[]float64](), reflect.TypeFor[[]byte](), reflect.TypeFor[[]string](),
		reflect.TypeFor[[][2]int](),
	} {
		wanted[typ] = true
	}
	for _, typ := range found {
		delete(wanted, typ)
		a := reflect.MakeSlice(typ, 2, 2)
		b := reflect.MakeSlice(typ, 2, 2)
		// MakeSlice values are not addressable, which suits the typed path.
		if _, decided := provenNumericSliceEqual(a, b); !decided {
			t.Errorf("the typed path declines %s, which the scene IR uses", typ)
		}
	}
	for typ := range wanted {
		t.Errorf("the scene IR no longer reaches %s; update this test if that is deliberate", typ)
	}
	t.Logf("numeric slice types reachable from the scene IR: %v", found)
}

// smallMeshScene builds five meshes, from 9 to 5001 indices, each with the vertex
// streams, uniforms and shader layout of a table mesh. Two calls build equal
// scenes that share no slice or map.
func smallMeshScene(tb testing.TB) SceneIR {
	tb.Helper()
	ir := SceneIR{Schema: SceneIRSchema}
	for index, indexCount := range []int{12, 300, 5001, 66, 9} {
		ir.Objects = append(ir.Objects, benchMeshObject(tb, index, indexCount))
	}
	return ir
}

// TestDiffSceneIsSafeForConcurrentUse diffs the same scenes from several
// goroutines at once. A server diffs from many request handlers, and the walk
// keeps one cache of type answers that all of them share. The race detector is
// the check, together with the answers.
func TestDiffSceneIsSafeForConcurrentUse(t *testing.T) {
	previous, next := smallMeshScene(t), smallMeshScene(t)
	changed := smallMeshScene(t)
	changed.Objects[2].Vertices.Indices[4]++
	options := DiffOptions{PatchObjectState: true}
	var group sync.WaitGroup
	for worker := range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 20 {
				if diff := DiffScene(previous, next, options); len(diff.Commands) != 0 {
					t.Errorf("worker %d: an unchanged scene produced %d commands", worker, len(diff.Commands))
					return
				}
				if diff := DiffScene(previous, changed, options); len(diff.Commands) != 2 {
					t.Errorf("worker %d: a changed index produced %d commands, want 2", worker, len(diff.Commands))
					return
				}
			}
		}()
	}
	group.Wait()
}

// diffSignature lists the commands of a diff as kind and object ID.
func diffSignature(commands []Command) []string {
	signature := make([]string, len(commands))
	for index, command := range commands {
		signature[index] = fmt.Sprintf("%d:%s", command.Kind, command.ObjectID)
	}
	return signature
}

// TestDiffCommandsMatchReferenceOnMeshes runs the whole diff over lowered meshes
// and requires the commands the original marshal-and-compare algorithm gives.
// Each case changes one thing that a typed loop or the shared-memory shortcut
// could miss. The "shared" cases hand the next scene the previous scene's slices
// and maps, which is where a wrong shortcut loses an edit.
func TestDiffCommandsMatchReferenceOnMeshes(t *testing.T) {
	build := func() SceneIR { return smallMeshScene(t) }
	type mutation func(next *SceneIR)
	cases := []struct {
		name   string
		share  bool
		mutate mutation
	}{
		{"unchanged copy", false, func(*SceneIR) {}},
		{"unchanged shared", true, func(*SceneIR) {}},
		{"unchanged shared slices under new vertex records", true, func(next *SceneIR) {
			for index := range next.Objects {
				vertices := *next.Objects[index].Vertices
				next.Objects[index].Vertices = &vertices
			}
		}},
		{"first index changes", false, func(next *SceneIR) { next.Objects[2].Vertices.Indices[0]++ }},
		{"last index changes", false, func(next *SceneIR) {
			indices := next.Objects[2].Vertices.Indices
			indices[len(indices)-1]++
		}},
		{"index list gets shorter", false, func(next *SceneIR) {
			next.Objects[1].Vertices.Indices = next.Objects[1].Vertices.Indices[:299]
		}},
		{"index list becomes empty", false, func(next *SceneIR) { next.Objects[1].Vertices.Indices = []uint32{} }},
		{"index list becomes nil", false, func(next *SceneIR) { next.Objects[1].Vertices.Indices = nil }},
		{"index list changes when shared", true, func(next *SceneIR) {
			next.Objects[2].Vertices = &MeshVertices{
				Count: next.Objects[2].Vertices.Count, Positions: next.Objects[2].Vertices.Positions,
				Normals: next.Objects[2].Vertices.Normals, UVs: next.Objects[2].Vertices.UVs,
				Indices: append([]uint32(nil), next.Objects[2].Vertices.Indices...),
			}
			next.Objects[2].Vertices.Indices[7]++
		}},
		{"one position changes", false, func(next *SceneIR) { next.Objects[3].Vertices.Positions[5] += 0.5 }},
		{"a position becomes negative zero", false, func(next *SceneIR) {
			next.Objects[3].Vertices.Positions[3] = 0
			next.Objects[3].Vertices.Positions[4] = negativeZero
		}},
		{"a normal becomes NaN", false, func(next *SceneIR) { next.Objects[3].Vertices.Normals[2] = math.NaN() }},
		{"a position is NaN in both scenes", false, func(next *SceneIR) { next.Objects[3].Vertices.Positions[2] = math.NaN() }},
		{"a position is NaN in a shared stream", true, func(next *SceneIR) { next.Objects[3].Vertices.Positions[2] = math.NaN() }},
		{"a position is infinite in a shared stream", true, func(next *SceneIR) { next.Objects[3].Vertices.Positions[2] = math.Inf(1) }},
		{"a layout value changes", false, func(next *SceneIR) {
			next.Objects[0].ShaderLayout["uniformBlock"].(map[string]any)["size"] = 512.0
		}},
		{"a deep layout value changes", false, func(next *SceneIR) {
			fields := next.Objects[0].ShaderLayout["uniformBlock"].(map[string]any)["fields"].([]any)
			fields[3].(map[string]any)["offset"] = 999.0
		}},
		{"a layout key is added", false, func(next *SceneIR) { next.Objects[0].ShaderLayout["extra"] = true }},
		{"a layout holds a NaN", false, func(next *SceneIR) { next.Objects[0].ShaderLayout["nan"] = math.NaN() }},
		{"a layout holds a NaN in a shared tree", true, func(next *SceneIR) { next.Objects[0].ShaderLayout["nan"] = math.NaN() }},
		// The encoder rejects each value below, so the original algorithm calls the
		// object changed even though both scenes share the map that holds it.
		{"a layout holds an infinity in a shared tree", true, func(next *SceneIR) { next.Objects[0].ShaderLayout["inf"] = math.Inf(-1) }},
		{"a uniform table holds a complex number when shared", true, func(next *SceneIR) { next.Objects[0].CustomUniforms["c"] = complex(1, 2) }},
		{"a uniform table holds a function when shared", true, func(next *SceneIR) { next.Objects[0].CustomUniforms["f"] = func() {} }},
		{"a uniform table holds a channel when shared", true, func(next *SceneIR) { next.Objects[0].CustomUniforms["c"] = make(chan int) }},
		{"a layout struct member changes", false, func(next *SceneIR) {
			next.Objects[0].ShaderLayout["targetRequires"].(map[string]any)["gles"] = benchRequirements{Features: []string{"x"}}
		}},
		{"a uniform vector changes", false, func(next *SceneIR) { next.Objects[0].CustomUniforms["sunDir"].([]float64)[2] = 0.25 }},
		{"a uniform vector gets longer", false, func(next *SceneIR) {
			next.Objects[0].CustomUniforms["sunDir"] = append(next.Objects[0].CustomUniforms["sunDir"].([]float64), 1)
		}},
		{"a uniform has another type", false, func(next *SceneIR) { next.Objects[0].CustomUniforms["variant"] = 3 }},
		{"a uniform string changes", false, func(next *SceneIR) { next.Objects[0].CustomUniforms["environmentRadiance"] = "other" }},
		{"a tangent changes", false, func(next *SceneIR) { next.Objects[0].Vertices.Tangents[1] += 0.125 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			previous := build()
			var next SceneIR
			if testCase.share {
				// The records are copies; their slices, maps and pointers are the
				// previous scene's. A case that edits shared memory edits both scenes.
				next = shareStreams(previous)
			} else {
				next = build()
			}
			testCase.mutate(&next)
			want := diffSignature(referenceDiffCommands(previous, next))
			got := diffSignature(DiffCommands(previous, next))
			if !slices.Equal(got, want) {
				t.Errorf("DiffCommands = %v, the original algorithm gives %v", got, want)
			}
			if reverse, wantReverse := diffSignature(DiffCommands(next, previous)), diffSignature(referenceDiffCommands(next, previous)); !slices.Equal(reverse, wantReverse) {
				t.Errorf("reversed, DiffCommands = %v, the original algorithm gives %v", reverse, wantReverse)
			}
			if testCase.name == "unchanged copy" || testCase.name == "unchanged shared" {
				if len(got) != 0 {
					t.Errorf("an unchanged scene produced commands %v", got)
				}
			}
		})
	}
}
