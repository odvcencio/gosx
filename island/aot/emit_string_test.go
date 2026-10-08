package aot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

func stringUnit(t *testing.T, op program.OpCode, left, right string) (Unit, program.ExprID) {
	u := staticUnit(t)
	a := addExpression(&u, program.OpLitString, program.TypeString, String, left)
	b := addExpression(&u, program.OpLitString, program.TypeString, String, right)
	kind, typ := String, program.TypeString
	args := []program.ExprID{a, b}
	if op == program.OpLen {
		kind, typ, args = Int, program.TypeInt, args[:1]
	} else if op >= program.OpEq && op <= program.OpGte {
		kind, typ = Bool, program.TypeBool
	}
	root := addExpression(&u, op, typ, kind, "", args...)
	return refreshUnit(t, u), root
}

func TestEmitStringsMatchByteSemantics(t *testing.T) {
	var units []Unit
	var roots []program.ExprID
	for _, tc := range []struct {
		op          program.OpCode
		left, right string
	}{
		{program.OpConcat, "", ""}, {program.OpConcat, "a\x00b", "\x00c"},
		{program.OpAdd, "e\u0301", "🌴"}, {program.OpConcat, "é", "e\u0301"},
		{program.OpLen, "héllo 🌴", ""}, {program.OpLen, "\x00🌴", ""},
		{program.OpEq, "é", "e\u0301"}, {program.OpNeq, "\x00", ""},
		{program.OpLt, "\x7f", "é"}, {program.OpGt, "🌴", "z"},
		{program.OpLte, "a\x00b", "a\x00c"}, {program.OpGte, "ab", "a"},
		{program.OpEq, "", ""}, {program.OpLt, "a", "ab"},
	} {
		u, root := stringUnit(t, tc.op, tc.left, tc.right)
		units, roots = append(units, u), append(roots, root)
	}
	for i, got := range executeScalars(t, units, roots, 2) {
		want := vm.NewVM(units[i].Program, nil).Eval(roots[i])
		text, err := base64.StdEncoding.DecodeString(got.Text)
		flags := uint32(0)
		if want.Type == program.TypeString {
			flags = 1
		} else if want.Type == program.TypeBool && want.Truth() {
			flags = 2
		}
		number := int64(got.High)<<32 | int64(uint32(got.Low))
		if err != nil || got.Status != 0 || got.Pointer == 0 || got.Type != uint32(want.Type) || got.Flags != flags || string(text) != want.Text() || number != int64(want.Number()) {
			t.Fatalf("case %d: %+v want %q/%v/%v", i, got, want.Text(), want.Type, want.Number())
		}
		if i == 4 && number != 11 || i == 5 && number != 5 {
			t.Fatal("string length did not count UTF-8 bytes")
		}
	}
}

func TestEmitConcatUsesRawTextAndConditionalIsLazy(t *testing.T) {
	u := staticUnit(t)
	a := addExpression(&u, program.OpLitString, program.TypeString, String, "text")
	b := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "7")
	c := addExpression(&u, program.OpLitBool, program.TypeBool, Bool, "true")
	first := addExpression(&u, program.OpConcat, program.TypeString, String, "", a, b)
	second := addExpression(&u, program.OpConcat, program.TypeString, String, "", c, first)
	large := addExpression(&u, program.OpLitString, program.TypeString, String, strings.Repeat("x", 3000))
	tooLarge := addExpression(&u, program.OpConcat, program.TypeString, String, "", large, large)
	root := addExpression(&u, program.OpCond, program.TypeString, String, "", c, second, tooLarge)
	u = refreshUnit(t, u)
	got := executeScalars(t, []Unit{u}, []program.ExprID{root}, 1)[0]
	text, _ := base64.StdEncoding.DecodeString(got.Text)
	if got.Status != 0 || got.Flags != 1 || string(text) != "text" || vm.NewVM(u.Program, nil).Eval(root).String() != "text" {
		t.Fatalf("raw concat/lazy conditional: %+v", got)
	}
}

func TestEmitStringLengthAndArenaLimits(t *testing.T) {
	valid, root := stringUnit(t, program.OpConcat, strings.Repeat("x", 4095), "y")
	tooLong, failure := stringUnit(t, program.OpConcat, strings.Repeat("x", 4095), "yz")
	results := executeScalars(t, []Unit{valid, tooLong}, []program.ExprID{root, failure}, 1)
	if results[0].Status != 0 || results[0].Length != 4096 {
		t.Fatalf("exact string limit: %+v", results[0])
	}
	if results[1].Status != statusStringLimit || results[1].Pointer != 0 || results[1].Slot != hex.EncodeToString(bytes.Repeat([]byte{0xa5}, valueBytes)) {
		t.Fatalf("over string limit wrote a result: %+v", results[1])
	}
	got := executeScalars(t, []Unit{valid}, []program.ExprID{root}, 100)[0]
	if got.Status != statusArenaLimit || got.Pointer != 0 {
		t.Fatalf("temporary arena overrun: %+v", got)
	}
	invalid, _ := stringUnit(t, program.OpConcat, "valid", "")
	invalid.Program.Exprs[0].Value = "\xff"
	if e, err := emitExpressions(invalid); err == nil || e != nil {
		t.Fatal("accepted invalid static UTF-8")
	}
}

// This harness only transports bytes and invokes emitted functions. Expression
// evaluation and formatting remain in the module and native VM references.
func runExpressionModule(t *testing.T, module wasmgen.Module, script string, input, output any) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is unavailable for WebAssembly execution")
	}
	binary, err := wasmgen.Encode(module)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasmgen.Validate(binary); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bootstrap := `const fs = require('node:fs');
const data = JSON.parse(fs.readFileSync(0, 'utf8'));
(async () => {
  const unexpected = () => { throw new Error('unexpected expression host call'); };
  const {instance} = await WebAssembly.instantiate(Buffer.from(process.argv[1], 'base64'),
    {gosx_aot_v1: {input: unexpected, bind: unexpected, patch: unexpected}});
  const memory = new Uint8Array(instance.exports.memory.buffer);
  const view = new DataView(memory.buffer);
` + script + `
})().catch(error => { process.stderr.write(String(error)); process.exitCode = 1; });`
	cmd := exec.CommandContext(ctx, node, "-e", bootstrap, base64.StdEncoding.EncodeToString(binary))
	cmd.Stdin = bytes.NewReader(payload)
	result, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("WebAssembly helper execution: %v: %s", err, result)
	}
	if err := json.Unmarshal(result, output); err != nil {
		t.Fatal(err)
	}
}

func TestEmitCheckedCopiesAndAllocator(t *testing.T) {
	u, _ := stringUnit(t, program.OpConcat, "a", "b")
	e, err := emitExpressions(u)
	if err != nil {
		t.Fatal(err)
	}
	// A test-only status reset lets each invalid slice test start cleanly.
	var b instructions
	b.index(0x23, errorGlobal)
	b.i32(0)
	b.index(0x24, errorGlobal)
	b.op(0x0b)
	status := uint32(len(e.module.Imports) + len(e.module.Functions))
	e.module.Functions = append(e.module.Functions, wasmgen.Function{Signature: i32Signature(0), Body: b})
	e.module.Exports = []wasmgen.Export{{Name: "copy", Function: e.helpers[helperCopy]},
		{Name: "allocate", Function: e.helpers[helperAllocate]}, {Name: "status", Function: status}}
	var got []uint32
	runExpressionModule(t, e.module, `
  memory.fill(165, 190000, 190032);
  const first = instance.exports.allocate(1);
  instance.exports.allocate(-1);
  const allocationStatus = instance.exports.status();
  const second = instance.exports.allocate(1);
  const statuses = [];
  for (const args of [[190000, -1, 8], [-1, 1024, 8], [196607, 1024, 2], [190000, 196607, 2]]) {
    instance.exports.copy(...args);
    statuses.push(instance.exports.status());
  }
  process.stdout.write(JSON.stringify([allocationStatus, second - first, ...statuses,
    memory.slice(190000, 190032).every(byte => byte === 165) ? 1 : 0]));`, nil, &got)
	if len(got) != 7 || got[0] != statusArenaLimit || got[1] != 8 || got[6] != 1 {
		t.Fatalf("allocation/copy bounds: %v", got)
	}
	for _, status := range got[2:6] {
		if status != statusBadInput {
			t.Fatalf("unchecked wrapped/out-of-range slice: %v", got)
		}
	}
}

func TestEmitStringConstantsAreSortedAndDeterministic(t *testing.T) {
	u, _ := stringUnit(t, program.OpConcat, "z\x00", "é")
	e, err := emitExpressions(u)
	if err != nil {
		t.Fatal(err)
	}
	if string(e.module.Data) != "z\x00é" || e.strings["z\x00"].pointer >= e.strings["é"].pointer {
		t.Fatalf("constant ordering: %q", e.module.Data)
	}
	again, err := emitExpressions(u)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := wasmgen.Encode(e.module)
	b, _ := wasmgen.Encode(again.module)
	if !bytes.Equal(a, b) {
		t.Fatal("string helper bytes are nondeterministic")
	}
}

func TestEmitStringRecordBoundsAndZeroKinds(t *testing.T) {
	u, _ := stringUnit(t, program.OpConcat, "a", "b")
	e, err := emitExpressions(u)
	if err != nil {
		t.Fatal(err)
	}
	var b instructions
	b.index(0x23, errorGlobal)
	b.i32(0)
	b.index(0x24, errorGlobal)
	b.op(0x0b)
	status := uint32(len(e.module.Imports) + len(e.module.Functions))
	e.module.Functions = append(e.module.Functions, wasmgen.Function{Signature: i32Signature(0), Body: b})
	e.module.Exports = []wasmgen.Export{{Name: "join", Function: e.helpers[helperJoin]}, {Name: "status", Function: status}}
	var got []uint32
	runExpressionModule(t, e.module, `
  const left = 17408, right = 17432;
  memory.fill(0, left, right + 24);
  view.setUint32(right + 4, 1, true);
  const joined = instance.exports.join(left, right);
  const results = [instance.exports.status(), view.getUint32(joined + 4, true),
    view.getUint32(left + 4, true), view.getUint32(right + 4, true),
    view.getUint32(joined + 16, true), view.getUint32(joined + 20, true)];
  for (const fields of data) {
    memory.fill(0, left, left + 24);
    for (let index = 0; index < fields.length; index++) view.setUint32(left + index * 4, fields[index], true);
    instance.exports.join(left, right);
    results.push(instance.exports.status());
  }
  instance.exports.join(-1, right);
  results.push(instance.exports.status());
  process.stdout.write(JSON.stringify(results));`, [][]uint32{
		{0, 4, 0, 0, 0, 0}, {0, 1, 1, 0, 0, 0}, {1, 0, 0, 0, 0, 0},
		{0, 1, 0, 0, 1024, 0}, {0, 1, 0, 0, 0, 1}, {0, 0, 0, 0, 1024, 1},
		{0, 1, 0, 0, 1024, 4097}, {0, 1, 0, 0, 196607, 2},
	}, &got)
	want := []uint32{0, 1, 0, 1, 0, 0, 2, 2, 2, 2, 2, 2, 4, 2, 2}
	if len(got) != len(want) {
		t.Fatalf("record result count: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("record case %d: %v want %v", i, got, want)
		}
	}
}
