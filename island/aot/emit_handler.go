package aot

import (
	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

func (e *expressionEmitter) setupHandlers() {
	e.handlers = make([]uint32, len(e.unit.Program.Handlers))
	for i, handler := range e.unit.Program.Handlers {
		e.handlers[i] = uint32(len(e.module.Imports) + len(e.module.Functions))
		var b instructions
		b.index(0x23, pendingGlobal)
		b.op(0x45)
		b.statusFailure(statusBusy)
		b.statusGuard()
		b.get(0)
		b.index(0x10, e.state.lookup)
		b.op(0x1a)
		b.statusGuard()
		for _, expr := range handler.Body {
			b.get(0)
			b.index(0x10, e.functions[expr])
			b.op(0x1a)
			b.statusGuard()
		}
		b.i32(0)
		b.op(0x0b)
		e.module.Functions = append(e.module.Functions, wasmgen.Function{Signature: i32Signature(1), Body: b})
	}
}

func (e *expressionEmitter) sequenceExpression(id program.ExprID, expr program.Expr) wasmgen.Function {
	var b instructions
	b.errorGuard()
	for _, child := range expr.Operands {
		e.value(&b, child, 2)
	}
	if len(expr.Operands) == 0 {
		b.record(id, program.TypeAny, i64Instructions(0), i32Instructions(0))
	} else {
		b.copyRecord(id, 2)
	}
	return wasmgen.Function{I32Locals: 2, Body: b}
}
