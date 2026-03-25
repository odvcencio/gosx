package vm

import (
	"math"
	"testing"

	"github.com/odvcencio/gosx/island/program"
)

func TestStringVal(t *testing.T) {
	v := StringVal("hello")
	if v.Type != program.TypeString {
		t.Fatalf("expected TypeString, got %d", v.Type)
	}
	if v.Str != "hello" {
		t.Fatalf("expected 'hello', got %q", v.Str)
	}
}

func TestIntVal(t *testing.T) {
	v := IntVal(42)
	if v.Type != program.TypeInt {
		t.Fatalf("expected TypeInt, got %d", v.Type)
	}
	if v.Num != 42 {
		t.Fatalf("expected 42, got %f", v.Num)
	}
}

func TestFloatVal(t *testing.T) {
	v := FloatVal(3.14)
	if v.Type != program.TypeFloat {
		t.Fatalf("expected TypeFloat, got %d", v.Type)
	}
	if v.Num != 3.14 {
		t.Fatalf("expected 3.14, got %f", v.Num)
	}
}

func TestBoolVal(t *testing.T) {
	v := BoolVal(true)
	if v.Type != program.TypeBool {
		t.Fatalf("expected TypeBool, got %d", v.Type)
	}
	if !v.Bool {
		t.Fatal("expected true")
	}

	v2 := BoolVal(false)
	if v2.Bool {
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
		if v.Str != tt.wantStr {
			t.Errorf("ZeroValue(%d): Str = %q, want %q", tt.typ, v.Str, tt.wantStr)
		}
		if v.Num != tt.wantNum {
			t.Errorf("ZeroValue(%d): Num = %f, want %f", tt.typ, v.Num, tt.wantNum)
		}
		if v.Bool != tt.wantBol {
			t.Errorf("ZeroValue(%d): Bool = %v, want %v", tt.typ, v.Bool, tt.wantBol)
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
	if r.Num != 13 {
		t.Fatalf("expected 13, got %f", r.Num)
	}
}

func TestAddFloat(t *testing.T) {
	a := FloatVal(1.5)
	b := FloatVal(2.5)
	r := a.Add(b)
	if r.Type != program.TypeFloat {
		t.Fatalf("expected TypeFloat, got %d", r.Type)
	}
	if r.Num != 4.0 {
		t.Fatalf("expected 4.0, got %f", r.Num)
	}
}

func TestAddIntFloat(t *testing.T) {
	a := IntVal(1)
	b := FloatVal(2.5)
	r := a.Add(b)
	if r.Type != program.TypeFloat {
		t.Fatalf("expected TypeFloat for mixed add, got %d", r.Type)
	}
	if r.Num != 3.5 {
		t.Fatalf("expected 3.5, got %f", r.Num)
	}
}

func TestSubInt(t *testing.T) {
	a := IntVal(10)
	b := IntVal(3)
	r := a.Sub(b)
	if r.Type != program.TypeInt {
		t.Fatalf("expected TypeInt, got %d", r.Type)
	}
	if r.Num != 7 {
		t.Fatalf("expected 7, got %f", r.Num)
	}
}

func TestSubFloat(t *testing.T) {
	a := FloatVal(5.5)
	b := FloatVal(2.0)
	r := a.Sub(b)
	if r.Num != 3.5 {
		t.Fatalf("expected 3.5, got %f", r.Num)
	}
}

func TestMulInt(t *testing.T) {
	a := IntVal(4)
	b := IntVal(5)
	r := a.Mul(b)
	if r.Type != program.TypeInt {
		t.Fatalf("expected TypeInt, got %d", r.Type)
	}
	if r.Num != 20 {
		t.Fatalf("expected 20, got %f", r.Num)
	}
}

func TestMulFloat(t *testing.T) {
	a := FloatVal(2.5)
	b := FloatVal(4.0)
	r := a.Mul(b)
	if r.Num != 10.0 {
		t.Fatalf("expected 10.0, got %f", r.Num)
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
	if r.Num != 3 {
		t.Fatalf("expected 3 (integer division), got %f", r.Num)
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
	if math.Abs(r.Num-expected) > 1e-12 {
		t.Fatalf("expected %f, got %f", expected, r.Num)
	}
}

func TestDivByZeroInt(t *testing.T) {
	a := IntVal(10)
	b := IntVal(0)
	r := a.Div(b)
	if r.Num != 0 {
		t.Fatalf("expected 0 for div by zero, got %f", r.Num)
	}
}

func TestDivByZeroFloat(t *testing.T) {
	a := FloatVal(10.0)
	b := FloatVal(0.0)
	r := a.Div(b)
	if r.Num != 0 {
		t.Fatalf("expected 0 for div by zero, got %f", r.Num)
	}
}

func TestModInt(t *testing.T) {
	a := IntVal(10)
	b := IntVal(3)
	r := a.Mod(b)
	if r.Type != program.TypeInt {
		t.Fatalf("expected TypeInt, got %d", r.Type)
	}
	if r.Num != 1 {
		t.Fatalf("expected 1, got %f", r.Num)
	}
}

func TestModFloat(t *testing.T) {
	a := FloatVal(10.5)
	b := FloatVal(3.0)
	r := a.Mod(b)
	expected := math.Mod(10.5, 3.0)
	if math.Abs(r.Num-expected) > 1e-12 {
		t.Fatalf("expected %f, got %f", expected, r.Num)
	}
}

func TestNeg(t *testing.T) {
	a := IntVal(5)
	r := a.Neg()
	if r.Num != -5 {
		t.Fatalf("expected -5, got %f", r.Num)
	}
	if r.Type != program.TypeInt {
		t.Fatalf("expected TypeInt, got %d", r.Type)
	}

	b := FloatVal(3.14)
	r2 := b.Neg()
	if r2.Num != -3.14 {
		t.Fatalf("expected -3.14, got %f", r2.Num)
	}
}

// --- Integer semantics ---

func TestIntSemantics(t *testing.T) {
	// Integer arithmetic should truncate, not produce fractional results
	a := IntVal(7)
	b := IntVal(2)

	div := a.Div(b)
	if div.Num != 3 {
		t.Fatalf("7/2 should be 3 (int), got %f", div.Num)
	}

	mod := a.Mod(b)
	if mod.Num != 1 {
		t.Fatalf("7%%2 should be 1, got %f", mod.Num)
	}
}

// --- Comparisons ---

func TestEq(t *testing.T) {
	if !IntVal(5).Eq(IntVal(5)).Bool {
		t.Fatal("5 == 5 should be true")
	}
	if IntVal(5).Eq(IntVal(6)).Bool {
		t.Fatal("5 == 6 should be false")
	}
	if !StringVal("hi").Eq(StringVal("hi")).Bool {
		t.Fatal(`"hi" == "hi" should be true`)
	}
	if StringVal("hi").Eq(StringVal("bye")).Bool {
		t.Fatal(`"hi" == "bye" should be false`)
	}
	if !BoolVal(true).Eq(BoolVal(true)).Bool {
		t.Fatal("true == true should be true")
	}
}

func TestNeq(t *testing.T) {
	if IntVal(5).Neq(IntVal(5)).Bool {
		t.Fatal("5 != 5 should be false")
	}
	if !IntVal(5).Neq(IntVal(6)).Bool {
		t.Fatal("5 != 6 should be true")
	}
}

func TestLt(t *testing.T) {
	if !IntVal(3).Lt(IntVal(5)).Bool {
		t.Fatal("3 < 5 should be true")
	}
	if IntVal(5).Lt(IntVal(3)).Bool {
		t.Fatal("5 < 3 should be false")
	}
	if IntVal(5).Lt(IntVal(5)).Bool {
		t.Fatal("5 < 5 should be false")
	}
}

func TestGt(t *testing.T) {
	if !IntVal(5).Gt(IntVal(3)).Bool {
		t.Fatal("5 > 3 should be true")
	}
	if IntVal(3).Gt(IntVal(5)).Bool {
		t.Fatal("3 > 5 should be false")
	}
}

func TestLte(t *testing.T) {
	if !IntVal(3).Lte(IntVal(5)).Bool {
		t.Fatal("3 <= 5 should be true")
	}
	if !IntVal(5).Lte(IntVal(5)).Bool {
		t.Fatal("5 <= 5 should be true")
	}
	if IntVal(6).Lte(IntVal(5)).Bool {
		t.Fatal("6 <= 5 should be false")
	}
}

func TestGte(t *testing.T) {
	if !IntVal(5).Gte(IntVal(3)).Bool {
		t.Fatal("5 >= 3 should be true")
	}
	if !IntVal(5).Gte(IntVal(5)).Bool {
		t.Fatal("5 >= 5 should be true")
	}
	if IntVal(3).Gte(IntVal(5)).Bool {
		t.Fatal("3 >= 5 should be false")
	}
}

// --- Boolean ops ---

func TestAnd(t *testing.T) {
	if !BoolVal(true).And(BoolVal(true)).Bool {
		t.Fatal("true && true should be true")
	}
	if BoolVal(true).And(BoolVal(false)).Bool {
		t.Fatal("true && false should be false")
	}
	if BoolVal(false).And(BoolVal(true)).Bool {
		t.Fatal("false && true should be false")
	}
}

func TestOr(t *testing.T) {
	if !BoolVal(true).Or(BoolVal(false)).Bool {
		t.Fatal("true || false should be true")
	}
	if !BoolVal(false).Or(BoolVal(true)).Bool {
		t.Fatal("false || true should be true")
	}
	if BoolVal(false).Or(BoolVal(false)).Bool {
		t.Fatal("false || false should be false")
	}
}

func TestNot(t *testing.T) {
	if BoolVal(true).Not().Bool {
		t.Fatal("!true should be false")
	}
	if !BoolVal(false).Not().Bool {
		t.Fatal("!false should be true")
	}
}

// --- String ops ---

func TestConcat(t *testing.T) {
	a := StringVal("hello")
	b := StringVal(" world")
	r := a.Concat(b)
	if r.Str != "hello world" {
		t.Fatalf("expected 'hello world', got %q", r.Str)
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
	if len(v.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(v.Items))
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
	if len(v2.Items) != 3 {
		t.Fatalf("expected 3 items after append, got %d", len(v2.Items))
	}
	if v2.Items[2].Num != 3 {
		t.Fatalf("expected last item=3, got %f", v2.Items[2].Num)
	}
	// Original should be unchanged (immutability)
	if len(v.Items) != 2 {
		t.Fatalf("original should still have 2 items, got %d", len(v.Items))
	}
}

func TestFilterFunc(t *testing.T) {
	v := ArrayVal([]Value{IntVal(1), IntVal(2), IntVal(3), IntVal(4)})
	even := v.FilterFunc(func(val Value) bool {
		return int(val.Num)%2 == 0
	})
	if len(even.Items) != 2 {
		t.Fatalf("expected 2 even items, got %d", len(even.Items))
	}
	if even.Items[0].Num != 2 || even.Items[1].Num != 4 {
		t.Fatalf("expected [2, 4], got [%f, %f]", even.Items[0].Num, even.Items[1].Num)
	}
}

func TestMapFunc(t *testing.T) {
	v := ArrayVal([]Value{IntVal(1), IntVal(2), IntVal(3)})
	doubled := v.MapFunc(func(val Value, i int) Value {
		return IntVal(int(val.Num) * 2)
	})
	if len(doubled.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(doubled.Items))
	}
	if doubled.Items[0].Num != 2 || doubled.Items[1].Num != 4 || doubled.Items[2].Num != 6 {
		t.Fatalf("expected [2, 4, 6]")
	}
}

func TestContainsValArray(t *testing.T) {
	v := ArrayVal([]Value{IntVal(1), IntVal(2), IntVal(3)})
	if !v.ContainsVal(IntVal(2)).Bool {
		t.Fatal("array should contain 2")
	}
	if v.ContainsVal(IntVal(5)).Bool {
		t.Fatal("array should not contain 5")
	}
}

func TestContainsValString(t *testing.T) {
	v := StringVal("hello world")
	if !v.ContainsVal(StringVal("world")).Bool {
		t.Fatal("string should contain 'world'")
	}
	if v.ContainsVal(StringVal("xyz")).Bool {
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

	if !a.Eq(b).Bool {
		t.Fatal("[1,2] == [1,2] should be true")
	}
	if a.Eq(c).Bool {
		t.Fatal("[1,2] == [1,3] should be false")
	}
	if a.Eq(d).Bool {
		t.Fatal("[1,2] == [1] should be false (different length)")
	}
}

func TestSliceVal(t *testing.T) {
	v := ArrayVal([]Value{IntVal(1), IntVal(2), IntVal(3), IntVal(4)})
	s := v.SliceVal(1, 3)
	if len(s.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(s.Items))
	}
	if s.Items[0].Num != 2 || s.Items[1].Num != 3 {
		t.Fatalf("expected [2, 3]")
	}
}

func TestSliceValBoundsClamp(t *testing.T) {
	v := ArrayVal([]Value{IntVal(1), IntVal(2)})
	s := v.SliceVal(-1, 100)
	if len(s.Items) != 2 {
		t.Fatalf("expected clamped to 2 items, got %d", len(s.Items))
	}
}

func TestJoinVal(t *testing.T) {
	v := ArrayVal([]Value{StringVal("a"), StringVal("b"), StringVal("c")})
	joined := v.JoinVal(", ")
	if joined.Str != "a, b, c" {
		t.Fatalf("expected 'a, b, c', got %q", joined.Str)
	}
}

// --- String methods ---

func TestToUpper(t *testing.T) {
	v := StringVal("hello")
	if v.ToUpper().Str != "HELLO" {
		t.Fatalf("expected 'HELLO', got %q", v.ToUpper().Str)
	}
}

func TestToLower(t *testing.T) {
	v := StringVal("HELLO")
	if v.ToLower().Str != "hello" {
		t.Fatalf("expected 'hello', got %q", v.ToLower().Str)
	}
}

func TestTrimVal(t *testing.T) {
	v := StringVal("  hello  ")
	if v.TrimVal().Str != "hello" {
		t.Fatalf("expected 'hello', got %q", v.TrimVal().Str)
	}
}

func TestSplitVal(t *testing.T) {
	v := StringVal("a,b,c")
	result := v.SplitVal(",")
	if len(result.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result.Items))
	}
	if result.Items[0].Str != "a" || result.Items[1].Str != "b" || result.Items[2].Str != "c" {
		t.Fatal("split items don't match")
	}
}

func TestReplaceVal(t *testing.T) {
	v := StringVal("hello world world")
	r := v.ReplaceVal("world", "go")
	if r.Str != "hello go go" {
		t.Fatalf("expected 'hello go go', got %q", r.Str)
	}
}

func TestSubstringVal(t *testing.T) {
	v := StringVal("hello world")
	s := v.SubstringVal(0, 5)
	if s.Str != "hello" {
		t.Fatalf("expected 'hello', got %q", s.Str)
	}
}

func TestSubstringValBoundsClamp(t *testing.T) {
	v := StringVal("hi")
	s := v.SubstringVal(-1, 100)
	if s.Str != "hi" {
		t.Fatalf("expected clamped to 'hi', got %q", s.Str)
	}
}

func TestStartsWithVal(t *testing.T) {
	v := StringVal("hello world")
	if !v.StartsWithVal(StringVal("hello")).Bool {
		t.Fatal("should start with 'hello'")
	}
	if v.StartsWithVal(StringVal("world")).Bool {
		t.Fatal("should not start with 'world'")
	}
}

func TestEndsWithVal(t *testing.T) {
	v := StringVal("hello world")
	if !v.EndsWithVal(StringVal("world")).Bool {
		t.Fatal("should end with 'world'")
	}
	if v.EndsWithVal(StringVal("hello")).Bool {
		t.Fatal("should not end with 'hello'")
	}
}

// --- Type conversions ---

func TestToStringVal(t *testing.T) {
	if IntVal(42).ToStringVal().Str != "42" {
		t.Fatal("int 42 should convert to string '42'")
	}
	if FloatVal(3.14).ToStringVal().Str != "3.14" {
		t.Fatal("float 3.14 should convert to string '3.14'")
	}
	if BoolVal(true).ToStringVal().Str != "true" {
		t.Fatal("bool true should convert to string 'true'")
	}
	if StringVal("hi").ToStringVal().Str != "hi" {
		t.Fatal("string should stay the same")
	}
}

func TestToIntVal(t *testing.T) {
	// From string
	v := StringVal("42").ToIntVal()
	if v.Type != program.TypeInt || v.Num != 42 {
		t.Fatalf("expected IntVal(42), got type=%d num=%f", v.Type, v.Num)
	}

	// From float (truncates)
	v = FloatVal(3.9).ToIntVal()
	if v.Type != program.TypeInt || v.Num != 3 {
		t.Fatalf("expected IntVal(3), got type=%d num=%f", v.Type, v.Num)
	}

	// From bool
	if BoolVal(true).ToIntVal().Num != 1 {
		t.Fatal("true should convert to 1")
	}
	if BoolVal(false).ToIntVal().Num != 0 {
		t.Fatal("false should convert to 0")
	}

	// Invalid string
	v = StringVal("abc").ToIntVal()
	if v.Num != 0 {
		t.Fatalf("invalid string should give 0, got %f", v.Num)
	}
}

func TestToFloatVal(t *testing.T) {
	// From string
	v := StringVal("3.14").ToFloatVal()
	if v.Type != program.TypeFloat || v.Num != 3.14 {
		t.Fatalf("expected FloatVal(3.14), got type=%d num=%f", v.Type, v.Num)
	}

	// From int (promotes)
	v = IntVal(5).ToFloatVal()
	if v.Type != program.TypeFloat || v.Num != 5 {
		t.Fatalf("expected FloatVal(5), got type=%d num=%f", v.Type, v.Num)
	}

	// Invalid string
	v = StringVal("abc").ToFloatVal()
	if v.Num != 0 {
		t.Fatalf("invalid string should give 0, got %f", v.Num)
	}
}
