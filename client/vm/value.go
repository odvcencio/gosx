package vm

import (
	"fmt"
	"math"

	"github.com/odvcencio/gosx/island/program"
)

// Value is the runtime representation of all values in the island expression VM.
type Value struct {
	Type program.ExprType
	Str  string
	Num  float64
	Bool bool
}

// StringVal creates a string Value.
func StringVal(s string) Value {
	return Value{Type: program.TypeString, Str: s}
}

// IntVal creates an integer Value.
func IntVal(n int) Value {
	return Value{Type: program.TypeInt, Num: float64(n)}
}

// FloatVal creates a float Value.
func FloatVal(f float64) Value {
	return Value{Type: program.TypeFloat, Num: f}
}

// BoolVal creates a boolean Value.
func BoolVal(b bool) Value {
	return Value{Type: program.TypeBool, Bool: b}
}

// ZeroValue returns the zero Value for the given type.
func ZeroValue(typ program.ExprType) Value {
	return Value{Type: typ}
}

// isInt reports whether both v and b are integer-typed.
func isInt(a, b Value) bool {
	return a.Type == program.TypeInt && b.Type == program.TypeInt
}

// resultType returns TypeInt if both operands are int, otherwise TypeFloat.
func resultType(a, b Value) program.ExprType {
	if isInt(a, b) {
		return program.TypeInt
	}
	return program.TypeFloat
}

// Add returns a + b. Uses integer semantics when both operands are TypeInt.
func (v Value) Add(b Value) Value {
	if isInt(v, b) {
		return Value{Type: program.TypeInt, Num: float64(int64(v.Num) + int64(b.Num))}
	}
	return Value{Type: program.TypeFloat, Num: v.Num + b.Num}
}

// Sub returns a - b.
func (v Value) Sub(b Value) Value {
	if isInt(v, b) {
		return Value{Type: program.TypeInt, Num: float64(int64(v.Num) - int64(b.Num))}
	}
	return Value{Type: program.TypeFloat, Num: v.Num - b.Num}
}

// Mul returns a * b.
func (v Value) Mul(b Value) Value {
	if isInt(v, b) {
		return Value{Type: program.TypeInt, Num: float64(int64(v.Num) * int64(b.Num))}
	}
	return Value{Type: program.TypeFloat, Num: v.Num * b.Num}
}

// Div returns a / b. Division by zero returns 0.
func (v Value) Div(b Value) Value {
	if isInt(v, b) {
		if int64(b.Num) == 0 {
			return Value{Type: program.TypeInt, Num: 0}
		}
		return Value{Type: program.TypeInt, Num: float64(int64(v.Num) / int64(b.Num))}
	}
	if b.Num == 0 {
		return Value{Type: resultType(v, b), Num: 0}
	}
	return Value{Type: program.TypeFloat, Num: v.Num / b.Num}
}

// Mod returns a % b.
func (v Value) Mod(b Value) Value {
	if isInt(v, b) {
		if int64(b.Num) == 0 {
			return Value{Type: program.TypeInt, Num: 0}
		}
		return Value{Type: program.TypeInt, Num: float64(int64(v.Num) % int64(b.Num))}
	}
	return Value{Type: program.TypeFloat, Num: math.Mod(v.Num, b.Num)}
}

// Neg returns -v.
func (v Value) Neg() Value {
	return Value{Type: v.Type, Num: -v.Num}
}

// --- Comparisons --- all return BoolVal

// Eq returns whether v == b.
func (v Value) Eq(b Value) Value {
	if v.Type == program.TypeString || b.Type == program.TypeString {
		return BoolVal(v.Str == b.Str)
	}
	if v.Type == program.TypeBool || b.Type == program.TypeBool {
		return BoolVal(v.Bool == b.Bool)
	}
	return BoolVal(v.Num == b.Num)
}

// Neq returns whether v != b.
func (v Value) Neq(b Value) Value {
	r := v.Eq(b)
	r.Bool = !r.Bool
	return r
}

// Lt returns whether v < b.
func (v Value) Lt(b Value) Value {
	return BoolVal(v.Num < b.Num)
}

// Gt returns whether v > b.
func (v Value) Gt(b Value) Value {
	return BoolVal(v.Num > b.Num)
}

// Lte returns whether v <= b.
func (v Value) Lte(b Value) Value {
	return BoolVal(v.Num <= b.Num)
}

// Gte returns whether v >= b.
func (v Value) Gte(b Value) Value {
	return BoolVal(v.Num >= b.Num)
}

// --- Boolean ops ---

// And returns v && b.
func (v Value) And(b Value) Value {
	return BoolVal(v.Bool && b.Bool)
}

// Or returns v || b.
func (v Value) Or(b Value) Value {
	return BoolVal(v.Bool || b.Bool)
}

// Not returns !v.
func (v Value) Not() Value {
	return BoolVal(!v.Bool)
}

// --- String ops ---

// Concat returns the string concatenation of v and b.
func (v Value) Concat(b Value) Value {
	return StringVal(v.Str + b.Str)
}

// String converts any Value to its string representation.
func (v Value) String() string {
	switch v.Type {
	case program.TypeString:
		return v.Str
	case program.TypeInt:
		return fmt.Sprintf("%d", int64(v.Num))
	case program.TypeFloat:
		return fmt.Sprintf("%g", v.Num)
	case program.TypeBool:
		if v.Bool {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v.Num)
	}
}
