package aot

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

func addExpression(u *Unit, op program.OpCode, typ program.ExprType, kind ScalarKind, value string, operands ...program.ExprID) program.ExprID {
	id := program.ExprID(len(u.Program.Exprs))
	u.Program.Exprs = append(u.Program.Exprs, program.Expr{Op: op, Type: typ, Value: value, Operands: operands})
	u.Contract.Expressions = append(u.Contract.Expressions, ExpressionContract{Expr: id, Kind: kind, Pure: op != program.OpSignalSet})
	return id
}

func scalarTestUnit(t *testing.T) Unit {
	u := literalUnit(t)
	addExpression(&u, program.OpLitString, program.TypeString, String, "héllo 🌴\x00")
	addExpression(&u, program.OpLitBool, program.TypeBool, Bool, "false")
	u.Program.Signals = []program.SignalDef{{Name: "$count", Init: 0, Type: program.TypeInt}}
	u.Contract.Signals = []StateContract{{Name: "$count", Kind: Int}}
	return u
}

func TestExpressionScalarKinds(t *testing.T) {
	for _, tc := range []struct {
		op   program.OpCode
		kind ScalarKind
		args []program.ExprID
	}{
		{program.OpAdd, Int, []program.ExprID{0, 0}}, {program.OpSub, Int, []program.ExprID{0, 0}},
		{program.OpMul, Int, []program.ExprID{0, 0}}, {program.OpNeg, Int, []program.ExprID{0}},
		{program.OpAdd, String, []program.ExprID{1, 1}},
		{program.OpEq, Bool, []program.ExprID{0, 0}}, {program.OpNeq, Bool, []program.ExprID{2, 2}},
		{program.OpLt, Bool, []program.ExprID{1, 1}}, {program.OpGt, Bool, []program.ExprID{0, 0}},
		{program.OpLte, Bool, []program.ExprID{1, 1}}, {program.OpGte, Bool, []program.ExprID{0, 0}},
		{program.OpAnd, Bool, []program.ExprID{2, 2}}, {program.OpOr, Bool, []program.ExprID{2, 2}},
		{program.OpNot, Bool, []program.ExprID{2}}, {program.OpConcat, String, []program.ExprID{0, 2}},
		{program.OpFormat, String, []program.ExprID{0, 1, 2}}, {program.OpFormat, String, nil},
		{program.OpToString, String, []program.ExprID{0}}, {program.OpCond, String, []program.ExprID{2, 1, 1}},
		{program.OpLen, Int, []program.ExprID{1}}, {program.OpSeq, AnyZero, nil},
		{program.OpSeq, String, []program.ExprID{0, 1}},
	} {
		u := scalarTestUnit(t)
		addExpression(&u, tc.op, program.TypeAny, tc.kind, "prefix:", tc.args...)
		if r := opcodeRules(u.Program); r != nil {
			t.Fatalf("arity %v: %+v", tc.op, r)
		}
		if r := expressionTypes(u); r != nil {
			t.Fatalf("kind %v: %+v", tc.op, r)
		}
	}
	u := scalarTestUnit(t)
	addExpression(&u, program.OpSignalGet, program.TypeInt, Int, "$count")
	addExpression(&u, program.OpSignalSet, program.TypeAny, AnyZero, "$count", 0)
	if r := expressionTypes(u); r != nil {
		t.Fatalf("shared state: %+v", r)
	}
}

func TestExpressionKindsRejectCoercionAndAggregates(t *testing.T) {
	for _, tc := range []struct {
		op   program.OpCode
		kind ScalarKind
		args []program.ExprID
	}{
		{program.OpAdd, String, []program.ExprID{0, 1}}, {program.OpSub, Int, []program.ExprID{1, 0}},
		{program.OpMul, Int, []program.ExprID{0, 2}}, {program.OpNeg, Int, []program.ExprID{1}},
		{program.OpEq, Bool, []program.ExprID{0, 1}}, {program.OpLt, Bool, []program.ExprID{2, 2}},
		{program.OpAnd, Bool, []program.ExprID{0, 2}}, {program.OpOr, Bool, []program.ExprID{2, 1}},
		{program.OpNot, Bool, []program.ExprID{1}}, {program.OpLen, Int, []program.ExprID{0}},
		{program.OpCond, Int, []program.ExprID{0, 0, 0}}, {program.OpCond, Int, []program.ExprID{2, 1, 0}},
	} {
		u := scalarTestUnit(t)
		id := addExpression(&u, tc.op, program.TypeAny, tc.kind, "", tc.args...)
		if r := expressionTypes(u); r == nil || r.index != int(id) || r.reason != "type_mismatch" {
			t.Fatalf("%v: %+v", tc, r)
		}
	}
	u := scalarTestUnit(t)
	u.Contract.Expressions[1].Kind = SelectorPath
	addExpression(&u, program.OpFormat, program.TypeString, String, "", 1)
	if r := expressionTypes(u); r == nil {
		t.Fatal("formatted an aggregate")
	}
}

func TestInputDefaults(t *testing.T) {
	p := &program.Program{Props: []program.PropDef{{Name: "Label", Type: program.TypeString}, {Name: "Count", Type: program.TypeInt}, {Name: "Checked", Type: program.TypeBool}, {Name: "Detail", Type: program.TypeAny}}}
	for _, tc := range []struct {
		input InputContract
		typ   program.ExprType
		valid bool
	}{
		{InputContract{Source: "prop", Root: "Label", Kind: String}, program.TypeString, true},
		{InputContract{Source: "prop", Root: "Count", Kind: Int32}, program.TypeInt, true},
		{InputContract{Source: "prop", Root: "Checked", Kind: Bool}, program.TypeBool, true},
		{InputContract{Source: "prop", Root: "props", Path: []string{"Label"}, Kind: String}, program.TypeAny, true},
		{InputContract{Source: "prop", Root: "Detail", Path: []string{"Label"}, Kind: String}, program.TypeAny, true},
		{InputContract{Source: "event", Root: "value", Kind: String}, program.TypeString, true},
		{InputContract{Source: "event", Root: "checked", Kind: Bool}, program.TypeBool, true},
		{InputContract{Source: "event", Root: "selectedIndex", Kind: Int}, program.TypeInt, true},
		{InputContract{Source: "event", Root: "clientX", Kind: Int}, 0, false},
		{InputContract{Source: "event", Root: "unknown", Kind: Bool}, 0, false},
		{InputContract{Source: "event", Root: "value", Path: []string{"x"}, Kind: String}, 0, false},
		{InputContract{Source: "prop", Root: "Label", Path: []string{"x"}, Kind: String}, 0, false},
		{InputContract{Source: "prop", Root: "absent", Kind: String}, 0, false},
		{InputContract{Source: "prop", Root: "Label", Kind: Bool}, 0, false},
	} {
		typ, err := InputDefaultType(p, tc.input)
		if (err == nil) != tc.valid || tc.valid && typ != tc.typ {
			t.Fatalf("%+v: type=%v err=%v", tc.input, typ, err)
		}
	}
	if _, err := InputDefaultType(nil, InputContract{}); err == nil {
		t.Fatal("accepted missing input proof")
	}
}

func selectorUnit(t *testing.T) Unit {
	u := staticUnit(t)
	root := addExpression(&u, program.OpPropGet, program.TypeAny, SelectorPath, "props")
	key := addExpression(&u, program.OpLitString, program.TypeString, String, "Detail")
	prefix := addExpression(&u, program.OpIndex, program.TypeAny, SelectorPath, "", root, key)
	key = addExpression(&u, program.OpLitString, program.TypeString, String, "Label")
	leaf := addExpression(&u, program.OpIndex, program.TypeAny, String, "", prefix, key)
	u.Contract.Inputs = []InputContract{{Source: "prop", Root: "props", Path: []string{"Detail", "Label"}, Kind: String, Exprs: []program.ExprID{leaf}}}
	return u
}

func TestStaticInputProof(t *testing.T) {
	u := selectorUnit(t)
	if r := inputRules(u); r != nil {
		t.Fatalf("selector: %+v", r)
	}
	for _, mutate := range []func(*Unit){
		func(u *Unit) { u.Contract.Inputs[0].Root = "other" },
		func(u *Unit) { u.Contract.Inputs[0].Path[1] = "Other" },
		func(u *Unit) { u.Contract.Inputs = nil },
		func(u *Unit) { u.Program.Exprs[0].Value = "undeclared" },
		func(u *Unit) { u.Program.Exprs[3].Op = program.OpPropGet },
	} {
		u := selectorUnit(t)
		mutate(&u)
		if r := inputRules(u); r == nil {
			t.Fatal("accepted a mismatched selector proof")
		}
	}
	u = staticUnit(t)
	id := addExpression(&u, program.OpEventGet, program.TypeString, String, "value")
	u.Contract.Inputs = []InputContract{{Source: "event", Root: "value", Path: []string{}, Kind: String, Exprs: []program.ExprID{id}}}
	if r := inputRules(u); r != nil {
		t.Fatalf("event: %+v", r)
	}
	u.Program.Exprs[id].Type = program.TypeAny
	if r := inputRules(u); r == nil {
		t.Fatal("accepted an erased event default")
	}
}
