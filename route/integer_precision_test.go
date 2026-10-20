package route

import (
	"fmt"
	"go/token"
	"math"
	"testing"
)

func TestFileIntegerPrecisionArithmetic(t *testing.T) {
	pairs := [][2]int64{{1 << 53, 1}, {1<<53 + 1, 3}, {-(1<<53 + 1), 3}, {math.MaxInt64, 1}, {math.MinInt64, -1}, {math.MaxInt64, math.MinInt64}, {math.MinInt64, 0}}
	for _, pair := range pairs {
		a, b := pair[0], pair[1]
		div, rem := int64(0), int64(0)
		if b != 0 {
			div, rem = a/b, a%b
		}
		for _, item := range []struct {
			op   token.Token
			want int64
		}{{token.ADD, a + b}, {token.SUB, a - b}, {token.MUL, a * b}, {token.QUO, div}, {token.REM, rem}} {
			if got := fmt.Sprint(applyFileBinaryOp(item.op, a, b)); got != fmt.Sprint(item.want) {
				t.Errorf("%d %s %d = %s, want %d", a, item.op, b, got, item.want)
			}
		}
		if got := fmt.Sprint(applyFileUnaryOp(token.SUB, a)); got != fmt.Sprint(-a) {
			t.Errorf("negate %d = %s, want %d", a, got, -a)
		}
		if got := fmt.Sprint(applyFileUnaryOp(token.ADD, a)); got != fmt.Sprint(a) {
			t.Errorf("positive %d = %s", a, got)
		}
	}
}

func TestFileIntegerPrecisionUnsigned(t *testing.T) {
	for _, pair := range [][2]uint64{{math.MaxUint64, 1}, {1 << 63, 3}, {0, 1}, {math.MaxUint64, 0}} {
		a, b := pair[0], pair[1]
		div, rem := uint64(0), uint64(0)
		if b != 0 {
			div, rem = a/b, a%b
		}
		for _, item := range []struct {
			op   token.Token
			want uint64
		}{{token.ADD, a + b}, {token.SUB, a - b}, {token.MUL, a * b}, {token.QUO, div}, {token.REM, rem}} {
			if got := fmt.Sprint(applyFileBinaryOp(item.op, a, b)); got != fmt.Sprint(item.want) {
				t.Errorf("%d %s %d = %s, want %d", a, item.op, b, got, item.want)
			}
		}
		if got := fmt.Sprint(applyFileUnaryOp(token.SUB, a)); got != fmt.Sprint(-a) {
			t.Errorf("unsigned negate %d = %s", a, got)
		}
	}
	// A route integer literal is signed; it must not round an unsigned prop.
	if got := fmt.Sprint(applyFileBinaryOp(token.ADD, uint64(math.MaxUint64), int64(0))); got != "18446744073709551615" {
		t.Fatalf("unsigned prop + literal zero = %s", got)
	}
}

func TestFileIntegerPrecisionOrdering(t *testing.T) {
	pairs := []struct {
		a, b any
		want int
	}{
		{int64(1<<53 + 1), int64(1 << 53), 1},
		{uint64(math.MaxUint64), uint64(math.MaxUint64 - 1), 1},
		{int64(-1), uint64(math.MaxUint64), -1},
		{int64(math.MaxInt64), uint64(1 << 63), -1},
		{int64(42), uint64(42), 0},
		{int64(math.MinInt64), int64(math.MinInt64 + 1), -1},
	}
	for _, pair := range pairs {
		if got := compareValues(pair.a, pair.b); got != pair.want {
			t.Errorf("compare %v, %v = %d, want %d", pair.a, pair.b, got, pair.want)
		}
	}
}

func TestFileIntegerPrecisionNamedAndPointerValues(t *testing.T) {
	type identifier int64
	type sequence uint64
	id := identifier(1<<53 + 1)
	next := sequence(math.MaxUint64)
	if got := fmt.Sprint(applyFileBinaryOp(token.ADD, &id, int64(1))); got != "9007199254740994" {
		t.Fatalf("named pointer addition = %s", got)
	}
	if got := fmt.Sprint(applyFileBinaryOp(token.SUB, &next, uint64(1))); got != "18446744073709551614" {
		t.Fatalf("unsigned pointer subtraction = %s", got)
	}
	if compareValues(&id, int64(1<<53)) != 1 {
		t.Fatal("named pointer ordering rounded")
	}
}

func TestFileIntegerPrecisionLegacyFloatAndStringPaths(t *testing.T) {
	for _, item := range []struct {
		op   token.Token
		a, b any
		want string
	}{
		{token.ADD, int64(3), 0.5, "3.5"}, {token.QUO, int64(7), 2.0, "3.5"},
		{token.ADD, "id-", int64(9007199254740993), "id-9007199254740993"},
		{token.MUL, "2", int64(3), "6"}, {token.ADD, true, int64(1), "2"},
	} {
		if got := fmt.Sprint(applyFileBinaryOp(item.op, item.a, item.b)); got != item.want {
			t.Errorf("%v %s %v = %s, want %s", item.a, item.op, item.b, got, item.want)
		}
	}
	if compareValues("10", "2") != -1 {
		t.Fatal("string ordering changed")
	}
}

func TestFileIntegerPrecisionManyValues(t *testing.T) {
	state := uint64(3)
	for i := 0; i < 1024; i++ {
		state = state*6364136223846793005 + 1442695040888963407
		a := int64(state)
		state = state*6364136223846793005 + 1442695040888963407
		b := int64(state)
		got := applyFileBinaryOp(token.ADD, a, b)
		if got != a+b {
			t.Fatalf("%d + %d = %v, want %d", a, b, got, a+b)
		}
		want := 0
		if a < b {
			want = -1
		} else if a > b {
			want = 1
		}
		if compareValues(a, b) != want {
			t.Fatalf("compare %d, %d lost precision", a, b)
		}
	}
}

func TestFileIntegerPrecisionUintptr(t *testing.T) {
	if got := applyFileBinaryOp(token.ADD, uintptr(3), int64(4)); got != uint64(7) {
		t.Fatalf("uintptr integer addition = %v", got)
	}
	if got := applyFileBinaryOp(token.ADD, uintptr(3), 0.5); got != 3.5 {
		t.Fatalf("uintptr float addition = %v", got)
	}
	if !equalValues(uintptr(3), float64(3)) {
		t.Fatal("uintptr numeric equality failed")
	}
}
