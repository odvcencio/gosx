package aot

import (
	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

const helperFormat = 4

// numericShape rejects hidden payloads and reserved flags instead of coercing
// an actual input tag into the kind that the source contract expected.
func (b *instructions) numericShape(record uint32, flags int32, zeroNumber bool) {
	b.get(record)
	b.memory(0x28, 2, 4)
	b.i32(^flags)
	b.op(0x71)
	b.guard(statusBadInput)
	b.get(record)
	b.memory(0x29, 3, 16)
	b.op(0x50)
	b.op(0x45)
	b.guard(statusBadInput)
	if zeroNumber {
		b.get(record)
		b.memory(0x29, 3, 8)
		b.op(0x50)
		b.op(0x45)
		b.guard(statusBadInput)
	}
}

func (b *instructions) typeBranch(typ program.ExprType) {
	b.get(1)
	b.i32(int32(typ))
	b.op(0x46)
	b.op(0x04)
	b.op(0x40)
}

func (e *expressionEmitter) constantText(b *instructions, value string) {
	c := e.strings[value]
	b.i32(c.pointer)
	b.set(3)
	b.i32(c.length)
	b.set(4)
}

func (e *expressionEmitter) formatFunction() wasmgen.Function {
	var b instructions
	b.errorGuard()
	b.get(0)
	b.op(0xad)
	b.i64(valueBytes)
	b.op(0x7c)
	b.i64(196608)
	b.op(0x56)
	b.guard(statusBadInput)
	b.get(0)
	b.memory(0x28, 2, 0)
	b.set(1)
	b.typeBranch(program.TypeString)
	b.textFields(0, 3, 4)
	b.op(0x05)
	b.typeBranch(program.TypeInt)
	b.numericShape(0, 0, false)
	b.get(0)
	b.memory(0x29, 3, 8)
	b.set(8)
	b.integerGuard(8)
	b.i32(11)
	e.callHelper(&b, helperAllocate)
	b.set(3)
	b.errorGuard()
	b.get(8)
	b.i64(0)
	b.op(0x53)
	b.set(7)
	// Keep the magnitude negative: -2147483648 never needs an out-of-domain
	// positive counterpart. Only individual decimal digits are negated.
	b.get(8)
	b.i64(0)
	b.op(0x55)
	b.op(0x04)
	b.op(0x40)
	b.i64(0)
	b.get(8)
	b.op(0x7d)
	b.set(8)
	b.op(0x0b)
	b.op(0x03)
	b.op(0x40)
	b.get(8)
	b.i64(10)
	b.op(0x81) // i64.rem_s
	b.set(9)
	b.get(3)
	b.i32(10)
	b.op(0x6a)
	b.get(4)
	b.op(0x6b)
	b.i64(48)
	b.get(9)
	b.op(0x7d)
	b.op(0xa7)
	b.memory(0x3a, 0, 0)
	b.get(4)
	b.i32(1)
	b.op(0x6a)
	b.set(4)
	b.get(8)
	b.i64(10)
	b.op(0x7f) // i64.div_s, quotient stays in the admitted domain
	b.index(0x22, 8)
	b.op(0x50)
	b.op(0x45)
	b.index(0x0d, 0)
	b.op(0x0b)
	b.get(7)
	b.op(0x04)
	b.op(0x40)
	b.get(3)
	b.i32(10)
	b.op(0x6a)
	b.get(4)
	b.op(0x6b)
	b.i32('-')
	b.memory(0x3a, 0, 0)
	b.get(4)
	b.i32(1)
	b.op(0x6a)
	b.set(4)
	b.op(0x0b)
	b.get(3)
	b.i32(11)
	b.op(0x6a)
	b.get(4)
	b.op(0x6b)
	b.set(3)
	b.op(0x05)
	b.typeBranch(program.TypeBool)
	b.numericShape(0, 2, true)
	b.get(0)
	b.memory(0x28, 2, 4)
	b.op(0x04)
	b.op(0x40)
	e.constantText(&b, "true")
	b.op(0x05)
	e.constantText(&b, "false")
	b.op(0x0b)
	b.op(0x05)
	b.typeBranch(program.TypeAny)
	b.numericShape(0, 0, true)
	e.constantText(&b, "0")
	b.op(0x05)
	b.failure(statusBadInput)
	for i := 0; i < 4; i++ {
		b.op(0x0b)
	}
	b.i32(valueBytes)
	e.callHelper(&b, helperAllocate)
	b.set(2)
	b.errorGuard()
	b.writeString(2, localInstructions(3), localInstructions(4))
	b.get(2)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(1), I32Locals: 7, I64Locals: 2, Body: b}
}

func (e *expressionEmitter) formatExpression(id program.ExprID, expr program.Expr) (wasmgen.Function, error) {
	var b instructions
	b.errorGuard()
	if expr.Op == program.OpToString {
		e.value(&b, expr.Operands[0], 2)
		b.get(2)
		e.callHelper(&b, helperFormat)
		b.set(2)
		b.errorGuard()
	} else {
		c := e.strings[expr.Value]
		b.i32(valueBytes)
		e.callHelper(&b, helperAllocate)
		b.set(2)
		b.errorGuard()
		b.writeString(2, i32Instructions(c.pointer), i32Instructions(c.length))
		for _, operand := range expr.Operands {
			e.value(&b, operand, 3)
			b.get(3)
			e.callHelper(&b, helperFormat)
			b.set(3)
			b.errorGuard()
			b.get(2)
			b.get(3)
			e.callHelper(&b, helperJoin)
			b.set(2)
			b.errorGuard()
		}
	}
	b.copyRecord(id, 2)
	return wasmgen.Function{I32Locals: 3, Body: b}, nil
}
