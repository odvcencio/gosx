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
