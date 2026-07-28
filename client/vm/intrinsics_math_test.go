package vm

import (
	"math"
	"testing"
)

func TestMathSin(t *testing.T) {
	fn, ok := LookupIntrinsic("math.Sin")
	if !ok {
		t.Fatalf("math.Sin not registered")
	}
	v, err := fn([]Value{FloatVal(math.Pi / 2)})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if math.Abs(v.num-1.0) > 1e-9 {
		t.Errorf("math.Sin(π/2) = %f, want 1.0", v.num)
	}
}

func TestMathCos(t *testing.T) {
	fn, _ := LookupIntrinsic("math.Cos")
	v, _ := fn([]Value{FloatVal(0)})
	if math.Abs(v.num-1.0) > 1e-9 {
		t.Errorf("math.Cos(0) = %f, want 1.0", v.num)
	}
}

func TestMathSqrt(t *testing.T) {
	fn, _ := LookupIntrinsic("math.Sqrt")
	v, _ := fn([]Value{FloatVal(16)})
	if v.num != 4 {
		t.Errorf("math.Sqrt(16) = %f, want 4", v.num)
	}
}

func TestMathAbs(t *testing.T) {
	fn, _ := LookupIntrinsic("math.Abs")
	v, _ := fn([]Value{FloatVal(-3.5)})
	if v.num != 3.5 {
		t.Errorf("math.Abs(-3.5) = %f, want 3.5", v.num)
	}
}

func TestMathFloor(t *testing.T) {
	fn, _ := LookupIntrinsic("math.Floor")
	v, _ := fn([]Value{FloatVal(3.9)})
	if v.num != 3 {
		t.Errorf("math.Floor(3.9) = %f, want 3", v.num)
	}
}

func TestMathCeil(t *testing.T) {
	fn, _ := LookupIntrinsic("math.Ceil")
	v, _ := fn([]Value{FloatVal(3.1)})
	if v.num != 4 {
		t.Errorf("math.Ceil(3.1) = %f, want 4", v.num)
	}
}

func TestMathAtan2(t *testing.T) {
	fn, _ := LookupIntrinsic("math.Atan2")
	v, _ := fn([]Value{FloatVal(1), FloatVal(0)})
	if math.Abs(v.num-math.Pi/2) > 1e-9 {
		t.Errorf("math.Atan2(1,0) = %f, want π/2", v.num)
	}
}

func TestMathMax(t *testing.T) {
	fn, _ := LookupIntrinsic("math.Max")
	v, _ := fn([]Value{FloatVal(3), FloatVal(7)})
	if v.num != 7 {
		t.Errorf("math.Max(3,7) = %f, want 7", v.num)
	}
}

func TestMathMin(t *testing.T) {
	fn, _ := LookupIntrinsic("math.Min")
	v, _ := fn([]Value{FloatVal(3), FloatVal(7)})
	if v.num != 3 {
		t.Errorf("math.Min(3,7) = %f, want 3", v.num)
	}
}

func TestMathPi(t *testing.T) {
	fn, _ := LookupIntrinsic("math.Pi")
	v, err := fn(nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v.num != math.Pi {
		t.Errorf("math.Pi = %f, want %f", v.num, math.Pi)
	}
}

func TestMathArgCountErrors(t *testing.T) {
	fn, _ := LookupIntrinsic("math.Sin")
	if _, err := fn(nil); err == nil {
		t.Error("math.Sin with no args should error")
	}
	fn2, _ := LookupIntrinsic("math.Max")
	if _, err := fn2([]Value{FloatVal(1)}); err == nil {
		t.Error("math.Max with 1 arg should error")
	}
}
