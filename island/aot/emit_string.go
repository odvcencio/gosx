package aot

import (
	"fmt"
	"sort"

	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

const (
	helperAllocate = iota
	helperCopy
	helperCompare
	helperJoin
)

type stringConstant struct{ pointer, length int32 }

func i32Signature(count int) wasmgen.Signature {
	params := make([]wasmgen.ValueType, count)
	for i := range params {
		params[i] = wasmgen.I32
	}
	return wasmgen.Signature{Params: params, Result: wasmgen.I32}
}

func (e *expressionEmitter) setupStrings() error {
	needed := e.transactional
	formatNeeded := false
	values := map[string]bool{}
	for i, expr := range e.unit.Program.Exprs {
		needed = needed || e.unit.Contract.Expressions[i].Kind == String
		formatNeeded = formatNeeded || expr.Op == program.OpFormat || expr.Op == program.OpToString
		if expr.Op == program.OpLitString || expr.Op == program.OpFormat {
			values[expr.Value] = true
		}
	}
	if !needed {
		return nil
	}
	if formatNeeded {
		for _, value := range []string{"true", "false", "0"} {
			values[value] = true
		}
	}
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	e.strings = make(map[string]stringConstant, len(keys))
	for _, value := range keys {
		c := stringConstant{length: int32(len(value))}
		if value != "" {
			c.pointer = int32(wasmgen.ConstantOffset + len(e.module.Data))
			e.module.Data = append(e.module.Data, value...)
		}
		e.strings[value] = c
	}
	if len(e.module.Data) > wasmgen.MaxDataBytes {
		return fmt.Errorf("string constants exceed the constant segment")
	}
	for i := range e.helpers {
		e.helpers[i] = uint32(len(e.module.Imports) + len(e.module.Functions) + i)
	}
	e.module.Functions = append(e.module.Functions, e.allocateFunction(), e.copyFunction(),
		e.compareFunction(), e.joinFunction())
	if formatNeeded {
		e.module.Functions = append(e.module.Functions, e.formatFunction())
	}
	return nil
}

func (e *expressionEmitter) callHelper(b *instructions, helper int) {
	b.index(0x10, e.helpers[helper])
}

// Allocation uses an arena-relative cursor and widened end arithmetic. Empty
// byte strings use 0/0; a failed allocation never advances the cursor.
func (e *expressionEmitter) allocateFunction() wasmgen.Function {
	var b instructions
	b.errorGuard()
	b.get(0)
	b.op(0x45)
	b.op(0x04)
	b.op(0x40)
	b.i32(0)
	b.op(0x0f)
	b.op(0x0b)
	b.index(0x23, allocationGlobal)
	b.set(1)
	b.get(1)
	b.op(0xad) // i64.extend_i32_u
	b.get(0)
	b.op(0xad)
	b.op(0x7c)
	b.i64(7)
	b.op(0x7c)
	b.i64(-8)
	b.op(0x83)
	b.index(0x22, 2)
	b.i64(65536)
	b.op(0x56)
	b.guard(statusArenaLimit)
	b.get(2)
	b.op(0xa7)
	b.index(0x24, allocationGlobal)
	b.index(0x23, arenaBaseGlobal)
	b.get(1)
	b.op(0x6a)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(1), I32Locals: 1, I64Locals: 1, Body: b}
}

func (b *instructions) sliceGuard(pointer, length uint32) {
	b.get(pointer)
	b.op(0xad)
	b.get(length)
	b.op(0xad)
	b.op(0x7c)
	b.i64(196608)
	b.op(0x56)
	b.guard(statusBadInput)
}

// copyFunction is an internal byte loop. Both slices are checked before the
// first store, including pointer addition that would wrap in i32.
func (e *expressionEmitter) copyFunction() wasmgen.Function {
	var b instructions
	b.errorGuard()
	b.sliceGuard(0, 2)
	b.sliceGuard(1, 2)
	b.op(0x02)
	b.op(0x40)
	b.op(0x03)
	b.op(0x40)
	b.get(3)
	b.get(2)
	b.op(0x4f)
	b.index(0x0d, 1)
	b.get(0)
	b.get(3)
	b.op(0x6a)
	b.get(1)
	b.get(3)
	b.op(0x6a)
	b.memory(0x2d, 0, 0)
	b.memory(0x3a, 0, 0)
	b.get(3)
	b.i32(1)
	b.op(0x6a)
	b.set(3)
	b.index(0x0c, 0)
	b.op(0x0b)
	b.op(0x0b)
	b.i32(0)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(3), I32Locals: 1, Body: b}
}

func (b *instructions) textFields(record, pointer, length uint32) {
	b.get(record)
	b.op(0xad)
	b.i64(valueBytes)
	b.op(0x7c)
	b.i64(196608)
	b.op(0x56)
	b.guard(statusBadInput)
	b.get(record)
	b.memory(0x28, 2, 0)
	b.i32(int32(program.TypeString))
	b.op(0x47)
	b.guard(statusBadInput)
	b.get(record)
	b.memory(0x28, 2, 4)
	b.i32(-2)
	b.op(0x71)
	b.guard(statusBadInput)
	b.get(record)
	b.memory(0x29, 3, 8)
	b.op(0x50)
	b.op(0x45)
	b.guard(statusBadInput)
	for i, offset := range []uint32{16, 20} {
		b.get(record)
		b.memory(0x28, 2, offset)
		local := pointer
		if i == 1 {
			local = length
		}
		b.set(local)
	}
	b.get(length)
	b.i32(4096)
	b.op(0x4b)
	b.guard(statusStringLimit)
	b.get(length)
	b.op(0x45)
	b.get(pointer)
	b.op(0x45)
	b.op(0x47)
	b.guard(statusBadInput)
	// A typed string zero has no string-kind flag and no payload.
	b.get(record)
	b.memory(0x28, 2, 4)
	b.op(0x45)
	b.get(length)
	b.op(0x45)
	b.op(0x45)
	b.op(0x71)
	b.guard(statusBadInput)
	b.sliceGuard(pointer, length)
}

func (b *instructions) writeString(record uint32, pointer, length instructions) {
	for offset := uint32(0); offset < valueBytes; offset += 4 {
		b.get(record)
		switch offset {
		case 4:
			b.i32(1)
		case 16:
			*b = append(*b, pointer...)
		case 20:
			*b = append(*b, length...)
		default:
			b.i32(0)
		}
		b.memory(0x36, 2, offset)
	}
}

func localInstructions(local uint32) instructions { var b instructions; b.get(local); return b }

func (e *expressionEmitter) joinFunction() wasmgen.Function {
	var b instructions
	b.errorGuard()
	b.textFields(0, 2, 3)
	b.textFields(1, 4, 5)
	b.get(3)
	b.op(0xad)
	b.get(5)
	b.op(0xad)
	b.op(0x7c)
	b.index(0x22, 9)
	b.i64(4096)
	b.op(0x56)
	b.guard(statusStringLimit)
	b.get(9)
	b.op(0xa7)
	b.index(0x22, 6)
	e.callHelper(&b, helperAllocate)
	b.set(7)
	b.errorGuard()
	b.i32(valueBytes)
	e.callHelper(&b, helperAllocate)
	b.set(8)
	b.errorGuard()
	for _, args := range [][3]uint32{{7, 2, 3}, {7, 4, 5}} {
		b.get(args[0])
		if args[1] == 4 {
			b.get(3)
			b.op(0x6a)
		}
		b.get(args[1])
		b.get(args[2])
		e.callHelper(&b, helperCopy)
		b.op(0x1a)
		b.errorGuard()
	}
	b.writeString(8, localInstructions(7), localInstructions(6))
	b.get(8)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(2), I32Locals: 7, I64Locals: 1, Body: b}
}

func (e *expressionEmitter) stringExpression(id program.ExprID, expr program.Expr) (wasmgen.Function, error) {
	var b instructions
	b.errorGuard()
	if expr.Op == program.OpLitString {
		c := e.strings[expr.Value]
		b.destination(id, 1)
		b.writeString(1, i32Instructions(c.pointer), i32Instructions(c.length))
		b.get(1)
		b.op(0x0b)
		return wasmgen.Function{I32Locals: 1, Body: b}, nil
	}
	e.value(&b, expr.Operands[0], 2)
	if expr.Op == program.OpLen {
		b.textFields(2, 3, 4)
		number := localInstructions(4)
		number.op(0xad)
		b.record(id, program.TypeInt, number, i32Instructions(0))
		return wasmgen.Function{I32Locals: 4, Body: b}, nil
	}
	e.value(&b, expr.Operands[1], 3)
	// Concat reads raw text, so an integer/bool contributes no bytes. Add
	// has proved string operands and therefore the same admitted byte result.
	for i, operand := range expr.Operands {
		if e.unit.Contract.Expressions[operand].Kind != String {
			b.i32(valueBytes)
			e.callHelper(&b, helperAllocate)
			b.set(uint32(2 + i))
			b.errorGuard()
			b.writeString(uint32(2+i), i32Instructions(0), i32Instructions(0))
		}
	}
	b.get(2)
	b.get(3)
	e.callHelper(&b, helperJoin)
	b.set(2)
	b.errorGuard()
	b.copyRecord(id, 2)
	return wasmgen.Function{I32Locals: 3, Body: b}, nil
}

// Byte ordering is unsigned and stops at the first different byte. Length
// breaks ties only after an equal prefix; NUL has no special meaning.
func (e *expressionEmitter) compareFunction() wasmgen.Function {
	var b instructions
	b.errorGuard()
	b.sliceGuard(0, 1)
	b.sliceGuard(2, 3)
	b.op(0x02)
	b.op(0x40)
	b.op(0x03)
	b.op(0x40)
	for _, length := range []uint32{1, 3} {
		b.get(4)
		b.get(length)
		b.op(0x4f)
		b.index(0x0d, 1)
	}
	for i, pointer := range []uint32{0, 2} {
		b.get(pointer)
		b.get(4)
		b.op(0x6a)
		b.memory(0x2d, 0, 0)
		b.set(uint32(5 + i))
	}
	b.get(5)
	b.get(6)
	b.op(0x47)
	b.op(0x04)
	b.op(0x40)
	b.i32(-1)
	b.i32(1)
	b.get(5)
	b.get(6)
	b.op(0x49)
	b.op(0x1b)
	b.op(0x0f)
	b.op(0x0b)
	b.get(4)
	b.i32(1)
	b.op(0x6a)
	b.set(4)
	b.index(0x0c, 0)
	b.op(0x0b)
	b.op(0x0b)
	b.get(1)
	b.get(3)
	b.op(0x4b)
	b.get(1)
	b.get(3)
	b.op(0x49)
	b.op(0x6b)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(4), I32Locals: 3, Body: b}
}

func (e *expressionEmitter) stringComparison(id program.ExprID, expr program.Expr) wasmgen.Function {
	var b, flags instructions
	b.errorGuard()
	e.value(&b, expr.Operands[0], 2)
	e.value(&b, expr.Operands[1], 3)
	b.textFields(2, 4, 5)
	b.textFields(3, 6, 7)
	for _, local := range []uint32{4, 5, 6, 7} {
		b.get(local)
	}
	e.callHelper(&b, helperCompare)
	b.set(8)
	b.errorGuard()
	flags.get(8)
	flags.i32(0)
	op := map[program.OpCode]byte{program.OpEq: 0x46, program.OpNeq: 0x47, program.OpLt: 0x48,
		program.OpGt: 0x4a, program.OpLte: 0x4c, program.OpGte: 0x4e}[expr.Op]
	flags.op(op)
	flags.i32(1)
	flags.op(0x74)
	b.record(id, program.TypeBool, i64Instructions(0), flags)
	return wasmgen.Function{I32Locals: 8, Body: b}
}
