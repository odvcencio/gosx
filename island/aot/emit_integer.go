package aot

import (
	"fmt"
	"math"
	"strconv"

	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

func (e *expressionEmitter) integer(id program.ExprID, expr program.Expr) (wasmgen.Function, error) {
	if !integerKind(e.unit.Contract.Expressions[id].Kind) {
		return wasmgen.Function{}, fmt.Errorf("arithmetic kind not implemented")
	}
	var b, number instructions
	b.errorGuard()
	if expr.Op == program.OpLitInt {
		value, err := strconv.ParseInt(expr.Value, 10, 32)
		if err != nil {
			return wasmgen.Function{}, err
		}
		number.i64(value)
		b.record(id, program.TypeInt, number, i32Instructions(0))
		return wasmgen.Function{I32Locals: 1, Body: b}, nil
	}
	locals := uint32(2)
	e.operand(&b, expr.Operands[0], program.TypeInt)
	b.index(0x21, 2)
	if expr.Op == program.OpNeg {
		b.i64(0)
		b.index(0x20, 2)
		b.op(0x7d)
	} else {
		locals = 3
		e.operand(&b, expr.Operands[1], program.TypeInt)
		b.index(0x21, 3)
		b.index(0x20, 2)
		b.index(0x20, 3)
		switch expr.Op {
		case program.OpAdd:
			b.op(0x7c)
		case program.OpSub:
			b.op(0x7d)
		case program.OpMul:
			b.op(0x7e)
		}
	}
	result := uint32(1) + locals
	b.index(0x21, result)
	b.integerGuard(result)
	number.index(0x20, result)
	b.record(id, program.TypeInt, number, i32Instructions(0))
	return wasmgen.Function{I32Locals: 1, I64Locals: locals, Body: b}, nil
}

func (b *instructions) integerGuard(local uint32) {
	b.index(0x20, local)
	b.i64(math.MinInt32)
	b.op(0x53)
	b.index(0x20, local)
	b.i64(math.MaxInt32)
	b.op(0x55)
	b.op(0x72)
	b.op(0x04)
	b.op(0x40)
	b.failure(statusIntegerDomain)
	b.op(0x0b)
}
