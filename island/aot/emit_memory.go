package aot

import (
	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

const (
	arenaBaseGlobal        = 3
	committedBaseGlobal    = 4
	pendingGlobal          = 5
	committedLoGlobal      = 6
	committedHiGlobal      = 7
	pendingLoGlobal        = 8
	pendingHiGlobal        = 9
	committedCursorGlobal  = 10
	workingStringsGlobal   = 11
	committedStringsGlobal = 12
	initializedGlobal      = 13
	initialAttemptGlobal   = 14
	statusBusy             = 7
	statusBadSequence      = 8
)

func (e *expressionEmitter) memoryGlobals() {
	values := []int32{65536, 0, 0, 0, 0, 0, int32(e.rootSlots) * valueBytes, 0, 0, 0, 0}
	for _, value := range values {
		e.module.Globals = append(e.module.Globals, wasmgen.Global{Mutable: true, Initial: value})
	}
}

func (b *instructions) statusGuard() {
	b.index(0x23, errorGlobal)
	b.op(0x04)
	b.op(0x40)
	b.index(0x23, errorGlobal)
	b.op(0x0f)
	b.op(0x0b)
}

func (b *instructions) statusFailure(status int32) {
	b.op(0x04)
	b.op(0x40)
	b.i32(status)
	b.op(0x0f)
	b.op(0x0b)
}

// copyRoot validates the complete scalar before touching a destination root.
// String bytes are copied into the working generation. The third parameter
// is the replaced root's live length, not temporary allocation high water.
func (e *expressionEmitter) copyRootFunction() wasmgen.Function {
	var b instructions
	b.errorGuard()
	b.get(0)
	b.i32(1024)
	b.op(0x49)
	b.guard(statusBadInput)
	b.get(0)
	b.op(0xad)
	b.i64(valueBytes)
	b.op(0x7c)
	b.i64(196608)
	b.op(0x56)
	b.guard(statusBadInput)
	b.get(0)
	b.memory(0x28, 2, 0)
	b.set(3)
	b.typeBranch(3, program.TypeString)
	b.textFields(0, 4, 5)
	b.op(0x05)
	b.typeBranch(3, program.TypeInt)
	b.numericShape(0, 0, false)
	b.get(0)
	b.memory(0x29, 3, 8)
	b.set(7)
	b.integerGuard(7)
	b.op(0x05)
	b.typeBranch(3, program.TypeBool)
	b.numericShape(0, 2, true)
	b.op(0x05)
	b.typeBranch(3, program.TypeAny)
	b.numericShape(0, 0, true)
	b.op(0x05)
	b.failure(statusBadInput)
	for i := 0; i < 4; i++ {
		b.op(0x0b)
	}
	b.get(2)
	b.index(0x23, workingStringsGlobal)
	b.op(0x4b)
	b.guard(statusBadInput)
	b.index(0x23, workingStringsGlobal)
	b.get(2)
	b.op(0x6b)
	b.op(0xad)
	b.get(5)
	b.op(0xad)
	b.op(0x7c)
	b.index(0x22, 8)
	b.i64(16384)
	b.op(0x56)
	b.guard(statusStringLimit)
	b.get(5)
	e.callHelper(&b, helperAllocate)
	b.set(6)
	b.errorGuard()
	b.get(6)
	b.get(4)
	b.get(5)
	e.callHelper(&b, helperCopy)
	b.op(0x1a)
	b.errorGuard()
	for offset := uint32(0); offset < valueBytes; offset += 8 {
		b.get(1)
		b.get(0)
		b.memory(0x29, 3, offset)
		b.memory(0x37, 3, offset)
	}
	b.get(3)
	b.i32(int32(program.TypeString))
	b.op(0x46)
	b.op(0x04)
	b.op(0x40)
	b.get(1)
	b.get(6)
	b.memory(0x36, 2, 16)
	b.op(0x0b)
	b.get(8)
	b.op(0xa7)
	b.index(0x24, workingStringsGlobal)
	b.get(1)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(3), I32Locals: 4, I64Locals: 2, Body: b}
}

func (e *expressionEmitter) storeRootFunction() wasmgen.Function {
	var b instructions
	b.index(0x23, pendingGlobal)
	b.op(0x45)
	b.statusFailure(statusBusy)
	b.get(0)
	b.i32(int32(e.rootSlots))
	b.op(0x4f)
	b.op(0x04)
	b.op(0x40)
	b.i32(statusBadInput)
	b.index(0x24, errorGlobal)
	b.i32(statusBadInput)
	b.op(0x0f)
	b.op(0x0b)
	b.statusGuard()
	b.index(0x23, arenaBaseGlobal)
	b.get(0)
	b.i32(valueBytes)
	b.op(0x6c)
	b.op(0x6a)
	b.set(2)
	b.get(2)
	b.memory(0x28, 2, 0)
	b.i32(int32(program.TypeString))
	b.op(0x46)
	b.op(0x04)
	b.op(0x40)
	b.get(2)
	b.memory(0x28, 2, 20)
	b.set(3)
	b.op(0x0b)
	b.get(1)
	b.get(2)
	b.get(3)
	b.index(0x10, e.rootCopy)
	b.op(0x1a)
	b.statusGuard()
	b.i32(0)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(2), I32Locals: 2, Body: b}
}
