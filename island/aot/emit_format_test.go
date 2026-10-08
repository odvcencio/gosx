package aot

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"math"
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

func TestEmitScalarFormattingMatchesVM(t *testing.T) {
	var units []Unit
	var roots []program.ExprID
	var goldens []string
	for _, number := range []int64{math.MinInt32, -1000000010, -100, -10, -1, 0, 1, 10, 100, math.MaxInt32} {
		u := staticUnit(t)
		text := strconv.FormatInt(number, 10)
		value := addExpression(&u, program.OpLitInt, program.TypeInt, Int, text)
		root := addExpression(&u, program.OpToString, program.TypeString, String, "", value)
		units, roots, goldens = append(units, refreshUnit(t, u)), append(roots, root), append(goldens, text)
	}
	for _, tc := range []struct {
		op    program.OpCode
		typ   program.ExprType
		kind  ScalarKind
		value string
	}{
		{program.OpLitBool, program.TypeBool, Bool, "false"}, {program.OpLitBool, program.TypeBool, Bool, "true"},
		{program.OpLitString, program.TypeString, String, ""}, {program.OpLitString, program.TypeString, String, "\x00e\u0301🌴"},
	} {
		u := staticUnit(t)
		value := addExpression(&u, tc.op, tc.typ, tc.kind, tc.value)
		root := addExpression(&u, program.OpToString, program.TypeString, String, "", value)
		units, roots, goldens = append(units, refreshUnit(t, u)), append(roots, root), append(goldens, tc.value)
	}
	for i, got := range executeScalars(t, units, roots, 3) {
		text, err := base64.StdEncoding.DecodeString(got.Text)
		want := vm.NewVM(units[i].Program, nil).Eval(roots[i]).String()
		if err != nil || want != goldens[i] || string(text) != want || got.Status != 0 || got.Type != uint32(program.TypeString) || got.Flags != 1 || got.Low != 0 || got.High != 0 || got.Length != uint32(len(want)) {
			t.Fatalf("format %d: %+v text %q want %q", i, got, text, goldens[i])
		}
	}
}

func TestEmitFormatPrefixAndOperandOrder(t *testing.T) {
	u := staticUnit(t)
	prefix := "%d:\x00🌴 "
	want := prefix
	var operands []program.ExprID
	for i := 0; i < 64; i++ {
		value := strconv.Itoa(i - 32)
		operands = append(operands, addExpression(&u, program.OpLitInt, program.TypeInt, Int, value))
		want += value
	}
	root := addExpression(&u, program.OpFormat, program.TypeString, String, prefix, operands...)
	u = refreshUnit(t, u)
	got := executeScalars(t, []Unit{u}, []program.ExprID{root}, 2)[0]
	text, _ := base64.StdEncoding.DecodeString(got.Text)
	if got.Status != 0 || string(text) != want || vm.NewVM(u.Program, nil).Eval(root).String() != want {
		t.Fatalf("literal prefix/order: %+v %q", got, text)
	}
	empty := staticUnit(t)
	emptyRoot := addExpression(&empty, program.OpFormat, program.TypeString, String, "")
	got = executeScalars(t, []Unit{refreshUnit(t, empty)}, []program.ExprID{emptyRoot}, 1)[0]
	if got.Status != 0 || got.Flags != 1 || got.StringPointer != 0 || got.Length != 0 {
		t.Fatalf("empty format lost its string kind: %+v", got)
	}
}

func TestEmitFormatMixedScalarsAndDecodedBytes(t *testing.T) {
	u := staticUnit(t)
	a := addExpression(&u, program.OpLitString, program.TypeString, String, "\\u0061\x00e\u0301🌴")
	b := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "-7")
	c := addExpression(&u, program.OpLitBool, program.TypeBool, Bool, "true")
	root := addExpression(&u, program.OpFormat, program.TypeString, String, "%s:", a, b, c)
	u = refreshUnit(t, u)
	got := executeScalars(t, []Unit{u}, []program.ExprID{root}, 1)[0]
	text, _ := base64.StdEncoding.DecodeString(got.Text)
	want := "%s:\\u0061\x00e\u0301🌴-7true"
	if got.Status != 0 || string(text) != want || vm.NewVM(u.Program, nil).Eval(root).String() != want {
		t.Fatalf("mixed scalars/decoded bytes: %+v %q", got, text)
	}
}

func TestEmitFormattingFailureLeavesRootUntouched(t *testing.T) {
	var units []Unit
	var roots []program.ExprID
	for _, length := range []int{4092, 4093} {
		u := staticUnit(t)
		value := addExpression(&u, program.OpLitBool, program.TypeBool, Bool, "true")
		root := addExpression(&u, program.OpFormat, program.TypeString, String, strings.Repeat("x", length), value)
		units, roots = append(units, refreshUnit(t, u)), append(roots, root)
	}
	u, overflow := integerUnit(t, program.OpAdd, math.MaxInt32, 1)
	root := addExpression(&u, program.OpFormat, program.TypeString, String, "prefix:", overflow)
	units, roots = append(units, refreshUnit(t, u)), append(roots, root)
	results := executeScalars(t, units, roots, 1)
	if results[0].Status != 0 || results[0].Length != 4096 {
		t.Fatalf("format exact limit: %+v", results[0])
	}
	for i, status := range []uint32{statusStringLimit, statusIntegerDomain} {
		got := results[i+1]
		if got.Status != status || got.Pointer != 0 || got.Slot != hex.EncodeToString(bytes.Repeat([]byte{0xa5}, valueBytes)) {
			t.Fatalf("format failure %d: %+v", i, got)
		}
	}
}

func TestEmitFormatterActualTagsAndDefaults(t *testing.T) {
	u := staticUnit(t)
	value := addExpression(&u, program.OpLitString, program.TypeString, String, "")
	addExpression(&u, program.OpToString, program.TypeString, String, "", value)
	e, err := emitExpressions(refreshUnit(t, u))
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
	e.module.Exports = []wasmgen.Export{{Name: "format", Function: e.helpers[helperFormat]}, {Name: "status", Function: status}}
	type request struct{ Record string }
	type result struct {
		Status, Flags, Length uint32
		Text                  string
	}
	var requests []request
	var goldens []string
	var statuses []uint32
	for _, tc := range []struct {
		fields [6]uint32
		text   string
		want   vm.Value
		status uint32
	}{
		{[6]uint32{0, 0}, "", vm.ZeroValue(program.TypeString), 0},
		{[6]uint32{0, 1}, "", vm.StringVal(""), 0},
		{[6]uint32{5}, "", vm.ZeroValue(program.TypeAny), 0},
		{[6]uint32{3}, "", vm.BoolVal(false), 0},
		{[6]uint32{3, 2}, "", vm.BoolVal(true), 0},
		{[6]uint32{0, 1, 0, 0, 32792, 11}, "héllo 🌴", vm.StringVal("héllo 🌴"), 0},
		{[6]uint32{1, 0, 0x80000000, 0xffffffff}, "", vm.IntVal(math.MinInt32), 0},
		{[6]uint32{1, 0, math.MaxInt32}, "", vm.IntVal(math.MaxInt32), 0},
		{[6]uint32{1, 0, 0x80000000, 0}, "", vm.Value{}, statusIntegerDomain},
		{[6]uint32{1, 0, 0x7fffffff, 0xffffffff}, "", vm.Value{}, statusIntegerDomain},
		{[6]uint32{2}, "", vm.Value{}, statusBadInput}, {[6]uint32{4}, "", vm.Value{}, statusBadInput},
		{[6]uint32{5, 0, 1}, "", vm.Value{}, statusBadInput},
		{[6]uint32{3, 1}, "", vm.Value{}, statusBadInput},
		{[6]uint32{1, 0, 0, 0, 1024}, "", vm.Value{}, statusBadInput},
		{[6]uint32{0, 0, 0, 0, 32792, 1}, "x", vm.Value{}, statusBadInput},
	} {
		record := make([]byte, valueBytes+len(tc.text))
		for i, field := range tc.fields {
			binary.LittleEndian.PutUint32(record[i*4:], field)
		}
		copy(record[valueBytes:], tc.text)
		requests = append(requests, request{base64.StdEncoding.EncodeToString(record)})
		goldens, statuses = append(goldens, tc.want.String()), append(statuses, tc.status)
	}
	var results []result
	runExpressionModule(t, e.module, `
  const results = [];
  for (const request of data) {
    memory.set(Buffer.from(request.Record, 'base64'), 32768);
    const pointer = instance.exports.format(32768);
    results.push({Status: instance.exports.status(),
      Flags: pointer ? view.getUint32(pointer + 4, true) : 0,
      Length: pointer ? view.getUint32(pointer + 20, true) : 0,
      Text: pointer ? Buffer.from(memory.slice(view.getUint32(pointer + 16, true),
        view.getUint32(pointer + 16, true) + view.getUint32(pointer + 20, true))).toString('base64') : ''});
  }
  process.stdout.write(JSON.stringify(results));`, requests, &results)
	if len(results) != len(requests) {
		t.Fatal("actual tag result count")
	}
	for i, got := range results {
		text, _ := base64.StdEncoding.DecodeString(got.Text)
		if got.Status != statuses[i] || got.Status == 0 && (got.Flags != 1 || string(text) != goldens[i] || got.Length != uint32(len(goldens[i]))) {
			t.Fatalf("actual tag %d: %+v want %q/status %d", i, got, goldens[i], statuses[i])
		}
	}
}
