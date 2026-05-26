package vm

import (
	"testing"
)

func TestRandSeedThenFloat64IsDeterministic(t *testing.T) {
	seed, _ := LookupIntrinsic("rand.Seed")
	flt, _ := LookupIntrinsic("rand.Float64")

	_, _ = seed([]Value{IntVal(42)})
	a, _ := flt(nil)
	b, _ := flt(nil)
	_, _ = seed([]Value{IntVal(42)})
	c, _ := flt(nil)
	d, _ := flt(nil)
	if a.Num != c.Num || b.Num != d.Num {
		t.Errorf("seeded sequence not reproducible: (%f,%f) vs (%f,%f)", a.Num, b.Num, c.Num, d.Num)
	}
}

func TestRandIntnRange(t *testing.T) {
	seed, _ := LookupIntrinsic("rand.Seed")
	intn, _ := LookupIntrinsic("rand.Intn")
	_, _ = seed([]Value{IntVal(1)})
	for i := 0; i < 50; i++ {
		v, err := intn([]Value{IntVal(10)})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if v.Num < 0 || v.Num >= 10 {
			t.Errorf("rand.Intn(10) out of range: %f", v.Num)
		}
	}
}

func TestRandIntnZeroErrors(t *testing.T) {
	intn, _ := LookupIntrinsic("rand.Intn")
	if _, err := intn([]Value{IntVal(0)}); err == nil {
		t.Error("rand.Intn(0) should error")
	}
}
