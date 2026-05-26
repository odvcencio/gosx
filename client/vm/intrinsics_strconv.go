// strconv.* intrinsics — Slice X.B. Itoa, Atoi, FormatFloat, ParseFloat.
//
// Atoi and ParseFloat surface conversion errors as intrinsic errors;
// the VM's evalControlExpr path turns them into structured diagnostics
// and a zero return. This matches Go's behavior (callers must check err)
// without forcing the lowerer to emit explicit error checks.

package vm

import (
	"errors"
	"strconv"
)

func init() {
	RegisterIntrinsic("strconv.Itoa", func(args []Value) (Value, error) {
		if len(args) != 1 {
			return Value{}, errors.New("strconv.Itoa expects 1 argument")
		}
		return StringVal(strconv.Itoa(int(args[0].Num))), nil
	})
	RegisterIntrinsic("strconv.Atoi", func(args []Value) (Value, error) {
		if len(args) != 1 {
			return Value{}, errors.New("strconv.Atoi expects 1 argument")
		}
		n, err := strconv.Atoi(args[0].Str)
		if err != nil {
			return Value{}, err
		}
		return IntVal(n), nil
	})
	RegisterIntrinsic("strconv.FormatFloat", func(args []Value) (Value, error) {
		// Go signature: FormatFloat(f float64, fmt byte, prec, bitSize int)
		if len(args) != 4 {
			return Value{}, errors.New("strconv.FormatFloat expects 4 arguments")
		}
		// fmt byte is encoded as a single-char string.
		var format byte = 'g'
		if args[1].Str != "" {
			format = args[1].Str[0]
		}
		return StringVal(strconv.FormatFloat(args[0].Num, format, int(args[2].Num), int(args[3].Num))), nil
	})
	RegisterIntrinsic("strconv.ParseFloat", func(args []Value) (Value, error) {
		// Go signature: ParseFloat(s string, bitSize int)
		if len(args) != 2 {
			return Value{}, errors.New("strconv.ParseFloat expects 2 arguments")
		}
		f, err := strconv.ParseFloat(args[0].Str, int(args[1].Num))
		if err != nil {
			return Value{}, err
		}
		return FloatVal(f), nil
	})
}
