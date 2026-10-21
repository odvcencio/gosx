package vm

import (
	"math"
	"testing"
)

func TestEqualJSONStrictSnapshots(t *testing.T) {
	for _, tc := range []struct {
		left, right string
		equal       bool
	}{
		{`{"a":[1,true,null],"b":"x"}`, `{"b":"x","a":[1,true,null]}`, true},
		{`[1,2]`, `[2,1]`, false},
		{`1`, `"1"`, false},
		{`false`, `0`, false},
		{`null`, `[]`, false},
		{`[]`, `{}`, false},
		{`{"a":null}`, `{}`, false},
		{`{"a":1}`, `{"b":1}`, false},
	} {
		a, err := ValueFromJSON(tc.left)
		if err != nil {
			t.Fatal(err)
		}
		b, err := ValueFromJSON(tc.right)
		if err != nil {
			t.Fatal(err)
		}
		if got := EqualJSON(a, b); got != tc.equal {
			t.Errorf("%s / %s: %v, want %v", tc.left, tc.right, got, tc.equal)
		}
	}
	for _, number := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if EqualJSON(FloatVal(number), FloatVal(number)) {
			t.Fatal("non-JSON number compared equal")
		}
	}
	cycle := ObjectVal(map[string]Value{})
	cycle.Map()["self"] = cycle
	if EqualJSON(cycle, cycle) {
		t.Fatal("cycle compared equal")
	}
}

func TestEqualJSONPreservesWideIntegerLowBitsAndScalarMetadata(t *testing.T) {
	numbers := []int64{0, 1, -1, 1<<53 - 1, 1 << 53, 1<<53 + 1, 1<<53 + 2, -(1 << 53) - 1, -(1 << 53) - 2, -(1 << 53) - 3, math.MaxInt64, math.MaxInt64 - 1, math.MinInt64, math.MinInt64 + 1}
	for _, left := range numbers {
		for _, right := range numbers {
			if got := EqualJSON(Int64Val(left), Int64Val(right)); got != (left == right) {
				t.Fatalf("wide/integer equality %d / %d = %v", left, right, got)
			}
		}
	}
	if EqualJSON(ArrayVal(nil), ArrayVal([]Value{})) || EqualJSON(ObjectVal(nil), ObjectVal(map[string]Value{})) {
		t.Fatal("nil and empty payloads compared equal")
	}
	if EqualJSON(FloatVal(0), FloatVal(math.Copysign(0, -1))) || EqualJSON(BoolVal(true), BoolVal(false)) {
		t.Fatal("signed zero or boolean payload was lost")
	}
	if EqualJSON(IntVal(1), FloatVal(1)) || EqualJSON(IntVal(1), IntVal(1).WithControl(ControlReturn)) {
		t.Fatal("static type or control metadata was lost")
	}
}
