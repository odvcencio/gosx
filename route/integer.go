package route

import (
	"go/token"
	"reflect"
)

// fileInteger retains the original 64 bits and signedness. Route expressions
// normalize integer arithmetic to int64, or uint64 when either integer operand
// is unsigned; floating-point and permissive string/bool arithmetic stay on
// numericValue's existing path.
type fileInteger struct {
	bits     uint64
	unsigned bool
}

func integerOperand(value any) (fileInteger, bool) {
	switch v := value.(type) {
	case int:
		return fileInteger{bits: uint64(v)}, true
	case int64:
		return fileInteger{bits: uint64(v)}, true
	case int32:
		return fileInteger{bits: uint64(v)}, true
	case int16:
		return fileInteger{bits: uint64(v)}, true
	case int8:
		return fileInteger{bits: uint64(v)}, true
	case uint:
		return fileInteger{bits: uint64(v), unsigned: true}, true
	case uint64:
		return fileInteger{bits: v, unsigned: true}, true
	case uint32:
		return fileInteger{bits: uint64(v), unsigned: true}, true
	case uint16:
		return fileInteger{bits: uint64(v), unsigned: true}, true
	case uint8:
		return fileInteger{bits: uint64(v), unsigned: true}, true
	case uintptr:
		return fileInteger{bits: uint64(v), unsigned: true}, true
	}
	rv, ok := indirectValueOf(value)
	if !ok {
		return fileInteger{}, false
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fileInteger{bits: uint64(rv.Int())}, true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return fileInteger{bits: rv.Uint(), unsigned: true}, true
	}
	return fileInteger{}, false
}

func fileIntegerBinary(op token.Token, left, right any) (any, bool) {
	a, ok := integerOperand(left)
	if !ok {
		return nil, false
	}
	b, ok := integerOperand(right)
	if !ok {
		return nil, false
	}
	if a.unsigned || b.unsigned {
		return integerArithmetic(op, a.bits, b.bits), true
	}
	return integerArithmetic(op, int64(a.bits), int64(b.bits)), true
}

func integerArithmetic[T int64 | uint64](op token.Token, left, right T) T {
	switch op {
	case token.ADD:
		return left + right
	case token.SUB:
		return left - right
	case token.MUL:
		return left * right
	case token.QUO:
		if right != 0 {
			return left / right
		}
	case token.REM:
		if right != 0 {
			return left % right
		}
	}
	return 0
}

func fileIntegerUnary(op token.Token, value any) (any, bool) {
	n, ok := integerOperand(value)
	if !ok {
		return nil, false
	}
	if op == token.SUB {
		n.bits = -n.bits
	}
	if n.unsigned {
		return n.bits, true
	}
	return int64(n.bits), true
}
