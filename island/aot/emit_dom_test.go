package aot

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

func exportDOMModule(e *expressionEmitter) {
	exportStateModule(e)
	e.module.Exports = append(e.module.Exports, wasmgen.Export{Name: "bind", Function: e.dom.bind}, wasmgen.Export{Name: "render", Function: e.dom.render})
	if e.computed != nil {
		e.module.Exports = append(e.module.Exports, wasmgen.Export{Name: "initialize", Function: e.computed.initialize})
	}
}

const domTestImports = `{input: unexpected,
  bind: (id, binding, kind, tag) => { bound.push([id, binding, kind, tag >>> 0]); return binding === data.BadBind ? data.BindStatus : 0; },
  patch: (id, kind, binding, attribute, pointer) => {
    const descriptor = data.Bindings.bindings[binding];
    const patch = {kind, path: descriptor.path};
    if (kind === 2) { if (pointer) throw new Error('remove reads a value'); }
    else {
      if (view.getUint32(pointer, true) !== 0 || view.getUint32(pointer + 4, true) !== 1) throw new Error('unformatted patch');
      const start = view.getUint32(pointer + 16, true), length = view.getUint32(pointer + 20, true);
      const text = Buffer.from(memory.subarray(start, start + length)).toString('utf8');
      if (text) patch.text = text;
    }
    if (attribute !== -1) patch.attrName = data.Bindings.attributes[attribute];
    patches.push(patch);
    return data.PatchStatus || 0;
  }} `

type domTestData struct {
	Bindings                BindingSet
	Roots                   []string
	BadBind                 int
	BindStatus, PatchStatus int
}

func domData(e *expressionEmitter) domTestData {
	data := domTestData{Bindings: e.dom.bindings, BadBind: -1}
	for range e.state.mutableRoots {
		data.Roots = append(data.Roots, scalarTransport(program.TypeInt, 0, 0, ""))
	}
	return data
}

func TestEmitDOMPatchesMatchNativeVM(t *testing.T) {
	u := fixedBindingUnit(t)
	e, err := emitDOMExpressions(u, []uint32{7})
	if err != nil {
		t.Fatal(err)
	}
	exportDOMModule(e)
	var got struct {
		Bound    [][]uint32
		Patches  [][]vm.PatchOp
		Statuses []uint32
	}
	runExpressionModule(t, e.module, `
  const api = instance.exports, bound = [], patches = [], batches = [], statuses = [];
  statuses.push(api.begin(0, 0, 1),api.bind(7));
  for (let slot = 0; slot < data.Roots.length; slot++) {
    memory.set(Buffer.from(data.Roots[slot], 'base64'),32768); statuses.push(api.store(slot,32768));
  }
  statuses.push(api.render(7,1),api.commit(0,0));
  if (patches.length) throw new Error('baseline initialization emitted patches');
  for (let seq = 1; seq <= 3; seq++) {
    statuses.push(api.begin(seq,0,0),api.handler(7),api.render(7,0));
    batches.push(patches.splice(0));
    statuses.push(api.render(7,0));
    if (patches.length) throw new Error('repeated render emitted patches');
    statuses.push(api.commit(seq,0));
  }
  process.stdout.write(JSON.stringify({Bound:bound,Patches:batches,Statuses:statuses}));`, domData(e), &got, domTestImports)
	for _, status := range got.Statuses {
		if status != 0 {
			t.Fatalf("DOM status: %+v", got)
		}
	}
	for i, binding := range e.dom.bindings.Bindings {
		want := []uint32{7, binding.ID, uint32(binding.Kind), binding.TagID}
		if !reflect.DeepEqual(got.Bound[i], want) {
			t.Fatalf("binding %d: %v want %v", i, got.Bound[i], want)
		}
	}
	model := vm.NewIsland(u.Program, "")
	for i, patches := range got.Patches {
		want := model.Dispatch("increment", "")
		if !reflect.DeepEqual(patches, want) {
			t.Fatalf("patches %d: %+v want %+v", i, patches, want)
		}
	}
}

func boolDOMUnit(t *testing.T) Unit {
	u := staticUnit(t)
	yes := addExpression(&u, program.OpLitBool, program.TypeBool, Bool, "true")
	no := addExpression(&u, program.OpLitBool, program.TypeBool, Bool, "false")
	text := addExpression(&u, program.OpLitString, program.TypeString, String, "héllo\x00🌴")
	other := addExpression(&u, program.OpLitString, program.TypeString, String, "e\u0301")
	for i, def := range []struct {
		name string
		kind ScalarKind
		init program.ExprID
	}{{"enabled", Bool, yes}, {"text", String, text}} {
		u.Program.Signals = append(u.Program.Signals, program.SignalDef{Name: def.name, Type: u.Program.Exprs[def.init].Type, Init: def.init})
		u.Contract.Signals = append(u.Contract.Signals, StateContract{Slot: uint32(i), Name: def.name, Kind: def.kind})
	}
	flag := addExpression(&u, program.OpSignalGet, program.TypeBool, Bool, "enabled")
	read := addExpression(&u, program.OpSignalGet, program.TypeString, String, "text")
	set := func(name string, value program.ExprID) program.ExprID {
		id := addExpression(&u, program.OpSignalSet, program.TypeAny, AnyZero, name, value)
		u.Contract.Expressions[id].Pure = false
		return id
	}
	u.Program.Handlers = []program.Handler{{Name: "off", Body: []program.ExprID{set("enabled", no), set("text", other)}}, {Name: "on", Body: []program.ExprID{set("enabled", yes), set("text", text)}}}
	u.Program.Nodes = []program.Node{{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1, 2}},
		{Kind: program.NodeElement, Tag: "input", Attrs: []program.Attr{{Kind: program.AttrExpr, Name: "disabled", Expr: flag}, {Kind: program.AttrExpr, Name: "value", Expr: read}, {Kind: program.AttrExpr, Name: "checked", Expr: flag}, {Kind: program.AttrExpr, Name: "data-enabled", Expr: flag}}},
		{Kind: program.NodeExpr, Expr: read}}
	return refreshBindingUnit(t, u)
}

func TestEmitDOMBooleanRemovalOrderAndUnicode(t *testing.T) {
	u := boolDOMUnit(t)
	e, err := emitDOMExpressions(u, []uint32{7})
	if err != nil {
		t.Fatal(err)
	}
	exportDOMModule(e)
	data := domData(e)
	data.Roots = []string{scalarTransport(program.TypeBool, 2, 0, ""), scalarTransport(program.TypeString, 1, 0, "héllo\x00🌴")}
	var got [][]vm.PatchOp
	runExpressionModule(t, e.module, `
  const api = instance.exports, bound = [], patches = [], batches = [];
  api.begin(0,0,1); api.bind(7);
  for (let slot = 0; slot < data.Roots.length; slot++) {
    memory.set(Buffer.from(data.Roots[slot],'base64'),32768); api.store(slot,32768);
  }
  if (api.render(7,1) || api.commit(0,0)) throw new Error('initialization');
  for (const [seq, handler] of [[1,'handler'],[2,'other'],[3,'handler']]) {
    if (api.begin(seq,0,0) || api[handler](7) || api.render(7,0)) throw new Error('render');
    batches.push(patches.splice(0));
    if (api.commit(seq,0)) throw new Error('commit');
  }
  process.stdout.write(JSON.stringify(batches));`, data, &got, domTestImports)
	model := vm.NewIsland(u.Program, "")
	for i, name := range []string{"off", "on", "off"} {
		want := model.Dispatch(name, "")
		if !reflect.DeepEqual(got[i], want) {
			t.Fatalf("boolean/Unicode batch %d: %+v want %+v", i, got[i], want)
		}
	}
	if len(got[0]) != 5 || got[0][0].Kind != vm.PatchSetValue || got[0][2].Kind != vm.PatchRemoveAttr || got[0][3].AttrName != "checked" {
		t.Fatalf("removal order: %+v", got[0])
	}
}

func TestEmitDOMImportFailuresPreserveCommittedBaselines(t *testing.T) {
	for _, status := range []int{-6, 6, 42, -2147483648} {
		u := fixedBindingUnit(t)
		e, err := emitDOMExpressions(u, []uint32{7})
		if err != nil {
			t.Fatal(err)
		}
		exportDOMModule(e)
		data := domData(e)
		data.BadBind = 2
		data.BindStatus = status
		var got []uint32
		runExpressionModule(t, e.module, `
  const api = instance.exports, bound = [], patches = [];
  api.begin(0,0,1);
  const result = [api.bind(7),api.commit(0,0),bound.length,api.abort()];
  process.stdout.write(JSON.stringify(result));`, data, &got, domTestImports)
		if !reflect.DeepEqual(got, []uint32{6, 6, 3, 0}) {
			t.Fatalf("bind status %d: %v", status, got)
		}
	}
	u := fixedBindingUnit(t)
	e, err := emitDOMExpressions(u, []uint32{7})
	if err != nil {
		t.Fatal(err)
	}
	exportDOMModule(e)
	data := domData(e)
	data.PatchStatus = -2
	var got []uint32
	runExpressionModule(t, e.module, `
  const api = instance.exports, bound = [], patches = [];
  api.begin(0,0,1);
  for (let slot = 0; slot < data.Roots.length; slot++) {
    memory.set(Buffer.from(data.Roots[slot],'base64'),32768); api.store(slot,32768);
  }
  api.render(7,1); api.commit(0,0); api.begin(1,0,0); api.handler(7);
  const before = Buffer.from(memory.subarray(api.committed(),api.committed()+data.Roots.length*24)).toString('base64');
  const result = [api.render(7,0),api.commit(1,0),patches.length,api.abort()];
  if (Buffer.from(memory.subarray(api.committed(),api.committed()+data.Roots.length*24)).toString('base64') !== before) throw new Error('committed roots changed');
  if (api.begin(2,0,0)) throw new Error('begin');
  data.PatchStatus=0;
  result.push(api.handler(7),api.render(7,0),api.commit(2,0));
  process.stdout.write(JSON.stringify(result));`, data, &got, domTestImports)
	if !reflect.DeepEqual(got, []uint32{2, 2, 1, 0, 0, 0, 0}) {
		t.Fatalf("patch failure: %v", got)
	}
}

func TestEmitDOMPatchLimitIsExactAndTransactional(t *testing.T) {
	for _, count := range []int{128, 129} {
		u := stateCounterUnit(t)
		for i := 0; i < count; i++ {
			u.Program.Nodes[0].Attrs = append(u.Program.Nodes[0].Attrs, program.Attr{Kind: program.AttrExpr, Name: "data-count-" + strconv.Itoa(i), Expr: 3})
		}
		u = refreshBindingUnit(t, u)
		e, err := emitDOMExpressions(u, []uint32{7})
		if err != nil {
			t.Fatal(err)
		}
		exportDOMModule(e)
		var got []uint32
		runExpressionModule(t, e.module, `
  const api = instance.exports, bound = [], patches = [];
  api.begin(0,0,1);
  for (let slot = 0; slot < data.Roots.length; slot++) {
    memory.set(Buffer.from(data.Roots[slot],'base64'),32768); api.store(slot,32768);
  }
  if (api.render(7,1) || api.commit(0,0) || api.begin(1,0,0) || api.handler(7)) throw new Error('initialization');
  const status=api.render(7,0), committed=api.commit(1,0);
  const result=[status,committed,patches.length,view.getUint32(api.committed()+8,true),api.abort()];
  process.stdout.write(JSON.stringify(result));`, domData(e), &got, domTestImports)
		want := []uint32{0, 0, 128, 1, 0}
		if count == 129 {
			want = []uint32{9, 9, 128, 0, 0}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("patch limit %d: %v want %v", count, got, want)
		}
	}
}

func TestEmitDOMErasedBooleanDefaultRetainsVMAttributeText(t *testing.T) {
	u := staticUnit(t)
	no := addExpression(&u, program.OpLitBool, program.TypeBool, Bool, "false")
	yes := addExpression(&u, program.OpLitBool, program.TypeBool, Bool, "true")
	u.Program.Signals = []program.SignalDef{{Name: "enabled", Type: program.TypeBool, Init: no}}
	u.Contract.Signals = []StateContract{{Slot: 0, Name: "enabled", Kind: Bool}}
	later := addExpression(&u, program.OpSignalGet, program.TypeAny, Bool, "later")
	addComputed(&u, "forward", Bool, later)
	flag := addExpression(&u, program.OpSignalGet, program.TypeBool, Bool, "enabled")
	addComputed(&u, "later", Bool, flag)
	forward := addExpression(&u, program.OpSignalGet, program.TypeBool, Bool, "forward")
	write := addExpression(&u, program.OpSignalSet, program.TypeAny, AnyZero, "enabled", yes)
	u.Contract.Expressions[write].Pure = false
	u.Program.Handlers = []program.Handler{{Name: "enable", Body: []program.ExprID{write}}}
	u.Program.Nodes[0].Attrs = []program.Attr{{Kind: program.AttrExpr, Name: "disabled", Expr: forward}, {Kind: program.AttrExpr, Name: "hidden", Expr: later}}
	u = refreshBindingUnit(t, u)
	e, err := emitDOMExpressions(u, []uint32{7})
	if err != nil {
		t.Fatal(err)
	}
	exportDOMModule(e)
	data := domData(e)
	data.Roots = []string{scalarTransport(program.TypeBool, 0, 0, "")}
	var got []vm.PatchOp
	runExpressionModule(t, e.module, `
  const api=instance.exports,bound=[],patches=[];
  api.begin(0,0,1);memory.set(Buffer.from(data.Roots[0],'base64'),32768);api.store(0,32768);
  if(api.initialize()||api.render(7,1)||api.commit(0,0)||api.begin(1,0,0)||api.handler(7)||api.render(7,0)) throw new Error('render');
  const baseline=api.working()+3*24;
  if(view.getUint32(baseline,true)!==0 || view.getUint32(baseline+20,true)!==1 || memory[view.getUint32(baseline+16,true)]!==48) throw new Error('erased default lost text');
  if(api.commit(1,0)) throw new Error('commit');
  process.stdout.write(JSON.stringify(patches));`, data, &got, domTestImports)
	model := vm.NewIsland(u.Program, "")
	want := model.Dispatch("enable", "")
	if !reflect.DeepEqual(got, want) || len(got) != 1 || got[0].AttrName != "hidden" || got[0].Text != "" {
		t.Fatalf("erased boolean: %+v want %+v", got, want)
	}
}

func TestEmitDOMRepeatedLargeValuesReclaimArena(t *testing.T) {
	u := boolDOMUnit(t)
	large := strings.Repeat("é", 2048)
	u.Program.Exprs[2].Value = large
	u = refreshBindingUnit(t, u)
	e, err := emitDOMExpressions(u, []uint32{7})
	if err != nil {
		t.Fatal(err)
	}
	exportDOMModule(e)
	data := domData(e)
	data.Roots = []string{scalarTransport(program.TypeBool, 2, 0, ""), scalarTransport(program.TypeString, 1, 0, large)}
	var got []uint32
	runExpressionModule(t, e.module, `
  const api=instance.exports,bound=[],patches=[];
  api.begin(0,0,1);
  for(let slot=0;slot<data.Roots.length;slot++){memory.set(Buffer.from(data.Roots[slot],'base64'),32768);api.store(slot,32768);}
  if(api.render(7,1)||api.commit(0,0)) throw new Error('initialization');
  for(let seq=1;seq<=200;seq++){
    if(api.begin(seq,0,0)||api[seq%2?'handler':'other'](7)||api.render(7,0)||api.commit(seq,0)) throw new Error('large update '+seq);
    patches.length=0;
  }
  process.stdout.write(JSON.stringify([api.status(),memory.byteLength,view.getUint32(api.committed()+24+20,true)]));`, data, &got, domTestImports)
	if !reflect.DeepEqual(got, []uint32{0, 196608, 4096}) {
		t.Fatalf("DOM arena reclamation: %v", got)
	}
}
