package aot

import (
	"encoding/base64"
	"reflect"
	"strconv"
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

func stateCounterUnit(t *testing.T) Unit {
	u := staticUnit(t)
	zero := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "0")
	one := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "1")
	seven := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "7")
	for i, name := range []string{"count", "observed", "$count", "$Count", "$$count"} {
		u.Program.Signals = append(u.Program.Signals, program.SignalDef{Name: name, Type: program.TypeInt, Init: zero})
		u.Contract.Signals = append(u.Contract.Signals, StateContract{Slot: uint32(i), Name: name, Kind: Int})
	}
	read := addExpression(&u, program.OpSignalGet, program.TypeInt, Int, "count")
	add := addExpression(&u, program.OpAdd, program.TypeInt, Int, "", read, one)
	write := addExpression(&u, program.OpSignalSet, program.TypeAny, AnyZero, "count", add)
	u.Contract.Expressions[write].Pure = false
	observe := addExpression(&u, program.OpSignalSet, program.TypeAny, AnyZero, "observed", read)
	u.Contract.Expressions[observe].Pure = false
	share := addExpression(&u, program.OpSignalSet, program.TypeAny, AnyZero, "$count", seven)
	u.Contract.Expressions[share].Pure = false
	seq := addExpression(&u, program.OpSeq, program.TypeAny, AnyZero, "", write, observe, share, share)
	u.Contract.Expressions[seq].Pure = false
	u.Program.Handlers = []program.Handler{{Name: "increment", Body: []program.ExprID{seq}}}
	return refreshUnit(t, u)
}

func exportStateModule(e *expressionEmitter) {
	for i, name := range []string{"begin", "commit", "abort", "store"} {
		e.module.Exports = append(e.module.Exports, wasmgen.Export{Name: name, Function: e.transactions[i]})
	}
	for _, item := range []struct {
		name   string
		global uint32
	}{
		{"working", arenaBaseGlobal}, {"committed", committedBaseGlobal}, {"status", errorGlobal}} {
		index := uint32(len(e.module.Imports) + len(e.module.Functions))
		var b instructions
		b.index(0x23, item.global)
		b.op(0x0b)
		e.module.Functions = append(e.module.Functions, wasmgen.Function{Signature: i32Signature(0), Body: b})
		e.module.Exports = append(e.module.Exports, wasmgen.Export{Name: item.name, Function: index})
	}
	for i, fn := range e.handlers {
		name := "handler"
		if i == 1 {
			name = "other"
		} else if i > 1 {
			name = "handler" + strconv.Itoa(i)
		}
		e.module.Exports = append(e.module.Exports, wasmgen.Export{Name: name, Function: fn})
	}
}

// Instrument the actual internal store call in test modules. The wrapper
// copies each successful logical write; shipped modules have no trace code.
func instrumentRootWrites(e *expressionEmitter) {
	index := e.transactions[transactionStore] - uint32(len(e.module.Imports))
	original := uint32(len(e.module.Imports) + len(e.module.Functions))
	e.module.Functions = append(e.module.Functions, e.module.Functions[index])
	var b instructions
	b.get(0)
	b.get(1)
	b.index(0x10, original)
	b.index(0x22, 2)
	b.op(0x04)
	b.op(0x40)
	b.get(2)
	b.op(0x0f)
	b.op(0x0b)
	b.i32(50004)
	b.i32(50000)
	b.memory(0x28, 2, 0)
	b.i32(28)
	b.op(0x6c)
	b.op(0x6a)
	b.set(3)
	b.get(3)
	b.get(0)
	b.memory(0x36, 2, 0)
	for offset := uint32(0); offset < valueBytes; offset += 8 {
		b.get(3)
		b.get(1)
		b.memory(0x29, 3, offset)
		b.memory(0x37, 3, offset+4)
	}
	b.i32(50000)
	b.i32(50000)
	b.memory(0x28, 2, 0)
	b.i32(1)
	b.op(0x6a)
	b.memory(0x36, 2, 0)
	b.i32(0)
	b.op(0x0b)
	e.module.Functions[index] = wasmgen.Function{Signature: i32Signature(2), I32Locals: 2, Body: b}
}

func TestEmitStateHandlersMatchNativeWritesAndSharedIdentity(t *testing.T) {
	u := stateCounterUnit(t)
	// An erased wire type must use the proved source kind on writes.
	u.Program.Signals[0].Type = program.TypeAny
	u = refreshUnit(t, u)
	e, err := emitStateExpressions(u, []uint32{42, 7})
	if err != nil {
		t.Fatal(err)
	}
	exportStateModule(e)
	instrumentRootWrites(e)
	initial := []int64{10, 0, 20, 0, 300, 200, 100}
	var input []string
	for _, value := range initial {
		input = append(input, scalarTransport(program.TypeInt, 0, value, ""))
	}
	var got struct {
		Statuses []uint32
		States   [][]int64
		Writes   [][2]int64
	}
	runExpressionModule(t, e.module, `
  const api = instance.exports, statuses = [], states = [], writes = [];
  const read = base => data.map((_, slot) => view.getInt32(base + slot * 24 + 8, true));
  statuses.push(api.handler(7), api.begin(0, 0, 1));
  for (let slot = 0; slot < data.length; slot++) {
    memory.set(Buffer.from(data[slot], 'base64'), 32768);
    statuses.push(api.store(slot, 32768));
  }
  statuses.push(api.commit(0, 0));
  for (const [sequence, instanceID] of [[1, 7], [2, 42]]) {
    statuses.push(api.begin(sequence, 0, 0));
    view.setUint32(50000, 0, true);
    statuses.push(api.handler(instanceID));
    for (let item = 0; item < view.getUint32(50000, true); item++) {
      const pointer = 50004 + item * 28;
      writes.push([view.getUint32(pointer, true), view.getInt32(pointer + 12, true)]);
    }
    states.push(read(api.working()));
    statuses.push(api.commit(sequence, 0));
  }
  statuses.push(api.begin(3, 0, 0), api.handler(99), api.commit(3, 0), api.abort());
  states.push(read(api.committed()));
  process.stdout.write(JSON.stringify({Statuses: statuses, States: states, Writes: writes}));`, input, &got)
	wantStates := [][]int64{{11, 11, 20, 0, 300, 200, 7}, {11, 11, 21, 21, 300, 200, 7}, {11, 11, 21, 21, 300, 200, 7}}
	wantWrites := [][2]int64{{0, 11}, {1, 11}, {6, 7}, {6, 7}, {2, 21}, {3, 21}, {6, 7}, {6, 7}}
	if !reflect.DeepEqual(got.States, wantStates) || !reflect.DeepEqual(got.Writes, wantWrites) || len(got.Statuses) != 20 {
		t.Fatalf("state/write trace: %+v", got)
	}
	for i, status := range got.Statuses {
		want := uint32(0)
		if i == 0 {
			want = statusBusy
		}
		if i == 17 || i == 18 {
			want = statusBadInput
		}
		if status != want {
			t.Fatalf("status %d: %v", i, got.Statuses)
		}
	}
	models := []*vm.VM{vm.NewVM(u.Program, nil), vm.NewVM(u.Program, nil)}
	shared := map[string]*signal.Signal[vm.Value]{}
	for _, name := range []string{"$$count", "$Count", "$count"} {
		slot := e.state.rows[e.state.signals[name]]
		shared[name] = signal.New(vm.IntVal(int(initial[slot])))
	}
	var nativeWrites [][2]int64
	for i, model := range models {
		vm.InitSignals(model, u.Program)
		for _, def := range u.Program.Signals {
			slot := e.state.rows[i*len(u.Program.Signals)+int(e.state.signals[def.Name])]
			value := shared[def.Name]
			if value == nil {
				value = signal.New(vm.IntVal(int(initial[slot])))
			}
			model.SetSignal(def.Name, value)
			if i == 0 || shared[def.Name] == nil {
				value.Subscribe(func() { nativeWrites = append(nativeWrites, [2]int64{int64(slot), int64(value.Get().Number())}) })
			}
		}
	}
	for i, model := range models {
		model.Eval(u.Program.Handlers[0].Body[0])
		if int64(model.Eval(3).Number()) != wantStates[i][i*2] {
			t.Fatal("native read-after-write")
		}
	}
	if !reflect.DeepEqual(nativeWrites, wantWrites) {
		t.Fatalf("native writes: %v want %v", nativeWrites, wantWrites)
	}
}

func TestEmitStateLayoutRejectsInvalidIdentityAndIsDeterministic(t *testing.T) {
	u := stateCounterUnit(t)
	for _, ids := range [][]uint32{nil, {7, 7}, make([]uint32, 17)} {
		if e, err := emitStateExpressions(u, ids); err == nil || e != nil {
			t.Fatalf("accepted instance layout %v", ids)
		}
	}
	a, err := emitStateExpressions(u, []uint32{0xffffffff, 7})
	if err != nil {
		t.Fatal(err)
	}
	b, err := emitStateExpressions(u, []uint32{7, 0xffffffff})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := wasmgen.Encode(a.module)
	second, _ := wasmgen.Encode(b.module)
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(a.state.rows, []uint32{0, 1, 6, 5, 4, 2, 3, 6, 5, 4}) {
		t.Fatal("instance ordering or exact shared identities changed")
	}
}

func TestEmitSequenceScalarAndZeroResults(t *testing.T) {
	u := staticUnit(t)
	a := addExpression(&u, program.OpLitString, program.TypeString, String, "first")
	b := addExpression(&u, program.OpLitString, program.TypeString, String, "last\x00🌴")
	root := addExpression(&u, program.OpSeq, program.TypeString, String, "", a, b)
	u.Program.Handlers = []program.Handler{{Name: "sequence", Body: []program.ExprID{root}}}
	u = refreshUnit(t, u)
	got := executeScalars(t, []Unit{u}, []program.ExprID{root}, 1)[0]
	text, _ := base64.StdEncoding.DecodeString(got.Text)
	if got.Status != 0 || string(text) != vm.NewVM(u.Program, nil).Eval(root).String() || string(text) != "last\x00🌴" {
		t.Fatalf("sequence result: %+v", got)
	}
	empty := staticUnit(t)
	zero := addExpression(&empty, program.OpSeq, program.TypeAny, AnyZero, "")
	empty.Program.Handlers = []program.Handler{{Name: "empty", Body: []program.ExprID{zero}}}
	got = executeScalars(t, []Unit{refreshUnit(t, empty)}, []program.ExprID{zero}, 1)[0]
	if got.Status != 0 || got.Type != uint32(program.TypeAny) || got.Flags != 0 || got.Low != 0 || got.High != 0 {
		t.Fatalf("empty sequence: %+v", got)
	}
}

func TestEmitHandlerFaultStopsWritesBeforeCommit(t *testing.T) {
	u := stateCounterUnit(t)
	maximum := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "2147483647")
	overflow := addExpression(&u, program.OpAdd, program.TypeInt, Int, "", maximum, 1)
	write := addExpression(&u, program.OpSignalSet, program.TypeAny, AnyZero, "observed", overflow)
	u.Contract.Expressions[write].Pure = false
	sequence := addExpression(&u, program.OpSeq, program.TypeAny, AnyZero, "", 5, write, 7)
	u.Contract.Expressions[sequence].Pure = false
	u.Program.Handlers[0].Body = []program.ExprID{sequence}
	u.Program.Handlers = append(u.Program.Handlers, program.Handler{Name: "original", Body: []program.ExprID{8}})
	u = refreshUnit(t, u)
	e, err := emitStateExpressions(u, []uint32{7})
	if err != nil {
		t.Fatal(err)
	}
	exportStateModule(e)
	instrumentRootWrites(e)
	var got []uint32
	runExpressionModule(t, e.module, `
  const api = instance.exports;
  api.begin(0, 0, 1);
  memory.set(Buffer.from(data.Record, 'base64'), 32768);
  for (let slot = 0; slot < data.Roots; slot++) api.store(slot, 32768);
  api.commit(0, 0);
  api.begin(1, 0, 0);
  view.setUint32(50000, 0, true);
  const status = api.handler(7);
  const results = [status, api.commit(1, 0), view.getUint32(50000, true),
    view.getUint32(api.working() + 8, true), view.getUint32(api.working() + 24 + 8, true),
    view.getUint32(api.committed() + 8, true)];
  results.push(api.abort(), view.getUint32(api.committed() + 8, true));
  process.stdout.write(JSON.stringify(results));`, struct {
		Record string
		Roots  uint32
	}{
		scalarTransport(program.TypeInt, 0, 0, ""), e.rootSlots}, &got)
	if !reflect.DeepEqual(got, []uint32{3, 3, 1, 1, 0, 0, 0, 0}) {
		t.Fatalf("handler failure published or continued writes: %v", got)
	}
}

func addComputed(u *Unit, name string, kind ScalarKind, body program.ExprID) {
	slot := uint32(len(u.Program.Computeds))
	u.Program.Computeds = append(u.Program.Computeds, program.ComputedDef{Name: name, Type: u.Program.Exprs[body].Type, Expr: body})
	u.Contract.Computeds = append(u.Contract.Computeds, StateContract{Slot: slot, Name: name, Kind: kind})
}

func computedReadUnit(t *testing.T) (Unit, []program.ExprID) {
	u := staticUnit(t)
	zero := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "0")
	one := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "1")
	two := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "2")
	yes := addExpression(&u, program.OpLitBool, program.TypeBool, Bool, "true")
	no := addExpression(&u, program.OpLitBool, program.TypeBool, Bool, "false")
	for i, def := range []struct {
		name string
		kind ScalarKind
		init program.ExprID
	}{
		{"count", Int, zero}, {"enabled", Bool, yes},
	} {
		u.Program.Signals = append(u.Program.Signals, program.SignalDef{Name: def.name, Type: u.Program.Exprs[def.init].Type, Init: def.init})
		u.Contract.Signals = append(u.Contract.Signals, StateContract{Slot: uint32(i), Name: def.name, Kind: def.kind})
	}
	count := addExpression(&u, program.OpSignalGet, program.TypeInt, Int, "count")
	enabled := addExpression(&u, program.OpSignalGet, program.TypeBool, Bool, "enabled")
	later := addExpression(&u, program.OpSignalGet, program.TypeInt, Int, "later")
	addComputed(&u, "forward", Int, later)
	product := addExpression(&u, program.OpMul, program.TypeInt, Int, "", count, two)
	addComputed(&u, "later", Int, product)
	sum := addExpression(&u, program.OpAdd, program.TypeInt, Int, "", later, one)
	addComputed(&u, "tally", Int, sum)
	tally := addExpression(&u, program.OpSignalGet, program.TypeInt, Int, "tally")
	maximum := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "2147483647")
	overflow := addExpression(&u, program.OpAdd, program.TypeInt, Int, "", maximum, one)
	choice := addExpression(&u, program.OpCond, program.TypeInt, Int, "", enabled, tally, overflow)
	addComputed(&u, "choice", Int, choice)
	positive := addExpression(&u, program.OpGt, program.TypeBool, Bool, "", count, zero)
	both := addExpression(&u, program.OpAnd, program.TypeBool, Bool, "", no, positive)
	addComputed(&u, "both", Bool, both)
	reads := []program.ExprID{}
	for _, def := range u.Program.Computeds {
		kind := u.Contract.Computeds[len(reads)].Kind
		reads = append(reads, addExpression(&u, program.OpSignalGet, def.Type, kind, def.Name))
	}
	return refreshUnit(t, u), reads
}

func exportComputedReads(e *expressionEmitter, reads []program.ExprID) {
	exportStateModule(e)
	e.module.Exports = append(e.module.Exports, wasmgen.Export{Name: "initialize", Function: e.computed.initialize})
	for i, expr := range reads {
		e.module.Exports = append(e.module.Exports, wasmgen.Export{Name: "read" + strconv.Itoa(i), Function: e.functions[expr]})
	}
}

func TestEmitComputedSourceOrderNestedTrackingAndLazyBranches(t *testing.T) {
	u, reads := computedReadUnit(t)
	e, err := emitStateExpressions(u, []uint32{42, 7})
	if err != nil {
		t.Fatal(err)
	}
	exportComputedReads(e, reads)
	var got struct {
		Statuses     []uint32
		Values       [][]int64
		Dependencies [][]uint32
	}
	runExpressionModule(t, e.module, `
  const api = instance.exports, statuses = [], values = [], dependencies = [];
  statuses.push(api.begin(0, 0, 1));
  for (let slot = 0; slot < data.Roots.length; slot++) {
    memory.set(Buffer.from(data.Roots[slot], 'base64'), 32768);
    statuses.push(api.store(slot, 32768));
  }
  statuses.push(api.initialize());
  for (const id of [7, 42]) {
    const row = [];
    for (let item = 0; item < data.Count; item++) {
      const pointer = api['read' + item](id);
      row.push(view.getUint32(pointer + 4, true), view.getInt32(pointer + 8, true));
    }
    values.push(row);
  }
  for (let frame = 0; frame < 2; frame++) {
    dependencies.push(Array.from({length: data.Count}, (_, index) =>
      view.getUint32(17408 + (frame * data.Count + index) * 32 + 20, true)));
  }
  statuses.push(api.commit(0, 0), api.begin(1, 0, 0));
  const pointer = api.read2(7);
  statuses.push(api.status(), api.abort());
  process.stdout.write(JSON.stringify({Statuses: statuses, Values: values, Dependencies: dependencies}));`, struct {
		Roots []string
		Count uint32
	}{[]string{
		scalarTransport(program.TypeInt, 0, 3, ""), scalarTransport(program.TypeBool, 2, 0, ""),
		scalarTransport(program.TypeInt, 0, 8, ""), scalarTransport(program.TypeBool, 2, 0, ""),
	}, e.state.computedCount}, &got)
	for _, status := range got.Statuses {
		if status != 0 {
			t.Fatalf("computed status: %+v", got)
		}
	}
	if !reflect.DeepEqual(got.Dependencies, [][]uint32{{0, 1, 1 << 17, 2 | 1<<18, 1}, {0, 1, 1 << 17, 2 | 1<<18, 1}}) {
		t.Fatalf("dynamic dependencies: %v", got.Dependencies)
	}
	var want [][]int64
	for _, initial := range []int{3, 8} {
		model := vm.NewVM(u.Program, nil)
		vm.InitSignals(model, u.Program)
		model.SetSignal("count", signal.New(vm.IntVal(initial)))
		row := []int64{}
		for _, read := range reads {
			value := model.Eval(read)
			flags := int64(0)
			if value.Type == program.TypeBool && value.Truth() {
				flags = 2
			}
			row = append(row, flags, int64(value.Number()))
		}
		want = append(want, row)
	}
	if !reflect.DeepEqual(got.Values, want) || got.Values[0][1] != 0 {
		t.Fatalf("computed values: %v want %v", got.Values, want)
	}
}

func TestEmitComputedStringsPreserveDefaultAndPayloadIdentity(t *testing.T) {
	u := staticUnit(t)
	later := addExpression(&u, program.OpSignalGet, program.TypeString, String, "later")
	addComputed(&u, "forward", String, later)
	empty := addExpression(&u, program.OpLitString, program.TypeString, String, "")
	addComputed(&u, "later", String, empty)
	text := addExpression(&u, program.OpLitString, program.TypeString, String, "héllo\x00🌴")
	addComputed(&u, "text", String, text)
	reads := []program.ExprID{addExpression(&u, program.OpSignalGet, program.TypeString, String, "forward"), later,
		addExpression(&u, program.OpSignalGet, program.TypeString, String, "text")}
	u = refreshUnit(t, u)
	e, err := emitStateExpressions(u, []uint32{7})
	if err != nil {
		t.Fatal(err)
	}
	exportComputedReads(e, reads)
	var got []struct {
		Type, Flags uint32
		Text        string
	}
	runExpressionModule(t, e.module, `
  const api = instance.exports;
  if (api.begin(0, 0, 1) || api.initialize()) throw new Error('initialization');
  const result = [];
  for (let item = 0; item < 3; item++) {
    const pointer = api['read' + item](7);
    result.push({Type: view.getUint32(pointer, true), Flags: view.getUint32(pointer + 4, true),
      Text: Buffer.from(memory.subarray(view.getUint32(pointer + 16, true),
        view.getUint32(pointer + 16, true) + view.getUint32(pointer + 20, true))).toString('base64')});
  }
  if (api.commit(0, 0)) throw new Error('commit');
  process.stdout.write(JSON.stringify(result));`, nil, &got)
	model := vm.NewVM(u.Program, nil)
	vm.InitSignals(model, u.Program)
	for i, result := range got {
		want := model.Eval(reads[i])
		text, _ := base64.StdEncoding.DecodeString(result.Text)
		flags := uint32(1)
		if i == 0 {
			flags = 0
		}
		if result.Type != uint32(want.Type) || result.Flags != flags || string(text) != want.Text() {
			t.Fatalf("computed string %d: %+v want %q", i, result, want.Text())
		}
	}
}

func TestEmitComputedAbortPreservesCommittedMetadata(t *testing.T) {
	u, reads := computedReadUnit(t)
	e, err := emitStateExpressions(u, []uint32{7})
	if err != nil {
		t.Fatal(err)
	}
	exportComputedReads(e, reads)
	var got []uint32
	runExpressionModule(t, e.module, `
  const api = instance.exports, result = [];
  api.begin(0, 0, 1);
  memory.set(Buffer.from(data[0], 'base64'), 32768); api.store(0, 32768);
  memory.set(Buffer.from(data[1], 'base64'), 32768); api.store(1, 32768);
  result.push(api.initialize(), api.initialize(), api.commit(0, 0));
  api.begin(1, 0, 0);
  const meta = 17408 + 32 + 16;
  view.setUint32(meta, 3, true);
  const pointer = api.read1(7);
  result.push(view.getInt32(pointer + 8, true), api.abort(), api.begin(2, 0, 0));
  result.push(view.getUint32(meta, true), view.getUint32(meta + 4, true), api.abort());
  process.stdout.write(JSON.stringify(result));`, []string{scalarTransport(program.TypeInt, 0, 6, ""),
		scalarTransport(program.TypeBool, 2, 0, "")}, &got)
	if !reflect.DeepEqual(got, []uint32{0, statusBadSequence, 0, 12, 0, 0, 1, 1, 0}) {
		t.Fatalf("computed metadata transaction: %v", got)
	}
}

func computedBatchUnit(t *testing.T, diamond bool) (Unit, []program.ExprID) {
	u := staticUnit(t)
	zero := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "0")
	one := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "1")
	two := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "2")
	for i, name := range []string{"count", "observed"} {
		u.Program.Signals = append(u.Program.Signals, program.SignalDef{Name: name, Type: program.TypeInt, Init: zero})
		u.Contract.Signals = append(u.Contract.Signals, StateContract{Slot: uint32(i), Name: name, Kind: Int})
	}
	count := addExpression(&u, program.OpSignalGet, program.TypeInt, Int, "count")
	observed := addExpression(&u, program.OpSignalGet, program.TypeInt, Int, "observed")
	product := addExpression(&u, program.OpMul, program.TypeInt, Int, "", count, two)
	addComputed(&u, "double", Int, product)
	read := addExpression(&u, program.OpSignalGet, program.TypeInt, Int, "double")
	if diamond {
		sum := addExpression(&u, program.OpAdd, program.TypeInt, Int, "", count, one)
		addComputed(&u, "next", Int, sum)
		next := addExpression(&u, program.OpSignalGet, program.TypeInt, Int, "next")
		total := addExpression(&u, program.OpAdd, program.TypeInt, Int, "", read, next)
		addComputed(&u, "total", Int, total)
		read = addExpression(&u, program.OpSignalGet, program.TypeInt, Int, "total")
	}
	increment := addExpression(&u, program.OpAdd, program.TypeInt, Int, "", count, one)
	write := addExpression(&u, program.OpSignalSet, program.TypeAny, AnyZero, "count", increment)
	equal := addExpression(&u, program.OpSignalSet, program.TypeAny, AnyZero, "count", count)
	see := addExpression(&u, program.OpSignalSet, program.TypeAny, AnyZero, "observed", read)
	for _, id := range []program.ExprID{write, equal, see} {
		u.Contract.Expressions[id].Pure = false
	}
	u.Program.Handlers = []program.Handler{{Name: "increment", Body: []program.ExprID{write, see}}, {Name: "equal", Body: []program.ExprID{equal, equal}}}
	return refreshUnit(t, u), []program.ExprID{count, observed, read}
}

func TestEmitComputedBatchReadsMatchNativeVM(t *testing.T) {
	for _, diamond := range []bool{false, true} {
		u, reads := computedBatchUnit(t, diamond)
		e, err := emitStateExpressions(u, []uint32{7})
		if err != nil {
			t.Fatal(err)
		}
		exportComputedReads(e, reads)
		steps := []struct {
			Handler int
			Read    bool
		}{{0, false}, {0, false}, {1, false}, {0, true}, {0, false}, {1, true}, {0, false}}
		var got [][]int64
		runExpressionModule(t, e.module, `
  const api = instance.exports, states = [];
  if (api.begin(0, 0, 1)) throw new Error('begin');
  memory.set(Buffer.from(data.Zero, 'base64'), 32768);
  if (api.store(0, 32768) || api.store(1, 32768) || api.initialize() || api.commit(0, 0)) throw new Error('initialization');
  for (let index = 0; index < data.Steps.length; index++) {
    const step = data.Steps[index];
    if (api.begin(index + 1, 0, 0) || (step.Handler ? api.other(7) : api.handler(7))) throw new Error('handler');
    if (step.Read) api.read2(7);
    states.push([view.getInt32(api.working() + 8, true), view.getInt32(api.working() + 32, true)]);
    if (api.commit(index + 1, 0)) throw new Error('commit');
  }
  process.stdout.write(JSON.stringify(states));`, struct {
			Zero  string
			Steps any
		}{scalarTransport(program.TypeInt, 0, 0, ""), steps}, &got)
		model := vm.NewVM(u.Program, nil)
		vm.InitSignals(model, u.Program)
		var want [][]int64
		for _, step := range steps {
			signal.Batch(func() {
				for _, expr := range u.Program.Handlers[step.Handler].Body {
					model.Eval(expr)
				}
			})
			if step.Read {
				model.Eval(reads[2])
			}
			want = append(want, []int64{int64(model.Eval(reads[0]).Number()), int64(model.Eval(reads[1]).Number())})
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("diamond=%v: %v want %v", diamond, got, want)
		}
		if !diamond && !reflect.DeepEqual(got[:2], [][]int64{{1, 0}, {2, 4}}) {
			t.Fatalf("lazy batch reads: %v", got)
		}
	}
}

func TestEmitComputedDuplicateNotificationsKeepSubscriptionOrder(t *testing.T) {
	u, reads := computedBatchUnit(t, true)
	addComputed(&u, "copy", Int, reads[2])
	u = refreshUnit(t, u)
	e, err := emitStateExpressions(u, []uint32{7})
	if err != nil {
		t.Fatal(err)
	}
	exportComputedReads(e, reads)
	instrumentRootWrites(e)
	var got []uint32
	runExpressionModule(t, e.module, `
  const api = instance.exports, writes = [];
  api.begin(0, 0, 1);
  memory.set(Buffer.from(data, 'base64'), 32768);
  api.store(0, 32768); api.store(1, 32768);
  if (api.initialize() || api.commit(0, 0) || api.begin(1, 0, 0)) throw new Error('initialization');
  view.setUint32(50000, 0, true);
  if (api.other(7)) throw new Error('equal handler');
  for (let item = 0; item < view.getUint32(50000, true); item++) writes.push(view.getUint32(50004 + item * 28, true));
  if (api.commit(1, 0)) throw new Error('commit');
  process.stdout.write(JSON.stringify(writes));`, scalarTransport(program.TypeInt, 0, 0, ""), &got)
	// Use the native reactive implementation to record refresh order. Value
	// signals notify on equal writes, unlike comparable Go scalar signals.
	base := signal.New(vm.IntVal(0))
	var refreshes []uint32
	a := signal.Derive(func() vm.Value { refreshes = append(refreshes, 2); return base.Get().Mul(vm.IntVal(2)) })
	b := signal.Derive(func() vm.Value { refreshes = append(refreshes, 3); return base.Get().Add(vm.IntVal(1)) })
	c := signal.Derive(func() vm.Value { refreshes = append(refreshes, 4); return a.Get().Add(b.Get()) })
	d := signal.Derive(func() vm.Value { return c.Get() })
	defer a.Stop()
	defer b.Stop()
	defer c.Stop()
	defer d.Stop()
	refreshes = nil
	signal.Batch(func() { base.Set(base.Get()); base.Set(base.Get()) })
	want := append([]uint32{0, 0}, refreshes...)
	if !reflect.DeepEqual(got, want) || len(refreshes) != 8 {
		t.Fatalf("callback order: %v want %v", got, want)
	}
}

func TestEmitComputedDynamicSubscriptionsMatchNativeTrace(t *testing.T) {
	u := staticUnit(t)
	yes := addExpression(&u, program.OpLitBool, program.TypeBool, Bool, "true")
	no := addExpression(&u, program.OpLitBool, program.TypeBool, Bool, "false")
	ten := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "10")
	twenty := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "20")
	zero := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "0")
	one := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "1")
	for i, def := range []struct {
		name string
		kind ScalarKind
		init program.ExprID
	}{{"enabled", Bool, yes}, {"left", Int, ten}, {"right", Int, twenty}, {"observed", Int, zero}} {
		u.Program.Signals = append(u.Program.Signals, program.SignalDef{Name: def.name, Type: u.Program.Exprs[def.init].Type, Init: def.init})
		u.Contract.Signals = append(u.Contract.Signals, StateContract{Slot: uint32(i), Name: def.name, Kind: def.kind})
	}
	flag := addExpression(&u, program.OpSignalGet, program.TypeBool, Bool, "enabled")
	left := addExpression(&u, program.OpSignalGet, program.TypeInt, Int, "left")
	right := addExpression(&u, program.OpSignalGet, program.TypeInt, Int, "right")
	choice := addExpression(&u, program.OpCond, program.TypeInt, Int, "", flag, left, right)
	addComputed(&u, "choice", Int, choice)
	read := addExpression(&u, program.OpSignalGet, program.TypeInt, Int, "choice")
	set := func(name string, value program.ExprID) program.ExprID {
		id := addExpression(&u, program.OpSignalSet, program.TypeAny, AnyZero, name, value)
		u.Contract.Expressions[id].Pure = false
		return id
	}
	incLeft := addExpression(&u, program.OpAdd, program.TypeInt, Int, "", left, one)
	incRight := addExpression(&u, program.OpAdd, program.TypeInt, Int, "", right, one)
	writeLeft, writeRight := set("left", incLeft), set("right", incRight)
	see, enable, disable := set("observed", read), set("enabled", yes), set("enabled", no)
	u.Program.Handlers = []program.Handler{{Name: "left", Body: []program.ExprID{writeLeft}}, {Name: "disable", Body: []program.ExprID{disable, see, writeLeft}},
		{Name: "enable", Body: []program.ExprID{enable, writeLeft, see}}, {Name: "right", Body: []program.ExprID{writeRight, see}}}
	u = refreshUnit(t, u)
	e, err := emitStateExpressions(u, []uint32{7})
	if err != nil {
		t.Fatal(err)
	}
	exportComputedReads(e, []program.ExprID{read})
	steps := []uint32{0, 1, 3, 2}
	seed := uint32(19)
	for range 200 {
		seed = seed*1664525 + 1013904223
		steps = append(steps, (seed>>16)%4)
	}
	var got [][]int64
	runExpressionModule(t, e.module, `
  const api = instance.exports, states = [];
  api.begin(0, 0, 1);
  for (let slot = 0; slot < data.Initial.length; slot++) {
    memory.set(Buffer.from(data.Initial[slot], 'base64'), 32768); api.store(slot, 32768);
  }
  if (api.initialize() || api.commit(0, 0)) throw new Error('initialization');
  for (let index = 0; index < data.Steps.length; index++) {
    const handler = ['handler','other','handler2','handler3'][data.Steps[index]];
    if (api.begin(index + 1, 0, 0) || api[handler](7)) throw new Error('handler');
    const base = api.working();
    states.push([view.getUint32(base + 4, true), ...[1,2,3].map(slot => view.getInt32(base + slot * 24 + 8, true))]);
    if (api.commit(index + 1, 0)) throw new Error('commit');
  }
  process.stdout.write(JSON.stringify(states));`, struct {
		Initial []string
		Steps   []uint32
	}{[]string{
		scalarTransport(program.TypeBool, 2, 0, ""), scalarTransport(program.TypeInt, 0, 10, ""),
		scalarTransport(program.TypeInt, 0, 20, ""), scalarTransport(program.TypeInt, 0, 0, "")}, steps}, &got)
	var want [][]int64
	observedRead := addExpression(&u, program.OpSignalGet, program.TypeInt, Int, "observed")
	// The read is test-only and has no effect on the already compiled module.
	u = refreshUnit(t, u)
	model := vm.NewVM(u.Program, nil)
	vm.InitSignals(model, u.Program)
	for _, step := range steps {
		signal.Batch(func() {
			for _, expr := range u.Program.Handlers[step].Body {
				model.Eval(expr)
			}
		})
		flags := int64(0)
		if model.Eval(flag).Truth() {
			flags = 2
		}
		want = append(want, []int64{flags, int64(model.Eval(left).Number()), int64(model.Eval(right).Number()), int64(model.Eval(observedRead).Number())})
	}
	if !reflect.DeepEqual(got, want) {
		for i := range want {
			if !reflect.DeepEqual(got[i], want[i]) {
				t.Fatalf("dynamic trace %d: %v want %v", i, got[i], want[i])
			}
		}
	}
}

func TestEmitComputedSharedPeersMatchNativeVM(t *testing.T) {
	u, reads := computedBatchUnit(t, false)
	u.Program.Signals[0].Name, u.Contract.Signals[0].Name = "$count", "$count"
	for i := range u.Program.Exprs {
		expr := &u.Program.Exprs[i]
		if (expr.Op == program.OpSignalGet || expr.Op == program.OpSignalSet) && expr.Value == "count" {
			expr.Value = "$count"
		}
	}
	u = refreshUnit(t, u)
	e, err := emitStateExpressions(u, []uint32{42, 7})
	if err != nil {
		t.Fatal(err)
	}
	exportComputedReads(e, reads)
	var got [][]int64
	runExpressionModule(t, e.module, `
  const api = instance.exports, states = [];
  api.begin(0, 0, 1);
  for (let slot = 0; slot < data.length; slot++) {
    memory.set(Buffer.from(data[slot], 'base64'), 32768); api.store(slot, 32768);
  }
  if (api.initialize() || api.commit(0, 0)) throw new Error('initialization');
  for (const [index, id] of [7,42,7,42].entries()) {
    if (api.begin(index + 1, 0, 0) || api.handler(id)) throw new Error('shared handler');
    states.push([0,1,2].map(slot => view.getInt32(api.working() + slot * 24 + 8, true)));
    if (api.commit(index + 1, 0)) throw new Error('commit');
  }
  process.stdout.write(JSON.stringify(states));`, []string{scalarTransport(program.TypeInt, 0, 0, ""), scalarTransport(program.TypeInt, 0, 0, ""), scalarTransport(program.TypeInt, 0, 10, "")}, &got)
	shared := signal.New(vm.IntVal(10))
	models := []*vm.VM{vm.NewVM(u.Program, nil), vm.NewVM(u.Program, nil)}
	for _, model := range models {
		vm.InitSignals(model, u.Program)
		model.SetSignal("$count", shared)
	}
	var want [][]int64
	for _, frame := range []int{0, 1, 0, 1} {
		signal.Batch(func() {
			for _, expr := range u.Program.Handlers[0].Body {
				models[frame].Eval(expr)
			}
		})
		want = append(want, []int64{int64(models[0].Eval(reads[1]).Number()), int64(models[1].Eval(reads[1]).Number()), int64(shared.Get().Number())})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("shared computed trace: %v want %v", got, want)
	}
}

func TestEmitComputedNotificationFaultAbortsWholeTransaction(t *testing.T) {
	u, reads := computedBatchUnit(t, false)
	addComputed(&u, "copy", Int, reads[2])
	u = refreshUnit(t, u)
	e, err := emitStateExpressions(u, []uint32{7})
	if err != nil {
		t.Fatal(err)
	}
	exportComputedReads(e, reads)
	var got []uint32
	runExpressionModule(t, e.module, `
  const api = instance.exports, result = [];
  api.begin(0, 0, 1);
  for (let slot = 0; slot < data.length; slot++) {
    memory.set(Buffer.from(data[slot], 'base64'), 32768); api.store(slot, 32768);
  }
  result.push(api.initialize(),api.commit(0,0),api.begin(1,0,0),api.handler(7),api.commit(1,0));
  result.push(view.getUint32(api.committed()+8,true),view.getUint32(api.committed()+32,true));
  result.push(api.abort(),api.begin(2,0,0),api.other(7),api.commit(2,0));
  result.push(view.getUint32(api.committed()+8,true),view.getUint32(17408,true));
  process.stdout.write(JSON.stringify(result));`, []string{scalarTransport(program.TypeInt, 0, 1073741823, ""), scalarTransport(program.TypeInt, 0, 0, "")}, &got)
	if !reflect.DeepEqual(got, []uint32{0, 0, 0, 3, 3, 1073741823, 0, 0, 0, 0, 0, 1073741823, 1}) {
		t.Fatalf("notification fault: %v", got)
	}
}
