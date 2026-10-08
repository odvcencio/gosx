package aot

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

func layoutUnit(t *testing.T, name string, locals, computeds, fields int, shared ...string) Unit {
	t.Helper()
	u := literalUnit(t)
	u.Component = "example/components." + name
	u.Contract.Component = u.Component
	u.Program.Name = name
	for i := range locals + len(shared) {
		name := "local" + strconv.Itoa(i)
		if i >= locals {
			name = shared[i-locals]
		}
		u.Program.Signals = append(u.Program.Signals, program.SignalDef{Name: name, Type: program.TypeInt, Init: 0})
		u.Contract.Signals = append(u.Contract.Signals, StateContract{Slot: uint32(i), Name: name, Kind: Int})
	}
	for i := range computeds {
		name := "computed" + strconv.Itoa(i)
		u.Program.Computeds = append(u.Program.Computeds, program.ComputedDef{Name: name, Type: program.TypeInt, Expr: 0})
		u.Contract.Computeds = append(u.Contract.Computeds, StateContract{Slot: uint32(i), Name: name, Kind: Int})
	}
	if fields != 0 {
		u.Program.StaticMask[0] = false
		for range fields {
			id := program.NodeID(len(u.Program.Nodes))
			u.Program.Nodes[0].Children = append(u.Program.Nodes[0].Children, id)
			// An element separates expression text into distinct DOM fields.
			u.Program.Nodes = append(u.Program.Nodes, program.Node{Kind: program.NodeElement, Tag: "span", Children: []program.NodeID{id + 1}}, program.Node{Kind: program.NodeExpr, Expr: 0})
			u.Program.StaticMask = append(u.Program.StaticMask, false, false)
		}
	}
	bindings, err := ContractBindings(u.Program)
	if err != nil {
		t.Fatal(err)
	}
	u.Contract.Bindings = bindings
	return refreshUnit(t, u)
}

func linkedProgramByName(t *testing.T, layout *linkedLayout, name string) *linkedProgram {
	t.Helper()
	for i := range layout.programs {
		if layout.programs[i].unit.Program.Name == name {
			return &layout.programs[i]
		}
	}
	t.Fatal("missing linked program")
	return nil
}

func TestLinkedLayoutCanonicalSetAndFrameIsolation(t *testing.T) {
	a := layoutUnit(t, "A", 2, 1, 1, "$same", "$A")
	b := layoutUnit(t, "B", 1, 2, 2, "$same", "$a")
	first, err := buildLinkedLayout([]Unit{b, a, b}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildLinkedLayout([]Unit{a, b}, DefaultOptions())
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("insertion order or duplicates changed layout: %v", err)
	}
	if first.localStride != 2 || first.computedStride != 2 || first.baselineStride != 2 || first.roots != 99 {
		t.Fatalf("frame banks: %+v", first)
	}
	if first.inputBase != 32 || first.sharedBase != 32 || first.computedBase != 35 || first.baselineBase != 67 {
		t.Fatal("frame bank boundaries differ")
	}
	if len(first.programs) != 2 || len(first.shared) != 3 || first.shared[0].name != "$A" || first.shared[1].name != "$a" || first.shared[2].name != "$same" {
		t.Fatal("exact shared identities or ordering changed")
	}
	for _, name := range []string{"A", "B"} {
		p := linkedProgramByName(t, first, name)
		if len(p.state.instances) != 16 || p.state.mutableRoots != 35 || p.state.roots != 99 || p.dom.rootBase != 67 {
			t.Fatal("program did not use full shared frame capacity")
		}
		for frame := range 16 {
			row := p.state.rows[frame*len(p.unit.Program.Signals) : (frame+1)*len(p.unit.Program.Signals)]
			if row[0] != uint32(frame*2) || row[len(row)-2] != 34 {
				t.Fatalf("%s frame %d: %v", name, frame, row)
			}
			if name == "A" && (row[1] != uint32(frame*2+1) || row[3] != 32) || name == "B" && row[2] != 33 {
				t.Fatal("source-order local or shared slots changed")
			}
		}
	}
	a.Program.Signals[0].Name = "changed"
	a.Contract.Signals[0].Name = "changed"
	if linkedProgramByName(t, first, "A").unit.Program.Signals[0].Name != "local0" {
		t.Fatal("layout borrowed source tables")
	}
}

func TestLinkedLayoutTagIDsCoverEntireSet(t *testing.T) {
	a, b := layoutUnit(t, "Div", 0, 0, 1), layoutUnit(t, "Input", 0, 0, 0)
	b.Program.Nodes[0].Tag = "input"
	b.Contract.Bindings[0].Tag = "input"
	b = refreshUnit(t, b)
	l, err := buildLinkedLayout([]Unit{b, a}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(l.tags, []string{"div", "input", "span"}) {
		t.Fatalf("linked tags: %v", l.tags)
	}
	for _, p := range l.programs {
		if !reflect.DeepEqual(p.dom.bindings.Tags, l.tags) {
			t.Fatal("program descriptor has a private tag table")
		}
		for i, binding := range p.dom.bindings.Bindings {
			if binding.Kind == program.NodeText {
				if binding.TagID != NoBindingName {
					t.Fatal("text acquired a tag")
				}
				continue
			}
			if l.tags[binding.TagID] != p.unit.Contract.Bindings[i].Tag {
				t.Fatal("tag ID does not address the linked table")
			}
		}
	}
}

func TestLinkedLayoutKeepsEventInputsOutOfCommittedRoots(t *testing.T) {
	u := layoutUnit(t, "Inputs", 1, 0, 0)
	for _, def := range []struct {
		source, name string
		typ          program.ExprType
		kind         ScalarKind
	}{{"event", "value", program.TypeString, String}, {"prop", "count", program.TypeInt, Int}, {"prop", "title", program.TypeString, String}} {
		op := program.OpEventGet
		if def.source == "prop" {
			op = program.OpPropGet
			u.Program.Props = append(u.Program.Props, program.PropDef{Name: def.name, Type: def.typ})
		}
		id := addExpression(&u, op, def.typ, def.kind, def.name)
		if def.source == "event" {
			u.Program.Handlers = append(u.Program.Handlers, program.Handler{Name: "input", Body: []program.ExprID{id}})
		}
		u.Contract.Inputs = append(u.Contract.Inputs, InputContract{ID: uint32(len(u.Contract.Inputs)), Source: def.source, Root: def.name, Kind: def.kind, Exprs: []program.ExprID{id}})
	}
	u = refreshUnit(t, u)
	l, err := buildLinkedLayout([]Unit{u}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if l.localStride != 1 || l.inputStride != 2 || l.roots != 48 || len(l.programs[0].inputs) != 48 {
		t.Fatalf("input bank: %+v", l)
	}
	for frame := range 16 {
		want := []uint32{NoBindingName, uint32(16 + frame*2), uint32(17 + frame*2)}
		if !reflect.DeepEqual(l.programs[0].inputs[frame*3:(frame+1)*3], want) {
			t.Fatal("input IDs were renumbered or aliased between frames")
		}
	}
}

func TestLinkedLayoutCombinedRootBoundary(t *testing.T) {
	u := layoutUnit(t, "AtLimit", 16, 15, 1)
	l, err := buildLinkedLayout([]Unit{u}, DefaultOptions())
	if err != nil || l.roots != 512 || l.frameTable != 25088 || l.tablesEnd != 25344 {
		t.Fatalf("exact root/table boundary: %+v %v", l, err)
	}
	// Separate valid programs can exceed the combined fixed-bank allowance.
	a, b := layoutUnit(t, "Locals", 16, 0, 0), layoutUnit(t, "Caches", 0, 16, 1)
	for _, unit := range []Unit{a, b} {
		if receipt := Classify(unit, ScalarDOMV1); !receipt.Eligible {
			t.Fatalf("fixture is not independently eligible: %+v", receipt)
		}
	}
	if _, err := buildLinkedLayout([]Unit{a, b}, DefaultOptions()); err == nil {
		t.Fatal("admitted combined root capacity above 512")
	}
	options := DefaultOptions()
	options.Limits.Instances = 1
	if _, err := buildLinkedLayout([]Unit{a, b}, options); err == nil {
		t.Fatal("route instance guard narrowed standalone compiler proof")
	}
}

func TestLinkedLayoutSharedIdentityCompatibilityAndMaximum(t *testing.T) {
	var units []Unit
	for group := range 5 {
		var names []string
		for i := range 16 {
			names = append(names, "$group"+strconv.Itoa(group)+"-"+strconv.Itoa(i))
		}
		units = append(units, layoutUnit(t, "Group"+strconv.Itoa(group), 0, 0, 0, names...))
	}
	l, err := buildLinkedLayout(units[:4], DefaultOptions())
	if err != nil || len(l.shared) != 64 || l.roots != 64 || l.tablesEnd != 18688 {
		t.Fatalf("shared maximum: %+v %v", l, err)
	}
	if _, err := buildLinkedLayout(units, DefaultOptions()); err == nil {
		t.Fatal("admitted more than 64 exact shared names")
	}
	a, b := layoutUnit(t, "Wide", 0, 0, 0, "$count"), layoutUnit(t, "Narrow", 0, 0, 0, "$count")
	b.Contract.Signals[0].Kind = Int32
	b = refreshUnit(t, b)
	if receipt := Classify(b, ScalarDOMV1); !receipt.Eligible {
		t.Fatalf("narrow fixture is not eligible: %+v", receipt)
	}
	if _, err := buildLinkedLayout([]Unit{a, b}, DefaultOptions()); err == nil {
		t.Fatal("admitted incompatible source declarations for one shared name")
	}
}

func TestLinkedLayoutPreservesProgramLocalBindingAndHandlerIDs(t *testing.T) {
	a := fixedBindingUnit(t)
	b := staticUnit(t)
	b.Component, b.Contract.Component = "example/components.Article", "example/components.Article"
	b.Program.Name, b.Program.Nodes[0].Tag = "Article", "article"
	b = refreshBindingUnit(t, b)
	l, err := buildLinkedLayout([]Unit{a, b}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range l.programs {
		want, err := BuildBindings(p.unit)
		if err != nil {
			t.Fatal(err)
		}
		for i, binding := range p.dom.bindings.Bindings {
			// Only tag IDs change when the name table grows to the page set.
			binding.TagID = want.Bindings[i].TagID
			if !reflect.DeepEqual(binding, want.Bindings[i]) {
				t.Fatalf("program-local binding %d changed: %+v", i, binding)
			}
		}
	}
	if l.tags[0] != "article" || l.tags[1] != "button" {
		t.Fatal("fixture did not shift existing tag IDs")
	}
}

func TestLinkedLayoutAllowsFullCatalogWithoutActiveInstances(t *testing.T) {
	var units []Unit
	for i := range 16 {
		units = append(units, namedUnit(t, i))
	}
	l, err := buildLinkedLayout(units, DefaultOptions())
	if err != nil || len(l.programs) != 16 || l.roots != 0 || l.tablesEnd != 17664 {
		t.Fatalf("static catalog maximum: %+v %v", l, err)
	}
	for _, p := range l.programs {
		if len(p.state.rows) != 0 || len(p.inputs) != 0 || len(p.state.instances) != 16 {
			t.Fatal("unused catalog program required a mutable root or lost frame capacity")
		}
	}
}

func scalarInputUnit(t *testing.T, source string, kind ScalarKind, nested bool) (Unit, program.ExprID) {
	t.Helper()
	u := staticUnit(t)
	typ := program.TypeString
	if integerKind(kind) {
		typ = program.TypeInt
	} else if kind == Bool {
		typ = program.TypeBool
	}
	root, path := "value", []string{}
	op := program.OpEventGet
	if source == "prop" {
		op, root = program.OpPropGet, "Value"
		u.Program.Props = []program.PropDef{{Name: root, Type: typ}}
	}
	var leaf program.ExprID
	if nested {
		root, path = "props", []string{"detail", "value"}
		u.Program.Props = nil
		prefix := addExpression(&u, program.OpPropGet, program.TypeAny, SelectorPath, root)
		key := addExpression(&u, program.OpLitString, program.TypeString, String, path[0])
		prefix = addExpression(&u, program.OpIndex, program.TypeAny, SelectorPath, "", prefix, key)
		key = addExpression(&u, program.OpLitString, program.TypeString, String, path[1])
		leaf = addExpression(&u, program.OpIndex, typ, kind, "", prefix, key)
	} else {
		leaf = addExpression(&u, op, typ, kind, root)
	}
	u.Contract.Inputs = []InputContract{{Source: source, Root: root, Path: path, Kind: kind, Exprs: []program.ExprID{leaf}}}
	if source == "event" {
		if typ == program.TypeInt {
			u.Program.Exprs[leaf].Value, u.Contract.Inputs[0].Root = "selectedIndex", "selectedIndex"
		} else if typ == program.TypeBool {
			u.Program.Exprs[leaf].Value, u.Contract.Inputs[0].Root = "checked", "checked"
		}
		u.Program.Handlers = []program.Handler{{Name: "read", Body: []program.ExprID{leaf}}}
	}
	return refreshUnit(t, u), leaf
}

func inputTestModule(t *testing.T, u Unit, leaf program.ExprID) *expressionEmitter {
	t.Helper()
	e, err := emitStateExpressions(u, []uint32{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	exportStateModule(e)
	e.module.Exports = append(e.module.Exports, wasmgen.Export{Name: "read", Function: e.functions[leaf]})
	return e
}

func TestEmitImmutableInputsKeepTagsDefaultsAndFrameOwnership(t *testing.T) {
	for _, tc := range []struct {
		kind   ScalarKind
		value  vm.Value
		absent vm.Value
		nested bool
	}{{Int, vm.IntVal(-2147483648), vm.ZeroValue(program.TypeInt), false},
		{Bool, vm.BoolVal(false), vm.ZeroValue(program.TypeBool), false},
		{String, vm.StringVal("héllo\x00🌴"), vm.ZeroValue(program.TypeString), false},
		{String, vm.StringVal(""), vm.ZeroValue(program.TypeString), false},
		{String, vm.StringVal("e\u0301"), vm.ZeroValue(program.TypeAny), true}} {
		u, leaf := scalarInputUnit(t, "prop", tc.kind, tc.nested)
		e := inputTestModule(t, u, leaf)
		for i, proof := range u.Contract.Expressions {
			if proof.Kind == SelectorPath && e.functions[i] != NoBindingName {
				t.Fatal("aggregate selector prefix has a runtime function")
			}
		}
		encode := func(value vm.Value, present bool) string {
			flags := uint32(0)
			if value.Type == program.TypeString && present {
				flags = 1
			} else if value.Type == program.TypeBool && value.Truth() {
				flags = 2
			}
			return scalarTransport(value.Type, flags, int64(value.Number()), value.Text())
		}
		var got struct {
			Statuses []uint32
			Records  []string
			Texts    []string
		}
		runExpressionModule(t, e.module, `
  const api = instance.exports, statuses = [api.begin(0,0,1)], records = [], texts = [];
  for (let frame = 0; frame < 2; frame++) {
    memory.set(Buffer.from(data[frame],'base64'),32768); statuses.push(api.store(frame,32768));
  }
  statuses.push(api.commit(0,0),api.begin(1,0,0));
  for (let frame = 0; frame < 2; frame++) {
    const p = api.read(frame); statuses.push(api.status());
    records.push(Buffer.from(memory.slice(p,p+16)).toString('base64'));
    texts.push(Buffer.from(memory.slice(view.getUint32(p+16,true),view.getUint32(p+16,true)+view.getUint32(p+20,true))).toString('base64'));
  }
  api.abort(); process.stdout.write(JSON.stringify({Statuses:statuses,Records:records,Texts:texts}));`, []string{encode(tc.value, true), encode(tc.absent, false)}, &got)
		for _, status := range got.Statuses {
			if status != 0 {
				t.Fatalf("prop/default status: %+v", got)
			}
		}
		for frame, value := range []vm.Value{tc.value, tc.absent} {
			transport, _ := base64.StdEncoding.DecodeString(encode(value, frame == 0))
			if got.Records[frame] != base64.StdEncoding.EncodeToString(transport[:16]) || got.Texts[frame] != base64.StdEncoding.EncodeToString([]byte(value.Text())) {
				t.Fatalf("prop/default scalar changed: %+v", got)
			}
		}
	}
}

type importedInputCase struct {
	Record string
	Result int32
}

const inputTestImports = `{input: (id, field, dst, cap) => {
  if (id !== 0 || field !== 0 || dst !== 32768 || cap !== 4120) throw new Error('input ownership');
  const entry = data.Cases[data.Index], bytes = Buffer.from(entry.Record,'base64');
  memory.set(bytes,dst); return entry.Result;
}, bind: unexpected, patch: unexpected}`

func TestEmitEventInputFramingUTF8AndScalarGuards(t *testing.T) {
	for _, kind := range []ScalarKind{String, Int, Bool} {
		u, leaf := scalarInputUnit(t, "event", kind, false)
		e := inputTestModule(t, u, leaf)
		var cases []importedInputCase
		var want []uint32
		add := func(record string, result int32, status uint32) {
			cases = append(cases, importedInputCase{record, result})
			want = append(want, status)
		}
		base := scalarTransport(program.TypeString, 1, 0, "héllo\x00🌴")
		if kind == Int {
			base = scalarTransport(program.TypeInt, 0, 2147483647, "")
		} else if kind == Bool {
			base = scalarTransport(program.TypeBool, 2, 0, "")
		}
		raw, _ := base64.StdEncoding.DecodeString(base)
		add(base, int32(len(raw)), 0)
		for _, result := range []int32{-1, -2, -10, -11, -2147483648, 0, 23, int32(len(raw) + 1), 4121} {
			status := uint32(statusBadInput)
			if result < 0 && result >= -10 {
				status = uint32(-result)
			}
			add(base, result, status)
		}
		for _, offset := range []int{0, 4, 8, 16, 20} {
			bad := append([]byte{}, raw...)
			binary.LittleEndian.PutUint32(bad[offset:], ^uint32(0))
			add(base64.StdEncoding.EncodeToString(bad), int32(len(bad)), statusBadInput)
			if kind == String && offset == 20 {
				want[len(want)-1] = statusStringLimit
			} else if kind == Int && offset == 8 {
				want[len(want)-1] = statusIntegerDomain
			}
		}
		if kind == Int {
			add(scalarTransport(program.TypeInt, 0, 2147483648, ""), 24, statusIntegerDomain)
			add(scalarTransport(program.TypeInt, 0, -2147483649, ""), 24, statusIntegerDomain)
		}
		if kind == String {
			for _, text := range []string{"", "\x00", "e\u0301", "🌴", "\xc2\x80", "\xe0\xa0\x80", "\xed\x9f\xbf", "\xf0\x90\x80\x80", "\xf4\x8f\xbf\xbf", "\x80", "\xc0\x80", "\xc1\xbf", "\xe0\x9f\xbf", "\xed\xa0\x80", "\xf0\x8f\xbf\xbf", "\xf4\x90\x80\x80", "\xf5\x80\x80\x80", "\xff", "\xe2\x82", "\xc2a"} {
				status := uint32(0)
				if !utf8.ValidString(text) {
					status = statusBadInput
				}
				add(scalarTransport(program.TypeString, 1, 0, text), int32(valueBytes+len(text)), status)
			}
			add(scalarTransport(program.TypeString, 0, 0, ""), 24, 0)
		}
		var got []uint32
		runExpressionModule(t, e.module, `
  const api = instance.exports, statuses = [];
  if (api.begin(0,0,1) || api.commit(0,0)) throw new Error('initialization');
  for (data.Index = 0; data.Index < data.Cases.length; data.Index++) {
    if (api.begin(data.Index+1,0,0)) throw new Error('begin');
    const pointer = api.read(0), status = api.status(); statuses.push(status);
    if ((status === 0) !== (pointer !== 0)) throw new Error('partial input result');
    if (pointer) {
      const bytes = Buffer.from(data.Cases[data.Index].Record,'base64');
      if (!Buffer.from(memory.slice(pointer,pointer+16)).equals(bytes.subarray(0,16))) throw new Error('input tag or payload changed');
      const p = view.getUint32(pointer+16,true), n = view.getUint32(pointer+20,true);
      memory.fill(165,32768,65536);
      if (n && p < api.working()) throw new Error('retained IO pointer');
      if (!Buffer.from(memory.slice(p,p+n)).equals(bytes.subarray(24))) throw new Error('input bytes changed');
    }
    api.abort();
  }
  process.stdout.write(JSON.stringify(statuses));`, struct {
			Cases []importedInputCase
			Index int
		}{Cases: cases}, &got, inputTestImports)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s event statuses: %v want %v", kind, got, want)
		}
	}
}

func linkedComputedUnit(t *testing.T, name string, count, fields, start int) Unit {
	t.Helper()
	u := layoutUnit(t, name, 1, count, fields)
	u.Program.Exprs[0].Value = strconv.Itoa(start)
	one := addExpression(&u, program.OpLitInt, program.TypeInt, Int, "1")
	local := addExpression(&u, program.OpSignalGet, program.TypeInt, Int, "local0")
	increment := addExpression(&u, program.OpAdd, program.TypeInt, Int, "", local, one)
	write := addExpression(&u, program.OpSignalSet, program.TypeAny, AnyZero, "local0", increment)
	u.Contract.Expressions[write].Pure = false
	u.Program.Handlers = []program.Handler{{Name: "increment", Body: []program.ExprID{write}}}
	previous := local
	for i := range count {
		u.Program.Computeds[i].Expr = addExpression(&u, program.OpAdd, program.TypeInt, Int, "", previous, one)
		previous = addExpression(&u, program.OpSignalGet, program.TypeInt, Int, u.Program.Computeds[i].Name)
	}
	for i := range u.Program.Nodes {
		if u.Program.Nodes[i].Kind == program.NodeExpr {
			u.Program.Nodes[i].Expr = previous
		}
	}
	return refreshBindingUnit(t, u)
}

func TestLinkedProgramUsesPageStridesAndOwnedManifestFrames(t *testing.T) {
	l, err := buildLinkedLayout([]Unit{linkedComputedUnit(t, "Small", 1, 1, 4), linkedComputedUnit(t, "Larger", 2, 2, 9)}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for programID, p := range l.programs {
		e, err := l.emitProgram(uint32(programID))
		if err != nil {
			t.Fatal(err)
		}
		exportDOMModule(e)
		var got struct {
			Patches   [][]vm.PatchOp
			Statuses  []uint32
			Cache     []int32
			Rejected  []uint32
			Unchanged bool
		}
		data := struct {
			domTestData
			ProgramID, OtherID, FrameTable, ComputedBase, BaselineBase uint32
			ComputedCount, ComputedStride, BaselineStride              uint32
			Initial                                                    string
		}{domTestData: domData(e), ProgramID: uint32(programID), OtherID: uint32(1 - programID), FrameTable: l.frameTable,
			ComputedBase: l.computedBase, BaselineBase: l.baselineBase, ComputedCount: p.state.computedCount,
			ComputedStride: l.computedStride, BaselineStride: l.baselineStride}
		initial, _ := strconv.ParseInt(p.unit.Program.Exprs[0].Value, 10, 32)
		data.Initial = scalarTransport(program.TypeInt, 0, initial, "")
		runExpressionModule(t, e.module, `
  const api = instance.exports, bound = [], patches = [], batches = [], statuses = [];
  for (const frame of [0,15]) {
    view.setUint32(data.FrameTable+frame*16,data.ProgramID,true);
    view.setUint32(data.FrameTable+frame*16+4,1,true);
  }
  view.setUint32(data.FrameTable+7*16,data.OtherID,true); view.setUint32(data.FrameTable+7*16+4,1,true);
  statuses.push(api.begin(0,0,1));
  for (const frame of [0,15]) {
    memory.set(Buffer.from(data.Initial,'base64'),32768);
    statuses.push(api.store(frame,32768),api.initialize(frame),api.bind(frame),api.render(frame,1));
  }
  if (patches.length) throw new Error('initial patches');
  statuses.push(api.commit(0,0));
  const cache = [];
  for (const frame of [0,15]) cache.push(view.getInt32(api.committed()+(data.ComputedBase+frame*data.ComputedStride+data.ComputedCount-1)*24+8,true));
  for (const [index,frame] of [0,15,0].entries()) {
    statuses.push(api.begin(index+1,0,0),api.handler(frame),api.render(frame,0),api.commit(index+1,0));
    batches.push(patches.splice(0));
  }
  const before = Buffer.from(memory.slice(api.committed(),api.committed()+data.ComputedBase*24));
  const rejected = [];
  for (const frame of [7,1,16,-1]) {
    statuses.push(api.begin(4,0,0)); rejected.push(api.handler(frame));
    rejected.push(api.commit(4,0)); api.abort();
  }
  const unchanged = before.equals(Buffer.from(memory.slice(api.committed(),api.committed()+data.ComputedBase*24)));
  process.stdout.write(JSON.stringify({Patches:batches,Statuses:statuses,Cache:cache,Rejected:rejected,Unchanged:unchanged}));`, data, &got, domTestImports)
		for _, status := range got.Statuses {
			if status != 0 {
				t.Fatalf("linked program status: %+v", got)
			}
		}
		if !got.Unchanged || !reflect.DeepEqual(got.Cache, []int32{int32(initial) + int32(p.state.computedCount), int32(initial) + int32(p.state.computedCount)}) {
			t.Fatalf("cache stride or committed ownership: %+v", got)
		}
		for _, status := range got.Rejected {
			if status != statusBadInput {
				t.Fatalf("foreign/inactive frame was admitted: %+v", got)
			}
		}
		models := []*vm.Island{vm.NewIsland(p.unit.Program, ""), vm.NewIsland(p.unit.Program, "")}
		for step, model := range []int{0, 1, 0} {
			if want := models[model].Dispatch("increment", ""); !reflect.DeepEqual(got.Patches[step], want) {
				t.Fatalf("linked frame patches: %+v want %+v", got.Patches[step], want)
			}
		}
		if e.reserved != (l.roots+l.expressions)*valueBytes || !bytes.Equal(e.module.Data, l.data) {
			t.Fatal("program used private scratch or constants")
		}
	}
}

func TestLinkedConstantsAreSortedDeduplicatedAlignedAndBounded(t *testing.T) {
	a, b := layoutUnit(t, "First", 1, 0, 0), layoutUnit(t, "Second", 1, 0, 0)
	for _, u := range []*Unit{&a, &b} {
		addExpression(u, program.OpLitString, program.TypeString, String, "héllo\x00🌴")
		addExpression(u, program.OpLitString, program.TypeString, String, "")
	}
	a, b = refreshUnit(t, a), refreshUnit(t, b)
	l, err := buildLinkedLayout([]Unit{b, a}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if c := l.strings[""]; c.pointer != 0 || c.length != 0 {
		t.Fatal("empty constant has storage")
	}
	if bytes.Count(l.data, []byte("héllo\x00🌴")) != 1 {
		t.Fatal("linked literals were not deduplicated")
	}
	var previous int32
	values := map[string]bool{}
	for value := range l.strings {
		values[value] = true
	}
	for _, value := range sortedBindingNames(values) {
		if value == "" {
			continue
		}
		c := l.strings[value]
		if c.pointer < previous || !bytes.Equal(l.data[c.pointer-1024:c.pointer-1024+c.length], []byte(value)) {
			t.Fatal("constant byte order or pointer changed")
		}
		previous = c.pointer + c.length
	}
	for _, p := range l.programs {
		if p.state.dataBase%4 != 0 || p.state.inputDataBase%4 != 0 {
			t.Fatal("descriptor tables are unaligned")
		}
		for i, root := range p.state.rows {
			offset := int(p.state.dataBase) - 1024 + i*4
			if binary.LittleEndian.Uint32(l.data[offset:]) != root {
				t.Fatal("root table does not address the page frame bank")
			}
		}
	}
	for i := 0; i < 5; i++ {
		addExpression(&a, program.OpLitString, program.TypeString, String, strings.Repeat(string(rune('a'+i)), 4096))
	}
	a = refreshUnit(t, a)
	if _, err := buildLinkedLayout([]Unit{a}, DefaultOptions()); err == nil {
		t.Fatal("admitted constants above the 16 KiB segment")
	}
}

func TestLinkedIdenticalRootTablesShareConstantStorage(t *testing.T) {
	var units []Unit
	for i := range 16 {
		units = append(units, layoutUnit(t, "Bank"+strconv.Itoa(i), 16, 0, 0))
	}
	l, err := buildLinkedLayout(units, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range l.programs {
		if p.state.dataBase != l.programs[0].state.dataBase {
			t.Fatal("identical frame tables have separate constant allocations")
		}
	}
	if len(l.data) > 1200 || l.roots != 256 {
		t.Fatalf("deduplicated catalog layout: %d bytes, %d roots", len(l.data), l.roots)
	}
}

func exportLinkedTestModule(c *linkedCode) wasmgen.Module {
	e := &expressionEmitter{module: c.module}
	for i, index := range c.programs[0].transactions {
		e.transactions[i] = c.indices[0][index]
	}
	exportStateModule(e)
	for p, source := range c.programs {
		for name, index := range map[string]uint32{
			"initialize": source.computed.initialize, "bind": source.dom.bind,
			"render": source.dom.render, "handler": source.handlers[0],
		} {
			e.module.Exports = append(e.module.Exports, wasmgen.Export{Name: name + strconv.Itoa(p), Function: c.indices[p][index]})
		}
	}
	return e.module
}

func TestLinkedCodeRunsProgramsAndRepeatedInstancesInOneModule(t *testing.T) {
	units := []Unit{linkedComputedUnit(t, "Small", 1, 1, 4), linkedComputedUnit(t, "Larger", 2, 2, 9)}
	l, err := buildLinkedLayout(units, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	c, err := linkProgramCode(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.module.Imports) != 3 || len(c.module.Exports) != 0 || len(c.module.Globals) != 25 || !bytes.Equal(c.module.Data, l.data) {
		t.Fatal("linked library changed storage or exposed a private function")
	}
	for helper, index := range commonFunctions(c.programs[0]) {
		if c.indices[0][index] != c.indices[1][commonFunctions(c.programs[1])[helper]] {
			t.Fatal("programs retained duplicate helpers")
		}
	}
	otherLayout, err := buildLinkedLayout([]Unit{units[1], units[0], units[1]}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	other, err := linkProgramCode(otherLayout)
	if err != nil || !reflect.DeepEqual(c.module, other.module) {
		t.Fatalf("input order changed linked code: %v", err)
	}
	type testProgram struct {
		Bindings BindingSet
		Initial  string
	}
	data := struct {
		Programs                []testProgram
		FramePrograms           []int
		FrameTable, LocalStride uint32
	}{FramePrograms: make([]int, 16), FrameTable: l.frameTable, LocalStride: l.localStride}
	for i := range data.FramePrograms {
		data.FramePrograms[i] = -1
	}
	data.FramePrograms[0], data.FramePrograms[15], data.FramePrograms[7] = 0, 0, 1
	for _, p := range l.programs {
		initial, _ := strconv.ParseInt(p.unit.Program.Exprs[0].Value, 10, 32)
		data.Programs = append(data.Programs, testProgram{p.dom.bindings, scalarTransport(program.TypeInt, 0, initial, "")})
	}
	var got struct {
		Statuses []uint32
		Patches  [][]vm.PatchOp
		Memory   uint32
	}
	runExpressionModule(t, exportLinkedTestModule(c), `
  const api = instance.exports, patches = [], batches = [], statuses = [];
  for (const [frame,owner] of data.FramePrograms.entries()) {
    if (owner < 0) continue;
    view.setUint32(data.FrameTable+frame*16,owner,true);
    view.setUint32(data.FrameTable+frame*16+4,1,true);
  }
  statuses.push(api.begin(0,0,1));
  for (const [frame,owner] of data.FramePrograms.entries()) {
    if (owner < 0) continue;
    memory.set(Buffer.from(data.Programs[owner].Initial,'base64'),32768);
    statuses.push(api.store(frame*data.LocalStride,32768),api['initialize'+owner](frame),api['bind'+owner](frame),api['render'+owner](frame,1));
  }
  if (patches.length) throw new Error('initial patches');
  statuses.push(api.commit(0,0));
  for (const [index,frame] of [0,7,15,7,0].entries()) {
    const owner = data.FramePrograms[frame];
    statuses.push(api.begin(index+1,0,0),api['handler'+owner](frame),api['render'+owner](frame,0),api.commit(index+1,0));
    batches.push(patches.splice(0));
  }
  process.stdout.write(JSON.stringify({Statuses:statuses,Patches:batches,Memory:memory.length}));`, data, &got, `{input: unexpected,
  bind: (id,binding,kind,tag) => {
    const descriptor = data.Programs[data.FramePrograms[id]].Bindings.bindings[binding];
    if (descriptor.kind !== kind || (descriptor.tagId >>> 0) !== (tag >>> 0)) throw new Error('foreign binding');
    return 0;
  },
  patch: (id,kind,binding,attribute,pointer) => {
    const descriptor = data.Programs[data.FramePrograms[id]].Bindings.bindings[binding];
    if (kind !== 0 || attribute !== -1 || view.getUint32(pointer,true) !== 0 || view.getUint32(pointer+4,true) !== 1) throw new Error('patch shape');
    const start = view.getUint32(pointer+16,true), length = view.getUint32(pointer+20,true);
    patches.push({kind,path:descriptor.path,text:Buffer.from(memory.subarray(start,start+length)).toString('utf8')});
    return 0;
  }}`)
	for _, status := range got.Statuses {
		if status != 0 {
			t.Fatalf("linked transaction failed: %+v", got)
		}
	}
	if got.Memory != 196608 || len(got.Patches) != 5 {
		t.Fatalf("memory or event count: %+v", got)
	}
	models := map[int]*vm.Island{}
	for frame, owner := range data.FramePrograms {
		if owner >= 0 {
			models[frame] = vm.NewIsland(l.programs[owner].unit.Program, "")
		}
	}
	for step, frame := range []int{0, 7, 15, 7, 0} {
		if want := models[frame].Dispatch("increment", ""); !reflect.DeepEqual(got.Patches[step], want) {
			t.Fatalf("frame %d patches: %+v want %+v", frame, got.Patches[step], want)
		}
	}
	before := append([]byte{}, c.module.Functions[0].Body...)
	source := &c.programs[0].module.Functions[commonFunctions(c.programs[0])[0]-3]
	source.Body[0] ^= 1
	source.Signature.Params[0] = wasmgen.Void
	if !bytes.Equal(c.module.Functions[0].Body, before) || c.module.Functions[0].Signature.Params[0] != wasmgen.I32 {
		t.Fatal("linked helper aliases a private emitter")
	}
}

func TestFunctionRelocationPreservesConstantsAndExpandsIndices(t *testing.T) {
	var body instructions
	body.index(0x10, 3)
	body.op(0x1a)
	body.index(0x23, 1)
	body.i32(16)
	body.op(0x6a)
	body.i64(2048)
	body.op(0x1a)
	body.op(0x0b)
	source := wasmgen.Function{Signature: i32Signature(1), Body: body}
	fn, err := relocateFunction(source, []uint32{0, 1, 2, 131}, []uint32{0, 129})
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0x10, 0x83, 0x01, 0x1a, 0x23, 0x81, 0x01}, body[5:]...)
	if !bytes.Equal(fn.Body, want) {
		t.Fatalf("relocated framing: %x want %x", fn.Body, want)
	}
	m := wasmgen.Module{Functions: make([]wasmgen.Function, 132), Globals: make([]wasmgen.Global, 130), Exports: []wasmgen.Export{{Name: "run", Function: 0}}}
	for i := range m.Functions {
		m.Functions[i] = wasmgen.Function{Signature: i32Signature(0), Body: []byte{0x41, 0, 0x0b}}
	}
	m.Functions[0], m.Functions[131] = fn, wasmgen.Function{Signature: i32Signature(0), Body: []byte{0x41, 7, 0x0b}}
	m.Globals[129].Initial = 10
	var got int
	runExpressionModule(t, m, `process.stdout.write(JSON.stringify(instance.exports.run(0)));`, nil, &got)
	if got != 26 {
		t.Fatalf("relocated call/global result: %d", got)
	}
	fn.Body[0], fn.Signature.Params[0] = 0, wasmgen.Void
	if source.Body[0] != 0x10 || source.Signature.Params[0] != wasmgen.I32 {
		t.Fatal("relocation changed source storage")
	}
	for _, value := range []int64{-1 << 63, -1 << 31, -65, 0, 63, 64, 1<<31 - 1, 1<<63 - 1} {
		var b instructions
		b.i64(value)
		b.op(0x0b)
		fn, err := relocateFunction(wasmgen.Function{Body: b}, nil, nil)
		if err != nil || !bytes.Equal(fn.Body, b) {
			t.Fatalf("signed constant %d changed: %v", value, err)
		}
	}
}

func TestFunctionRelocationRejectsMalformedFramingAndUnresolvedIndices(t *testing.T) {
	for _, body := range [][]byte{
		{}, {0x10}, {0x10, 0x80}, {0x10, 0x80, 0, 0x0b}, {0x10, 1, 0x0b}, {0x23, 1, 0x0b},
		{0x24, 0, 0x0b}, {0x41, 0x80, 0x80, 0x80, 0x80, 8, 0x0b}, {0x41, 0x0b},
		{0x02}, {0x02, 0x7d, 0x0b}, {0x0e, 127, 0x0b}, {0x28, 0, 0x80}, {0xfc, 0, 0x0b}, {0x40, 0, 0x0b},
	} {
		if _, err := relocateFunction(wasmgen.Function{Body: body}, []uint32{0}, []uint32{NoBindingName}); err == nil {
			t.Fatalf("accepted malformed relocation: %x", body)
		}
	}
	if _, err := linkProgramCode(nil); err == nil {
		t.Fatal("admitted an empty code set")
	}
	l, err := buildLinkedLayout([]Unit{layoutUnit(t, "First", 0, 0, 0, "$same"), layoutUnit(t, "Second", 0, 0, 0, "$same")}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := linkProgramCode(l); err == nil {
		t.Fatal("admitted cross-program subscriptions before their dispatcher")
	}
}
