package aot

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

func TestOpcodeAllowlistAndArity(t *testing.T) {
	allowed := map[program.OpCode][2]int{
		program.OpLitString: {0, 0}, program.OpLitInt: {0, 0}, program.OpLitBool: {0, 0},
		program.OpPropGet: {0, 0}, program.OpEventGet: {0, 0}, program.OpSignalGet: {0, 0},
		program.OpSignalSet: {1, 1}, program.OpNeg: {1, 1}, program.OpNot: {1, 1},
		program.OpToString: {1, 1}, program.OpLen: {1, 1}, program.OpAdd: {2, 2},
		program.OpSub: {2, 2}, program.OpMul: {2, 2}, program.OpEq: {2, 2},
		program.OpNeq: {2, 2}, program.OpLt: {2, 2}, program.OpGt: {2, 2},
		program.OpLte: {2, 2}, program.OpGte: {2, 2}, program.OpAnd: {2, 2},
		program.OpOr: {2, 2}, program.OpConcat: {2, 2}, program.OpIndex: {2, 2},
		program.OpCond: {3, 3}, program.OpFormat: {0, 64}, program.OpSeq: {0, 64},
	}
	// Cover future/unknown byte values as well as every current VM instruction.
	for code := 0; code <= 255; code++ {
		op := program.OpCode(code)
		bounds, want := allowed[op]
		min, max, ok := opcodeArity(op)
		if ok != want || ok && (min != bounds[0] || max != bounds[1]) {
			t.Fatalf("opcode %d: %d..%d allowed=%v", code, min, max, ok)
		}
		if !ok {
			p := &program.Program{Exprs: []program.Expr{{Op: op}}}
			if r := opcodeRules(p); r == nil || r.reason != "opcode_unsupported" {
				t.Fatalf("opcode %d: %+v", code, r)
			}
			continue
		}
		for _, arity := range []int{min, max, min - 1, max + 1} {
			if arity < 0 {
				continue
			}
			p := &program.Program{Exprs: []program.Expr{{Op: op, Operands: make([]program.ExprID, arity)}}}
			r := opcodeRules(p)
			if (r == nil) != (arity >= min && arity <= max) {
				t.Fatalf("opcode %d arity %d: %+v", code, arity, r)
			}
		}
	}
}
