package vm

import (
	"reflect"
	"testing"
	"unsafe"
)

// TestValueStaysCompact pins the size of Value.
//
// Every VM method takes and returns a Value by value, so one binary operation
// copies the struct three times. A field added in the wrong place silently
// widens every copy on the interactive hot path.
func TestValueStaysCompact(t *testing.T) {
	const want = 32
	if got := unsafe.Sizeof(Value{}); got != want {
		t.Errorf("unsafe.Sizeof(Value{}) = %d, want %d; store new payload state "+
			"in the spare bits of tag, not in a new field", got, want)
	}
}

// TestValuePassesInRegisters pins the leaf-field budget that keeps a Value in
// CPU registers.
//
// The amd64 register ABI gives the whole call at most 9 integer registers and
// 15 float registers. It assigns one register per leaf field. A binary method
// such as Add passes a receiver and one argument, so each Value may spend at
// most 4 integer-class leaf fields. A fifth pushes the argument back onto the
// stack, which is exactly the cost the tagged union removed.
//
// Verify the assembly directly with:
//
//	go build -gcflags=-S ./client/vm 2>&1 | grep 'TEXT.*Value..Add'
func TestValuePassesInRegisters(t *testing.T) {
	const maxIntFields = 4
	const maxFloatFields = 7

	ints, floats := countLeafFields(reflect.TypeOf(Value{}))
	if ints > maxIntFields {
		t.Errorf("Value has %d integer-class leaf fields, want at most %d; "+
			"a fifth spills the second Value argument to the stack",
			ints, maxIntFields)
	}
	if floats > maxFloatFields {
		t.Errorf("Value has %d float leaf fields, want at most %d", floats, maxFloatFields)
	}
}

// countLeafFields flattens t the way the register ABI does and returns the
// number of integer-class leaf fields and the number of float leaf fields.
// An array of more than one element disqualifies register passing outright,
// so it reports a count over any budget.
func countLeafFields(t reflect.Type) (ints, floats int) {
	switch t.Kind() {
	case reflect.Float32, reflect.Float64:
		return 0, 1
	case reflect.Complex64, reflect.Complex128:
		return 0, 2
	case reflect.String, reflect.Interface:
		return 2, 0
	case reflect.Slice:
		return 3, 0
	case reflect.Array:
		if t.Len() > 1 {
			// The ABI never puts a multi-element array in registers.
			return 99, 0
		}
		if t.Len() == 0 {
			return 0, 0
		}
		return countLeafFields(t.Elem())
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			fi, ff := countLeafFields(t.Field(i).Type)
			ints += fi
			floats += ff
		}
		return ints, floats
	default:
		return 1, 0
	}
}

// TestValueStaysIncomparable pins that Go refuses to compare two Values with ==.
//
// signal.New installs a == based change test for every comparable type. On a
// union, == compares the payload pointer, and OpIndexSet mutates the array
// behind that pointer in place. A comparable Value therefore makes a signal
// report "unchanged" for contents that just changed, and the signal never
// notifies. The pre-union layout held a map field, so it was never comparable.
// Keep it that way. Use Eq, which walks the payload.
func TestValueStaysIncomparable(t *testing.T) {
	if reflect.TypeOf(Value{}).Comparable() {
		t.Error("Value is comparable; signal.New will install a pointer-based " +
			"change test that misses in-place mutation. Keep the noCompare field.")
	}
}

// TestMapFitsOnePointerWord pins the assumption Map() and ObjectVal depend on.
//
// The union stores the object payload by copying the map header into the
// pointer word and reading it back. That only works while a map value is
// exactly one pointer wide. Both the gc toolchain and TinyGo represent a map
// as one pointer today. A toolchain that changes it must fail here, loudly,
// instead of corrupting every object Value.
func TestMapFitsOnePointerWord(t *testing.T) {
	var m map[string]Value
	if got, want := unsafe.Sizeof(m), unsafe.Sizeof(unsafe.Pointer(nil)); got != want {
		t.Fatalf("unsafe.Sizeof(map[string]Value) = %d, want %d; "+
			"Value.Map() and ObjectVal must stop reinterpreting the map header", got, want)
	}
}

// TestObjectValRoundTripsTheSameMap pins that the pointer-word round trip
// hands back the identical map, not a copy. The whole in-place mutation
// contract rests on it.
func TestObjectValRoundTripsTheSameMap(t *testing.T) {
	original := map[string]Value{"a": IntVal(1)}
	got := ObjectVal(original).Map()

	got["b"] = IntVal(2)
	if _, ok := original["b"]; !ok {
		t.Fatal("write through Map() did not reach the original map")
	}
	original["c"] = IntVal(3)
	if _, ok := got["c"]; !ok {
		t.Fatal("write through the original map did not reach Map()")
	}
	if len(got) != len(original) {
		t.Fatalf("len(Map()) = %d, len(original) = %d", len(got), len(original))
	}
}

// TestArrayValRoundTripsTheSameArray pins the array half of the same
// contract: List() must alias the caller's backing array.
func TestArrayValRoundTripsTheSameArray(t *testing.T) {
	original := []Value{IntVal(1), IntVal(2), IntVal(3)}
	v := ArrayVal(original)

	if !v.SetIndex(1, IntVal(9)) {
		t.Fatal("SetIndex reported no write")
	}
	if original[1].Number() != 9 {
		t.Fatalf("original[1] = %v, want 9; SetIndex did not write in place", original[1].Number())
	}
	got := v.List()
	if len(got) != len(original) {
		t.Fatalf("len(List()) = %d, want %d", len(got), len(original))
	}
	if &got[0] != &original[0] {
		t.Fatal("List() returned a copy, not an alias")
	}
}

// TestValueTagBitsDoNotOverlap pins the packing inside the tag byte.
//
// tag carries the payload kind, the boolean payload, and the control signal.
// Three separate bytes would cost three registers, which breaks the budget
// TestValuePassesInRegisters guards. Overlapping masks would corrupt one
// field when another is written, so pin them.
func TestValueTagBitsDoNotOverlap(t *testing.T) {
	controlBits := tagControlMask
	if tagKindMask&tagTruthBit != 0 {
		t.Errorf("tagKindMask %#x overlaps tagTruthBit %#x", tagKindMask, tagTruthBit)
	}
	if tagKindMask&controlBits != 0 {
		t.Errorf("tagKindMask %#x overlaps tagControlMask %#x", tagKindMask, controlBits)
	}
	if tagTruthBit&controlBits != 0 {
		t.Errorf("tagTruthBit %#x overlaps tagControlMask %#x", tagTruthBit, controlBits)
	}
	if uint8(kindClosure) > tagKindMask {
		t.Errorf("kindClosure %d does not fit in tagKindMask %#x", kindClosure, tagKindMask)
	}
	if uint8(ControlContinue)<<tagControlShift&^tagControlMask != 0 {
		t.Errorf("ControlContinue %d does not fit in tagControlMask %#x",
			ControlContinue, tagControlMask)
	}
}

// TestValueTagRoundTrips pins that each packed field survives a write to the
// others. A shared byte only works if every setter masks its own bits.
func TestValueTagRoundTrips(t *testing.T) {
	kinds := []Value{
		ZeroValue(0),
		StringVal("payload"),
		ArrayVal([]Value{IntVal(1)}),
		ObjectVal(map[string]Value{"k": IntVal(1)}),
		ClosureVal("fn", []string{"a"}, nil),
	}
	controls := []ControlSignal{ControlNone, ControlReturn, ControlBreak, ControlContinue}

	for _, base := range kinds {
		for _, truth := range []bool{false, true} {
			v := base
			if truth {
				v.tag |= tagTruthBit
			}
			for _, ctrl := range controls {
				got := v.WithControl(ctrl)
				if got.Control() != ctrl {
					t.Errorf("Control() = %d, want %d", got.Control(), ctrl)
				}
				if got.Truth() != truth {
					t.Errorf("WithControl(%d) changed Truth() to %v, want %v",
						ctrl, got.Truth(), truth)
				}
				if got.kind() != base.kind() {
					t.Errorf("WithControl(%d) changed kind() to %d, want %d",
						ctrl, got.kind(), base.kind())
				}
			}
		}
	}
}
