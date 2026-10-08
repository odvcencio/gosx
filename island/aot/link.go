package aot

import (
	"fmt"
	"strings"

	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

type linkedShared struct {
	id, root uint32
	name     string
	kind     ScalarKind
}

type linkedProgram struct {
	unit   Unit
	state  *stateLayout
	dom    *domLayout
	inputs []uint32 // Frame-major input IDs; event leaves have no arena root.
}

// All programs address the same frame banks. Only the program assigned to an
// active manifest instance owns that frame; catalog programs need no instances.
// Capacity is fixed at the profile maximum, independently of SSR route guards.
type linkedLayout struct {
	programs                                                 []linkedProgram
	shared                                                   []linkedShared
	tags                                                     []string
	localStride, inputStride, computedStride, baselineStride uint32
	inputBase, sharedBase, computedBase, baselineBase, roots uint32
	frameTable, sharedVersions, tablesEnd                    uint32
	expressions                                              uint32
}

func buildLinkedLayout(units []Unit, options Options) (*linkedLayout, error) {
	ordered, _, _, err := canonicalUnits(units, options)
	if err != nil {
		return nil, err
	}
	l := &linkedLayout{}
	shared := map[string]ScalarKind{}
	tags := map[string]bool{}
	instances := make([]uint32, options.Limits.Instances)
	for i := range instances {
		instances[i] = uint32(i)
	}
	for _, unit := range ordered {
		state, dom, err := buildDOMLayout(unit, instances)
		if err != nil {
			return nil, err
		}
		locals, inputs := uint32(0), uint32(0)
		for _, signal := range unit.Contract.Signals {
			if !strings.HasPrefix(signal.Name, "$") {
				locals++
				continue
			}
			if kind, exists := shared[signal.Name]; exists && kind != signal.Kind {
				return nil, fmt.Errorf("incompatible shared declarations")
			}
			shared[signal.Name] = signal.Kind
		}
		for _, input := range unit.Contract.Inputs {
			if input.Source == "prop" {
				inputs++
			}
		}
		for _, tag := range dom.bindings.Tags {
			tags[tag] = true
		}
		l.localStride = max(l.localStride, locals)
		l.inputStride = max(l.inputStride, inputs)
		l.computedStride = max(l.computedStride, state.computedCount)
		l.baselineStride = max(l.baselineStride, uint32(len(dom.fields)))
		l.expressions = max(l.expressions, uint32(len(unit.Program.Exprs)))
		l.programs = append(l.programs, linkedProgram{unit: unit, state: state, dom: dom})
	}
	if len(shared) > int(options.Limits.SharedNames) {
		return nil, fmt.Errorf("linked shared names exceed the profile")
	}
	sharedNames := map[string]bool{}
	for name := range shared {
		sharedNames[name] = true
	}
	for i, name := range sortedBindingNames(sharedNames) {
		l.shared = append(l.shared, linkedShared{id: uint32(i), name: name, kind: shared[name]})
	}
	l.tags = sortedBindingNames(tags)
	capacity := options.Limits.Instances
	l.inputBase = capacity * l.localStride
	l.sharedBase = l.inputBase + capacity*l.inputStride
	l.computedBase = l.sharedBase + uint32(len(l.shared))
	l.baselineBase = l.computedBase + capacity*l.computedStride
	l.roots = l.baselineBase + capacity*l.baselineStride
	if l.roots > options.Limits.Values || uint64(l.roots+l.expressions)*valueBytes > 65536 {
		return nil, fmt.Errorf("linked scalar roots exceed the profile")
	}
	l.frameTable = computedMetaBase + capacity*l.computedStride*computedMetaBytes
	l.sharedVersions = l.frameTable + capacity*16
	l.tablesEnd = l.sharedVersions + uint32(len(l.shared))*16
	if l.tablesEnd > 32768 {
		return nil, fmt.Errorf("linked tables exceed the fixed interval")
	}
	sharedIDs := map[string]uint32{}
	for i := range l.shared {
		l.shared[i].root = l.sharedBase + uint32(i)
		sharedIDs[l.shared[i].name] = uint32(i)
	}
	tagIDs := bindingNameIDs(l.tags)
	for index := range l.programs {
		p := &l.programs[index]
		p.state.rows = nil
		p.state.mutableRoots = l.computedBase
		p.state.roots = l.roots
		p.dom.rootBase = l.baselineBase
		for i := range p.dom.bindings.Bindings {
			b := &p.dom.bindings.Bindings[i]
			if b.Kind == program.NodeElement {
				b.TagID = tagIDs[p.unit.Contract.Bindings[b.ID].Tag]
			}
		}
		p.dom.bindings.Tags = append([]string{}, l.tags...)
		for frame := uint32(0); frame < capacity; frame++ {
			local, input := uint32(0), uint32(0)
			for _, signal := range p.unit.Program.Signals {
				root := frame*l.localStride + local
				if shared, exists := sharedIDs[signal.Name]; exists {
					root = l.sharedBase + shared
				} else {
					local++
				}
				p.state.rows = append(p.state.rows, root)
			}
			for _, leaf := range p.unit.Contract.Inputs {
				root := NoBindingName
				if leaf.Source == "prop" {
					root = l.inputBase + frame*l.inputStride + input
					input++
				}
				p.inputs = append(p.inputs, root)
			}
		}
		p.state.inputRows = append([]uint32{}, p.inputs...)
	}
	return l, nil
}

// Selector leaves load the normalized input directly. Compiler-only aggregate
// prefixes have no emitted function, allocation, or runtime lookup chain.
func (e *expressionEmitter) inputExpression(id program.ExprID) (wasmgen.Function, error) {
	if e.state == nil || !e.transactional {
		return wasmgen.Function{}, fmt.Errorf("input access requires a bound state layout")
	}
	var input *InputContract
	for i := range e.unit.Contract.Inputs {
		for _, expr := range e.unit.Contract.Inputs[i].Exprs {
			if expr == id {
				input = &e.unit.Contract.Inputs[i]
			}
		}
	}
	if input == nil {
		return wasmgen.Function{}, fmt.Errorf("input expression has no scalar leaf")
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
	if input.Source == "prop" {
		b.i32(e.state.inputDataBase + int32(input.ID)*4)
		b.get(3)
		b.i32(int32(len(e.unit.Contract.Inputs)) * 4)
		b.op(0x6c)
		b.op(0x6a)
		b.memory(0x28, 2, 0)
		b.i32(valueBytes)
		b.op(0x6c)
		b.index(0x23, arenaBaseGlobal)
		b.op(0x6a)
		b.set(2)
		e.inputKindGuard(&b, input, 2)
		b.copyRecord(id, 2)
		return wasmgen.Function{I32Locals: 3, Body: b}, nil
	}
	// Imports may overwrite IO, so a successful read owns its bytes before
	// returning to any expression that could perform a second input call.
	b.i32(32768)
	b.set(2)
	b.get(0)
	b.i32(int32(input.ID))
	b.get(2)
	b.i32(valueBytes + 4096)
	b.index(0x10, 0)
	b.set(4)
	b.get(4)
	b.i32(0)
	b.op(0x48)
	b.op(0x04)
	b.op(0x40)
	b.i32(0)
	b.get(4)
	b.op(0x6b)
	b.set(4)
	b.get(4)
	b.i32(10)
	b.op(0x4b)
	b.op(0x04)
	b.op(0x40)
	b.i32(statusBadInput)
	b.set(4)
	b.op(0x0b)
	b.get(4)
	b.index(0x24, errorGlobal)
	b.i32(0)
	b.op(0x0f)
	b.op(0x0b)
	b.get(4)
	b.i32(valueBytes)
	b.op(0x49)
	b.get(4)
	b.i32(valueBytes + 4096)
	b.op(0x4b)
	b.op(0x72)
	b.guard(statusBadInput)
	e.inputKindGuard(&b, input, 2)
	b.get(2)
	b.memory(0x28, 2, 0)
	b.set(3)
	b.typeBranch(3, program.TypeString)
	b.textFields(2, 5, 6)
	b.get(6)
	b.op(0x04)
	b.op(0x40)
	b.get(5)
	b.i32(32768 + valueBytes)
	b.op(0x47)
	b.guard(statusBadInput)
	b.get(5)
	b.get(6)
	b.index(0x10, e.inputUTF8)
	b.op(0x45)
	b.guard(statusBadInput)
	b.op(0x0b)
	b.op(0x05)
	b.typeBranch(3, program.TypeInt)
	b.numericShape(2, 0, false)
	b.get(2)
	b.memory(0x29, 3, 8)
	b.set(8)
	b.integerGuard(8)
	b.op(0x05)
	b.typeBranch(3, program.TypeBool)
	b.numericShape(2, 2, true)
	b.op(0x05)
	b.failure(statusBadInput)
	for range 3 {
		b.op(0x0b)
	}
	b.get(4)
	b.i32(valueBytes)
	b.get(6)
	b.op(0x6a)
	b.op(0x47)
	b.guard(statusBadInput)
	b.get(6)
	e.callHelper(&b, helperAllocate)
	b.set(7)
	b.errorGuard()
	b.get(7)
	b.get(5)
	b.get(6)
	e.callHelper(&b, helperCopy)
	b.op(0x1a)
	b.errorGuard()
	b.destination(id, 1)
	for offset := uint32(0); offset < valueBytes; offset += 8 {
		b.get(1)
		b.get(2)
		b.memory(0x29, 3, offset)
		b.memory(0x37, 3, offset)
	}
	b.get(3)
	b.i32(int32(program.TypeString))
	b.op(0x46)
	b.op(0x04)
	b.op(0x40)
	b.get(1)
	b.get(7)
	b.memory(0x36, 2, 16)
	b.op(0x0b)
	b.get(1)
	b.op(0x0b)
	return wasmgen.Function{I32Locals: 7, I64Locals: 1, Body: b}, nil
}

func (e *expressionEmitter) inputKindGuard(b *instructions, input *InputContract, record uint32) {
	typ := program.TypeInt
	if input.Kind == Bool {
		typ = program.TypeBool
	} else if input.Kind == String {
		typ = program.TypeString
	}
	b.get(record)
	b.memory(0x28, 2, 0)
	b.i32(int32(typ))
	b.op(0x47)
	if absent, _ := InputDefaultType(e.unit.Program, *input); absent == program.TypeAny {
		b.get(record)
		b.memory(0x28, 2, 0)
		b.i32(int32(program.TypeAny))
		b.op(0x47)
		b.op(0x71)
	}
	b.guard(statusBadInput)
}

// inputUTF8Function accepts the exact UTF-8 scalar encodings produced by Go
// JSON decoding. It excludes overlong forms, surrogates, and values above U+10FFFF.
// Its caller has already checked the complete byte interval with widened sums.
func inputUTF8Function() wasmgen.Function {
	var b instructions
	b.op(0x03)
	b.op(0x40)
	b.get(1)
	b.op(0x45)
	b.statusFailure(1)
	b.get(0)
	b.memory(0x2d, 0, 0)
	b.set(2)
	b.i32(1)
	b.set(3)
	for _, encoding := range []struct{ low, high, count int32 }{{0x80, 0xff, 0}, {0xc2, 0xdf, 2}, {0xe0, 0xef, 3}, {0xf0, 0xf4, 4}} {
		b.get(2)
		b.i32(encoding.low)
		b.op(0x4f)
		b.get(2)
		b.i32(encoding.high)
		b.op(0x4d)
		b.op(0x71)
		b.op(0x04)
		b.op(0x40)
		b.i32(encoding.count)
		b.set(3)
		b.op(0x0b)
	}
	b.get(3)
	b.op(0x45)
	b.get(3)
	b.get(1)
	b.op(0x4b)
	b.op(0x72)
	b.statusFailure(0)
	for offset := uint32(1); offset <= 3; offset++ {
		b.get(3)
		b.i32(int32(offset))
		b.op(0x4b)
		b.op(0x04)
		b.op(0x40)
		b.get(0)
		b.memory(0x2d, 0, offset)
		b.set(4)
		b.get(4)
		b.i32(0x80)
		b.op(0x49)
		b.get(4)
		b.i32(0xbf)
		b.op(0x4b)
		b.op(0x72)
		b.statusFailure(0)
		if offset == 1 {
			for _, edge := range []struct {
				lead, bound int32
				op          byte
			}{{0xe0, 0xa0, 0x49}, {0xed, 0x9f, 0x4b}, {0xf0, 0x90, 0x49}, {0xf4, 0x8f, 0x4b}} {
				b.get(2)
				b.i32(edge.lead)
				b.op(0x46)
				b.get(4)
				b.i32(edge.bound)
				b.op(edge.op)
				b.op(0x71)
				b.statusFailure(0)
			}
		}
		b.op(0x0b)
	}
	b.get(0)
	b.get(3)
	b.op(0x6a)
	b.set(0)
	b.get(1)
	b.get(3)
	b.op(0x6b)
	b.set(1)
	b.index(0x0c, 0)
	b.op(0x0b)
	b.i32(1)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(2), I32Locals: 3, Body: b}
}
