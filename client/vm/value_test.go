package vm

import (
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/island/program"
)

func TestStringVal(t *testing.T) {
	v := StringVal("hello")
	if v.Type != program.TypeString {
		t.Fatalf("expected TypeString, got %d", v.Type)
	}
	if v.Text() != "hello" {
		t.Fatalf("expected 'hello', got %q", v.Text())
	}
}

func TestIntVal(t *testing.T) {
	v := IntVal(42)
	if v.Type != program.TypeInt {
		t.Fatalf("expected TypeInt, got %d", v.Type)
	}
	if v.num != 42 {
		t.Fatalf("expected 42, got %f", v.num)
	}
}

func TestFloatVal(t *testing.T) {
	v := FloatVal(3.14)
	if v.Type != program.TypeFloat {
		t.Fatalf("expected TypeFloat, got %d", v.Type)
	}
	if v.num != 3.14 {
		t.Fatalf("expected 3.14, got %f", v.num)
	}
}

func TestBoolVal(t *testing.T) {
	v := BoolVal(true)
	if v.Type != program.TypeBool {
		t.Fatalf("expected TypeBool, got %d", v.Type)
	}
	if !v.Truth() {
		t.Fatal("expected true")
	}

	v2 := BoolVal(false)
	if v2.Truth() {
		t.Fatal("expected false")
	}
}

func TestZeroValue(t *testing.T) {
	tests := []struct {
		typ     program.ExprType
		wantStr string
		wantNum float64
		wantBol bool
	}{
		{program.TypeString, "", 0, false},
		{program.TypeInt, "", 0, false},
		{program.TypeFloat, "", 0, false},
		{program.TypeBool, "", 0, false},
		{program.TypeAny, "", 0, false},
	}
	for _, tt := range tests {
		v := ZeroValue(tt.typ)
		if v.Type != tt.typ {
			t.Errorf("ZeroValue(%d): type = %d, want %d", tt.typ, v.Type, tt.typ)
		}
		if v.Text() != tt.wantStr {
			t.Errorf("ZeroValue(%d): Str = %q, want %q", tt.typ, v.Text(), tt.wantStr)
		}
		if v.num != tt.wantNum {
			t.Errorf("ZeroValue(%d): Num = %f, want %f", tt.typ, v.num, tt.wantNum)
		}
		if v.Truth() != tt.wantBol {
			t.Errorf("ZeroValue(%d): Bool = %v, want %v", tt.typ, v.Truth(), tt.wantBol)
		}
	}
}

// --- Arithmetic ---

func TestAddInt(t *testing.T) {
	a := IntVal(10)
	b := IntVal(3)
	r := a.Add(b)
	if r.Type != program.TypeInt {
		t.Fatalf("expected TypeInt, got %d", r.Type)
	}
	if r.num != 13 {
		t.Fatalf("expected 13, got %f", r.num)
	}
}

func TestAddFloat(t *testing.T) {
	a := FloatVal(1.5)
	b := FloatVal(2.5)
	r := a.Add(b)
	if r.Type != program.TypeFloat {
		t.Fatalf("expected TypeFloat, got %d", r.Type)
	}
	if r.num != 4.0 {
		t.Fatalf("expected 4.0, got %f", r.num)
	}
}

func TestAddIntFloat(t *testing.T) {
	a := IntVal(1)
	b := FloatVal(2.5)
	r := a.Add(b)
	if r.Type != program.TypeFloat {
		t.Fatalf("expected TypeFloat for mixed add, got %d", r.Type)
	}
	if r.num != 3.5 {
		t.Fatalf("expected 3.5, got %f", r.num)
	}
}

func TestSubInt(t *testing.T) {
	a := IntVal(10)
	b := IntVal(3)
	r := a.Sub(b)
	if r.Type != program.TypeInt {
		t.Fatalf("expected TypeInt, got %d", r.Type)
	}
	if r.num != 7 {
		t.Fatalf("expected 7, got %f", r.num)
	}
}

func TestSubFloat(t *testing.T) {
	a := FloatVal(5.5)
	b := FloatVal(2.0)
	r := a.Sub(b)
	if r.num != 3.5 {
		t.Fatalf("expected 3.5, got %f", r.num)
	}
}

func TestMulInt(t *testing.T) {
	a := IntVal(4)
	b := IntVal(5)
	r := a.Mul(b)
	if r.Type != program.TypeInt {
		t.Fatalf("expected TypeInt, got %d", r.Type)
	}
	if r.num != 20 {
		t.Fatalf("expected 20, got %f", r.num)
	}
}

func TestMulFloat(t *testing.T) {
	a := FloatVal(2.5)
	b := FloatVal(4.0)
	r := a.Mul(b)
	if r.num != 10.0 {
		t.Fatalf("expected 10.0, got %f", r.num)
	}
}

func TestDivInt(t *testing.T) {
	a := IntVal(10)
	b := IntVal(3)
	r := a.Div(b)
	if r.Type != program.TypeInt {
		t.Fatalf("expected TypeInt, got %d", r.Type)
	}
	// integer division: 10 / 3 = 3
	if r.num != 3 {
		t.Fatalf("expected 3 (integer division), got %f", r.num)
	}
}

func TestDivFloat(t *testing.T) {
	a := FloatVal(10.0)
	b := FloatVal(3.0)
	r := a.Div(b)
	if r.Type != program.TypeFloat {
		t.Fatalf("expected TypeFloat, got %d", r.Type)
	}
	expected := 10.0 / 3.0
	if math.Abs(r.num-expected) > 1e-12 {
		t.Fatalf("expected %f, got %f", expected, r.num)
	}
}

func TestDivByZeroInt(t *testing.T) {
	a := IntVal(10)
	b := IntVal(0)
	r := a.Div(b)
	if r.num != 0 {
		t.Fatalf("expected 0 for div by zero, got %f", r.num)
	}
}

func TestDivByZeroFloat(t *testing.T) {
	a := FloatVal(10.0)
	b := FloatVal(0.0)
	r := a.Div(b)
	if r.num != 0 {
		t.Fatalf("expected 0 for div by zero, got %f", r.num)
	}
}

func TestModInt(t *testing.T) {
	a := IntVal(10)
	b := IntVal(3)
	r := a.Mod(b)
	if r.Type != program.TypeInt {
		t.Fatalf("expected TypeInt, got %d", r.Type)
	}
	if r.num != 1 {
		t.Fatalf("expected 1, got %f", r.num)
	}
}

func TestModFloat(t *testing.T) {
	a := FloatVal(10.5)
	b := FloatVal(3.0)
	r := a.Mod(b)
	expected := math.Mod(10.5, 3.0)
	if math.Abs(r.num-expected) > 1e-12 {
		t.Fatalf("expected %f, got %f", expected, r.num)
	}
}

func TestNeg(t *testing.T) {
	a := IntVal(5)
	r := a.Neg()
	if r.num != -5 {
		t.Fatalf("expected -5, got %f", r.num)
	}
	if r.Type != program.TypeInt {
		t.Fatalf("expected TypeInt, got %d", r.Type)
	}

	b := FloatVal(3.14)
	r2 := b.Neg()
	if r2.num != -3.14 {
		t.Fatalf("expected -3.14, got %f", r2.num)
	}
}

// --- Integer semantics ---

func TestIntSemantics(t *testing.T) {
	// Integer arithmetic should truncate, not produce fractional results
	a := IntVal(7)
	b := IntVal(2)

	div := a.Div(b)
	if div.num != 3 {
		t.Fatalf("7/2 should be 3 (int), got %f", div.num)
	}

	mod := a.Mod(b)
	if mod.num != 1 {
		t.Fatalf("7%%2 should be 1, got %f", mod.num)
	}
}

// --- Comparisons ---

func TestEq(t *testing.T) {
	if !IntVal(5).Eq(IntVal(5)).Truth() {
		t.Fatal("5 == 5 should be true")
	}
	if IntVal(5).Eq(IntVal(6)).Truth() {
		t.Fatal("5 == 6 should be false")
	}
	if !StringVal("hi").Eq(StringVal("hi")).Truth() {
		t.Fatal(`"hi" == "hi" should be true`)
	}
	if StringVal("hi").Eq(StringVal("bye")).Truth() {
		t.Fatal(`"hi" == "bye" should be false`)
	}
	if !BoolVal(true).Eq(BoolVal(true)).Truth() {
		t.Fatal("true == true should be true")
	}
}

func TestNeq(t *testing.T) {
	if IntVal(5).Neq(IntVal(5)).Truth() {
		t.Fatal("5 != 5 should be false")
	}
	if !IntVal(5).Neq(IntVal(6)).Truth() {
		t.Fatal("5 != 6 should be true")
	}
}

func TestLt(t *testing.T) {
	if !IntVal(3).Lt(IntVal(5)).Truth() {
		t.Fatal("3 < 5 should be true")
	}
	if IntVal(5).Lt(IntVal(3)).Truth() {
		t.Fatal("5 < 3 should be false")
	}
	if IntVal(5).Lt(IntVal(5)).Truth() {
		t.Fatal("5 < 5 should be false")
	}
}

func TestGt(t *testing.T) {
	if !IntVal(5).Gt(IntVal(3)).Truth() {
		t.Fatal("5 > 3 should be true")
	}
	if IntVal(3).Gt(IntVal(5)).Truth() {
		t.Fatal("3 > 5 should be false")
	}
}

func TestLte(t *testing.T) {
	if !IntVal(3).Lte(IntVal(5)).Truth() {
		t.Fatal("3 <= 5 should be true")
	}
	if !IntVal(5).Lte(IntVal(5)).Truth() {
		t.Fatal("5 <= 5 should be true")
	}
	if IntVal(6).Lte(IntVal(5)).Truth() {
		t.Fatal("6 <= 5 should be false")
	}
}

func TestGte(t *testing.T) {
	if !IntVal(5).Gte(IntVal(3)).Truth() {
		t.Fatal("5 >= 3 should be true")
	}
	if !IntVal(5).Gte(IntVal(5)).Truth() {
		t.Fatal("5 >= 5 should be true")
	}
	if IntVal(3).Gte(IntVal(5)).Truth() {
		t.Fatal("3 >= 5 should be false")
	}
}

// --- Boolean ops ---

func TestAnd(t *testing.T) {
	if !BoolVal(true).And(BoolVal(true)).Truth() {
		t.Fatal("true && true should be true")
	}
	if BoolVal(true).And(BoolVal(false)).Truth() {
		t.Fatal("true && false should be false")
	}
	if BoolVal(false).And(BoolVal(true)).Truth() {
		t.Fatal("false && true should be false")
	}
}

func TestOr(t *testing.T) {
	if !BoolVal(true).Or(BoolVal(false)).Truth() {
		t.Fatal("true || false should be true")
	}
	if !BoolVal(false).Or(BoolVal(true)).Truth() {
		t.Fatal("false || true should be true")
	}
	if BoolVal(false).Or(BoolVal(false)).Truth() {
		t.Fatal("false || false should be false")
	}
}

func TestNot(t *testing.T) {
	if BoolVal(true).Not().Truth() {
		t.Fatal("!true should be false")
	}
	if !BoolVal(false).Not().Truth() {
		t.Fatal("!false should be true")
	}
}

// --- String ops ---

func TestConcat(t *testing.T) {
	a := StringVal("hello")
	b := StringVal(" world")
	r := a.Concat(b)
	if r.Text() != "hello world" {
		t.Fatalf("expected 'hello world', got %q", r.Text())
	}
	if r.Type != program.TypeString {
		t.Fatalf("expected TypeString, got %d", r.Type)
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		val  Value
		want string
	}{
		{StringVal("hello"), "hello"},
		{IntVal(42), "42"},
		{FloatVal(3.14), "3.14"},
		{BoolVal(true), "true"},
		{BoolVal(false), "false"},
	}
	for _, tt := range tests {
		got := tt.val.String()
		if got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

// --- Comparison return types ---

func TestComparisonReturnsBoolVal(t *testing.T) {
	r := IntVal(1).Eq(IntVal(1))
	if r.Type != program.TypeBool {
		t.Fatalf("Eq should return TypeBool, got %d", r.Type)
	}
	r = IntVal(1).Lt(IntVal(2))
	if r.Type != program.TypeBool {
		t.Fatalf("Lt should return TypeBool, got %d", r.Type)
	}
}

// --- Array operations ---

func TestArrayValCreation(t *testing.T) {
	items := []Value{IntVal(1), IntVal(2), IntVal(3)}
	v := ArrayVal(items)
	if v.Type != program.TypeAny {
		t.Fatalf("expected TypeAny, got %d", v.Type)
	}
	if len(v.List()) != 3 {
		t.Fatalf("expected 3 items, got %d", len(v.List()))
	}
}

func TestArrayLen(t *testing.T) {
	v := ArrayVal([]Value{IntVal(1), IntVal(2), IntVal(3)})
	if v.Len() != 3 {
		t.Fatalf("expected Len()=3, got %d", v.Len())
	}

	// String Len still works
	s := StringVal("hello")
	if s.Len() != 5 {
		t.Fatalf("expected Len()=5 for string, got %d", s.Len())
	}
}

func TestAppendVal(t *testing.T) {
	v := ArrayVal([]Value{IntVal(1), IntVal(2)})
	v2 := v.AppendVal(IntVal(3))
	if len(v2.List()) != 3 {
		t.Fatalf("expected 3 items after append, got %d", len(v2.List()))
	}
	if v2.List()[2].num != 3 {
		t.Fatalf("expected last item=3, got %f", v2.List()[2].num)
	}
	// Original should be unchanged (immutability)
	if len(v.List()) != 2 {
		t.Fatalf("original should still have 2 items, got %d", len(v.List()))
	}
}

func TestFilterFunc(t *testing.T) {
	v := ArrayVal([]Value{IntVal(1), IntVal(2), IntVal(3), IntVal(4)})
	even := v.FilterFunc(func(val Value) bool {
		return int(val.num)%2 == 0
	})
	if len(even.List()) != 2 {
		t.Fatalf("expected 2 even items, got %d", len(even.List()))
	}
	if even.List()[0].num != 2 || even.List()[1].num != 4 {
		t.Fatalf("expected [2, 4], got [%f, %f]", even.List()[0].num, even.List()[1].num)
	}
}

func TestMapFunc(t *testing.T) {
	v := ArrayVal([]Value{IntVal(1), IntVal(2), IntVal(3)})
	doubled := v.MapFunc(func(val Value, i int) Value {
		return IntVal(int(val.num) * 2)
	})
	if len(doubled.List()) != 3 {
		t.Fatalf("expected 3 items, got %d", len(doubled.List()))
	}
	if doubled.List()[0].num != 2 || doubled.List()[1].num != 4 || doubled.List()[2].num != 6 {
		t.Fatalf("expected [2, 4, 6]")
	}
}

func TestContainsValArray(t *testing.T) {
	v := ArrayVal([]Value{IntVal(1), IntVal(2), IntVal(3)})
	if !v.ContainsVal(IntVal(2)).Truth() {
		t.Fatal("array should contain 2")
	}
	if v.ContainsVal(IntVal(5)).Truth() {
		t.Fatal("array should not contain 5")
	}
}

func TestContainsValString(t *testing.T) {
	v := StringVal("hello world")
	if !v.ContainsVal(StringVal("world")).Truth() {
		t.Fatal("string should contain 'world'")
	}
	if v.ContainsVal(StringVal("xyz")).Truth() {
		t.Fatal("string should not contain 'xyz'")
	}
}

func TestArrayString(t *testing.T) {
	v := ArrayVal([]Value{IntVal(1), StringVal("two"), BoolVal(true)})
	got := v.String()
	want := "[1, two, true]"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestArrayEq(t *testing.T) {
	a := ArrayVal([]Value{IntVal(1), IntVal(2)})
	b := ArrayVal([]Value{IntVal(1), IntVal(2)})
	c := ArrayVal([]Value{IntVal(1), IntVal(3)})
	d := ArrayVal([]Value{IntVal(1)})

	if !a.Eq(b).Truth() {
		t.Fatal("[1,2] == [1,2] should be true")
	}
	if a.Eq(c).Truth() {
		t.Fatal("[1,2] == [1,3] should be false")
	}
	if a.Eq(d).Truth() {
		t.Fatal("[1,2] == [1] should be false (different length)")
	}
}

// TestValueStringSelfReferentialArrayDoesNotOverflow pins the
// unrecoverable-crash reproduction from OpIndexSet's in-place
// mutation. `arr[0] = arr` makes Items[0] alias the very array it
// lives in. Value.Items is a shared slice header, so the write is
// visible through every alias (see lhs_set.go's file header).
//
// The pre-fix String() recursed over Items with no depth guard and
// no visited set. A self-referential Value recurses forever, so the
// test hung until the goroutine stack hit its limit. The process
// then died with "fatal error: stack overflow", which recover()
// cannot catch. String() must now degrade to the cycle sentinel
// instead.
func TestValueStringSelfReferentialArrayDoesNotOverflow(t *testing.T) {
	arr := ArrayVal([]Value{{}})
	arr.List()[0] = arr // build the cycle exactly as OpIndexSet would

	got := arr.String()
	if got == "" {
		t.Fatal("String() on a self-referential array returned empty; want a bounded, non-empty result")
	}
	if !strings.Contains(got, cycleSentinel) {
		t.Fatalf("String() on a self-referential array = %q, want it to contain the cycle sentinel %q", got, cycleSentinel)
	}
}

// TestValueEqSelfReferentialArrayDoesNotOverflow mirrors the String()
// regression above for Eq(), which recurses over Items the same way.
func TestValueEqSelfReferentialArrayDoesNotOverflow(t *testing.T) {
	arr := ArrayVal([]Value{{}})
	arr.List()[0] = arr

	other := ArrayVal([]Value{{}})
	other.List()[0] = other

	// The assertion here is simply that Eq returns without
	// crashing. Depth-capped comparison on a genuine cycle is
	// inherently inconclusive. So BoolVal(false) is the safe answer
	// once the depth guard fires.
	got := arr.Eq(other)
	if got.Type != program.TypeBool {
		t.Fatalf("Eq on self-referential arrays = %+v, want a BoolVal", got)
	}
}

func TestSliceVal(t *testing.T) {
	v := ArrayVal([]Value{IntVal(1), IntVal(2), IntVal(3), IntVal(4)})
	s := v.SliceVal(1, 3)
	if len(s.List()) != 2 {
		t.Fatalf("expected 2 items, got %d", len(s.List()))
	}
	if s.List()[0].num != 2 || s.List()[1].num != 3 {
		t.Fatalf("expected [2, 3]")
	}
}

func TestSliceValBoundsClamp(t *testing.T) {
	v := ArrayVal([]Value{IntVal(1), IntVal(2)})
	s := v.SliceVal(-1, 100)
	if len(s.List()) != 2 {
		t.Fatalf("expected clamped to 2 items, got %d", len(s.List()))
	}
}

// TestSliceValNegativeEndDoesNotPanic pins the exact crash
// reproduction for `items[0:len(items)-1]` on an empty array. end
// (len(items)-1) evaluates to -1. The pre-fix SliceVal only clamped
// end on its high side (end > n), never on its low side.
//
// A negative end reached the start-over-end swap as the smaller
// value. It then reached v.Items[start:end] as a negative index and
// panicked with "slice bounds out of range [:-1]". Before the fix
// this test panicked. It now must return an empty array with no
// panic.
func TestSliceValNegativeEndDoesNotPanic(t *testing.T) {
	v := ArrayVal(nil)
	got := v.SliceVal(0, len(v.List())-1)
	if len(got.List()) != 0 {
		t.Fatalf("SliceVal(0, -1) on empty array = %+v, want 0 items", got)
	}
}

// TestSliceValInfEndDoesNotPanic pins the float-literal
// reproduction. A source expression like `9e999` parses to +Inf,
// and int(+Inf) is math.MinInt64 on amd64. Passed straight through
// as `end`, the old SliceVal panicked with "slice bounds out of
// range [:-9223372036854775808]".
//
// The fixed SliceVal clamps end into [0, n] regardless of how it
// got there. The degenerate cast lands on a deeply negative int
// here, so the safe, non-panicking answer is an empty slice, not
// the full array.
//
// vm.go's safeBoundInt intercepts the float itself before this
// conversion happens. So a real OpSlice dispatch sees +Inf clamp
// toward the array's length instead — see
// TestVMOpSliceFloatOverflowEndDoesNotPanic in safety_test.go. This
// test pins SliceVal's own defense-in-depth clamp in isolation.
func TestSliceValInfEndDoesNotPanic(t *testing.T) {
	v := ArrayVal([]Value{IntVal(1), IntVal(2)})
	end := int(math.Inf(1))
	got := v.SliceVal(0, end)
	if len(got.List()) != 0 {
		t.Fatalf("SliceVal(0, MinInt64-from-+Inf) = %+v, want an empty (not panicking) result", got)
	}
}

func TestJoinVal(t *testing.T) {
	v := ArrayVal([]Value{StringVal("a"), StringVal("b"), StringVal("c")})
	joined := v.JoinVal(", ")
	if joined.Text() != "a, b, c" {
		t.Fatalf("expected 'a, b, c', got %q", joined.Text())
	}
}

// --- String methods ---

func TestToUpper(t *testing.T) {
	v := StringVal("hello")
	if v.ToUpper().Text() != "HELLO" {
		t.Fatalf("expected 'HELLO', got %q", v.ToUpper().Text())
	}
}

func TestToLower(t *testing.T) {
	v := StringVal("HELLO")
	if v.ToLower().Text() != "hello" {
		t.Fatalf("expected 'hello', got %q", v.ToLower().Text())
	}
}

func TestTrimVal(t *testing.T) {
	v := StringVal("  hello  ")
	if v.TrimVal().Text() != "hello" {
		t.Fatalf("expected 'hello', got %q", v.TrimVal().Text())
	}
}

func TestSplitVal(t *testing.T) {
	v := StringVal("a,b,c")
	result := v.SplitVal(",")
	if len(result.List()) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result.List()))
	}
	if result.List()[0].Text() != "a" || result.List()[1].Text() != "b" || result.List()[2].Text() != "c" {
		t.Fatal("split items don't match")
	}
}

func TestReplaceVal(t *testing.T) {
	v := StringVal("hello world world")
	r := v.ReplaceVal("world", "go")
	if r.Text() != "hello go go" {
		t.Fatalf("expected 'hello go go', got %q", r.Text())
	}
}

func TestSubstringVal(t *testing.T) {
	v := StringVal("hello world")
	s := v.SubstringVal(0, 5)
	if s.Text() != "hello" {
		t.Fatalf("expected 'hello', got %q", s.Text())
	}
}

func TestSubstringValBoundsClamp(t *testing.T) {
	v := StringVal("hi")
	s := v.SubstringVal(-1, 100)
	if s.Text() != "hi" {
		t.Fatalf("expected clamped to 'hi', got %q", s.Text())
	}
}

// TestSubstringValNegativeEndDoesNotPanic pins the exact crash
// reproduction for `name[0:len(name)-1]` on an empty string. end
// evaluates to -1. The pre-fix SubstringVal panicked the same way
// SliceVal did ("slice bounds out of range [:-1]").
func TestSubstringValNegativeEndDoesNotPanic(t *testing.T) {
	v := StringVal("")
	got := v.SubstringVal(0, len(v.Text())-1)
	if got.Text() != "" {
		t.Fatalf("SubstringVal(0, -1) on empty string = %q, want \"\"", got.Text())
	}
}

// TestSubstringValInfEndDoesNotPanic mirrors
// TestSliceValInfEndDoesNotPanic for the string path: the degenerate
// MinInt64 cast clamps to an empty result rather than panicking.
func TestSubstringValInfEndDoesNotPanic(t *testing.T) {
	v := StringVal("hello")
	end := int(math.Inf(1))
	got := v.SubstringVal(0, end)
	if got.Text() != "" {
		t.Fatalf("SubstringVal(0, MinInt64-from-+Inf) = %q, want an empty (not panicking) result", got.Text())
	}
}

func TestStartsWithVal(t *testing.T) {
	v := StringVal("hello world")
	if !v.StartsWithVal(StringVal("hello")).Truth() {
		t.Fatal("should start with 'hello'")
	}
	if v.StartsWithVal(StringVal("world")).Truth() {
		t.Fatal("should not start with 'world'")
	}
}

func TestEndsWithVal(t *testing.T) {
	v := StringVal("hello world")
	if !v.EndsWithVal(StringVal("world")).Truth() {
		t.Fatal("should end with 'world'")
	}
	if v.EndsWithVal(StringVal("hello")).Truth() {
		t.Fatal("should not end with 'hello'")
	}
}

// --- Type conversions ---

func TestToStringVal(t *testing.T) {
	if IntVal(42).ToStringVal().Text() != "42" {
		t.Fatal("int 42 should convert to string '42'")
	}
	if FloatVal(3.14).ToStringVal().Text() != "3.14" {
		t.Fatal("float 3.14 should convert to string '3.14'")
	}
	if BoolVal(true).ToStringVal().Text() != "true" {
		t.Fatal("bool true should convert to string 'true'")
	}
	if StringVal("hi").ToStringVal().Text() != "hi" {
		t.Fatal("string should stay the same")
	}
}

func TestToIntVal(t *testing.T) {
	// From string
	v := StringVal("42").ToIntVal()
	if v.Type != program.TypeInt || v.num != 42 {
		t.Fatalf("expected IntVal(42), got type=%d num=%f", v.Type, v.num)
	}

	// From float (truncates)
	v = FloatVal(3.9).ToIntVal()
	if v.Type != program.TypeInt || v.num != 3 {
		t.Fatalf("expected IntVal(3), got type=%d num=%f", v.Type, v.num)
	}

	// From bool
	if BoolVal(true).ToIntVal().num != 1 {
		t.Fatal("true should convert to 1")
	}
	if BoolVal(false).ToIntVal().num != 0 {
		t.Fatal("false should convert to 0")
	}

	// Invalid string
	v = StringVal("abc").ToIntVal()
	if v.num != 0 {
		t.Fatalf("invalid string should give 0, got %f", v.num)
	}
}

func TestToFloatVal(t *testing.T) {
	// From string
	v := StringVal("3.14").ToFloatVal()
	if v.Type != program.TypeFloat || v.num != 3.14 {
		t.Fatalf("expected FloatVal(3.14), got type=%d num=%f", v.Type, v.num)
	}

	// From int (promotes)
	v = IntVal(5).ToFloatVal()
	if v.Type != program.TypeFloat || v.num != 5 {
		t.Fatalf("expected FloatVal(5), got type=%d num=%f", v.Type, v.num)
	}

	// Invalid string
	v = StringVal("abc").ToFloatVal()
	if v.num != 0 {
		t.Fatalf("invalid string should give 0, got %f", v.num)
	}
}
