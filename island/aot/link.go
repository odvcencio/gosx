package aot

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"reflect"
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
	data                                                     []byte
	strings                                                  map[string]stringConstant
	instancesBase                                            int32
	sharedMasksBase                                          int32
	checkpointPlanBase, digestBase                           int32
	inputSetSHA                                              [32]byte
	programs                                                 []linkedProgram
	shared                                                   []linkedShared
	tags                                                     []string
	localStride, inputStride, computedStride, baselineStride uint32
	inputBase, sharedBase, computedBase, baselineBase, roots uint32
	frameTable, frameSequences, sharedVersions, tablesEnd    uint32
	expressions                                              uint32
}

func buildLinkedLayout(units []Unit, options Options) (*linkedLayout, error) {
	ordered, _, digest, err := canonicalUnits(units, options)
	if err != nil {
		return nil, err
	}
	l := &linkedLayout{inputSetSHA: digest}
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
	l.frameSequences = l.frameTable + capacity*16
	l.sharedVersions = l.frameSequences + capacity*8
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
		p.state.linked, p.state.programID = l, uint32(index)
		p.state.computedStride = l.computedStride
		p.dom.frameStride = l.baselineStride
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
	if err := l.encodeData(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *linkedLayout) encodeData() error {
	values := map[string]bool{}
	for _, p := range l.programs {
		e := &expressionEmitter{unit: p.unit, dom: p.dom, transactional: true}
		strings, _, _ := e.stringValues()
		for value := range strings {
			values[value] = true
		}
	}
	l.strings = map[string]stringConstant{}
	for _, value := range sortedBindingNames(values) {
		constant := stringConstant{length: int32(len(value))}
		if value != "" {
			if len(l.data)+len(value) > wasmgen.MaxDataBytes {
				return fmt.Errorf("linked constants exceed the fixed interval")
			}
			constant.pointer = int32(wasmgen.ConstantOffset + len(l.data))
			l.data = append(l.data, value...)
		}
		l.strings[value] = constant
	}
	for len(l.data)%4 != 0 {
		l.data = append(l.data, 0)
	}
	tables := map[string]int32{}
	table := func(rows []uint32) int32 {
		var data []byte
		for _, row := range rows {
			data = binary.LittleEndian.AppendUint32(data, row)
		}
		if pointer, exists := tables[string(data)]; exists {
			return pointer
		}
		pointer := int32(wasmgen.ConstantOffset + len(l.data))
		tables[string(data)] = pointer
		l.data = append(l.data, data...)
		return pointer
	}
	for i := range l.programs {
		p := &l.programs[i]
		p.state.dataBase = table(p.state.rows)
		p.state.inputDataBase = table(p.inputs)
	}
	var instances []uint32
	for i := uint32(0); i < ProfileLimits().Instances; i++ {
		instances = append(instances, i)
	}
	l.instancesBase = table(instances)
	var masks []uint32
	for _, p := range l.programs {
		for _, shared := range l.shared {
			mask := uint32(0)
			for slot, signal := range p.unit.Program.Signals {
				if signal.Name == shared.name {
					mask = 1 << slot
				}
			}
			masks = append(masks, mask)
		}
	}
	l.sharedMasksBase = table(masks)
	var plans []uint32
	for _, p := range l.programs {
		var entries []uint32
		local, input := uint32(0), uint32(0)
		for slot, signal := range p.unit.Contract.Signals {
			if strings.HasPrefix(signal.Name, "$") {
				continue
			}
			tag, err := checkpointWireType(signal.Kind)
			if err != nil {
				return err
			}
			entries = append(entries, uint32(slot), local, l.localStride, uint32(tag), 0)
			local++
		}
		for _, leaf := range p.unit.Contract.Inputs {
			if leaf.Source != "prop" {
				continue
			}
			tag, err := checkpointWireType(leaf.Kind)
			if err != nil {
				return err
			}
			anyZero := uint32(0)
			if len(leaf.Path) != 0 {
				anyZero = 1
			}
			entries = append(entries, leaf.ID, l.inputBase+input, l.inputStride, uint32(tag), anyZero)
			input++
		}
		plans = append(plans, local, input, uint32(table(entries)))
	}
	l.checkpointPlanBase = table(plans)
	l.digestBase = int32(wasmgen.ConstantOffset + len(l.data))
	l.data = append(l.data, l.inputSetSHA[:]...)
	if len(l.data) > wasmgen.MaxDataBytes {
		return fmt.Errorf("linked constants exceed the fixed interval")
	}
	return nil
}

func (l *linkedLayout) emitProgram(index uint32) (*expressionEmitter, error) {
	if uint64(index) >= uint64(len(l.programs)) {
		return nil, fmt.Errorf("linked program ID out of range")
	}
	p := &l.programs[index]
	return emitConfiguredModule(p.unit, l.roots, true, p.state, p.dom)
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

type linkedCode struct {
	module                             wasmgen.Module
	programs                           []*expressionEmitter
	indices                            [][]uint32
	notify, publish, scalar, dispose   uint32
	dispatch, initialize, bind, render uint32
	checkpointValue                    uint32
}

const linkedRenderMaskGlobal = 25

func commonFunctions(e *expressionEmitter) []uint32 {
	indices := append([]uint32{}, e.helpers[:]...)
	indices = append(indices, e.inputUTF8, e.rootCopy)
	indices = append(indices, e.transactions[:]...)
	return append(indices, e.computed.begin, e.computed.commit, e.computed.abort, e.dom.patch,
		e.computed.snapshot, e.computed.deliver, e.computed.flush)
}

// linkProgramCode combines already proved programs without decoding source at
// runtime. Helpers have stable IDs; each program retains ExprID/handler order.
func linkProgramCode(l *linkedLayout) (*linkedCode, error) {
	if l == nil || len(l.programs) == 0 {
		return nil, fmt.Errorf("empty linked layout")
	}
	c := &linkedCode{}
	for i := range l.programs {
		e, err := l.emitProgram(uint32(i))
		if err != nil {
			return nil, err
		}
		c.programs = append(c.programs, e)
	}
	first := c.programs[0].module
	c.module = wasmgen.Module{Imports: append([]wasmgen.Import{}, first.Imports...), Globals: append([]wasmgen.Global{}, first.Globals...), Data: append([]byte{}, l.data...)}
	c.module.Globals = append(c.module.Globals, wasmgen.Global{Mutable: true})
	for i := range c.module.Imports {
		c.module.Imports[i].Signature.Params = append([]wasmgen.ValueType{}, first.Imports[i].Signature.Params...)
	}
	common := commonFunctions(c.programs[0])
	c.notify = uint32(len(first.Imports) + len(common))
	c.publish = c.notify + 1
	c.scalar = c.notify + 2
	c.dispose = c.notify + 3
	c.dispatch = c.notify + 4
	c.initialize = c.notify + 5
	c.bind = c.notify + 6
	c.render = c.notify + 7
	c.checkpointValue = c.notify + 8
	c.module.Functions = make([]wasmgen.Function, len(common)+9)
	for _, e := range c.programs {
		if !reflect.DeepEqual(first.Imports, e.module.Imports) || !reflect.DeepEqual(first.Globals, e.module.Globals) || !bytes.Equal(first.Data, e.module.Data) {
			return nil, fmt.Errorf("incompatible linked module storage or imports")
		}
		indices := make([]uint32, len(e.module.Imports)+len(e.module.Functions))
		for i := range indices {
			indices[i] = NoBindingName
		}
		for i := range e.module.Imports {
			indices[i] = uint32(i)
		}
		for id, index := range commonFunctions(e) {
			indices[index] = uint32(len(first.Imports) + id)
		}
		c.indices = append(c.indices, indices)
	}
	type functionSource struct{ program, index uint32 }
	var sources []functionSource
	for p, e := range c.programs {
		indices := c.indices[p]
		add := func(index uint32) {
			if index == NoBindingName || indices[index] != NoBindingName {
				return
			}
			indices[index] = uint32(len(first.Imports) + len(common) + 9 + len(sources))
			sources = append(sources, functionSource{uint32(p), index})
		}
		for _, index := range e.functions {
			add(index)
		}
		for _, index := range e.handlers {
			add(index)
		}
		for i := range e.module.Functions {
			add(uint32(len(first.Imports) + i))
		}
	}
	globals := make([]uint32, len(c.module.Globals))
	for i := range globals {
		globals[i] = uint32(i)
	}
	for helper := range common {
		for p, e := range c.programs {
			index := commonFunctions(e)[helper]
			fn := e.module.Functions[index-uint32(len(first.Imports))]
			mapping := c.indices[p]
			switch index {
			case e.computed.begin, e.computed.commit, e.computed.abort:
				fn = l.sharedBoundaryFunction(e, index)
			case e.computed.snapshot:
				fn = l.snapshotFunction(e)
			case e.computed.deliver:
				mapping = append([]uint32{}, mapping...)
				mapping[e.computed.notify] = c.notify
			}
			fn, err := relocateFunction(fn, mapping, globals)
			if err != nil {
				return nil, err
			}
			if p == 0 {
				c.module.Functions[helper] = fn
			} else if !reflect.DeepEqual(c.module.Functions[helper], fn) {
				return nil, fmt.Errorf("incompatible linked helper %d", helper)
			}
		}
	}
	c.module.Functions[len(common)] = l.notifyFunction(c)
	c.module.Functions[len(common)+1] = l.publishFunction(c)
	c.module.Functions[len(common)+2] = scalarValidationFunction(c.indices[0][c.programs[0].inputUTF8])
	c.module.Functions[len(common)+3] = l.disposeFunction(c)
	c.module.Functions[len(common)+4] = l.dispatchFunction(c)
	c.module.Functions[len(common)+5] = l.frameOperationFunction(c, "initialize")
	c.module.Functions[len(common)+6] = l.frameOperationFunction(c, "bind")
	c.module.Functions[len(common)+7] = l.frameOperationFunction(c, "render")
	c.module.Functions[len(common)+8] = checkpointValueFunction(c)
	for _, source := range sources {
		e := c.programs[source.program]
		fn := e.module.Functions[source.index-uint32(len(first.Imports))]
		mapping := c.indices[source.program]
		if source.index == e.computed.publish {
			fn = l.programPublishFunction(e, uint32(len(mapping)))
			mapping = append(append([]uint32{}, mapping...), c.publish)
		}
		fn, err := relocateFunction(fn, mapping, globals)
		if err != nil {
			return nil, err
		}
		c.module.Functions = append(c.module.Functions, fn)
	}
	raw, err := wasmgen.Encode(c.module)
	if err == nil {
		err = wasmgen.Validate(raw)
	}
	if err != nil {
		return nil, fmt.Errorf("linked code: %w", err)
	}
	return c, nil
}

type relocationReader struct {
	data []byte
	pos  int
}

func (r *relocationReader) leb(bits int) (uint64, error) {
	start, value := r.pos, uint64(0)
	limit := 5
	if bits == 64 {
		limit = 10
	}
	for i := range limit {
		if r.pos == len(r.data) {
			return 0, fmt.Errorf("truncated instruction immediate")
		}
		b := r.data[r.pos]
		r.pos++
		value |= uint64(b&127) << (i * 7)
		if b&128 != 0 {
			continue
		}
		var canonical []byte
		if bits == 0 {
			if value > uint64(^uint32(0)) {
				return 0, fmt.Errorf("instruction index overflow")
			}
			canonical = wasmgen.AppendU32(nil, uint32(value))
		} else {
			if b&64 != 0 && (i+1)*7 < 64 {
				value |= ^uint64(0) << ((i + 1) * 7)
			}
			if bits == 32 {
				value = uint64(int64(int32(value)))
			}
			canonical = wasmgen.AppendI64(nil, int64(value))
		}
		if !bytes.Equal(canonical, r.data[start:r.pos]) {
			return 0, fmt.Errorf("noncanonical instruction immediate")
		}
		return value, nil
	}
	return 0, fmt.Errorf("instruction immediate overflow")
}

// Relocation walks instruction framing, never matching arbitrary byte values.
// Constants, branch/local indices and memory offsets retain their exact bytes.
func relocateFunction(fn wasmgen.Function, functions, globals []uint32) (wasmgen.Function, error) {
	r := relocationReader{data: fn.Body}
	var body []byte
	var lastOp byte
	for r.pos < len(r.data) {
		start := r.pos
		op := r.data[r.pos]
		lastOp = op
		r.pos++
		unsigned := func() (uint64, error) { return r.leb(0) }
		var err error
		switch {
		case op == 0x10 || op == 0x23 || op == 0x24:
			index, readErr := unsigned()
			if readErr != nil {
				return wasmgen.Function{}, readErr
			}
			mapping := functions
			if op != 0x10 {
				mapping = globals
			}
			if index >= uint64(len(mapping)) || mapping[index] == NoBindingName {
				return wasmgen.Function{}, fmt.Errorf("unresolved instruction index")
			}
			body = wasmgen.AppendU32(append(body, op), mapping[index])
			continue
		case op >= 0x02 && op <= 0x04:
			if r.pos == len(r.data) || r.data[r.pos] != 0x40 && r.data[r.pos] != byte(wasmgen.I32) && r.data[r.pos] != byte(wasmgen.I64) {
				return wasmgen.Function{}, fmt.Errorf("unsupported block type")
			}
			r.pos++
		case op == 0x0c || op == 0x0d || op >= 0x20 && op <= 0x22 || op == 0x3f:
			_, err = unsigned()
		case op == 0x0e:
			var count uint64
			count, err = unsigned()
			if count > uint64(len(r.data)-r.pos) {
				return wasmgen.Function{}, fmt.Errorf("branch table limit")
			}
			for i := uint64(0); i <= count && err == nil; i++ {
				_, err = unsigned()
			}
		case op == 0x28 || op == 0x29 || op >= 0x2c && op <= 0x37 || op >= 0x3a && op <= 0x3e:
			_, err = unsigned()
			if err == nil {
				_, err = unsigned()
			}
		case op == 0x41:
			_, err = r.leb(32)
		case op == 0x42:
			_, err = r.leb(64)
		case op == 0x00 || op == 0x01 || op == 0x05 || op == 0x0b || op == 0x0f || op == 0x1a || op == 0x1b || op >= 0x45 && op <= 0x5a || op >= 0x67 && op <= 0x8a || op == 0xa7 || op == 0xac || op == 0xad:
		default:
			return wasmgen.Function{}, fmt.Errorf("unsupported relocation instruction")
		}
		if err != nil {
			return wasmgen.Function{}, err
		}
		body = append(body, r.data[start:r.pos]...)
	}
	if len(body) == 0 || lastOp != 0x0b {
		return wasmgen.Function{}, fmt.Errorf("unterminated relocated function")
	}
	fn.Body = body
	fn.Signature.Params = append([]wasmgen.ValueType{}, fn.Signature.Params...)
	return fn, nil
}

func (l *linkedLayout) snapshotFunction(e *expressionEmitter) wasmgen.Function {
	var b instructions
	if l.computedStride == 0 {
		b.i32(0)
		b.op(0x0b)
		return wasmgen.Function{Signature: i32Signature(3), Body: b}
	}
	b.errorGuard()
	b.op(0x02)
	b.op(0x40)
	b.op(0x03)
	b.op(0x40)
	b.i32(-1)
	b.set(4)
	b.i64(-1)
	b.set(13)
	b.i32(0)
	b.set(3)
	b.op(0x02)
	b.op(0x40)
	b.op(0x03)
	b.op(0x40)
	b.get(3)
	b.i32(int32(len(e.state.instances)) * int32(e.state.computedStride))
	b.op(0x4f)
	b.index(0x0d, 1)
	b.i32(computedMetaBase + computedWorking)
	b.get(3)
	b.i32(computedMetaBytes)
	b.op(0x6c)
	b.op(0x6a)
	b.set(8)
	b.get(3)
	b.i32(int32(l.computedStride))
	b.op(0x6e)
	b.set(10)
	b.i32(int32(l.frameTable))
	b.get(10)
	b.i32(16)
	b.op(0x6c)
	b.op(0x6a)
	b.memory(0x28, 2, 0)
	b.set(11)
	b.get(2)
	b.i32(-1)
	b.op(0x46)
	b.op(0x04)
	b.op(byte(wasmgen.I32))
	b.i32(1)
	b.get(1)
	b.op(0x74)
	b.set(9)
	b.get(10)
	b.get(0)
	b.op(0x46)
	b.op(0x05)
	b.get(11)
	b.i32(int32(len(l.programs)))
	b.op(0x4f)
	b.get(2)
	b.i32(int32(len(l.shared)))
	b.op(0x4f)
	b.op(0x72)
	b.op(0x04)
	b.op(byte(wasmgen.I32))
	b.i32(0)
	b.op(0x05)
	b.i32(l.sharedMasksBase)
	b.get(11)
	b.i32(int32(len(l.shared)))
	b.op(0x6c)
	b.get(2)
	b.op(0x6a)
	b.i32(4)
	b.op(0x6c)
	b.op(0x6a)
	b.memory(0x28, 2, 0)
	b.op(0x0b)
	b.index(0x22, 9)
	b.op(0x45)
	b.op(0x45)
	b.op(0x0b)
	b.i32(int32(l.frameTable))
	b.get(10)
	b.i32(16)
	b.op(0x6c)
	b.op(0x6a)
	b.memory(0x28, 2, 4)
	b.i32(1)
	b.op(0x46)
	b.op(0x71)
	b.get(8)
	b.memory(0x28, 2, 0)
	b.i32(computedCreated)
	b.op(0x71)
	b.op(0x71)
	b.get(8)
	b.memory(0x28, 2, 4)
	b.get(9)
	b.op(0x71)
	b.op(0x45)
	b.op(0x45)
	b.op(0x71)
	b.op(0x04)
	b.op(0x40)
	b.get(8)
	b.memory(0x29, 3, 8)
	b.index(0x22, 14)
	b.get(12)
	b.op(0x56)
	b.get(4)
	b.i32(-1)
	b.op(0x46)
	b.get(14)
	b.get(13)
	b.op(0x54)
	b.op(0x72)
	b.op(0x71)
	b.op(0x04)
	b.op(0x40)
	b.get(3)
	b.set(4)
	b.get(14)
	b.set(13)
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
	b.get(13)
	b.set(12)
	b.index(0x0c, 0)
	b.op(0x0b)
	b.op(0x0b)
	b.get(6)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(3), I32Locals: 9, I64Locals: 3, Body: b}
}

// Versions share the same generation boundary as scalar roots and caches.
func (l *linkedLayout) sharedBoundaryFunction(e *expressionEmitter, index uint32) wasmgen.Function {
	fn := e.module.Functions[index-uint32(len(e.module.Imports))]
	var b instructions
	if index == e.computed.begin || index == e.computed.commit {
		from, to := uint32(0), uint32(8)
		if index == e.computed.commit {
			from, to = to, from
		}
		for i := range l.shared {
			base := int32(l.sharedVersions) + int32(i)*16
			b.i32(base)
			b.i32(base)
			b.memory(0x29, 3, from)
			b.memory(0x37, 3, to)
		}
		for frame := uint32(0); frame < ProfileLimits().Instances; frame++ {
			committed := int32(l.frameTable + frame*16 + 8)
			working := int32(l.frameSequences + frame*8)
			if index == e.computed.commit {
				b.i32(committed)
				b.i32(working)
			} else {
				b.i32(working)
				b.i32(committed)
			}
			b.memory(0x29, 3, 0)
			b.memory(0x37, 3, 0)
		}
	}
	if index != e.computed.commit {
		b.i32(0)
		b.index(0x24, linkedRenderMaskGlobal)
	}
	fn.Body = append(b, fn.Body...)
	return fn
}

func (b *instructions) poisonStatus(status int32) {
	b.op(0x04)
	b.op(0x40)
	b.i32(status)
	b.index(0x24, errorGlobal)
	b.i32(status)
	b.op(0x0f)
	b.op(0x0b)
}

func (l *linkedLayout) notifyFunction(c *linkedCode) wasmgen.Function {
	var b instructions
	b.statusGuard()
	if l.computedStride != 0 {
		b.get(0)
		b.i32(int32(ProfileLimits().Instances * l.computedStride))
		b.op(0x4f)
		b.poisonStatus(statusBadInput)
		b.get(0)
		b.i32(int32(l.computedStride))
		b.op(0x6e)
		b.set(1)
		b.i32(int32(l.frameTable))
		b.get(1)
		b.i32(16)
		b.op(0x6c)
		b.op(0x6a)
		b.index(0x22, 2)
		b.memory(0x28, 2, 4)
		b.i32(1)
		b.op(0x47)
		b.poisonStatus(statusBadInput)
		b.get(2)
		b.memory(0x28, 2, 0)
		b.set(2)
		for id, e := range c.programs {
			b.get(2)
			b.i32(int32(id))
			b.op(0x46)
			b.op(0x04)
			b.op(0x40)
			b.get(0)
			b.i32(int32(l.computedStride))
			b.op(0x70)
			b.i32(int32(e.state.computedCount))
			b.op(0x4f)
			b.poisonStatus(statusBadInput)
			b.get(0)
			b.index(0x10, c.indices[id][e.computed.notify])
			b.op(0x0f)
			b.op(0x0b)
		}
	}
	b.i32(statusBadInput)
	b.index(0x24, errorGlobal)
	b.i32(statusBadInput)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(1), I32Locals: 2, Body: b}
}

// The private publisher translates a source SignalID to the exact page-wide
// shared ID. Computed and local dependency bits keep their program meaning.
func (l *linkedLayout) programPublishFunction(e *expressionEmitter, common uint32) wasmgen.Function {
	var b instructions
	b.statusGuard()
	b.get(0)
	b.index(0x10, e.state.lookup)
	b.set(3)
	b.statusGuard()
	b.i32(-1)
	b.set(4)
	b.get(2)
	b.op(0x04)
	b.op(0x40)
	for slot, signal := range e.unit.Program.Signals {
		for _, shared := range l.shared {
			if signal.Name != shared.name {
				continue
			}
			b.get(1)
			b.i32(int32(slot))
			b.op(0x46)
			b.op(0x04)
			b.op(0x40)
			b.i32(int32(shared.id))
			b.set(4)
			b.op(0x0b)
		}
	}
	b.get(4)
	b.i32(-1)
	b.op(0x46)
	b.poisonStatus(statusBadInput)
	b.op(0x0b)
	b.get(3)
	b.get(1)
	b.get(4)
	b.index(0x10, common)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(3), I32Locals: 2, Body: b}
}

func (l *linkedLayout) publishFunction(c *linkedCode) wasmgen.Function {
	var b instructions
	b.statusGuard()
	b.index(0x23, pendingGlobal)
	b.op(0x45)
	b.statusFailure(statusBusy)
	b.get(0)
	b.i32(int32(ProfileLimits().Instances))
	b.op(0x4f)
	b.poisonStatus(statusBadInput)
	b.get(1)
	b.i32(32)
	b.op(0x4f)
	b.poisonStatus(statusBadInput)
	b.index(0x23, linkedRenderMaskGlobal)
	b.i32(1)
	b.get(0)
	b.op(0x74)
	b.op(0x72)
	b.index(0x24, linkedRenderMaskGlobal)
	b.get(2)
	b.i32(-1)
	b.op(0x47)
	b.op(0x04)
	b.op(0x40)
	b.get(2)
	b.i32(int32(len(l.shared)))
	b.op(0x4f)
	b.poisonStatus(statusBadInput)
	for i, global := range []uint32{pendingLoGlobal, pendingHiGlobal} {
		b.i32(int32(l.sharedVersions))
		b.get(2)
		b.i32(16)
		b.op(0x6c)
		b.op(0x6a)
		b.index(0x23, global)
		b.memory(0x36, 2, uint32(8+i*4))
	}
	for frame := uint32(0); frame < ProfileLimits().Instances; frame++ {
		base := int32(l.frameTable + frame*16)
		b.i32(base)
		b.memory(0x28, 2, 0)
		b.set(3)
		b.i32(base)
		b.memory(0x28, 2, 4)
		b.i32(1)
		b.op(0x46)
		b.get(3)
		b.i32(int32(len(l.programs)))
		b.op(0x49)
		b.op(0x71)
		b.op(0x04)
		b.op(0x40)
		b.i32(l.sharedMasksBase)
		b.get(3)
		b.i32(int32(len(l.shared)))
		b.op(0x6c)
		b.get(2)
		b.op(0x6a)
		b.i32(4)
		b.op(0x6c)
		b.op(0x6a)
		b.memory(0x28, 2, 0)
		b.op(0x04)
		b.op(0x40)
		b.index(0x23, linkedRenderMaskGlobal)
		b.i32(int32(uint32(1) << frame))
		b.op(0x72)
		b.index(0x24, linkedRenderMaskGlobal)
		b.op(0x0b)
		b.op(0x0b)
	}
	b.op(0x0b)
	b.get(0)
	b.get(1)
	b.get(2)
	b.index(0x10, c.indices[0][c.programs[0].computed.snapshot])
	b.set(4)
	b.statusGuard()
	b.get(4)
	b.index(0x10, c.indices[0][c.programs[0].computed.deliver])
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(3), I32Locals: 2, Body: b}
}

// Validate a scalar inside one bounded interval without allocating, importing
// or changing transaction state. Callers choose their error-publication policy.
func scalarValidationFunction(utf8 uint32) wasmgen.Function {
	var b instructions
	b.get(1)
	b.get(2)
	b.op(0x4b)
	b.get(2)
	b.i32(196608)
	b.op(0x4b)
	b.op(0x72)
	b.get(0)
	b.get(1)
	b.op(0x49)
	b.op(0x72)
	b.statusFailure(statusBadInput)
	b.get(0)
	b.op(0xad)
	b.i64(valueBytes)
	b.op(0x7c)
	b.get(2)
	b.op(0xad)
	b.op(0x56)
	b.statusFailure(statusBadInput)
	for i, offset := range []uint32{0, 4, 16, 20} {
		b.get(0)
		b.memory(0x28, 2, offset)
		b.set(uint32(3 + i))
	}
	b.get(0)
	b.memory(0x29, 3, 8)
	b.set(7)
	b.get(3)
	b.i32(0)
	b.op(0x46)
	b.op(0x04)
	b.op(0x40)
	b.get(4)
	b.i32(1)
	b.op(0x4b)
	b.get(7)
	b.i64(0)
	b.op(0x52)
	b.op(0x72)
	b.statusFailure(statusBadInput)
	b.get(6)
	b.i32(4096)
	b.op(0x4b)
	b.statusFailure(statusStringLimit)
	b.get(6)
	b.op(0x45)
	b.op(0x04)
	b.op(0x40)
	b.get(5)
	b.statusFailure(statusBadInput)
	b.op(0x05)
	b.get(4)
	b.op(0x45)
	b.get(5)
	b.get(1)
	b.op(0x49)
	b.op(0x72)
	b.statusFailure(statusBadInput)
	b.get(5)
	b.op(0xad)
	b.get(6)
	b.op(0xad)
	b.op(0x7c)
	b.get(2)
	b.op(0xad)
	b.op(0x56)
	b.statusFailure(statusBadInput)
	b.get(5)
	b.get(6)
	b.index(0x10, utf8)
	b.op(0x45)
	b.statusFailure(statusBadInput)
	b.op(0x0b)
	b.i32(0)
	b.op(0x0f)
	b.op(0x0b)
	b.get(5)
	b.get(6)
	b.op(0x72)
	b.statusFailure(statusBadInput)
	b.get(3)
	b.i32(1)
	b.op(0x46)
	b.op(0x04)
	b.op(0x40)
	b.get(4)
	b.statusFailure(statusBadInput)
	b.get(7)
	b.i64(-2147483648)
	b.op(0x53)
	b.get(7)
	b.i64(2147483647)
	b.op(0x55)
	b.op(0x72)
	b.statusFailure(statusIntegerDomain)
	b.i32(0)
	b.op(0x0f)
	b.op(0x0b)
	b.get(7)
	b.i64(0)
	b.op(0x52)
	b.statusFailure(statusBadInput)
	b.get(3)
	b.i32(3)
	b.op(0x46)
	b.op(0x04)
	b.op(0x40)
	b.get(4)
	b.i32(-3)
	b.op(0x71)
	b.statusFailure(statusBadInput)
	b.i32(0)
	b.op(0x0f)
	b.op(0x0b)
	b.get(3)
	b.i32(5)
	b.op(0x46)
	b.get(4)
	b.op(0x45)
	b.op(0x71)
	b.op(0x04)
	b.op(0x40)
	b.i32(0)
	b.op(0x0f)
	b.op(0x0b)
	b.i32(statusBadInput)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(3), I32Locals: 4, I64Locals: 1, Body: b}
}

// Disposal validates all owned roots before clearing any committed byte.
// The four frame banks exclude shared roots and preserve their versions.
func (l *linkedLayout) disposeFunction(c *linkedCode) wasmgen.Function {
	var b instructions
	b.index(0x23, pendingGlobal)
	b.statusFailure(statusBusy)
	b.get(0)
	b.i32(int32(ProfileLimits().Instances))
	b.op(0x4f)
	b.statusFailure(statusBadInput)
	b.i32(int32(l.frameTable))
	b.get(0)
	b.i32(16)
	b.op(0x6c)
	b.op(0x6a)
	b.index(0x22, 1)
	b.memory(0x28, 2, 4)
	b.op(0x45)
	b.statusFailure(0)
	b.get(1)
	b.memory(0x28, 2, 4)
	b.i32(1)
	b.op(0x47)
	b.get(1)
	b.memory(0x28, 2, 0)
	b.i32(int32(len(l.programs)))
	b.op(0x4f)
	b.op(0x72)
	b.statusFailure(statusBadInput)
	banks := [][2]uint32{{0, l.localStride}, {l.inputBase, l.inputStride}, {l.computedBase, l.computedStride}, {l.baselineBase, l.baselineStride}}
	visit := func(bank [2]uint32, clear bool) {
		if bank[1] == 0 {
			return
		}
		b.i32(0)
		b.set(2)
		b.op(0x02)
		b.op(0x40)
		b.op(0x03)
		b.op(0x40)
		b.get(2)
		b.i32(int32(bank[1]))
		b.op(0x4f)
		b.index(0x0d, 1)
		b.index(0x23, committedBaseGlobal)
		b.get(0)
		b.i32(int32(bank[1]))
		b.op(0x6c)
		b.i32(int32(bank[0]))
		b.op(0x6a)
		b.get(2)
		b.op(0x6a)
		b.i32(valueBytes)
		b.op(0x6c)
		b.op(0x6a)
		b.set(3)
		if clear {
			for offset := uint32(0); offset < valueBytes; offset += 8 {
				b.get(3)
				b.i64(0)
				b.memory(0x37, 3, offset)
			}
		} else {
			b.get(3)
			b.index(0x23, committedBaseGlobal)
			b.index(0x23, committedBaseGlobal)
			b.i32(65536)
			b.op(0x6a)
			b.index(0x10, c.scalar)
			b.index(0x22, 4)
			b.op(0x04)
			b.op(0x40)
			b.get(4)
			b.op(0x0f)
			b.op(0x0b)
			b.get(5)
			b.get(3)
			b.memory(0x28, 2, 20)
			b.op(0xad)
			b.op(0x7c)
			b.set(5)
		}
		b.get(2)
		b.i32(1)
		b.op(0x6a)
		b.set(2)
		b.index(0x0c, 0)
		b.op(0x0b)
		b.op(0x0b)
	}
	for _, bank := range banks {
		visit(bank, false)
	}
	b.get(5)
	b.index(0x23, committedStringsGlobal)
	b.op(0xad)
	b.op(0x56)
	b.statusFailure(statusBadInput)
	for _, bank := range banks {
		visit(bank, true)
	}
	b.index(0x23, committedStringsGlobal)
	b.get(5)
	b.op(0xa7)
	b.op(0x6b)
	b.index(0x24, committedStringsGlobal)
	for i := uint32(0); i < l.computedStride; i++ {
		for offset := uint32(0); offset < computedMetaBytes; offset += 8 {
			b.i32(computedMetaBase + int32(i*computedMetaBytes))
			b.get(0)
			b.i32(int32(l.computedStride * computedMetaBytes))
			b.op(0x6c)
			b.op(0x6a)
			b.i64(0)
			b.memory(0x37, 3, offset)
		}
	}
	b.get(1)
	b.i64(0)
	b.memory(0x37, 3, 8)
	b.i32(int32(l.frameSequences))
	b.get(0)
	b.i32(8)
	b.op(0x6c)
	b.op(0x6a)
	b.i64(0)
	b.memory(0x37, 3, 0)
	b.get(1)
	b.i32(0)
	b.memory(0x36, 2, 4)
	b.index(0x23, linkedRenderMaskGlobal)
	b.i32(1)
	b.get(0)
	b.op(0x74)
	b.i32(-1)
	b.op(0x73)
	b.op(0x71)
	b.index(0x24, linkedRenderMaskGlobal)
	b.i32(0)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(1), I32Locals: 4, I64Locals: 1, Body: b}
}

func (b *instructions) checkedStatusCall(function, result uint32) {
	b.index(0x10, function)
	b.index(0x22, result)
	b.op(0x04)
	b.op(0x40)
	b.get(result)
	b.index(0x24, errorGlobal)
	b.get(result)
	b.op(0x0f)
	b.op(0x0b)
}

func (l *linkedLayout) stampFrame(b *instructions, frame uint32) {
	for i, global := range []uint32{pendingLoGlobal, pendingHiGlobal} {
		b.i32(int32(l.frameSequences))
		b.get(frame)
		b.i32(8)
		b.op(0x6c)
		b.op(0x6a)
		b.index(0x23, global)
		b.memory(0x36, 2, uint32(i*4))
	}
}

func (l *linkedLayout) dispatchFunction(c *linkedCode) wasmgen.Function {
	var b instructions
	b.statusGuard()
	b.index(0x23, pendingGlobal)
	b.op(0x45)
	b.statusFailure(statusBusy)
	b.get(0)
	b.i32(int32(ProfileLimits().Instances))
	b.op(0x4f)
	b.poisonStatus(statusBadInput)
	b.i32(int32(l.frameTable))
	b.get(0)
	b.i32(16)
	b.op(0x6c)
	b.op(0x6a)
	b.index(0x22, 2)
	b.memory(0x28, 2, 4)
	b.i32(1)
	b.op(0x47)
	b.poisonStatus(statusBadInput)
	b.get(2)
	b.memory(0x28, 2, 0)
	b.set(3)
	for id, e := range c.programs {
		b.get(3)
		b.i32(int32(id))
		b.op(0x46)
		b.op(0x04)
		b.op(0x40)
		for handler, fn := range e.handlers {
			b.get(1)
			b.i32(int32(handler))
			b.op(0x46)
			b.op(0x04)
			b.op(0x40)
			b.get(0)
			b.checkedStatusCall(c.indices[id][fn], 4)
			l.stampFrame(&b, 0)
			b.i32(0)
			b.op(0x0f)
			b.op(0x0b)
		}
		b.op(0x0b)
	}
	b.i32(statusBadInput)
	b.index(0x24, errorGlobal)
	b.i32(statusBadInput)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(2), I32Locals: 3, Body: b}
}

// Page operations walk manifest frame order. Calls target compiled functions;
// no Program or expression opcode is decoded by the shipped module.
func (l *linkedLayout) frameOperationFunction(c *linkedCode, operation string) wasmgen.Function {
	params := uint32(0)
	if operation == "render" {
		params = 1
	}
	frame, address, owner, result := params, params+1, params+2, params+3
	var b instructions
	b.statusGuard()
	b.index(0x23, pendingGlobal)
	b.op(0x45)
	b.statusFailure(statusBusy)
	if operation == "render" {
		b.get(0)
		b.i32(1)
		b.op(0x4b)
		b.poisonStatus(statusBadInput)
	} else {
		b.index(0x23, initializedGlobal)
		b.poisonStatus(statusBadSequence)
	}
	b.op(0x02)
	b.op(0x40)
	b.op(0x03)
	b.op(0x40)
	b.get(frame)
	b.i32(int32(ProfileLimits().Instances))
	b.op(0x4f)
	b.index(0x0d, 1)
	b.i32(int32(l.frameTable))
	b.get(frame)
	b.i32(16)
	b.op(0x6c)
	b.op(0x6a)
	b.index(0x22, address)
	b.memory(0x28, 2, 4)
	b.op(0x04)
	b.op(0x40)
	b.get(address)
	b.memory(0x28, 2, 0)
	b.set(owner)
	b.get(address)
	b.memory(0x28, 2, 4)
	b.i32(1)
	b.op(0x47)
	b.get(owner)
	b.i32(int32(len(c.programs)))
	b.op(0x4f)
	b.op(0x72)
	b.poisonStatus(statusBadInput)
	if operation == "render" {
		b.get(0)
		b.index(0x23, linkedRenderMaskGlobal)
		b.i32(1)
		b.get(frame)
		b.op(0x74)
		b.op(0x71)
		b.op(0x72)
		b.op(0x04)
		b.op(0x40)
	}
	for id, e := range c.programs {
		fn := e.computed.initialize
		if operation == "bind" {
			fn = e.dom.bind
		} else if operation == "render" {
			fn = e.dom.render
		}
		b.get(owner)
		b.i32(int32(id))
		b.op(0x46)
		b.op(0x04)
		b.op(0x40)
		b.get(frame)
		if operation == "render" {
			b.get(0)
		}
		b.checkedStatusCall(c.indices[id][fn], result)
		b.op(0x0b)
	}
	if operation == "render" {
		b.get(0)
		b.op(0x45)
		b.op(0x04)
		b.op(0x40)
		l.stampFrame(&b, frame)
		b.op(0x0b)
		b.op(0x0b)
	}
	b.op(0x0b)
	b.get(frame)
	b.i32(1)
	b.op(0x6a)
	b.set(frame)
	b.index(0x0c, 0)
	b.op(0x0b)
	b.op(0x0b)
	b.i32(0)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(int(params)), I32Locals: 4, Body: b}
}

func checkpointWireType(kind ScalarKind) (program.ExprType, error) {
	if integerKind(kind) {
		return program.TypeInt, nil
	}
	switch kind {
	case Bool:
		return program.TypeBool, nil
	case String:
		return program.TypeString, nil
	default:
		return 0, fmt.Errorf("unsupported checkpoint scalar kind")
	}
}

// Copy one owned value to a checkpoint record and its dense string tail.
// Arguments are source, destination record, string cursor, document base and
// source arena. Return the next cursor, or zero without writing on rejection.
// This helper has no imports, allocation or transaction-global writes.
func checkpointValueFunction(c *linkedCode) wasmgen.Function {
	var b instructions
	b.get(4)
	b.i32(65536)
	b.op(0x47)
	b.get(4)
	b.i32(131072)
	b.op(0x47)
	b.op(0x71)
	b.statusFailure(0)
	b.get(0)
	b.get(4)
	b.get(4)
	b.i32(65536)
	b.op(0x6a)
	b.index(0x10, c.scalar)
	b.statusFailure(0)
	b.get(3)
	b.i32(32768)
	b.op(0x49)
	b.get(3)
	b.get(1)
	b.op(0x4b)
	b.op(0x72)
	b.get(1)
	b.i32(32768)
	b.op(0x49)
	b.op(0x72)
	b.get(2)
	b.i32(65536)
	b.op(0x4b)
	b.op(0x72)
	b.statusFailure(0)
	b.get(1)
	b.op(0xad)
	b.i64(valueBytes)
	b.op(0x7c)
	b.get(2)
	b.op(0xad)
	b.op(0x56)
	b.statusFailure(0)
	b.get(0)
	b.memory(0x28, 2, 16)
	b.set(5)
	b.get(0)
	b.memory(0x28, 2, 20)
	b.set(6)
	b.get(2)
	b.op(0xad)
	b.get(6)
	b.op(0xad)
	b.op(0x7c)
	b.i64(65536)
	b.op(0x56)
	b.statusFailure(0)
	for offset := uint32(0); offset < valueBytes; offset += 8 {
		b.get(1)
		b.get(0)
		b.memory(0x29, 3, offset)
		b.memory(0x37, 3, offset)
	}
	b.get(6)
	b.op(0x04)
	b.op(0x40)
	b.get(1)
	b.get(2)
	b.get(3)
	b.op(0x6b)
	b.memory(0x36, 2, 16)
	b.op(0x02)
	b.op(0x40)
	b.op(0x03)
	b.op(0x40)
	b.get(7)
	b.get(6)
	b.op(0x4f)
	b.index(0x0d, 1)
	b.get(2)
	b.get(7)
	b.op(0x6a)
	b.get(5)
	b.get(7)
	b.op(0x6a)
	b.memory(0x2d, 0, 0)
	b.memory(0x3a, 0, 0)
	b.get(7)
	b.i32(1)
	b.op(0x6a)
	b.set(7)
	b.index(0x0c, 0)
	b.op(0x0b)
	b.op(0x0b)
	b.op(0x0b)
	b.get(2)
	b.get(6)
	b.op(0x6a)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(5), I32Locals: 3, Body: b}
}
