package aot

import "m31labs.dev/gosx/internal/wasmgen"

const (
	transactionBegin = iota
	transactionCommit
	transactionAbort
	transactionStore
)

func (e *expressionEmitter) setupTransactions() {
	e.rootCopy = uint32(len(e.module.Imports) + len(e.module.Functions))
	e.module.Functions = append(e.module.Functions, e.copyRootFunction())
	for i := range e.transactions {
		e.transactions[i] = uint32(len(e.module.Imports) + len(e.module.Functions) + i)
	}
	e.module.Functions = append(e.module.Functions, e.beginFunction(), e.commitFunction(),
		e.abortFunction(), e.storeRootFunction())
}

func (b *instructions) sequenceEqual(lo, hi uint32) {
	for i, global := range []uint32{lo, hi} {
		b.get(uint32(i))
		b.index(0x23, global)
		b.op(0x46)
	}
	b.op(0x71)
}

// begin takes sequence words and an initialization flag. It reserves another
// generation without reclaiming the committed one. Initialization is attempted
// once; ordinary events require a committed initialization and a newer sequence.
func (e *expressionEmitter) beginFunction() wasmgen.Function {
	var b instructions
	b.index(0x23, pendingGlobal)
	b.statusFailure(statusBusy)
	b.get(2)
	b.i32(1)
	b.op(0x4b)
	b.statusFailure(statusBadInput)
	b.get(2)
	b.op(0x04)
	b.op(0x40)
	b.index(0x23, initialAttemptGlobal)
	b.statusFailure(statusBadSequence)
	b.op(0x05)
	b.index(0x23, initializedGlobal)
	b.op(0x45)
	b.statusFailure(statusBadSequence)
	b.get(0)
	b.get(1)
	b.op(0x72)
	b.op(0x45)
	b.statusFailure(statusBadSequence)
	b.get(1)
	b.index(0x23, committedHiGlobal)
	b.op(0x4b)
	b.get(1)
	b.index(0x23, committedHiGlobal)
	b.op(0x46)
	b.get(0)
	b.index(0x23, committedLoGlobal)
	b.op(0x4b)
	b.op(0x71)
	b.op(0x72)
	b.op(0x45)
	b.statusFailure(statusBadSequence)
	b.op(0x0b)
	b.i32(0)
	b.index(0x24, errorGlobal)
	b.i32(196608)
	b.index(0x23, committedBaseGlobal)
	b.op(0x6b)
	b.index(0x22, 4)
	b.index(0x24, arenaBaseGlobal)
	b.get(4)
	b.i32(int32(e.rootSlots) * valueBytes)
	b.op(0x6a)
	b.index(0x24, workingBaseGlobal)
	b.i32(int32(e.reserved))
	b.index(0x24, allocationGlobal)
	b.i32(0)
	b.index(0x24, workingStringsGlobal)
	if e.dom != nil {
		b.i32(0)
		b.index(0x24, e.dom.patchCount)
	}
	b.i32(1)
	b.index(0x24, pendingGlobal)
	for i, global := range []uint32{pendingLoGlobal, pendingHiGlobal} {
		b.get(uint32(i))
		b.index(0x24, global)
	}
	b.get(2)
	b.op(0x04)
	b.op(0x40)
	b.i32(1)
	b.index(0x24, initialAttemptGlobal)
	b.op(0x0b)
	b.op(0x02)
	b.op(0x40)
	b.op(0x03)
	b.op(0x40)
	b.get(3)
	b.i32(int32(e.rootSlots) * valueBytes)
	b.op(0x4f)
	b.index(0x0d, 1)
	b.get(2)
	b.op(0x04)
	b.op(0x40)
	for offset := uint32(0); offset < valueBytes; offset += 8 {
		b.get(4)
		b.get(3)
		b.op(0x6a)
		b.i64(0)
		b.memory(0x37, 3, offset)
	}
	b.op(0x05)
	b.index(0x23, committedBaseGlobal)
	b.get(3)
	b.op(0x6a)
	b.get(4)
	b.get(3)
	b.op(0x6a)
	b.i32(0)
	b.index(0x10, e.rootCopy)
	b.op(0x1a)
	b.statusGuard()
	b.op(0x0b)
	b.get(3)
	b.i32(valueBytes)
	b.op(0x6a)
	b.set(3)
	b.index(0x0c, 0)
	b.op(0x0b)
	b.op(0x0b)
	if e.computed != nil {
		b.get(2)
		b.index(0x10, e.computed.begin)
		b.op(0x1a)
		b.statusGuard()
	}
	b.i32(0)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(3), I32Locals: 2, Body: b}
}

func (e *expressionEmitter) commitFunction() wasmgen.Function {
	var b instructions
	b.sequenceEqual(committedLoGlobal, committedHiGlobal)
	b.index(0x23, initializedGlobal)
	b.op(0x71)
	b.op(0x04)
	b.op(0x40)
	b.i32(0)
	b.op(0x0f)
	b.op(0x0b)
	b.index(0x23, pendingGlobal)
	b.op(0x45)
	b.statusFailure(statusBadSequence)
	b.sequenceEqual(pendingLoGlobal, pendingHiGlobal)
	b.op(0x45)
	b.statusFailure(statusBadSequence)
	b.statusGuard()
	if e.computed != nil {
		b.index(0x10, e.computed.commit)
		b.op(0x1a)
		b.statusGuard()
	}
	for _, pair := range [][2]uint32{{arenaBaseGlobal, committedBaseGlobal},
		{allocationGlobal, committedCursorGlobal}, {workingStringsGlobal, committedStringsGlobal},
		{pendingLoGlobal, committedLoGlobal}, {pendingHiGlobal, committedHiGlobal}} {
		b.index(0x23, pair[0])
		b.index(0x24, pair[1])
	}
	b.i32(1)
	b.index(0x24, initializedGlobal)
	b.i32(0)
	b.index(0x24, pendingGlobal)
	b.i32(0)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(2), Body: b}
}

func (e *expressionEmitter) abortFunction() wasmgen.Function {
	var b instructions
	for _, global := range []uint32{pendingGlobal, errorGlobal, workingStringsGlobal} {
		b.i32(0)
		b.index(0x24, global)
	}
	b.i32(int32(e.reserved))
	b.index(0x24, allocationGlobal)
	if e.computed != nil {
		b.index(0x10, e.computed.abort)
		b.op(0x1a)
	}
	b.i32(0)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(0), Body: b}
}
