// math.* intrinsics — Slice X.B (AST-compiler initiative). The covered
// surface is exactly what the plan's "Supported subset" enumerates:
// Sin, Cos, Sqrt, Abs, Atan2, Pi, Max, Min, Floor, Ceil.
//
// Pi is exposed as a zero-arg function rather than a constant so the
// lowerer can treat it uniformly with the rest (math.Pi selector → call
// "math.Pi" with no args). This avoids a separate constant-lookup path
// in the VM for one symbol.

package vm

import (
	"errors"
	"math"
)

func init() {
	RegisterIntrinsic("math.Sin", mathUnary(math.Sin))
	RegisterIntrinsic("math.Cos", mathUnary(math.Cos))
	RegisterIntrinsic("math.Sqrt", mathUnary(math.Sqrt))
	RegisterIntrinsic("math.Abs", mathUnary(math.Abs))
	RegisterIntrinsic("math.Floor", mathUnary(math.Floor))
	RegisterIntrinsic("math.Ceil", mathUnary(math.Ceil))
	RegisterIntrinsic("math.Atan2", mathBinary(math.Atan2))
	RegisterIntrinsic("math.Max", mathBinary(math.Max))
	RegisterIntrinsic("math.Min", mathBinary(math.Min))
	RegisterIntrinsic("math.Pi", func(args []Value) (Value, error) {
		if len(args) != 0 {
			return Value{}, errors.New("math.Pi takes no arguments")
		}
		return FloatVal(math.Pi), nil
	})
}

// mathUnary lifts a single-argument float function into the Intrinsic
// shape. Argument count is checked once; the body stays one line.
func mathUnary(fn func(float64) float64) Intrinsic {
	return func(args []Value) (Value, error) {
		if len(args) != 1 {
			return Value{}, errors.New("math: function expects 1 argument")
		}
		return FloatVal(fn(args[0].Num)), nil
	}
}

// mathBinary lifts a two-argument float function. Same shape contract
// as mathUnary; the duplication is intentional so the call site reads
// without an arg-count parameter.
func mathBinary(fn func(float64, float64) float64) Intrinsic {
	return func(args []Value) (Value, error) {
		if len(args) != 2 {
			return Value{}, errors.New("math: function expects 2 arguments")
		}
		return FloatVal(fn(args[0].Num, args[1].Num)), nil
	}
}
