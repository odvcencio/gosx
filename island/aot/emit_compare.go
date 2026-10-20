package aot

import (
	"fmt"

	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

func (e *expressionEmitter) comparison(id program.ExprID, expr program.Expr) (wasmgen.Function, error) {
	if len(expr.Operands) != 0 && e.unit.Contract.Expressions[expr.Operands[0]].Kind == String {
		return e.stringComparison(id, expr), nil
	}
	var b, flags instructions
	b.errorGuard()
	fn := wasmgen.Function{I32Locals: 1}
	if expr.Op == program.OpLitBool {
		value := int32(0)
		if expr.Value == "true" {
			value = 2
		}
		flags.i32(value)
	} else {
		kind := e.unit.Contract.Expressions[expr.Operands[0]].Kind
		if integerKind(kind) {
			fn.I64Locals = 2
			e.operand(&b, expr.Operands[0], program.TypeInt)
			b.index(0x21, 2)
			e.operand(&b, expr.Operands[1], program.TypeInt)
			b.index(0x21, 3)
			flags.index(0x20, 2)
			flags.index(0x20, 3)
			switch expr.Op {
			case program.OpEq:
				flags.op(0x51)
			case program.OpNeq:
				flags.op(0x52)
			case program.OpLt:
				flags.op(0x53)
			case program.OpGt:
				flags.op(0x55)
			case program.OpLte:
				flags.op(0x57)
			case program.OpGte:
				flags.op(0x59)
			default:
				return wasmgen.Function{}, fmt.Errorf("integer comparison not implemented")
			}
			flags.i32(1)
			flags.op(0x74)
		} else if kind == Bool {
			fn.I32Locals = 3
			e.operand(&b, expr.Operands[0], program.TypeBool)
			b.index(0x21, 2)
			flags.index(0x20, 2)
			if expr.Op == program.OpNot {
				flags.op(0x45)
				flags.i32(1)
				flags.op(0x74)
			} else {
				e.operand(&b, expr.Operands[1], program.TypeBool)
				b.index(0x21, 3)
				flags.index(0x20, 3)
				switch expr.Op {
				case program.OpAnd:
					flags.op(0x71)
				case program.OpOr:
					flags.op(0x72)
				case program.OpEq:
					flags.op(0x46)
					flags.i32(1)
					flags.op(0x74)
				case program.OpNeq:
					flags.op(0x47)
					flags.i32(1)
					flags.op(0x74)
				default:
					return wasmgen.Function{}, fmt.Errorf("boolean comparison not implemented")
				}
			}
		} else {
			return wasmgen.Function{}, fmt.Errorf("comparison kind not implemented")
		}
	}
	b.record(id, program.TypeBool, i64Instructions(0), flags)
	fn.Body = b
	return fn, nil
}
