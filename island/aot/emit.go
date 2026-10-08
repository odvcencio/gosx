package aot

import (
	"fmt"

	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

const (
	valueBytes          = 24
	errorGlobal         = 0
	workingBaseGlobal   = 1
	statusBadInput      = 2
	statusIntegerDomain = 3
)

// expressionEmitter builds internal expression functions, not a page ABI.
// Every expression has an instance parameter and returns a scalar-record
// pointer or zero on failure. Records occupy bounded working-arena slots;
// committed roots and host effects are outside this expression library.
type expressionEmitter struct {
	unit      Unit
	module    wasmgen.Module
	functions []uint32
}

func emitExpressions(u Unit) (*expressionEmitter, error) {
	if r := Classify(u, ScalarDOMV1); !r.Eligible {
		return nil, fmt.Errorf("scalar profile: %s[%d]: %s", r.Table, r.Index, r.Reason)
	}
	sig := func(count int) wasmgen.Signature {
		params := make([]wasmgen.ValueType, count)
		for i := range params {
			params[i] = wasmgen.I32
		}
		return wasmgen.Signature{Params: params, Result: wasmgen.I32}
	}
	e := &expressionEmitter{unit: u, module: wasmgen.Module{
		Imports: []wasmgen.Import{
			{Module: "gosx_aot_v1", Name: "input", Signature: sig(4)},
			{Module: "gosx_aot_v1", Name: "bind", Signature: sig(4)},
			{Module: "gosx_aot_v1", Name: "patch", Signature: sig(5)},
		},
		Globals: []wasmgen.Global{{Mutable: true}, {Mutable: true, Initial: 131072}},
	}}
	e.functions = make([]uint32, len(u.Program.Exprs))
	e.module.Functions = make([]wasmgen.Function, len(u.Program.Exprs))
	for i := range e.functions {
		e.functions[i] = uint32(len(e.module.Imports) + i)
	}
	for i, expr := range u.Program.Exprs {
		fn, err := e.expression(program.ExprID(i), expr)
		if err != nil {
			return nil, fmt.Errorf("expression %d: %w", i, err)
		}
		fn.Signature = sig(1)
		e.module.Functions[i] = fn
	}
	binary, err := wasmgen.Encode(e.module)
	if err == nil {
		err = wasmgen.Validate(binary)
	}
	if err != nil {
		return nil, fmt.Errorf("expression module: %w", err)
	}
	return e, nil
}

func (e *expressionEmitter) expression(id program.ExprID, expr program.Expr) (wasmgen.Function, error) {
	switch expr.Op {
	case program.OpLitInt, program.OpAdd, program.OpSub, program.OpMul, program.OpNeg:
		return e.integer(id, expr)
	case program.OpLitBool, program.OpEq, program.OpNeq, program.OpLt, program.OpGt,
		program.OpLte, program.OpGte, program.OpAnd, program.OpOr, program.OpNot:
		return e.comparison(id, expr)
	case program.OpCond:
		return e.conditional(id, expr)
	default:
		return wasmgen.Function{}, fmt.Errorf("opcode not implemented by the scalar expression emitter")
	}
}

type instructions []byte

func (b *instructions) op(op byte)                  { *b = append(*b, op) }
func (b *instructions) index(op byte, index uint32) { b.op(op); *b = wasmgen.AppendU32(*b, index) }
func (b *instructions) i32(value int32)             { b.op(0x41); *b = wasmgen.AppendI32(*b, value) }
func (b *instructions) i64(value int64)             { b.op(0x42); *b = wasmgen.AppendI64(*b, value) }
func (b *instructions) memory(op byte, alignment, offset uint32) {
	b.op(op)
	*b = wasmgen.AppendU32(*b, alignment)
	*b = wasmgen.AppendU32(*b, offset)
}

func (b *instructions) failure(status int32) {
	b.i32(status)
	b.index(0x24, errorGlobal)
	b.i32(0)
	b.op(0x0f)
}

func (b *instructions) errorGuard() {
	b.index(0x23, errorGlobal)
	b.op(0x04)
	b.op(0x40)
	b.i32(0)
	b.op(0x0f)
	b.op(0x0b)
}

func (b *instructions) destination(id program.ExprID, local uint32) {
	b.index(0x23, workingBaseGlobal)
	b.i32(int32(id) * valueBytes)
	b.op(0x6a)
	b.index(0x21, local)
}

// operand loads after checking the called expression's status and exact tag.
// Actual input/default kinds cannot silently enter integer/bool arithmetic.
func (e *expressionEmitter) operand(b *instructions, id program.ExprID, typ program.ExprType) {
	b.index(0x20, 0)
	b.index(0x10, e.functions[id])
	b.index(0x21, 1)
	b.errorGuard()
	b.index(0x20, 1)
	b.memory(0x28, 2, 0)
	b.i32(int32(typ))
	b.op(0x47)
	b.op(0x04)
	b.op(0x40)
	b.failure(statusBadInput)
	b.op(0x0b)
	b.index(0x20, 1)
	if typ == program.TypeInt {
		b.memory(0x29, 3, 8)
	} else {
		b.memory(0x28, 2, 4)
		b.i32(2)
		b.op(0x71)
	}
}

// record writes a complete value only after all operands and guards succeed.
func (b *instructions) record(id program.ExprID, typ program.ExprType, number, flags instructions) {
	b.destination(id, 1)
	b.index(0x20, 1)
	b.i32(int32(typ))
	b.memory(0x36, 2, 0)
	b.index(0x20, 1)
	*b = append(*b, flags...)
	b.memory(0x36, 2, 4)
	b.index(0x20, 1)
	*b = append(*b, number...)
	b.memory(0x37, 3, 8)
	b.index(0x20, 1)
	b.i64(0)
	b.memory(0x37, 3, 16)
	b.index(0x20, 1)
	b.op(0x0b)
}

func i32Instructions(value int32) instructions { var b instructions; b.i32(value); return b }
func i64Instructions(value int64) instructions { var b instructions; b.i64(value); return b }

func (e *expressionEmitter) conditional(id program.ExprID, expr program.Expr) (wasmgen.Function, error) {
	kind := e.unit.Contract.Expressions[id].Kind
	if !integerKind(kind) && kind != Bool {
		return wasmgen.Function{}, fmt.Errorf("conditional kind not implemented")
	}
	var b instructions
	b.errorGuard()
	e.operand(&b, expr.Operands[0], program.TypeBool)
	b.op(0x04)
	b.op(byte(wasmgen.I32))
	for i, arm := range expr.Operands[1:] {
		if i != 0 {
			b.op(0x05)
		}
		b.index(0x20, 0)
		b.index(0x10, e.functions[arm])
	}
	b.op(0x0b)
	b.index(0x21, 1)
	b.errorGuard()
	b.destination(id, 2)
	for offset := uint32(0); offset < valueBytes; offset += 8 {
		b.index(0x20, 2)
		b.index(0x20, 1)
		b.memory(0x29, 3, offset)
		b.memory(0x37, 3, offset)
	}
	b.index(0x20, 2)
	b.op(0x0b)
	return wasmgen.Function{I32Locals: 2, Body: b}, nil
}
