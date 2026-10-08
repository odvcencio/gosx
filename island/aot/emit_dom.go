package aot

import (
	"fmt"

	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

const (
	statusBindingMismatch = 6
	statusPatchLimit      = 9
	patchSetText          = 0
	patchSetAttr          = 1
	patchRemoveAttr       = 2
	patchSetValue         = 8
)

type domField struct {
	binding, attribute uint32
	kind               int32
	nodes              []program.NodeID
	expr               program.ExprID
	presence           bool
}

type domLayout struct {
	frameStride                     uint32
	bindings                        BindingSet
	fields                          []domField
	rootBase                        uint32
	values                          []uint32
	bind, render, patch, patchCount uint32
}

func emitDOMExpressions(u Unit, instances []uint32) (*expressionEmitter, error) {
	state, dom, err := buildDOMLayout(u, instances)
	if err != nil {
		return nil, err
	}
	return emitConfiguredModule(u, state.roots, true, state, dom)
}

func buildDOMLayout(u Unit, instances []uint32) (*stateLayout, *domLayout, error) {
	bindings, err := BuildBindings(u)
	if err != nil {
		return nil, nil, err
	}
	state, err := buildStateLayout(u, instances)
	if err != nil {
		return nil, nil, err
	}
	d := &domLayout{bindings: bindings, rootBase: state.roots}
	for _, binding := range bindings.Bindings {
		if binding.Kind == program.NodeText {
			dynamic := false
			for _, node := range binding.SourceNodes {
				dynamic = dynamic || u.Program.Nodes[node].Kind == program.NodeExpr
			}
			if dynamic {
				d.fields = append(d.fields, domField{binding: binding.ID, attribute: NoBindingName, kind: patchSetText, nodes: binding.SourceNodes})
			}
			continue
		}
		n := u.Program.Nodes[binding.SourceNodes[0]]
		attribute := 0
		for _, attr := range n.Attrs {
			if attr.Kind == program.AttrEvent {
				continue
			}
			if attr.Kind == program.AttrExpr {
				kind := int32(patchSetAttr)
				if attr.Name == "value" && (n.Tag == "input" || n.Tag == "textarea" || n.Tag == "select") {
					kind = patchSetValue
				}
				d.fields = append(d.fields, domField{binding: binding.ID, attribute: uint32(attribute), kind: kind, expr: attr.Expr,
					presence: attr.Name == "checked" || attr.Name == "disabled" || attr.Name == "hidden" || attr.Name == "selected" || attr.Name == "required" || attr.Name == "readonly" || attr.Name == "multiple"})
			}
			attribute++
		}
	}
	if err := d.validate(); err != nil {
		return nil, nil, err
	}
	state.roots += uint32(len(state.instances) * len(d.fields))
	d.frameStride = uint32(len(d.fields))
	return state, d, nil
}

func (e *expressionEmitter) setupDOM() {
	d := e.dom
	d.patchCount = uint32(len(e.module.Globals))
	e.module.Globals = append(e.module.Globals, wasmgen.Global{Mutable: true})
	indices := []*uint32{&d.bind, &d.render, &d.patch}
	d.values = make([]uint32, len(d.fields))
	for i := range d.values {
		indices = append(indices, &d.values[i])
	}
	for _, index := range indices {
		*index = uint32(len(e.module.Imports) + len(e.module.Functions))
		e.module.Functions = append(e.module.Functions, wasmgen.Function{})
	}
	put := func(index uint32, fn wasmgen.Function) { e.module.Functions[index-uint32(len(e.module.Imports))] = fn }
	put(d.bind, e.domBindFunction())
	put(d.render, e.domRenderFunction())
	put(d.patch, e.domPatchFunction())
	for i, field := range d.fields {
		put(d.values[i], e.domValueFunction(field))
	}
	put(e.transactions[transactionBegin], e.beginFunction())
}

// Import status is zero or a bounded positive/negative ABI status. Unknown
// results become the import's declared failure instead of entering state.
func (b *instructions) importStatus(local uint32, fallback int32) {
	b.set(local)
	b.get(local)
	b.op(0x04)
	b.op(0x40)
	b.get(local)
	b.i32(0)
	b.op(0x48)
	b.op(0x04)
	b.op(0x40)
	b.i32(0)
	b.get(local)
	b.op(0x6b)
	b.set(local)
	b.op(0x0b)
	b.get(local)
	b.i32(10)
	b.op(0x4b)
	b.op(0x04)
	b.op(0x40)
	b.i32(fallback)
	b.set(local)
	b.op(0x0b)
	b.get(local)
	b.index(0x24, errorGlobal)
	b.statusGuard()
	b.op(0x0b)
}

func (e *expressionEmitter) domBindFunction() wasmgen.Function {
	var b instructions
	b.statusGuard()
	b.index(0x23, pendingGlobal)
	b.op(0x45)
	b.statusFailure(statusBusy)
	b.get(0)
	b.index(0x10, e.state.lookup)
	b.op(0x1a)
	b.statusGuard()
	for _, binding := range e.dom.bindings.Bindings {
		b.get(0)
		b.i32(int32(binding.ID))
		b.i32(int32(binding.Kind))
		b.i32(int32(binding.TagID))
		b.index(0x10, 1)
		b.importStatus(1, statusBindingMismatch)
	}
	b.i32(0)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(1), I32Locals: 1, Body: b}
}

func (e *expressionEmitter) domValueFunction(field domField) wasmgen.Function {
	var b instructions
	b.errorGuard()
	if field.kind == patchSetText {
		for i, id := range field.nodes {
			n := e.unit.Program.Nodes[id]
			if n.Kind == program.NodeExpr {
				e.value(&b, n.Expr, 2)
				b.get(2)
				e.callHelper(&b, helperFormat)
				b.set(2)
				b.errorGuard()
			} else {
				b.i32(valueBytes)
				e.callHelper(&b, helperAllocate)
				b.set(2)
				b.errorGuard()
				text := e.strings[n.Text]
				b.writeString(2, i32Instructions(text.pointer), i32Instructions(text.length))
			}
			if i == 0 {
				b.get(2)
				b.set(3)
			} else {
				b.get(3)
				b.get(2)
				e.callHelper(&b, helperJoin)
				b.set(3)
				b.errorGuard()
			}
		}
		b.get(3)
	} else {
		e.value(&b, field.expr, 2)
		if field.presence {
			b.get(2)
			b.memory(0x28, 2, 0)
			b.i32(int32(program.TypeBool))
			b.op(0x46)
			b.op(0x04)
			b.op(0x40)
			b.numericShape(2, 2, true)
			b.i32(valueBytes)
			e.callHelper(&b, helperAllocate)
			b.set(3)
			b.errorGuard()
			b.writeString(3, i32Instructions(0), i32Instructions(0))
			b.get(2)
			b.memory(0x28, 2, 4)
			b.i32(2)
			b.op(0x71)
			b.op(0x45)
			b.op(0x04)
			b.op(0x40)
			b.get(3)
			b.i32(int32(program.TypeAny))
			b.memory(0x36, 2, 0)
			b.get(3)
			b.i32(0)
			b.memory(0x36, 2, 4)
			b.op(0x0b)
			b.get(3)
			b.op(0x0f)
			b.op(0x0b)
		}
		b.get(2)
		e.callHelper(&b, helperFormat)
	}
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(1), I32Locals: 3, Body: b}
}

func (e *expressionEmitter) domRoot(b *instructions, field int) {
	b.get(2)
	b.i32(int32(e.dom.frameStride))
	b.op(0x6c)
	b.i32(int32(e.dom.rootBase) + int32(field))
	b.op(0x6a)
}

func (e *expressionEmitter) domPatchFunction() wasmgen.Function {
	var b instructions
	b.statusGuard()
	b.index(0x23, e.dom.patchCount)
	b.i32(128)
	b.op(0x4f)
	b.op(0x04)
	b.op(0x40)
	b.i32(statusPatchLimit)
	b.index(0x24, errorGlobal)
	b.statusGuard()
	b.op(0x0b)
	for local := uint32(0); local < 5; local++ {
		b.get(local)
	}
	b.index(0x10, 2)
	b.importStatus(5, statusBadInput)
	b.index(0x23, e.dom.patchCount)
	b.i32(1)
	b.op(0x6a)
	b.index(0x24, e.dom.patchCount)
	b.i32(0)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(5), I32Locals: 1, Body: b}
}

func (e *expressionEmitter) domRenderFunction() wasmgen.Function {
	var b instructions
	b.statusGuard()
	b.index(0x23, pendingGlobal)
	b.op(0x45)
	b.statusFailure(statusBusy)
	b.get(1)
	b.i32(1)
	b.op(0x4b)
	b.statusFailure(statusBadInput)
	b.get(1)
	b.index(0x23, initializedGlobal)
	b.op(0x6a)
	b.i32(1)
	b.op(0x47)
	b.statusFailure(statusBadSequence)
	b.get(0)
	b.index(0x10, e.state.lookup)
	b.set(2)
	b.statusGuard()
	patch := func(field domField, kind int32, value instructions) {
		b.get(0)
		b.i32(kind)
		b.i32(int32(field.binding))
		b.i32(int32(field.attribute))
		b = append(b, value...)
		b.index(0x10, e.dom.patch)
		b.op(0x1a)
		b.statusGuard()
	}
	for _, binding := range e.dom.bindings.Bindings {
		for i, field := range e.dom.fields {
			if field.binding != binding.ID {
				continue
			}
			b.get(0)
			b.index(0x10, e.dom.values[i])
			b.set(3)
			b.statusGuard()
			b.index(0x23, arenaBaseGlobal)
			e.domRoot(&b, i)
			b.i32(valueBytes)
			b.op(0x6c)
			b.op(0x6a)
			b.set(4)
			b.get(1)
			b.op(0x45)
			b.op(0x04)
			b.op(0x40)
			b.get(4)
			b.memory(0x28, 2, 0)
			b.get(3)
			b.memory(0x28, 2, 0)
			b.op(0x47)
			for _, record := range []uint32{4, 3} {
				b.get(record)
				b.memory(0x28, 2, 16)
				b.get(record)
				b.memory(0x28, 2, 20)
			}
			e.callHelper(&b, helperCompare)
			b.op(0x45)
			b.op(0x45)
			b.op(0x72)
			b.set(5)
			b.statusGuard()
			b.op(0x0b)
			e.domRoot(&b, i)
			b.get(3)
			b.index(0x10, e.transactions[transactionStore])
			b.op(0x1a)
			b.statusGuard()
			if field.presence {
				b.get(5)
				b.get(3)
				b.memory(0x28, 2, 0)
				b.i32(int32(program.TypeAny))
				b.op(0x46)
				b.op(0x71)
				b.set(uint32(7 + i))
			}
			b.get(5)
			if field.presence {
				b.get(3)
				b.memory(0x28, 2, 0)
				b.i32(int32(program.TypeAny))
				b.op(0x47)
				b.op(0x71)
			}
			b.op(0x04)
			b.op(0x40)
			patch(field, field.kind, localInstructions(3))
			b.op(0x0b)
		}
		for i, field := range e.dom.fields {
			if field.binding != binding.ID || !field.presence {
				continue
			}
			b.get(uint32(7 + i))
			b.op(0x04)
			b.op(0x40)
			patch(field, patchRemoveAttr, i32Instructions(0))
			b.op(0x0b)
		}
	}
	b.i32(0)
	b.op(0x0b)
	return wasmgen.Function{Signature: i32Signature(2), I32Locals: uint32(5 + len(e.dom.fields)), Body: b}
}

func (d *domLayout) validate() error {
	if len(d.fields) > int(ProfileLimits().Values) {
		return fmt.Errorf("DOM baselines exceed the profile")
	}
	return nil
}
