package aot

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

type stateLayout struct {
	instances     []uint32
	signals       map[string]uint32
	rows          []uint32
	roots         uint32
	dataBase      int32
	lookup        uint32
	mutableRoots  uint32
	computedCount uint32
}

func emitStateExpressions(u Unit, instances []uint32) (*expressionEmitter, error) {
	state, err := buildStateLayout(u, instances)
	if err != nil {
		return nil, err
	}
	return emitConfiguredModule(u, state.roots, true, state)
}

// Local roots are distinct per numeric instance. Shared roots are keyed by
// the complete, byte-sorted dollar-prefixed name; no prefix is removed.
func buildStateLayout(u Unit, instances []uint32) (*stateLayout, error) {
	if len(instances) == 0 || len(instances) > int(ProfileLimits().Instances) {
		return nil, fmt.Errorf("instance layout exceeds the profile")
	}
	s := &stateLayout{instances: append([]uint32(nil), instances...), signals: map[string]uint32{}}
	sort.Slice(s.instances, func(i, j int) bool { return s.instances[i] < s.instances[j] })
	for i := 1; i < len(s.instances); i++ {
		if s.instances[i] == s.instances[i-1] {
			return nil, fmt.Errorf("duplicate instance identity")
		}
	}
	locals := map[string]uint32{}
	shared := []string{}
	for i, signal := range u.Program.Signals {
		s.signals[signal.Name] = uint32(i)
		if strings.HasPrefix(signal.Name, "$") {
			shared = append(shared, signal.Name)
		} else {
			locals[signal.Name] = uint32(len(locals))
		}
	}
	sort.Strings(shared)
	sharedSlots := map[string]uint32{}
	for i, name := range shared {
		sharedSlots[name] = uint32(len(s.instances)*len(locals) + i)
	}
	s.roots = uint32(len(s.instances)*len(locals) + len(shared))
	s.mutableRoots = s.roots
	s.computedCount = uint32(len(u.Program.Computeds))
	s.roots += uint32(len(s.instances)) * s.computedCount
	if s.roots > ProfileLimits().Values || len(shared) > int(ProfileLimits().SharedNames) {
		return nil, fmt.Errorf("state roots exceed the profile")
	}
	for i := range s.instances {
		for _, signal := range u.Program.Signals {
			root, ok := sharedSlots[signal.Name]
			if !ok {
				root = uint32(i*len(locals)) + locals[signal.Name]
			}
			s.rows = append(s.rows, root)
		}
	}
	return s, nil
}

func (e *expressionEmitter) setupState() error {
	for len(e.module.Data)%4 != 0 {
		e.module.Data = append(e.module.Data, 0)
	}
	e.state.dataBase = int32(wasmgen.ConstantOffset + len(e.module.Data))
	for _, root := range e.state.rows {
		e.module.Data = binary.LittleEndian.AppendUint32(e.module.Data, root)
	}
	if len(e.module.Data) > wasmgen.MaxDataBytes {
		return fmt.Errorf("state tables exceed the constant segment")
	}
	e.state.lookup = uint32(len(e.module.Imports) + len(e.module.Functions))
	var b instructions
	b.errorGuard()
	for i, id := range e.state.instances {
		b.get(0)
		b.i32(int32(id))
		b.op(0x46)
		b.op(0x04)
		b.op(0x40)
		b.i32(int32(i))
		b.op(0x0f)
		b.op(0x0b)
	}
	b.failure(statusBadInput)
	b.op(0x0b)
	e.module.Functions = append(e.module.Functions, wasmgen.Function{Signature: i32Signature(1), Body: b})
	return nil
}

func (e *expressionEmitter) signalRoot(b *instructions, signal, local uint32) {
	b.get(0)
	b.index(0x10, e.state.lookup)
	b.set(local)
	b.errorGuard()
	b.i32(e.state.dataBase + int32(signal)*4)
	b.get(local)
	b.i32(int32(len(e.unit.Program.Signals)) * 4)
	b.op(0x6c)
	b.op(0x6a)
	b.memory(0x28, 2, 0)
	b.set(local)
}

func (e *expressionEmitter) stateExpression(id program.ExprID, expr program.Expr) (wasmgen.Function, error) {
	if e.state == nil {
		return wasmgen.Function{}, fmt.Errorf("signal access requires a bound state layout")
	}
	slot, ok := e.state.signals[expr.Value]
	if !ok {
		if expr.Op == program.OpSignalGet && e.computed != nil {
			return e.computedExpression(id, expr)
		}
		return wasmgen.Function{}, fmt.Errorf("signal name has no mutable root")
	}
	var b instructions
	b.errorGuard()
	b.index(0x23, pendingGlobal)
	b.op(0x45)
	b.guard(statusBusy)
	e.signalRoot(&b, slot, 3)
	if expr.Op == program.OpSignalGet {
		if e.computed != nil {
			e.trackDependency(&b, slot)
		}
		b.index(0x23, arenaBaseGlobal)
		b.get(3)
		b.i32(valueBytes)
		b.op(0x6c)
		b.op(0x6a)
		b.set(2)
		b.copyRecord(id, 2)
	} else {
		e.value(&b, expr.Operands[0], 2)
		expected := program.TypeInt
		switch e.unit.Contract.Signals[slot].Kind {
		case Bool:
			expected = program.TypeBool
		case String:
			expected = program.TypeString
		}
		b.get(2)
		b.memory(0x28, 2, 0)
		b.i32(int32(expected))
		b.op(0x47)
		b.guard(statusBadInput)
		b.get(3)
		b.get(2)
		b.index(0x10, e.transactions[transactionStore])
		b.op(0x1a)
		b.errorGuard()
		if e.computed != nil {
			b.get(0)
			b.i32(int32(slot))
			shared := int32(0)
			if strings.HasPrefix(expr.Value, "$") {
				shared = 1
			}
			b.i32(shared)
			b.index(0x10, e.computed.publish)
			b.op(0x1a)
			b.errorGuard()
		}
		b.record(id, program.TypeAny, i64Instructions(0), i32Instructions(0))
	}
	return wasmgen.Function{I32Locals: 3, Body: b}, nil
}
