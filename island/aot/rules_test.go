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

func handlerUnit(t *testing.T) Unit {
	u := scalarTestUnit(t)
	get := addExpression(&u, program.OpSignalGet, program.TypeInt, Int, "$count")
	next := addExpression(&u, program.OpAdd, program.TypeInt, Int, "", get, 0)
	set := addExpression(&u, program.OpSignalSet, program.TypeAny, AnyZero, "$count", next)
	seq := addExpression(&u, program.OpSeq, program.TypeAny, AnyZero, "", set)
	u.Contract.Expressions[seq].Pure = false
	u.Program.Handlers = []program.Handler{{Name: "increment", Body: []program.ExprID{seq}}}
	return refreshUnit(t, u)
}

func TestClassifierHandlersAndPureReads(t *testing.T) {
	u := handlerUnit(t)
	if r := Classify(u, ScalarDOMV1); !r.Eligible {
		t.Fatalf("handler: %+v", r)
	}
	for _, op := range []program.OpCode{program.OpAnd, program.OpOr, program.OpNot} {
		u := scalarTestUnit(t)
		args := []program.ExprID{2, 2}
		if op == program.OpNot {
			args = args[:1]
		}
		id := addExpression(&u, op, program.TypeBool, Bool, "", args...)
		u.Program.Computeds = []program.ComputedDef{{Name: "derived", Type: program.TypeBool, Expr: id}}
		u.Contract.Computeds = []StateContract{{Name: "derived", Kind: Bool}}
		addExpression(&u, program.OpSignalGet, program.TypeBool, Bool, "derived")
		u = refreshUnit(t, u)
		if r := Classify(u, ScalarDOMV1); !r.Eligible {
			t.Fatalf("pure %v: %+v", op, r)
		}
	}
	// A shared DAG has only 64 distinct expressions, not 2^63 evaluations in
	// the classifier. Context checks must memoize the graph as well as purity.
	u = scalarTestUnit(t)
	id := program.ExprID(2)
	for i := 0; i < 60; i++ {
		id = addExpression(&u, program.OpAnd, program.TypeBool, Bool, "", id, id)
	}
	u.Program.Computeds = []program.ComputedDef{{Name: "derived", Type: program.TypeBool, Expr: id}}
	u.Contract.Computeds = []StateContract{{Name: "derived", Kind: Bool}}
	u = refreshUnit(t, u)
	if r := Classify(u, ScalarDOMV1); !r.Eligible {
		t.Fatalf("shared DAG: %+v", r)
	}
}

func TestClassifierEffectContexts(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		mutate       func(*Unit)
	}{
		{"forged purity", "purity", func(u *Unit) { u.Contract.Expressions[5].Pure = true }},
		{"unused write", "effect_scope", func(u *Unit) { u.Program.Handlers = nil }},
		{"render write", "effect_scope", func(u *Unit) {
			u.Program.Nodes = append(u.Program.Nodes, program.Node{Kind: program.NodeExpr, Expr: 5})
			u.Program.Nodes[0].Children = []program.NodeID{1}
			u.Program.StaticMask = []bool{false, false}
			u.Contract.Bindings, _ = ContractBindings(u.Program)
		}},
		{"initializer write", "effect_scope", func(u *Unit) { u.Program.Signals[0].Init = 5 }},
		{"computed write", "effect_scope", func(u *Unit) {
			u.Program.Computeds = []program.ComputedDef{{Name: "derived", Expr: 5, Type: program.TypeAny}}
			u.Contract.Computeds = []StateContract{{Name: "derived", Kind: Int}}
		}},
		{"attribute write", "effect_scope", func(u *Unit) {
			id := addExpression(u, program.OpFormat, program.TypeString, String, "", 5)
			u.Contract.Expressions[id].Pure = false
			u.Program.Nodes[0].Attrs = []program.Attr{{Kind: program.AttrExpr, Name: "data-count", Expr: id}}
			u.Program.StaticMask[0] = false
			u.Contract.Bindings, _ = ContractBindings(u.Program)
		}},
		{"format write", "effect_scope", func(u *Unit) {
			id := addExpression(u, program.OpFormat, program.TypeString, String, "", 5)
			u.Contract.Expressions[id].Pure = false
			u.Program.Handlers[0].Body = []program.ExprID{id}
		}},
		{"selector escape", "selector_escape", func(u *Unit) {
			id := addExpression(u, program.OpPropGet, program.TypeAny, SelectorPath, "props")
			u.Program.Handlers[0].Body = []program.ExprID{id}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := handlerUnit(t)
			tc.mutate(&u)
			u = refreshUnit(t, u)
			if r := Classify(u, ScalarDOMV1); r.Eligible || r.Reason != tc.reason {
				t.Fatalf("receipt: %+v", r)
			}
		})
	}
}

func TestClassifierRejectsImpureBooleanOperands(t *testing.T) {
	for _, op := range []program.OpCode{program.OpAnd, program.OpOr, program.OpCond} {
		u := handlerUnit(t)
		seq := addExpression(&u, program.OpSeq, program.TypeBool, Bool, "", 5, 2)
		u.Contract.Expressions[seq].Pure = false
		args := []program.ExprID{seq, 2}
		if op == program.OpCond {
			args = append(args, 2)
		}
		id := addExpression(&u, op, program.TypeBool, Bool, "", args...)
		u.Contract.Expressions[id].Pure = false
		u.Program.Handlers[0].Body = []program.ExprID{id}
		u = refreshUnit(t, u)
		if r := Classify(u, ScalarDOMV1); r.Reason != "impure_operand" {
			t.Fatalf("%v: %+v", op, r)
		}
	}
}

func TestClassifierSelectorAndEventScopes(t *testing.T) {
	u := selectorUnit(t)
	u = refreshUnit(t, u)
	if r := Classify(u, ScalarDOMV1); !r.Eligible {
		t.Fatalf("selector: %+v", r)
	}
	// Rendering or storing an aggregate prefix is forbidden, even if all its
	// other uses are valid selector bases.
	u.Program.Handlers = []program.Handler{{Name: "read", Body: []program.ExprID{2}}}
	u = refreshUnit(t, u)
	if r := Classify(u, ScalarDOMV1); r.Reason != "selector_escape" {
		t.Fatalf("prefix: %+v", r)
	}
	u = staticUnit(t)
	id := addExpression(&u, program.OpEventGet, program.TypeString, String, "value")
	u.Contract.Inputs = []InputContract{{Source: "event", Root: "value", Path: []string{}, Kind: String, Exprs: []program.ExprID{id}}}
	u.Program.Handlers = []program.Handler{{Name: "read", Body: []program.ExprID{id}}}
	u = refreshUnit(t, u)
	if r := Classify(u, ScalarDOMV1); !r.Eligible {
		t.Fatalf("event: %+v", r)
	}
	u.Program.Nodes = append(u.Program.Nodes, program.Node{Kind: program.NodeExpr, Expr: id})
	u.Program.Nodes[0].Children = []program.NodeID{1}
	u.Program.StaticMask = []bool{false, false}
	u.Contract.Bindings, _ = ContractBindings(u.Program)
	u = refreshUnit(t, u)
	if r := Classify(u, ScalarDOMV1); r.Reason != "effect_scope" {
		t.Fatalf("render event: %+v", r)
	}
}
