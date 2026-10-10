package wasmgen

type controlFrame struct {
	op          byte
	height      int
	result      ValueType
	unreachable bool
	hasElse     bool
}

type codeValidator struct {
	r      *binaryReader
	stack  []ValueType
	frames []controlFrame
}

func (v *codeValidator) pop(expect ValueType) ValueType {
	frame := v.frames[len(v.frames)-1]
	if len(v.stack) == frame.height {
		if !frame.unreachable {
			v.r.fail("operand stack underflow")
		}
		return Void // Unknown type on a polymorphic unreachable stack.
	}
	actual := v.stack[len(v.stack)-1]
	v.stack = v.stack[:len(v.stack)-1]
	if expect != Void && actual != Void && actual != expect {
		v.r.fail("operand type mismatch")
	}
	return actual
}

func (v *codeValidator) push(typ ValueType) { v.stack = append(v.stack, typ) }

func (v *codeValidator) unreachable() {
	frame := &v.frames[len(v.frames)-1]
	v.stack = v.stack[:frame.height]
	frame.unreachable = true
}

func (v *codeValidator) finish() controlFrame {
	frame := v.frames[len(v.frames)-1]
	if frame.result != Void {
		v.pop(frame.result)
	}
	if len(v.stack) != frame.height {
		v.r.fail("wrong block result stack")
	}
	return frame
}

func (v *codeValidator) label(depth uint32) ValueType {
	if uint64(depth) >= uint64(len(v.frames)) {
		v.r.fail("branch depth")
		return Void
	}
	frame := v.frames[len(v.frames)-1-int(depth)]
	if frame.op == 0x03 {
		return Void
	} // Loops have no block parameters.
	return frame.result
}

func (v *codeValidator) numeric(input, output ValueType, arity int) {
	for i := 0; i < arity; i++ {
		v.pop(input)
	}
	v.push(output)
}

func validateCode(r *binaryReader, sig Signature, env validationEnv) error {
	i32, i64 := uint32(0), uint32(0)
	previous := ValueType(0)
	groups := r.count()
	if groups > 2 {
		r.fail("unsupported local groups")
	}
	for i := uint32(0); i < groups && r.err == nil; i++ {
		n, typ := r.u32(), ValueType(r.byte())
		if n == 0 || typ != I32 && typ != I64 || i > 0 && (previous != I32 || typ != I64) {
			r.fail("noncanonical local group")
		}
		if typ == I32 {
			i32 = n
		} else {
			i64 = n
		}
		previous = typ
	}
	if uint64(len(sig.Params))+uint64(i32)+uint64(i64) > 65536 {
		r.fail("local count limit")
	}
	v := codeValidator{r: r, frames: []controlFrame{{result: sig.Result}}}
	for r.err == nil && r.pos < len(r.data) && len(v.frames) != 0 {
		op := r.byte()
		switch {
		case op == 0x00:
			v.unreachable()
		case op == 0x01:
		case op >= 0x02 && op <= 0x04:
			if op == 0x04 {
				v.pop(I32)
			}
			result := ValueType(r.byte())
			if result == 0x40 {
				result = Void
			} else if result != I32 && result != I64 {
				r.fail("unsupported block type")
			}
			v.frames = append(v.frames, controlFrame{op: op, height: len(v.stack), result: result})
		case op == 0x05:
			frame := v.finish()
			if frame.op != 0x04 || frame.hasElse {
				r.fail("unexpected else")
			}
			v.stack = v.stack[:frame.height]
			frame.hasElse, frame.unreachable = true, false
			v.frames[len(v.frames)-1] = frame
		case op == 0x0b:
			frame := v.finish()
			if frame.op == 0x04 && !frame.hasElse && frame.result != Void {
				r.fail("result-bearing if requires else")
			}
			v.frames = v.frames[:len(v.frames)-1]
			if frame.result != Void && len(v.frames) != 0 {
				v.push(frame.result)
			}
		case op == 0x0c || op == 0x0d:
			result := v.label(r.u32())
			if op == 0x0d {
				v.pop(I32)
			}
			if result != Void {
				v.pop(result)
				if op == 0x0d {
					v.push(result)
				}
			}
			if op == 0x0c {
				v.unreachable()
			}
		case op == 0x0e:
			n := r.count()
			var result ValueType
			for i := uint32(0); i <= n && r.err == nil; i++ {
				typ := v.label(r.u32())
				if i > 0 && result != typ {
					r.fail("branch table type mismatch")
				}
				result = typ
			}
			v.pop(I32)
			if result != Void {
				v.pop(result)
			}
			v.unreachable()
		case op == 0x0f:
			if sig.Result != Void {
				v.pop(sig.Result)
			}
			v.unreachable()
		case op == 0x10:
			index := r.u32()
			if uint64(index) >= uint64(len(env.functions)) {
				r.fail("call function index")
				break
			}
			callee := env.types[env.functions[index]]
			for i := len(callee.Params) - 1; i >= 0; i-- {
				v.pop(callee.Params[i])
			}
			if callee.Result != Void {
				v.push(callee.Result)
			}
		case op == 0x1a:
			v.pop(Void)
		case op == 0x1b:
			v.pop(I32)
			b, a := v.pop(Void), v.pop(Void)
			if a != b && a != Void && b != Void {
				r.fail("select type mismatch")
			}
			if a == Void {
				a = b
			}
			v.push(a)
		case op >= 0x20 && op <= 0x22:
			index := uint64(r.u32())
			params := uint64(len(sig.Params))
			if index >= params+uint64(i32)+uint64(i64) {
				r.fail("local index")
				break
			}
			typ := I64
			if index < params {
				typ = sig.Params[index]
			} else if index < params+uint64(i32) {
				typ = I32
			}
			if op != 0x20 {
				v.pop(typ)
			}
			if op != 0x21 {
				v.push(typ)
			}
		case op == 0x23 || op == 0x24:
			index := r.u32()
			if uint64(index) >= uint64(len(env.globals)) {
				r.fail("global index")
				break
			}
			if op == 0x23 {
				v.push(I32)
			} else {
				if !env.globals[index] {
					r.fail("immutable global write")
				}
				v.pop(I32)
			}
		case memoryInstruction(op):
			typ, alignment, store := memoryType(op)
			if r.u32() > alignment {
				r.fail("memory alignment")
			}
			r.u32() // Core memory zero's offset immediate.
			if store {
				v.pop(typ)
			}
			v.pop(I32)
			if !store {
				v.push(typ)
			}
		case op == 0x3f:
			if r.u32() != 0 {
				r.fail("memory index")
			}
			v.push(I32)
		case op == 0x41:
			r.signed(32)
			v.push(I32)
		case op == 0x42:
			r.signed(64)
			v.push(I64)
		case op == 0x45:
			v.numeric(I32, I32, 1)
		case op >= 0x46 && op <= 0x4f:
			v.numeric(I32, I32, 2)
		case op == 0x50:
			v.numeric(I64, I32, 1)
		case op >= 0x51 && op <= 0x5a:
			v.numeric(I64, I32, 2)
		case op >= 0x67 && op <= 0x69:
			v.numeric(I32, I32, 1)
		case op >= 0x6a && op <= 0x78:
			v.numeric(I32, I32, 2)
		case op >= 0x79 && op <= 0x7b:
			v.numeric(I64, I64, 1)
		case op >= 0x7c && op <= 0x8a:
			v.numeric(I64, I64, 2)
		case op == 0xa7:
			v.numeric(I64, I32, 1)
		case op == 0xac || op == 0xad:
			v.numeric(I32, I64, 1)
		default:
			r.fail("unsupported instruction")
		}
	}
	if len(v.frames) != 0 {
		r.fail("unterminated control frame")
	}
	r.end()
	return r.err
}

func memoryInstruction(op byte) bool {
	return op == 0x28 || op == 0x29 || op >= 0x2c && op <= 0x37 || op >= 0x3a && op <= 0x3e
}

func memoryType(op byte) (ValueType, uint32, bool) {
	typ, alignment := I32, uint32(0)
	if op == 0x29 || op >= 0x30 && op <= 0x35 || op == 0x37 || op >= 0x3c {
		typ = I64
	}
	switch op {
	case 0x29, 0x37:
		alignment = 3
	case 0x28, 0x34, 0x35, 0x36, 0x3e:
		alignment = 2
	case 0x2e, 0x2f, 0x32, 0x33, 0x3b, 0x3d:
		alignment = 1
	}
	return typ, alignment, op >= 0x36
}
