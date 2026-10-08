package aot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

type scalarResult struct {
	Pointer, Status, Type, Flags uint32
	Low, High                    int32
	StringPointer, Length        uint32
	Slot                         string
	Text                         string
}

const scalarExecutionScript = `
const fs = require('node:fs');
const requests = JSON.parse(fs.readFileSync(0, 'utf8'));
(async () => {
  const results = [];
  for (const request of requests) {
    const unexpected = () => { throw new Error('unexpected expression host call'); };
    const {instance} = await WebAssembly.instantiate(Buffer.from(request.Binary, 'base64'),
      {gosx_aot_v1: {input: unexpected, bind: unexpected, patch: unexpected}});
    const memory = instance.exports.memory;
    new Uint8Array(memory.buffer, request.Slot, 24).fill(165);
    let pointer = 0;
    for (let run = 0; run < request.Runs; run++) pointer = instance.exports.evaluate(0);
    const value = new DataView(memory.buffer);
    results.push({Pointer: pointer, Status: instance.exports.status(),
      Type: pointer ? value.getUint32(pointer, true) : 0,
      Flags: pointer ? value.getUint32(pointer + 4, true) : 0,
      Low: pointer ? value.getInt32(pointer + 8, true) : 0,
      High: pointer ? value.getInt32(pointer + 12, true) : 0,
      StringPointer: pointer ? value.getUint32(pointer + 16, true) : 0,
      Length: pointer ? value.getUint32(pointer + 20, true) : 0,
      Text: pointer ? Buffer.from(new Uint8Array(memory.buffer,
        value.getUint32(pointer + 16, true), value.getUint32(pointer + 20, true))).toString('base64') : '',
      Slot: Buffer.from(new Uint8Array(memory.buffer, request.Slot, 24)).toString('hex')});
  }
  process.stdout.write(JSON.stringify(results));
})().catch(error => { process.stderr.write(String(error)); process.exitCode = 1; });
`

func executeScalars(t *testing.T, units []Unit, roots []program.ExprID, runs int) []scalarResult {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is unavailable for WebAssembly execution")
	}
	var requests []struct {
		Binary string
		Slot   uint32
		Runs   int
	}
	for i, u := range units {
		e, err := emitExpressions(u)
		if err != nil {
			t.Fatal(err)
		}
		status := uint32(len(e.module.Imports) + len(e.module.Functions))
		e.module.Functions = append(e.module.Functions, wasmgen.Function{Signature: wasmgen.Signature{Result: wasmgen.I32}, Body: []byte{0x23, errorGlobal, 0x0b}})
		e.module.Exports = []wasmgen.Export{{Name: "evaluate", Function: e.functions[roots[i]]}, {Name: "status", Function: status}}
		binary, err := wasmgen.Encode(e.module)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasmgen.Validate(binary); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, struct {
			Binary string
			Slot   uint32
			Runs   int
		}{base64.StdEncoding.EncodeToString(binary), 131072 + uint32(roots[i])*valueBytes, runs})
	}
	payload, err := json.Marshal(requests)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "-e", scalarExecutionScript)
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("WebAssembly execution: %v: %s", err, output)
	}
	var results []scalarResult
	if err := json.Unmarshal(output, &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != len(units) {
		t.Fatal("execution result count")
	}
	return results
}

func integerUnit(t *testing.T, op program.OpCode, left, right int64) (Unit, program.ExprID) {
	u := staticUnit(t)
	a := addExpression(&u, program.OpLitInt, program.TypeInt, Int, strconv.FormatInt(left, 10))
	b := addExpression(&u, program.OpLitInt, program.TypeInt, Int, strconv.FormatInt(right, 10))
	args := []program.ExprID{a, b}
	if op == program.OpNeg {
		args = args[:1]
	}
	kind, typ := Int, program.TypeInt
	if op >= program.OpEq && op <= program.OpGte {
		kind, typ = Bool, program.TypeBool
	}
	id := addExpression(&u, op, typ, kind, "", args...)
	return refreshUnit(t, u), id
}

func TestEmitIntegerAndComparisonConformance(t *testing.T) {
	var units []Unit
	var roots []program.ExprID
	var expected []struct {
		number int64
		flags  uint32
	}
	for _, tc := range []struct {
		op          program.OpCode
		left, right int64
		number      int64
		flags       uint32
	}{
		{program.OpAdd, math.MinInt32, math.MaxInt32, -1, 0}, {program.OpAdd, math.MaxInt32, 0, math.MaxInt32, 0},
		{program.OpSub, math.MinInt32, 0, math.MinInt32, 0}, {program.OpSub, -7, 3, -10, 0},
		{program.OpMul, -46340, 46340, -2147395600, 0}, {program.OpMul, math.MinInt32, 1, math.MinInt32, 0},
		{program.OpNeg, math.MaxInt32, 0, -2147483647, 0}, {program.OpNeg, -1, 0, 1, 0},
		{program.OpEq, math.MinInt32, math.MinInt32, 0, 2}, {program.OpNeq, -1, 0, 0, 2},
		{program.OpLt, math.MinInt32, math.MaxInt32, 0, 2}, {program.OpGt, -1, 1, 0, 0},
		{program.OpLte, 7, 7, 0, 2}, {program.OpGte, -7, -6, 0, 0},
	} {
		u, root := integerUnit(t, tc.op, tc.left, tc.right)
		units, roots = append(units, u), append(roots, root)
		expected = append(expected, struct {
			number int64
			flags  uint32
		}{tc.number, tc.flags})
	}
	results := executeScalars(t, units, roots, 3)
	for i, got := range results {
		want := expected[i]
		reference := vm.NewVM(units[i].Program, nil).Eval(roots[i])
		referenceFlags := uint32(0)
		if reference.Type == program.TypeBool && reference.Truth() {
			referenceFlags = 2
		}
		if int64(reference.Number()) != want.number || referenceFlags != want.flags {
			t.Fatalf("VM case %d differs from the scalar golden", i)
		}
		number := int64(got.High)<<32 | int64(uint32(got.Low))
		if got.Status != 0 || got.Pointer != 131072+uint32(roots[i])*valueBytes || got.Type != uint32(reference.Type) || got.Flags != want.flags || number != want.number || got.StringPointer != 0 || got.Length != 0 {
			t.Fatalf("case %d: %+v want %+v", i, got, want)
		}
	}
}

func TestEmitIntegerDomainBeforeRecordWrite(t *testing.T) {
	var units []Unit
	var roots []program.ExprID
	for _, tc := range []struct {
		op          program.OpCode
		left, right int64
	}{
		{program.OpAdd, math.MaxInt32, 1}, {program.OpSub, math.MinInt32, 1},
		{program.OpMul, math.MaxInt32, math.MaxInt32}, {program.OpMul, math.MinInt32, math.MinInt32},
		{program.OpNeg, math.MinInt32, 0},
	} {
		u, root := integerUnit(t, tc.op, tc.left, tc.right)
		units, roots = append(units, u), append(roots, root)
	}
	// The final subtraction would return to the domain; the addition must
	// still fail at its own intermediate, before the outer record is written.
	u, overflow := integerUnit(t, program.OpAdd, math.MaxInt32, 1)
	root := addExpression(&u, program.OpSub, program.TypeInt, Int, "", overflow, 1)
	units, roots = append(units, refreshUnit(t, u)), append(roots, root)
	for i, got := range executeScalars(t, units, roots, 2) {
		if got.Status != statusIntegerDomain || got.Pointer != 0 || got.Slot != hex.EncodeToString(bytes.Repeat([]byte{0xa5}, valueBytes)) {
			t.Fatalf("domain case %d: %+v", i, got)
		}
	}
}

func TestEmitBooleanOrderAndLazyConditional(t *testing.T) {
	var units []Unit
	var roots []program.ExprID
	for _, op := range []program.OpCode{program.OpAnd, program.OpOr, program.OpNot, program.OpEq, program.OpNeq} {
		u := staticUnit(t)
		a := addExpression(&u, program.OpLitBool, program.TypeBool, Bool, "false")
		b := addExpression(&u, program.OpLitBool, program.TypeBool, Bool, "true")
		args := []program.ExprID{a, b}
		if op == program.OpNot {
			args = args[:1]
		}
		root := addExpression(&u, op, program.TypeBool, Bool, "", args...)
		units, roots = append(units, refreshUnit(t, u)), append(roots, root)
	}
	for _, value := range []string{"true", "false"} {
		u, overflow := integerUnit(t, program.OpAdd, math.MaxInt32, 1)
		predicate := addExpression(&u, program.OpLitBool, program.TypeBool, Bool, value)
		root := addExpression(&u, program.OpCond, program.TypeInt, Int, "", predicate, 0, overflow)
		units, roots = append(units, refreshUnit(t, u)), append(roots, root)
	}
	// The right boolean expression overflows; false && right must evaluate it
	// just as the VM does, even though a Go boolean would short-circuit it.
	u, overflow := integerUnit(t, program.OpAdd, math.MaxInt32, 1)
	right := addExpression(&u, program.OpEq, program.TypeBool, Bool, "", overflow, 0)
	left := addExpression(&u, program.OpLitBool, program.TypeBool, Bool, "false")
	root := addExpression(&u, program.OpAnd, program.TypeBool, Bool, "", left, right)
	units, roots = append(units, refreshUnit(t, u)), append(roots, root)
	root = addExpression(&u, program.OpOr, program.TypeBool, Bool, "", predicateTrue(&u), right)
	units, roots = append(units, refreshUnit(t, u)), append(roots, root)
	results := executeScalars(t, units, roots, 1)
	for i := 0; i < 5; i++ {
		flags := []uint32{0, 2, 2, 0, 2}[i]
		reference := vm.NewVM(units[i].Program, nil).Eval(roots[i])
		if reference.Truth() != (flags != 0) {
			t.Fatalf("VM boolean case %d differs from the scalar golden", i)
		}
		if results[i].Status != 0 || results[i].Type != uint32(program.TypeBool) || results[i].Flags != flags {
			t.Fatalf("boolean %d: %+v", i, results[i])
		}
	}
	if results[5].Status != 0 || results[5].Low != math.MaxInt32 || results[6].Status != statusIntegerDomain || results[7].Status != statusIntegerDomain || results[8].Status != statusIntegerDomain {
		t.Fatalf("branch/eager results: %+v", results[5:])
	}
}

func predicateTrue(u *Unit) program.ExprID {
	return addExpression(u, program.OpLitBool, program.TypeBool, Bool, "true")
}

func TestEmitMaximumExpressionSlot(t *testing.T) {
	u := staticUnit(t)
	var root program.ExprID
	for i := 0; i < int(ProfileLimits().Expressions); i++ {
		root = addExpression(&u, program.OpLitInt, program.TypeInt, Int, strconv.Itoa(i))
	}
	u = refreshUnit(t, u)
	got := executeScalars(t, []Unit{u}, []program.ExprID{root}, 2)[0]
	if got.Status != 0 || got.Low != 1023 || got.High != 0 || got.Pointer+valueBytes > 196608 {
		t.Fatalf("last expression slot: %+v", got)
	}
}

func TestEmitRejectsWidthCasesAndUnimplementedKinds(t *testing.T) {
	for _, value := range []string{"2147483648", "-2147483649", "9007199254740993"} {
		u := literalUnit(t)
		u.Program.Exprs[0].Value = value
		u = refreshUnit(t, u)
		if e, err := emitExpressions(u); err == nil || e != nil {
			t.Fatalf("accepted target-width case %s", value)
		}
	}
	for _, op := range []program.OpCode{program.OpDiv, program.OpMod, program.OpToFloat, program.OpToRunes, program.OpHostCall} {
		u, _ := integerUnit(t, program.OpAdd, 1, 1)
		u.Program.Exprs[2].Op = op
		u = refreshUnit(t, u)
		if e, err := emitExpressions(u); err == nil || e != nil {
			t.Fatalf("accepted opcode %v", op)
		}
	}
	u := staticUnit(t)
	addExpression(&u, program.OpSeq, program.TypeAny, AnyZero, "")
	if e, err := emitExpressions(refreshUnit(t, u)); err == nil || e != nil {
		t.Fatal("returned partial statement emitter")
	}
}

func TestEmitExpressionBytesDeterministic(t *testing.T) {
	u, _ := integerUnit(t, program.OpMul, 46340, -46340)
	a, err := emitExpressions(u)
	if err != nil {
		t.Fatal(err)
	}
	b, err := emitExpressions(u)
	if err != nil {
		t.Fatal(err)
	}
	first, err := wasmgen.Encode(a.module)
	if err != nil {
		t.Fatal(err)
	}
	second, err := wasmgen.Encode(b.module)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("nondeterministic expressions: %v", err)
	}
}
