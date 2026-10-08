package aot

import (
	"fmt"

	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

// Each fixed row holds committed and working flags, dependency bits and a
// subscription-order stamp. Scalar caches live in the corresponding arena.
const (
	computedMetaBase   = 17408
	computedMetaBytes  = 32
	computedWorking    = 16
	computedCreated    = 1
	computedDirty      = 2
	computedRefreshing = 4
)

type computedLayout struct {
	getters                                                              []uint32
	begin, commit, abort, initialize                                     uint32
	mask, tracking, clockLo, clockHi, committedClockLo, committedClockHi uint32
}

func (e *expressionEmitter) setupComputed() error {
	if computedMetaBase+len(e.state.instances)*int(e.state.computedCount)*computedMetaBytes > 32768 {
		return fmt.Errorf("computed tables exceed the fixed interval")
	}
	// Caches without mutable writes retain source-order read semantics.
	for _, expr := range e.unit.Program.Exprs {
		if expr.Op == program.OpSignalSet {
			return fmt.Errorf("computed writes require notification support")
		}
	}
	c := &computedLayout{getters: make([]uint32, e.state.computedCount)}
	e.computed = c
	for _, global := range []*uint32{&c.mask, &c.tracking, &c.clockLo, &c.clockHi, &c.committedClockLo, &c.committedClockHi} {
		*global = uint32(len(e.module.Globals))
		e.module.Globals = append(e.module.Globals, wasmgen.Global{Mutable: true})
	}
	indices := []*uint32{&c.begin, &c.commit, &c.abort, &c.initialize}
	for i := range c.getters {
		indices = append(indices, &c.getters[i])
	}
	for _, index := range indices {
		*index = uint32(len(e.module.Imports) + len(e.module.Functions))
		e.module.Functions = append(e.module.Functions, wasmgen.Function{})
	}
	put := func(index uint32, fn wasmgen.Function) { e.module.Functions[index-uint32(len(e.module.Imports))] = fn }
	put(c.begin, e.computedBeginFunction())
	put(c.commit, e.computedCommitFunction())
	put(c.abort, e.computedAbortFunction())
	put(c.initialize, e.computedInitializeFunction())
	for i, def := range e.unit.Program.Computeds {
		put(c.getters[i], e.computedGetFunction(uint32(i), def.Expr))
	}
	put(e.transactions[transactionBegin], e.beginFunction())
	put(e.transactions[transactionCommit], e.commitFunction())
	put(e.transactions[transactionAbort], e.abortFunction())
	return nil
}

func (e *expressionEmitter) computedMeta(b *instructions, frame uint32, index, offset uint32) {
	b.i32(computedMetaBase + int32(index)*computedMetaBytes + int32(offset))
	b.get(frame)
	b.i32(int32(e.state.computedCount) * computedMetaBytes)
	b.op(0x6c)
	b.op(0x6a)
}

func (e *expressionEmitter) trackDependency(b *instructions, bit uint32) {
	b.index(0x23, e.computed.tracking)
	b.op(0x04)
	b.op(0x40)
	b.index(0x23, e.computed.mask)
	b.i32(int32(uint32(1) << bit))
	b.op(0x72)
	b.index(0x24, e.computed.mask)
	b.op(0x0b)
}

func (e *expressionEmitter) computedGetFunction(index uint32, body program.ExprID) wasmgen.Function {
	var b instructions
	b.errorGuard()
	b.get(0)
	b.index(0x10, e.state.lookup)
	b.set(1)
	b.errorGuard()
	e.computedMeta(&b, 1, index, computedWorking)
	b.set(2)
	b.index(0x23, arenaBaseGlobal)
	b.get(1)
	b.i32(int32(e.state.computedCount))
	b.op(0x6c)
	b.i32(int32(e.state.mutableRoots + index))
	b.op(0x6a)
	b.i32(valueBytes)
	b.op(0x6c)
	b.op(0x6a)
	b.set(3)
	b.get(2)
	b.memory(0x28, 2, 0)
	b.i32(computedDirty)
	b.op(0x71)
	b.op(0x04)
	b.op(0x40)
	// Save the enclosing tracker; nested computed bodies record only their
	// own reads. The caller later records this computed's signal identity.
	b.index(0x23, e.computed.mask)
	b.set(5)
	b.index(0x23, e.computed.tracking)
	b.set(6)
	b.i32(0)
	b.index(0x24, e.computed.mask)
	b.i32(1)
	b.index(0x24, e.computed.tracking)
	b.get(0)
	b.index(0x10, e.functions[body])
	b.set(4)
	b.index(0x23, e.computed.mask)
	b.set(7)
	for _, pair := range [][2]uint32{{5, e.computed.mask}, {6, e.computed.tracking}} {
		b.get(pair[0])
		b.index(0x24, pair[1])
	}
	b.errorGuard()
	b.get(1)
	b.i32(int32(e.state.computedCount))
	b.op(0x6c)
	b.i32(int32(e.state.mutableRoots + index))
	b.op(0x6a)
	b.get(4)
	b.index(0x10, e.transactions[transactionStore])
	b.op(0x1a)
	b.errorGuard()
	b.get(2)
	b.get(7)
	b.memory(0x36, 2, 4)
	// Every refresh unsubscribes and appends its new subscriptions, even
	// when the computed value or dependency set is unchanged.
	b.index(0x23, e.computed.clockLo)
	b.op(0xad)
	b.index(0x23, e.computed.clockHi)
	b.op(0xad)
	b.i64(32)
	b.op(0x86)
	b.op(0x84)
	b.i64(1)
	b.op(0x7c)
	b.index(0x22, 8)
	b.op(0x50)
	b.guard(statusBadInput)
	b.get(2)
	b.get(8)
	b.memory(0x37, 3, 8)
	b.get(8)
	b.op(0xa7)
	b.index(0x24, e.computed.clockLo)
	b.get(8)
	b.i64(32)
	b.op(0x88)
	b.op(0xa7)
	b.index(0x24, e.computed.clockHi)
	b.get(2)
	b.i32(computedCreated)
	b.memory(0x36, 2, 0)
	b.op(0x0b)
	b.get(3)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(1), I32Locals: 7, I64Locals: 1, Body: b}
}

func (e *expressionEmitter) computedExpression(id program.ExprID, expr program.Expr) (wasmgen.Function, error) {
	index := uint32(0)
	found := false
	for i, def := range e.unit.Program.Computeds {
		if def.Name == expr.Value {
			index, found = uint32(i), true
			break
		}
	}
	if !found {
		return wasmgen.Function{}, fmt.Errorf("computed name has no cache")
	}
	var b instructions
	b.errorGuard()
	b.index(0x23, pendingGlobal)
	b.op(0x45)
	b.guard(statusBusy)
	b.get(0)
	b.index(0x10, e.state.lookup)
	b.set(3)
	b.errorGuard()
	e.computedMeta(&b, 3, index, computedWorking)
	b.memory(0x28, 2, 0)
	b.i32(computedCreated)
	b.op(0x71)
	b.op(0x45)
	b.op(0x04)
	b.op(0x40)
	// Source-order construction can read a later declaration. The VM
	// returns the read expression's typed zero and records no dependency.
	b.record(id, expr.Type, i64Instructions(0), i32Instructions(0))
	b[len(b)-1] = 0x0f
	b.op(0x0b)
	b.get(0)
	b.index(0x10, e.computed.getters[index])
	b.set(2)
	b.errorGuard()
	e.trackDependency(&b, 16+index)
	b.copyRecord(id, 2)
	return wasmgen.Function{Signature: i32Signature(1), I32Locals: 3, Body: b}, nil
}

func (e *expressionEmitter) computedInitializeFunction() wasmgen.Function {
	var b instructions
	b.statusGuard()
	b.index(0x23, pendingGlobal)
	b.op(0x45)
	b.statusFailure(statusBusy)
	b.index(0x23, initializedGlobal)
	b.statusFailure(statusBadSequence)
	for frame, instance := range e.state.instances {
		for index := range e.unit.Program.Computeds {
			base := computedMetaBase + int32(frame*int(e.state.computedCount)+index)*computedMetaBytes + computedWorking
			b.i32(base)
			b.memory(0x28, 2, 0)
			b.statusFailure(statusBadSequence)
			b.i32(base)
			b.i32(computedDirty)
			b.memory(0x36, 2, 0)
			b.i32(int32(instance))
			b.index(0x10, e.computed.getters[index])
			b.op(0x1a)
			b.statusGuard()
		}
	}
	b.i32(0)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(0), Body: b}
}

func (e *expressionEmitter) computedBeginFunction() wasmgen.Function {
	var b instructions
	for row := 0; row < len(e.state.instances)*int(e.state.computedCount); row++ {
		base := computedMetaBase + int32(row)*computedMetaBytes
		for offset := uint32(0); offset < computedWorking; offset += 8 {
			b.i32(base + computedWorking)
			b.get(0)
			b.op(0x04)
			b.op(byte(wasmgen.I64))
			b.i64(0)
			b.op(0x05)
			b.i32(base)
			b.memory(0x29, 3, offset)
			b.op(0x0b)
			b.memory(0x37, 3, offset)
		}
	}
	for _, pair := range [][2]uint32{{e.computed.committedClockLo, e.computed.clockLo}, {e.computed.committedClockHi, e.computed.clockHi}} {
		b.index(0x23, pair[0])
		b.index(0x24, pair[1])
	}
	b.i32(0)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(1), Body: b}
}

func (e *expressionEmitter) computedCommitFunction() wasmgen.Function {
	var b instructions
	for row := 0; row < len(e.state.instances)*int(e.state.computedCount); row++ {
		base := computedMetaBase + int32(row)*computedMetaBytes
		for offset := uint32(0); offset < computedWorking; offset += 8 {
			b.i32(base)
			b.i32(base + computedWorking)
			b.memory(0x29, 3, offset)
			b.memory(0x37, 3, offset)
		}
	}
	for _, pair := range [][2]uint32{{e.computed.clockLo, e.computed.committedClockLo}, {e.computed.clockHi, e.computed.committedClockHi}} {
		b.index(0x23, pair[0])
		b.index(0x24, pair[1])
	}
	b.i32(0)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(0), Body: b}
}

func (e *expressionEmitter) computedAbortFunction() wasmgen.Function {
	var b instructions
	for _, global := range []uint32{e.computed.mask, e.computed.tracking} {
		b.i32(0)
		b.index(0x24, global)
	}
	b.i32(0)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(0), Body: b}
}
