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
