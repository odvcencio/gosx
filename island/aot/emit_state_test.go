package aot

import (
	"encoding/base64"
	"reflect"
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
		if i > 0 {
			name = "other"
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
	b.i32(17412)
	b.i32(17408)
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
	b.i32(17408)
	b.i32(17408)
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
    view.setUint32(17408, 0, true);
    statuses.push(api.handler(instanceID));
    for (let item = 0; item < view.getUint32(17408, true); item++) {
      const pointer = 17412 + item * 28;
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
  view.setUint32(17408, 0, true);
  const status = api.handler(7);
  const results = [status, api.commit(1, 0), view.getUint32(17408, true),
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
