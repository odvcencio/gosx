package aot

import (
	"encoding/binary"
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
	getters                                                                     []uint32
	begin, commit, abort, initialize, snapshot, publish, deliver, notify, flush uint32
	mask, tracking, clockLo, clockHi, committedClockLo, committedClockHi        uint32
	batch, head, tail                                                           uint32
	instancesBase                                                               int32
}

func (e *expressionEmitter) setupComputed() error {
	if computedMetaBase+len(e.state.instances)*int(e.state.computedCount)*computedMetaBytes > 32768 {
		return fmt.Errorf("computed tables exceed the fixed interval")
	}
	c := &computedLayout{getters: make([]uint32, e.state.computedCount)}
	e.computed = c
	for _, global := range []*uint32{&c.mask, &c.tracking, &c.clockLo, &c.clockHi, &c.committedClockLo, &c.committedClockHi, &c.batch, &c.head, &c.tail} {
		*global = uint32(len(e.module.Globals))
		e.module.Globals = append(e.module.Globals, wasmgen.Global{Mutable: true})
	}
	c.instancesBase = int32(wasmgen.ConstantOffset + len(e.module.Data))
	for _, id := range e.state.instances {
		e.module.Data = binary.LittleEndian.AppendUint32(e.module.Data, id)
	}
	if len(e.module.Data) > wasmgen.MaxDataBytes {
		return fmt.Errorf("computed instance table exceeds constants")
	}
	indices := []*uint32{&c.begin, &c.commit, &c.abort, &c.initialize, &c.snapshot, &c.publish, &c.deliver, &c.notify, &c.flush}
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
	put(c.snapshot, e.computedSnapshotFunction())
	put(c.publish, e.computedPublishFunction())
	put(c.deliver, e.computedDeliverFunction())
	put(c.notify, e.computedNotifyFunction())
	put(c.flush, e.computedFlushFunction())
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
	b.op(0x45)
	b.op(0x45)
	b.get(2)
	b.memory(0x28, 2, 0)
	b.i32(computedRefreshing)
	b.op(0x71)
	b.op(0x45)
	b.op(0x71)
	b.op(0x04)
	b.op(0x40)
	b.get(2)
	b.get(2)
	b.memory(0x28, 2, 0)
	b.i32(computedRefreshing)
	b.op(0x72)
	b.memory(0x36, 2, 0)
	b.op(0x03)
	b.op(0x40)
	b.get(2)
	b.memory(0x28, 2, 0)
	b.i32(computedCreated)
	b.op(0x71)
	b.set(8)
	b.get(2)
	b.get(2)
	b.memory(0x28, 2, 0)
	b.i32(^int32(computedDirty))
	b.op(0x71)
	b.memory(0x36, 2, 0)
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
	b.index(0x22, 9)
	b.op(0x50)
	b.guard(statusBadInput)
	b.get(2)
	b.get(9)
	b.memory(0x37, 3, 8)
	b.get(9)
	b.op(0xa7)
	b.index(0x24, e.computed.clockLo)
	b.get(9)
	b.i64(32)
	b.op(0x88)
	b.op(0xa7)
	b.index(0x24, e.computed.clockHi)
	b.get(2)
	b.get(2)
	b.memory(0x28, 2, 0)
	b.i32(computedCreated)
	b.op(0x72)
	b.memory(0x36, 2, 0)
	b.get(8)
	b.op(0x04)
	b.op(0x40)
	b.get(0)
	b.i32(int32(16 + index))
	b.i32(0)
	b.index(0x10, e.computed.publish)
	b.op(0x1a)
	b.errorGuard()
	b.op(0x0b)
	b.get(2)
	b.memory(0x28, 2, 0)
	b.i32(computedDirty)
	b.op(0x71)
	b.index(0x0d, 0)
	b.op(0x0b)
	b.get(2)
	b.get(2)
	b.memory(0x28, 2, 0)
	b.i32(^int32(computedRefreshing))
	b.op(0x71)
	b.memory(0x36, 2, 0)
	b.op(0x0b)
	b.get(3)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(1), I32Locals: 8, I64Locals: 1, Body: b}
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
	b.index(0x10, e.computed.abort)
	b.op(0x1a)
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
	for _, global := range []uint32{e.computed.mask, e.computed.tracking, e.computed.batch, e.computed.head, e.computed.tail} {
		b.i32(0)
		b.index(0x24, global)
	}
	b.i32(0)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(0), Body: b}
}

// snapshot captures callbacks in their current subscription order. Each
// write gets its own list, so duplicate writes and later unsubscriptions
// retain the same deferred callbacks as signal.Batch.
func (e *expressionEmitter) computedSnapshotFunction() wasmgen.Function {
	var b instructions
	b.errorGuard()
	b.op(0x02)
	b.op(0x40)
	b.op(0x03)
	b.op(0x40)
	b.i32(-1)
	b.set(4)
	b.i64(-1)
	b.set(10)
	b.i32(0)
	b.set(3)
	b.op(0x02)
	b.op(0x40)
	b.op(0x03)
	b.op(0x40)
	b.get(3)
	b.i32(int32(len(e.state.instances)) * int32(e.state.computedCount))
	b.op(0x4f)
	b.index(0x0d, 1)
	b.i32(computedMetaBase + computedWorking)
	b.get(3)
	b.i32(computedMetaBytes)
	b.op(0x6c)
	b.op(0x6a)
	b.set(8)
	b.get(2)
	b.get(3)
	b.i32(int32(e.state.computedCount))
	b.op(0x6e)
	b.get(0)
	b.op(0x46)
	b.op(0x72)
	b.get(8)
	b.memory(0x28, 2, 0)
	b.i32(computedCreated)
	b.op(0x71)
	b.op(0x71)
	b.get(8)
	b.memory(0x28, 2, 4)
	b.i32(1)
	b.get(1)
	b.op(0x74)
	b.op(0x71)
	b.op(0x45)
	b.op(0x45)
	b.op(0x71)
	b.op(0x04)
	b.op(0x40)
	b.get(8)
	b.memory(0x29, 3, 8)
	b.index(0x22, 11)
	b.get(9)
	b.op(0x56)
	b.get(4)
	b.i32(-1)
	b.op(0x46)
	b.get(11)
	b.get(10)
	b.op(0x54)
	b.op(0x72)
	b.op(0x71)
	b.op(0x04)
	b.op(0x40)
	b.get(3)
	b.set(4)
	b.get(11)
	b.set(10)
	b.op(0x0b)
	b.op(0x0b)
	b.get(3)
	b.i32(1)
	b.op(0x6a)
	b.set(3)
	b.index(0x0c, 0)
	b.op(0x0b)
	b.op(0x0b)
	b.get(4)
	b.i32(-1)
	b.op(0x46)
	b.index(0x0d, 1)
	b.i32(8)
	e.callHelper(&b, helperAllocate)
	b.set(5)
	b.errorGuard()
	b.get(5)
	b.i32(0)
	b.memory(0x36, 2, 0)
	b.get(5)
	b.get(4)
	b.memory(0x36, 2, 4)
	b.get(7)
	b.op(0x04)
	b.op(0x40)
	b.get(7)
	b.get(5)
	b.memory(0x36, 2, 0)
	b.op(0x05)
	b.get(5)
	b.set(6)
	b.op(0x0b)
	b.get(5)
	b.set(7)
	b.get(10)
	b.set(9)
	b.index(0x0c, 0)
	b.op(0x0b)
	b.op(0x0b)
	b.get(6)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(3), I32Locals: 6, I64Locals: 3, Body: b}
}

func (e *expressionEmitter) computedPublishFunction() wasmgen.Function {
	var b instructions
	b.statusGuard()
	b.get(0)
	b.index(0x10, e.state.lookup)
	b.set(3)
	b.statusGuard()
	b.get(3)
	b.get(1)
	b.get(2)
	b.index(0x10, e.computed.snapshot)
	b.set(4)
	b.statusGuard()
	b.get(4)
	b.index(0x10, e.computed.deliver)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(3), I32Locals: 2, Body: b}
}

func (e *expressionEmitter) computedDeliverFunction() wasmgen.Function {
	var b instructions
	b.statusGuard()
	b.get(0)
	b.op(0x45)
	b.statusFailure(0)
	b.get(0)
	b.set(1)
	b.index(0x23, e.computed.batch)
	b.op(0x04)
	b.op(0x40)
	b.index(0x23, e.computed.tail)
	b.op(0x04)
	b.op(0x40)
	b.index(0x23, e.computed.tail)
	b.get(0)
	b.memory(0x36, 2, 0)
	b.op(0x05)
	b.get(0)
	b.index(0x24, e.computed.head)
	b.op(0x0b)
	b.op(0x03)
	b.op(0x40)
	b.get(1)
	b.memory(0x28, 2, 0)
	b.index(0x22, 2)
	b.op(0x04)
	b.op(0x40)
	b.get(2)
	b.set(1)
	b.index(0x0c, 1)
	b.op(0x0b)
	b.op(0x0b)
	b.get(1)
	b.index(0x24, e.computed.tail)
	b.op(0x05)
	b.op(0x03)
	b.op(0x40)
	b.get(1)
	b.memory(0x28, 2, 0)
	b.set(2)
	b.get(1)
	b.memory(0x28, 2, 4)
	b.index(0x10, e.computed.notify)
	b.op(0x1a)
	b.statusGuard()
	b.get(2)
	b.index(0x22, 1)
	b.index(0x0d, 0)
	b.op(0x0b)
	b.op(0x0b)
	b.i32(0)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(1), I32Locals: 2, Body: b}
}

func (e *expressionEmitter) computedNotifyFunction() wasmgen.Function {
	var b instructions
	b.statusGuard()
	b.i32(computedMetaBase + computedWorking)
	b.get(0)
	b.i32(computedMetaBytes)
	b.op(0x6c)
	b.op(0x6a)
	b.set(1)
	b.get(1)
	b.get(1)
	b.memory(0x28, 2, 0)
	b.i32(computedDirty)
	b.op(0x72)
	b.memory(0x36, 2, 0)
	b.get(0)
	b.i32(int32(e.state.computedCount))
	b.op(0x6e)
	b.set(2)
	b.get(0)
	b.i32(int32(e.state.computedCount))
	b.op(0x70)
	b.set(3)
	// Only another computed is an internal subscriber. A cache with no
	// subscriber stays dirty until a read, matching Computed.onDepChange.
	b.op(0x02)
	b.op(0x40)
	b.op(0x03)
	b.op(0x40)
	b.get(4)
	b.i32(int32(e.state.computedCount))
	b.op(0x4f)
	b.index(0x0d, 1)
	b.i32(computedMetaBase + computedWorking)
	b.get(2)
	b.i32(int32(e.state.computedCount))
	b.op(0x6c)
	b.get(4)
	b.op(0x6a)
	b.i32(computedMetaBytes)
	b.op(0x6c)
	b.op(0x6a)
	b.memory(0x28, 2, 4)
	b.i32(1)
	b.get(3)
	b.i32(16)
	b.op(0x6a)
	b.op(0x74)
	b.op(0x71)
	b.op(0x04)
	b.op(0x40)
	b.i32(e.computed.instancesBase)
	b.get(2)
	b.i32(4)
	b.op(0x6c)
	b.op(0x6a)
	b.memory(0x28, 2, 0)
	b.set(5)
	for i, getter := range e.computed.getters {
		b.get(3)
		b.i32(int32(i))
		b.op(0x46)
		b.op(0x04)
		b.op(0x40)
		b.get(5)
		b.index(0x10, getter)
		b.op(0x1a)
		b.statusGuard()
		b.i32(0)
		b.op(0x0f)
		b.op(0x0b)
	}
	b.op(0x0b)
	b.get(4)
	b.i32(1)
	b.op(0x6a)
	b.set(4)
	b.index(0x0c, 0)
	b.op(0x0b)
	b.op(0x0b)
	b.i32(0)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(1), I32Locals: 5, Body: b}
}

func (e *expressionEmitter) computedFlushFunction() wasmgen.Function {
	var b instructions
	b.i32(0)
	b.index(0x24, e.computed.batch)
	b.index(0x23, e.computed.head)
	b.set(0)
	for _, global := range []uint32{e.computed.head, e.computed.tail} {
		b.i32(0)
		b.index(0x24, global)
	}
	b.get(0)
	b.index(0x10, e.computed.deliver)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(0), I32Locals: 1, Body: b}
}
