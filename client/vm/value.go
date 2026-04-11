package vm

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/odvcencio/gosx/island/program"
)

// Value is the runtime representation of all values in the island expression VM.
type Value struct {
	Type   program.ExprType
	Str    string
	Num    float64
	Bool   bool
	Items  []Value
	Fields map[string]Value
}

// ArrayVal creates an array Value from a slice of Values.
func ArrayVal(items []Value) Value {
	return Value{Type: program.TypeAny, Items: items}
}

// ObjectVal creates an object Value.
func ObjectVal(fields map[string]Value) Value {
	return Value{Type: program.TypeAny, Fields: fields}
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
	if v.Type == program.TypeString || b.Type == program.TypeString {
		return StringVal(v.String() + b.String())
	}
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
	// Array comparison
	if v.Items != nil || b.Items != nil {
		if len(v.Items) != len(b.Items) {
			return BoolVal(false)
		}
		for i := range v.Items {
			if !v.Items[i].Eq(b.Items[i]).Bool {
				return BoolVal(false)
			}
		}
		return BoolVal(true)
	}
	if v.Fields != nil || b.Fields != nil {
		if len(v.Fields) != len(b.Fields) {
			return BoolVal(false)
		}
		for key, val := range v.Fields {
			other, ok := b.Fields[key]
			if !ok || !val.Eq(other).Bool {
				return BoolVal(false)
			}
		}
		return BoolVal(true)
	}
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

// Len returns the length of a string or array Value as an int.
func (v Value) Len() int {
	if v.Items != nil {
		return len(v.Items)
	}
	if v.Fields != nil {
		return len(v.Fields)
	}
	return len(v.Str)
}

// String converts any Value to its string representation.
//
// The scalar paths use strconv directly instead of fmt.Sprintf so that
// an int-typed signal (by far the most common case, e.g. counter values)
// renders without the fmt format-state scratch allocation. Runs once per
// expression evaluation touching any int/float signal — in a typical
// counter island with a "{count}" display that's N calls per reconcile.
func (v Value) String() string {
	if v.Items != nil {
		parts := make([]string, len(v.Items))
		for i, item := range v.Items {
			parts[i] = item.String()
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	if v.Fields != nil {
		keys := make([]string, 0, len(v.Fields))
		for key := range v.Fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.Grow(2 + len(keys)*16)
		b.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(key)
			b.WriteByte(':')
			b.WriteString(v.Fields[key].String())
		}
		b.WriteByte('}')
		return b.String()
	}
	switch v.Type {
	case program.TypeString:
		return v.Str
	case program.TypeInt:
		return strconv.FormatInt(int64(v.Num), 10)
	case program.TypeFloat:
		return strconv.FormatFloat(v.Num, 'g', -1, 64)
	case program.TypeBool:
		if v.Bool {
			return "true"
		}
		return "false"
	default:
		return strconv.FormatFloat(v.Num, 'g', -1, 64)
	}
}

// IndexVal returns an indexed element from an array, object, or string.
func (v Value) IndexVal(index Value) Value {
	if v.Items != nil {
		idx := int(index.Num)
		if idx < 0 || idx >= len(v.Items) {
			return ZeroValue(program.TypeAny)
		}
		return v.Items[idx]
	}
	if v.Fields != nil {
		if field, ok := v.Fields[index.String()]; ok {
			return field
		}
		return ZeroValue(program.TypeAny)
	}
	if v.Type == program.TypeString {
		idx := int(index.Num)
		if idx < 0 || idx >= len(v.Str) {
			return ZeroValue(program.TypeString)
		}
		return StringVal(v.Str[idx : idx+1])
	}
	return ZeroValue(program.TypeAny)
}

// --- Array methods ---

// AppendVal returns a new Value with elem appended to Items.
func (v Value) AppendVal(elem Value) Value {
	newItems := make([]Value, len(v.Items), len(v.Items)+1)
	copy(newItems, v.Items)
	newItems = append(newItems, elem)
	return ArrayVal(newItems)
}

// FilterFunc returns a new array Value containing only items for which pred returns true.
func (v Value) FilterFunc(pred func(Value) bool) Value {
	var result []Value
	for _, item := range v.Items {
		if pred(item) {
			result = append(result, item)
		}
	}
	return ArrayVal(result)
}

// MapFunc returns a new array Value with fn applied to each item.
func (v Value) MapFunc(fn func(Value, int) Value) Value {
	result := make([]Value, len(v.Items))
	for i, item := range v.Items {
		result[i] = fn(item, i)
	}
	return ArrayVal(result)
}

// FindFunc returns the first item for which pred returns true, or ZeroValue.
func (v Value) FindFunc(pred func(Value) bool) Value {
	for _, item := range v.Items {
		if pred(item) {
			return item
		}
	}
	return ZeroValue(program.TypeAny)
}

// SliceVal returns Items[start:end] with bounds clamping.
func (v Value) SliceVal(start, end int) Value {
	n := len(v.Items)
	if start < 0 {
		start = 0
	}
	if end > n {
		end = n
	}
	if start > end {
		start = end
	}
	newItems := make([]Value, end-start)
	copy(newItems, v.Items[start:end])
	return ArrayVal(newItems)
}

// ContainsVal checks if elem is in Items (array) or if elem.Str is a substring of v.Str (string).
func (v Value) ContainsVal(elem Value) Value {
	if v.Items != nil {
		for _, item := range v.Items {
			if item.Eq(elem).Bool {
				return BoolVal(true)
			}
		}
		return BoolVal(false)
	}
	return BoolVal(strings.Contains(v.Str, elem.Str))
}

// JoinVal joins Items as strings with the given separator.
func (v Value) JoinVal(sep string) Value {
	parts := make([]string, len(v.Items))
	for i, item := range v.Items {
		parts[i] = item.String()
	}
	return StringVal(strings.Join(parts, sep))
}

// --- String methods ---

// ToUpper returns a new Value with v.Str uppercased.
func (v Value) ToUpper() Value {
	return StringVal(strings.ToUpper(v.Str))
}

// ToLower returns a new Value with v.Str lowercased.
func (v Value) ToLower() Value {
	return StringVal(strings.ToLower(v.Str))
}

// TrimVal returns a new Value with whitespace trimmed from v.Str.
func (v Value) TrimVal() Value {
	return StringVal(strings.TrimSpace(v.Str))
}

// SplitVal splits v.Str by sep and returns an ArrayVal of StringVals.
func (v Value) SplitVal(sep string) Value {
	parts := strings.Split(v.Str, sep)
	items := make([]Value, len(parts))
	for i, p := range parts {
		items[i] = StringVal(p)
	}
	return ArrayVal(items)
}

// ReplaceVal returns a new Value with all occurrences of old replaced by new in v.Str.
func (v Value) ReplaceVal(old, new string) Value {
	return StringVal(strings.ReplaceAll(v.Str, old, new))
}

// SubstringVal returns v.Str[start:end] with bounds clamping.
func (v Value) SubstringVal(start, end int) Value {
	n := len(v.Str)
	if start < 0 {
		start = 0
	}
	if end > n {
		end = n
	}
	if start > end {
		start = end
	}
	return StringVal(v.Str[start:end])
}

// StartsWithVal returns BoolVal indicating whether v.Str starts with prefix.Str.
func (v Value) StartsWithVal(prefix Value) Value {
	return BoolVal(strings.HasPrefix(v.Str, prefix.Str))
}

// EndsWithVal returns BoolVal indicating whether v.Str ends with suffix.Str.
func (v Value) EndsWithVal(suffix Value) Value {
	return BoolVal(strings.HasSuffix(v.Str, suffix.Str))
}

// --- Type conversions ---

// ToStringVal converts any Value to a StringVal.
func (v Value) ToStringVal() Value {
	return StringVal(v.String())
}

// ToIntVal converts a Value to an IntVal. Parses strings, truncates floats.
func (v Value) ToIntVal() Value {
	switch v.Type {
	case program.TypeInt:
		return v
	case program.TypeFloat:
		return IntVal(int(v.Num))
	case program.TypeString:
		n, err := strconv.ParseInt(v.Str, 10, 64)
		if err != nil {
			// Try parsing as float then truncating
			f, err2 := strconv.ParseFloat(v.Str, 64)
			if err2 != nil {
				return IntVal(0)
			}
			return IntVal(int(f))
		}
		return IntVal(int(n))
	case program.TypeBool:
		if v.Bool {
			return IntVal(1)
		}
		return IntVal(0)
	default:
		return IntVal(0)
	}
}

// ToFloatVal converts a Value to a FloatVal. Parses strings, promotes ints.
func (v Value) ToFloatVal() Value {
	switch v.Type {
	case program.TypeFloat:
		return v
	case program.TypeInt:
		return FloatVal(v.Num)
	case program.TypeString:
		f, err := strconv.ParseFloat(v.Str, 64)
		if err != nil {
			return FloatVal(0)
		}
		return FloatVal(f)
	case program.TypeBool:
		if v.Bool {
			return FloatVal(1)
		}
		return FloatVal(0)
	default:
		return FloatVal(0)
	}
}
