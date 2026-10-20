package aot

import "m31labs.dev/gosx/island/program"

// opcodeArity is the complete profile allowlist. Unsupported VM instructions
// never acquire an implementation through an arity or result-type default.
func opcodeArity(op program.OpCode) (min, max int, ok bool) {
	switch op {
	case program.OpLitString, program.OpLitInt, program.OpLitBool,
		program.OpPropGet, program.OpEventGet, program.OpSignalGet:
		return 0, 0, true
	case program.OpSignalSet, program.OpNeg, program.OpNot, program.OpToString, program.OpLen:
		return 1, 1, true
	case program.OpAdd, program.OpSub, program.OpMul, program.OpEq, program.OpNeq,
		program.OpLt, program.OpGt, program.OpLte, program.OpGte,
		program.OpAnd, program.OpOr, program.OpConcat, program.OpIndex:
		return 2, 2, true
	case program.OpCond:
		return 3, 3, true
	case program.OpFormat, program.OpSeq:
		return 0, 64, true
	default:
		return 0, 0, false
	}
}

func opcodeRules(p *program.Program) *rejection {
	for i, e := range p.Exprs {
		min, max, ok := opcodeArity(e.Op)
		if !ok {
			return reject("opcode_unsupported", "expressions", i)
		}
		if len(e.Operands) < min || len(e.Operands) > max {
			return reject("arity", "expressions", i)
		}
	}
	return nil
}
