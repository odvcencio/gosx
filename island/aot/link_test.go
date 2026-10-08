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
	"m31labs.dev/gosx/signal"
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
	if err != nil {
		t.Fatal(err)
	}
	if l.roots != 512 || l.frameTable != 25088 || l.frameSequences != 25344 || l.tablesEnd != 25472 {
		t.Fatalf("root/table boundary: %d roots, %d/%d/%d table offsets", l.roots, l.frameTable, l.frameSequences, l.tablesEnd)
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
	if err != nil || len(l.shared) != 64 || l.roots != 64 || l.tablesEnd != 18816 {
		t.Fatalf("shared maximum: %v", err)
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
	if err != nil || len(l.programs) != 16 || l.roots != 0 || l.tablesEnd != 17792 {
		t.Fatalf("static catalog maximum: %v", err)
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
	var rootTable []byte
	for _, root := range l.programs[0].state.rows {
		rootTable = binary.LittleEndian.AppendUint32(rootTable, root)
	}
	if bytes.Count(l.data, rootTable) != 1 || l.roots != 256 {
		t.Fatalf("deduplicated catalog layout: %d bytes, %d roots", len(l.data), l.roots)
	}
}

func TestLinkedCheckpointPlansKeepDeclaredIDsTypesAndFrameAddresses(t *testing.T) {
	u, _ := scalarInputUnit(t, "prop", Int32, true)
	u.Component, u.Contract.Component, u.Program.Name = "example/components.CheckpointInputs", "example/components.CheckpointInputs", "CheckpointInputs"
	event := addExpression(&u, program.OpEventGet, program.TypeString, String, "value")
	flag := addExpression(&u, program.OpPropGet, program.TypeBool, Bool, "Flag")
	u.Program.Props = []program.PropDef{{Name: "Flag", Type: program.TypeBool}}
	u.Program.Handlers = []program.Handler{{Name: "read", Body: []program.ExprID{event}}}
	nested := u.Contract.Inputs[0]
	nested.ID = 2
	u.Contract.Inputs = []InputContract{
		{ID: 0, Source: "event", Root: "value", Path: []string{}, Kind: String, Exprs: []program.ExprID{event}},
		{ID: 1, Source: "prop", Root: "Flag", Path: []string{}, Kind: Bool, Exprs: []program.ExprID{flag}}, nested,
	}
	integer := addExpression(&u, program.OpLitInt, program.TypeInt, Int32, "0")
	boolean := addExpression(&u, program.OpLitBool, program.TypeBool, Bool, "false")
	text := addExpression(&u, program.OpLitString, program.TypeString, String, "")
	for i, def := range []struct {
		name string
		kind ScalarKind
		typ  program.ExprType
		init program.ExprID
	}{{"number", Int32, program.TypeInt, integer}, {"$shared", String, program.TypeString, text},
		{"enabled", Bool, program.TypeBool, boolean}, {"text", String, program.TypeString, text}} {
		u.Program.Signals = append(u.Program.Signals, program.SignalDef{Name: def.name, Type: def.typ, Init: def.init})
		u.Contract.Signals = append(u.Contract.Signals, StateContract{Slot: uint32(i), Name: def.name, Kind: def.kind})
	}
	u = refreshUnit(t, u)
	other := layoutUnit(t, "OtherCheckpointInputs", 5, 0, 0)
	l, err := buildLinkedLayout([]Unit{other, u, other}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	read := func(pointer int32, count int) []uint32 {
		result := make([]uint32, count)
		for i := range result {
			result[i] = binary.LittleEndian.Uint32(l.data[int(pointer)-1024+i*4:])
		}
		return result
	}
	for index, p := range l.programs {
		header := read(l.checkpointPlanBase+int32(index*12), 3)
		if p.unit.Program.Name != u.Program.Name {
			if header[0] != 5 || header[1] != 0 {
				t.Fatal("checkpoint catalog did not retain inactive program schema")
			}
			continue
		}
		if header[0] != 3 || header[1] != 2 {
			t.Fatalf("checkpoint counts include shared state or events: %v", header)
		}
		entries := read(int32(header[2]), 25)
		want := []uint32{0, 0, 5, 1, 0, 2, 1, 5, 3, 0, 3, 2, 5, 0, 0,
			1, l.inputBase, 2, 3, 0, 2, l.inputBase + 1, 2, 1, 1}
		if !reflect.DeepEqual(entries, want) {
			t.Fatalf("checkpoint scalar plan: %v want %v", entries, want)
		}
		for _, frame := range []uint32{0, 15} {
			for i := range 5 {
				entry := entries[i*5 : (i+1)*5]
				root := entry[1] + frame*entry[2]
				want := p.state.rows[frame*4+entry[0]]
				if i >= 3 {
					want = p.inputs[frame*3+entry[0]]
				}
				if root != want {
					t.Fatal("checkpoint plan does not address the owned frame")
				}
			}
		}
	}
	_, _, digest, err := canonicalUnits([]Unit{u, other}, DefaultOptions())
	if err != nil || l.inputSetSHA != digest || !bytes.Equal(l.data[l.digestBase-1024:], digest[:]) {
		t.Fatalf("checkpoint digest differs from canonical catalog: %v", err)
	}
	for _, kind := range []ScalarKind{AnyZero, SelectorPath, ScalarKind("unknown")} {
		if _, err := checkpointWireType(kind); err == nil {
			t.Fatal("unsupported checkpoint declaration acquired a wire type")
		}
	}
}

func TestLinkedCheckpointPlansDeduplicateIdenticalSchemas(t *testing.T) {
	l, err := buildLinkedLayout([]Unit{layoutUnit(t, "FirstSchema", 3, 0, 0), layoutUnit(t, "SecondSchema", 3, 0, 0)}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	first := binary.LittleEndian.Uint32(l.data[l.checkpointPlanBase-1024+8:])
	second := binary.LittleEndian.Uint32(l.data[l.checkpointPlanBase-1024+20:])
	if first != second || first%4 != 0 || l.digestBase%4 != 0 {
		t.Fatal("identical checkpoint plans acquired separate or unaligned storage")
	}
	plan := l.data[first-1024 : first-1024+60]
	if bytes.Count(l.data, plan) != 1 {
		t.Fatal("checkpoint schema is duplicated in the constant segment")
	}
}

func checkpointValueTestModule(c *linkedCode) wasmgen.Module {
	m := exportLinkedTestModule(c)
	m.Exports = append(m.Exports, wasmgen.Export{Name: "encodeValue", Function: c.checkpointValue})
	for _, item := range []struct {
		name   string
		global uint32
	}{{"cursor", allocationGlobal}, {"pending", pendingGlobal}} {
		index := uint32(len(m.Imports) + len(m.Functions))
		var body instructions
		body.index(0x23, item.global)
		body.op(0x0b)
		m.Functions = append(m.Functions, wasmgen.Function{Signature: i32Signature(0), Body: body})
		m.Exports = append(m.Exports, wasmgen.Export{Name: item.name, Function: index})
	}
	return m
}

type checkpointTestValue struct {
	ID     uint32
	Packet string
}

type checkpointTestFrame struct {
	Instance, Program uint32
	Last              uint64
	Locals, Inputs    []checkpointTestValue
}

type checkpointTestShared struct {
	ID      uint32
	Version uint64
	Packet  string
}

// The wire oracle writes the specified fields directly, independently of the
// emitted address tables and WASM serialization helpers.
func checkpointTestBytes(t *testing.T, digest [32]byte, sequence uint64, frames []checkpointTestFrame, shared []checkpointTestShared) []byte {
	t.Helper()
	body := make([]byte, 64)
	var tail []byte
	var fixups []int
	appendValue := func(packet string) {
		raw, err := base64.StdEncoding.DecodeString(packet)
		if err != nil || len(raw) < 24 {
			t.Fatalf("invalid test scalar: %v", err)
		}
		start := len(body)
		body = append(body, raw[:24]...)
		if len(raw) > 24 {
			fixups = append(fixups, start+16)
			binary.LittleEndian.PutUint32(body[start+16:], uint32(len(tail)))
			tail = append(tail, raw[24:]...)
		}
	}
	for _, frame := range frames {
		for _, field := range []uint32{frame.Instance, frame.Program, uint32(len(frame.Locals)), uint32(len(frame.Inputs)), uint32(frame.Last), uint32(frame.Last >> 32)} {
			body = binary.LittleEndian.AppendUint32(body, field)
		}
		for _, entries := range [][]checkpointTestValue{frame.Locals, frame.Inputs} {
			for _, entry := range entries {
				body = binary.LittleEndian.AppendUint32(body, entry.ID)
				appendValue(entry.Packet)
			}
		}
	}
	for _, entry := range shared {
		body = binary.LittleEndian.AppendUint32(body, entry.ID)
		body = binary.LittleEndian.AppendUint64(body, entry.Version)
		appendValue(entry.Packet)
	}
	for _, offset := range fixups {
		binary.LittleEndian.PutUint32(body[offset:], uint32(len(body))+binary.LittleEndian.Uint32(body[offset:]))
	}
	copy(body, "GXAC")
	binary.LittleEndian.PutUint16(body[4:], 1)
	for i, field := range []uint32{uint32(len(body) + len(tail)), uint32(len(frames)), uint32(len(shared)), uint32(sequence), uint32(sequence >> 32), uint32(len(tail))} {
		binary.LittleEndian.PutUint32(body[8+i*4:], field)
	}
	copy(body[32:], digest[:])
	return append(body, tail...)
}

func checkpointRecordUnit(t *testing.T) Unit {
	t.Helper()
	u, _ := scalarInputUnit(t, "prop", String, false)
	u.Component, u.Contract.Component, u.Program.Name = "example/components.CheckpointRecords", "example/components.CheckpointRecords", "CheckpointRecords"
	for i, def := range []struct {
		name, value string
		kind        ScalarKind
		op          program.OpCode
		typ         program.ExprType
	}{{"number", "0", Int, program.OpLitInt, program.TypeInt}, {"enabled", "false", Bool, program.OpLitBool, program.TypeBool},
		{"text", "", String, program.OpLitString, program.TypeString}, {"$shared", "", String, program.OpLitString, program.TypeString}} {
		init := addExpression(&u, def.op, def.typ, def.kind, def.value)
		u.Program.Signals = append(u.Program.Signals, program.SignalDef{Name: def.name, Type: def.typ, Init: init})
		u.Contract.Signals = append(u.Contract.Signals, StateContract{Slot: uint32(i), Name: def.name, Kind: def.kind})
	}
	return refreshUnit(t, u)
}

func TestLinkedCheckpointPageBytesTrackPreparedCommittedAndDisposedState(t *testing.T) {
	u := checkpointRecordUnit(t)
	other, _ := scalarInputUnit(t, "prop", Int32, true)
	other.Component, other.Contract.Component, other.Program.Name = "example/components.NestedCheckpoint", "example/components.NestedCheckpoint", "NestedCheckpoint"
	other = refreshUnit(t, other)
	l, err := buildLinkedLayout([]Unit{other, u}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	c, err := linkProgramCode(l)
	if err != nil {
		t.Fatal(err)
	}
	m := checkpointValueTestModule(c)
	m.Exports = append(m.Exports, wasmgen.Export{Name: "checkpoint", Function: c.checkpoint}, wasmgen.Export{Name: "dispose", Function: c.dispose})
	integer := func(n int64) string { return scalarTransport(program.TypeInt, 0, n, "") }
	text := scalarTransport(program.TypeString, 1, 0, "héllo\x00🌴e\u0301")
	frames := []checkpointTestFrame{
		{Instance: 0, Last: 3, Locals: []checkpointTestValue{{0, integer(-2147483648)}, {1, scalarTransport(program.TypeBool, 0, 0, "")}, {2, scalarTransport(program.TypeString, 0, 0, "")}}, Inputs: []checkpointTestValue{{0, scalarTransport(program.TypeString, 1, 0, "")}}},
		{Instance: 7, Last: 6, Inputs: []checkpointTestValue{{0, scalarTransport(program.TypeAny, 0, 0, "")}}},
		{Instance: 15, Last: 1, Locals: []checkpointTestValue{{0, integer(2147483647)}, {1, scalarTransport(program.TypeBool, 2, 0, "")}, {2, text}}, Inputs: []checkpointTestValue{{0, text}}},
	}
	type root struct {
		ID     uint32
		Packet string
	}
	data := struct {
		Frames                                     []checkpointTestFrame
		Roots                                      []root
		FrameTable, FrameSequences, SharedVersions uint32
		Sequence                                   [2]uint32
		Changes                                    []root
	}{Frames: frames, FrameTable: l.frameTable, FrameSequences: l.frameSequences, SharedVersions: l.sharedVersions, Sequence: [2]uint32{0xfffffff0, 0x80000001}}
	for i := range data.Frames {
		name := u.Program.Name
		if data.Frames[i].Instance == 7 {
			name = other.Program.Name
		}
		p := linkedProgramByName(t, l, name)
		data.Frames[i].Program = p.state.programID
		for _, value := range data.Frames[i].Locals {
			data.Roots = append(data.Roots, root{p.state.rows[data.Frames[i].Instance*uint32(len(p.unit.Program.Signals))+value.ID], value.Packet})
		}
		for _, value := range data.Frames[i].Inputs {
			id := p.inputs[data.Frames[i].Instance*uint32(len(p.unit.Contract.Inputs))+value.ID]
			data.Roots = append(data.Roots, root{id, value.Packet})
			if data.Frames[i].Instance == 7 {
				data.Changes = append(data.Changes, root{id, integer(42)})
			}
		}
	}
	data.Roots = append(data.Roots, root{l.shared[0].root, text})
	data.Changes = append(data.Changes, root{15*l.localStride + 2, text}, root{l.shared[0].root, text})
	sequence := uint64(data.Sequence[1])<<32 | uint64(data.Sequence[0])
	shared := []checkpointTestShared{{0, 7, text}}
	initial := checkpointTestBytes(t, l.inputSetSHA, sequence, data.Frames, shared)
	changed := append([]checkpointTestFrame{}, data.Frames...)
	changed[1].Inputs = []checkpointTestValue{{0, integer(42)}}
	changed[1].Last, changed[2].Last = sequence+1, sequence+1
	prepared := checkpointTestBytes(t, l.inputSetSHA, sequence+1, changed, []checkpointTestShared{{0, sequence + 1, text}})
	var got struct {
		Documents []string
		Statuses  []int32
		Pure      bool
	}
	runExpressionModule(t, m, `
  const api = instance.exports, statuses = [], documents = [];
  for (const f of data.Frames) {
    view.setUint32(data.FrameTable+f.Instance*16,f.Program,true);
    view.setUint32(data.FrameTable+f.Instance*16+4,1,true);
    view.setUint32(data.FrameTable+f.Instance*16+8,f.Last,true);
  }
  view.setUint32(data.SharedVersions,7,true);
  const store = values => { for (const value of values) {
    memory.set(Buffer.from(value.Packet,'base64'),32768);
    statuses.push(api.store(value.ID,32768));
  }};
  let pure = true;
  const read = which => {
    const before = Buffer.concat([Buffer.from(memory.slice(0,32768)),Buffer.from(memory.slice(65536))]);
    const allocation = api.cursor(), status = api.status(), pending = api.pending();
    const length = api.checkpoint(which,32769,32767);
    pure = pure&&before.equals(Buffer.concat([Buffer.from(memory.slice(0,32768)),Buffer.from(memory.slice(65536))]))
      &&allocation===api.cursor()&&status===api.status()&&pending===api.pending();
    if (length>0) documents.push(Buffer.from(memory.slice(32769,32769+length)).toString('base64'));
    else statuses.push(length);
  };
  statuses.push(api.begin(data.Sequence[0],data.Sequence[1],1));
  store(data.Roots); read(1); read(0);
  statuses.push(api.commit(data.Sequence[0],data.Sequence[1])); read(0);
  const change = () => {
    statuses.push(api.begin(data.Sequence[0]+1,data.Sequence[1],0)); store(data.Changes);
    for (const frame of [7,15]) {
      view.setUint32(data.FrameSequences+frame*8,data.Sequence[0]+1,true);
      view.setUint32(data.FrameSequences+frame*8+4,data.Sequence[1],true);
    }
    view.setUint32(data.SharedVersions+8,data.Sequence[0]+1,true);
    view.setUint32(data.SharedVersions+12,data.Sequence[1],true);
  };
  change(); read(1); read(0); statuses.push(api.abort()); read(0); read(1);
  change(); statuses.push(api.commit(data.Sequence[0]+1,data.Sequence[1])); read(0);
  statuses.push(api.dispose(0)); read(0);
  process.stdout.write(JSON.stringify({Documents:documents,Statuses:statuses,Pure:pure}));`, data, &got)
	want := [][]byte{initial, initial, prepared, initial, initial, prepared,
		checkpointTestBytes(t, l.inputSetSHA, sequence+1, changed[1:], []checkpointTestShared{{0, sequence + 1, text}})}
	if len(got.Documents) != len(want) || !got.Pure {
		t.Fatalf("checkpoint generations or purity: %+v", got)
	}
	for i, document := range want {
		if got.Documents[i] != base64.StdEncoding.EncodeToString(document) {
			t.Fatalf("checkpoint generation %d differs from the exact GXAC wire contract", i)
		}
	}
	var errors []int32
	for _, status := range got.Statuses {
		if status != 0 {
			errors = append(errors, status)
		}
	}
	if !reflect.DeepEqual(errors, []int32{-statusBadSequence, -statusBusy}) {
		t.Fatalf("checkpoint transaction statuses: %v", got.Statuses)
	}
}

func TestLinkedCheckpointPageRejectsInvalidStateWithoutWriting(t *testing.T) {
	l, err := buildLinkedLayout([]Unit{layoutUnit(t, "RejectedCheckpoint", 2, 0, 0)}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	c, err := linkProgramCode(l)
	if err != nil {
		t.Fatal(err)
	}
	m := checkpointValueTestModule(c)
	m.Exports = append(m.Exports, wasmgen.Export{Name: "checkpoint", Function: c.checkpoint})
	type testCase struct {
		Which, Destination, Capacity uint32
		Offset, Value                uint32
		Mutate, ArenaRelative        bool
		Want                         int32
	}
	cases := []testCase{
		{Which: 2, Destination: 32768, Capacity: 32768, Want: -2},
		{Which: 4294967295, Destination: 32768, Capacity: 32768, Want: -2},
		{Destination: 32767, Capacity: 32768, Want: -2},
		{Destination: 65536, Capacity: 1, Want: -2},
		{Destination: 65536, Capacity: 0, Want: -10},
		{Destination: 32768, Capacity: 0, Want: -10},
		{Destination: 32768, Capacity: 143, Want: -10},
		{Destination: 32768, Capacity: 4294967295, Want: -2},
	}
	for _, mutation := range [][3]uint32{{l.frameTable + 4, 2, 10}, {l.frameTable, 1, 10}, {l.frameTable + 8, 6, 10},
		{65536, uint32(program.TypeString), 10}, {65536 + 4, 1, 2}, {65536 + 8, 2147483648, 3},
		{65536 + 16, 4294967295, 2}, {65536 + 24 + 4, 4, 2}} {
		item := testCase{Destination: 32768, Capacity: 32768, Offset: mutation[0], Value: mutation[1], Mutate: true, Want: -int32(mutation[2])}
		if item.Offset >= 65536 {
			item.Offset -= 65536
			item.ArenaRelative = true
		}
		cases = append(cases, item)
	}
	data := struct {
		FrameTable uint32
		Cases      []testCase
		Integer    string
		Invalid    string
	}{l.frameTable, cases, scalarTransport(program.TypeInt, 0, 0, ""), scalarTransport(program.TypeFloat, 0, 0, "")}
	var got struct {
		Statuses  []int32
		Unchanged []bool
		Poisoned  []int32
	}
	runExpressionModule(t, m, `
  const api = instance.exports, statuses = [], unchanged = [];
  view.setUint32(data.FrameTable+4,1,true);
  if (api.begin(5,0,1)!==0) throw new Error('initialization failed');
  memory.set(Buffer.from(data.Integer,'base64'),32768);
  if (api.store(0,32768)!==0||api.store(1,32768)!==0||api.commit(5,0)!==0) throw new Error('initial roots failed');
  const clean = Buffer.from(memory);
  for (const item of data.Cases) {
    memory.set(clean);
    if (item.Mutate) view.setUint32(item.Offset+(item.ArenaRelative?api.committed():0),item.Value,true);
    memory.fill(0xa5,32768,65536);
    const before = Buffer.from(memory), allocation = api.cursor();
    statuses.push(api.checkpoint(item.Which,item.Destination,item.Capacity));
    unchanged.push(before.equals(Buffer.from(memory))&&api.status()===0&&api.cursor()===allocation);
  }
  memory.set(clean);
  const poisoned = [api.begin(6,0,0)];
  memory.set(Buffer.from(data.Invalid,'base64'),32768);
  poisoned.push(api.store(0,32768));
  memory.fill(0xa5,32768,65536);
  const before = Buffer.from(memory);
  poisoned.push(api.checkpoint(1,32768,32768));
  unchanged.push(before.equals(Buffer.from(memory))&&api.status()===2);
  poisoned.push(api.checkpoint(0,65392,144));
  poisoned.push(view.getUint32(65392+20,true),api.status());
  process.stdout.write(JSON.stringify({Statuses:statuses,Unchanged:unchanged,Poisoned:poisoned}));`, data, &got)
	if len(got.Statuses) != len(cases) {
		t.Fatal("checkpoint rejection cases were skipped")
	}
	for i, item := range cases {
		if got.Statuses[i] != item.Want || !got.Unchanged[i] {
			t.Fatalf("checkpoint case %d: status %d want %d, unchanged %v", i, got.Statuses[i], item.Want, got.Unchanged[i])
		}
	}
	if !got.Unchanged[len(cases)] || !reflect.DeepEqual(got.Poisoned, []int32{0, 2, -2, 144, 5, 2}) {
		t.Fatalf("poisoned prepared generation changed committed export: %+v", got)
	}
}

func TestLinkedCheckpointPageEnforcesDenseStringBudgetAndExactIOEnd(t *testing.T) {
	u := staticUnit(t)
	init := addExpression(&u, program.OpLitString, program.TypeString, String, "")
	for i := range 5 {
		name := "text" + strconv.Itoa(i)
		u.Program.Signals = append(u.Program.Signals, program.SignalDef{Name: name, Type: program.TypeString, Init: init})
		u.Contract.Signals = append(u.Contract.Signals, StateContract{Slot: uint32(i), Name: name, Kind: String})
	}
	u = refreshUnit(t, u)
	l, err := buildLinkedLayout([]Unit{u}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	c, err := linkProgramCode(l)
	if err != nil {
		t.Fatal(err)
	}
	m := checkpointValueTestModule(c)
	m.Exports = append(m.Exports, wasmgen.Export{Name: "checkpoint", Function: c.checkpoint})
	text := strings.Repeat("x", 4096)
	frame := checkpointTestFrame{Instance: 0, Program: 0}
	for i := range 5 {
		value := text
		if i == 4 {
			value = ""
		}
		frame.Locals = append(frame.Locals, checkpointTestValue{uint32(i), scalarTransport(program.TypeString, 1, 0, value)})
	}
	want := checkpointTestBytes(t, l.inputSetSHA, 5, []checkpointTestFrame{frame}, nil)
	data := struct {
		FrameTable uint32
		Text       string
		Length     uint32
	}{l.frameTable, base64.StdEncoding.EncodeToString([]byte(text)), uint32(len(want))}
	var got struct {
		Status    int32
		Document  string
		Unchanged bool
	}
	runExpressionModule(t, m, `
  const api = instance.exports;
  view.setUint32(data.FrameTable+4,1,true);
  if (api.begin(5,0,1)!==0||api.commit(5,0)!==0) throw new Error('initialization failed');
  const base = api.committed();
  const text = base+8192;
  memory.set(Buffer.from(data.Text,'base64'),text);
  for (let i=0;i<5;i++) {
    view.setUint32(base+i*24+4,1,true);
    view.setUint32(base+i*24+16,i===4?0:text,true);
    view.setUint32(base+i*24+20,i===4?0:4096,true);
  }
  const length = api.checkpoint(0,65536-data.Length,data.Length);
  const document = length>0?Buffer.from(memory.slice(65536-data.Length,65536)).toString('base64'):'';
  view.setUint32(base+4*24+16,text,true);
  view.setUint32(base+4*24+20,4096,true);
  const before = Buffer.from(memory), allocation = api.cursor();
  const status = api.checkpoint(0,32768,32768);
  process.stdout.write(JSON.stringify({Status:status,Document:document,
    Unchanged:before.equals(Buffer.from(memory))&&api.cursor()===allocation&&api.status()===0}));`, data, &got)
	if got.Document != base64.StdEncoding.EncodeToString(want) || got.Status != -statusStringLimit || !got.Unchanged {
		t.Fatal("checkpoint deduplicated transport strings, exceeded the string budget or changed memory on rejection")
	}
}

func TestLinkedWireValuesValidateNativeScalarsAndAdvanceDenseCursor(t *testing.T) {
	l, err := buildLinkedLayout([]Unit{staticUnit(t)}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	c, err := linkProgramCode(l)
	if err != nil {
		t.Fatal(err)
	}
	m := checkpointValueTestModule(c)
	m.Exports = append(m.Exports, wasmgen.Export{Name: "wireValue", Function: c.wireScalar})
	frame := checkpointTestFrame{}
	for i, item := range []struct {
		value   vm.Value
		present bool
	}{{vm.ZeroValue(program.TypeString), false}, {vm.StringVal(""), true},
		{vm.StringVal("héllo\x00🌴e\u0301"), true}, {vm.StringVal("héllo\x00🌴e\u0301"), true},
		{vm.IntVal(-2147483648), true}, {vm.IntVal(2147483647), true},
		{vm.BoolVal(false), true}, {vm.BoolVal(true), true}, {vm.ZeroValue(program.TypeAny), false},
		{vm.StringVal(strings.Repeat("x", 4096)), true}} {
		flags := uint32(0)
		if item.value.Type == program.TypeString && item.present {
			flags = 1
		} else if item.value.Type == program.TypeBool && item.value.Truth() {
			flags = 2
		}
		packet := scalarTransport(item.value.Type, flags, int64(item.value.Number()), item.value.Text())
		frame.Locals = append(frame.Locals, checkpointTestValue{uint32(i), packet})
	}
	document := checkpointTestBytes(t, l.inputSetSHA, 0, []checkpointTestFrame{frame}, nil)
	start := uint32(len(document)) - binary.LittleEndian.Uint32(document[28:])
	data := struct {
		Document string
		Start    uint32
		Count    int
	}{base64.StdEncoding.EncodeToString(document), start, len(frame.Locals)}
	var got []struct {
		Cursors []int32
		Pure    bool
	}
	runExpressionModule(t, m, `
  const api = instance.exports, document = Buffer.from(data.Document,'base64'), results = [];
  for (const base of [32769,65536-document.length]) {
    memory.set(document,base);
    const before = Buffer.from(memory), allocation = api.cursor(), cursors = [];
    let cursor = base+data.Start;
    for (let i=0;i<data.Count;i++) {
      cursor = api.wireValue(base+92+i*28,base,base+document.length,cursor);
      cursors.push(cursor);
    }
    results.push({Cursors:cursors,Pure:before.equals(Buffer.from(memory))&&api.status()===0&&api.cursor()===allocation&&api.pending()===0});
  }
  process.stdout.write(JSON.stringify(results));`, data, &got)
	if len(got) != 2 {
		t.Fatal("both wire document positions were not exercised")
	}
	for position, result := range got {
		base := uint32(32769)
		if position != 0 {
			base = uint32(65536 - len(document))
		}
		cursor := base + start
		var want []int32
		for _, entry := range frame.Locals {
			raw, _ := base64.StdEncoding.DecodeString(entry.Packet)
			cursor += uint32(len(raw) - 24)
			want = append(want, int32(cursor))
		}
		if !result.Pure || !reflect.DeepEqual(result.Cursors, want) || cursor != base+uint32(len(document)) {
			t.Fatalf("wire scalar shape, dense cursor or validation purity: %+v", result)
		}
	}
}

func TestLinkedWireValuesRejectBadTagsPayloadsUTF8AndWidenedBounds(t *testing.T) {
	l, err := buildLinkedLayout([]Unit{staticUnit(t)}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	c, err := linkProgramCode(l)
	if err != nil {
		t.Fatal(err)
	}
	m := checkpointValueTestModule(c)
	m.Exports = append(m.Exports, wasmgen.Export{Name: "wireValue", Function: c.wireScalar})
	type testCase struct {
		Document                  string
		Record, Base, End, Cursor uint32
		Want                      int32
	}
	var cases []testCase
	add := func(packet string, want int32) {
		raw, _ := base64.StdEncoding.DecodeString(packet)
		if len(raw) > 24 {
			binary.LittleEndian.PutUint32(raw[16:], 24)
		}
		cases = append(cases, testCase{base64.StdEncoding.EncodeToString(raw), 32768, 32768, uint32(32768 + len(raw)), 32792, want})
	}
	for _, item := range []struct {
		tag    program.ExprType
		flags  uint32
		number int64
		text   string
		want   int32
	}{{program.TypeInt, 0, -2147483649, "", -3}, {program.TypeInt, 0, 2147483648, "", -3},
		{program.TypeInt, 1, 0, "", -2}, {program.TypeInt, 0, 0, "x", -2},
		{program.TypeBool, 1, 0, "", -2}, {program.TypeBool, 3, 0, "", -2}, {program.TypeBool, 2, 1, "", -2},
		{program.TypeString, 0, 0, "x", -2}, {program.TypeString, 2, 0, "", -2}, {program.TypeString, 1, 1, "", -2},
		{program.TypeString, 1, 0, strings.Repeat("x", 4097), -4},
		{program.TypeAny, 1, 0, "", -2}, {program.TypeAny, 0, 1, "", -2},
		{program.TypeFloat, 0, 0, "", -2}, {program.TypeNode, 0, 0, "", -2}, {255, 0, 0, "", -2}} {
		add(scalarTransport(item.tag, item.flags, item.number, item.text), item.want)
	}
	for _, text := range []string{"\xff", "\x80", "\xc0\x80", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\xe2\x82"} {
		add(scalarTransport(program.TypeString, 1, 0, text), -2)
	}
	for _, pointer := range []uint32{0, 1, 23, 25, 32792, 4294967295} {
		add(scalarTransport(program.TypeString, 1, 0, "x"), -2)
		raw, _ := base64.StdEncoding.DecodeString(cases[len(cases)-1].Document)
		binary.LittleEndian.PutUint32(raw[16:], pointer)
		cases[len(cases)-1].Document = base64.StdEncoding.EncodeToString(raw)
	}
	for _, fields := range [][2]uint32{{16, 24}, {20, 1}, {4, 4}} {
		add(scalarTransport(program.TypeAny, 0, 0, ""), -2)
		raw, _ := base64.StdEncoding.DecodeString(cases[len(cases)-1].Document)
		binary.LittleEndian.PutUint32(raw[fields[0]:], fields[1])
		cases[len(cases)-1].Document = base64.StdEncoding.EncodeToString(raw)
	}
	valid := scalarTransport(program.TypeInt, 0, 0, "")
	for _, bounds := range [][4]uint32{{32767, 32768, 32792, 32792}, {32769, 32768, 32792, 32792},
		{4294967295, 32768, 65536, 65536}, {32768, 32767, 32792, 32792}, {32768, 32793, 32792, 32792},
		{32768, 32768, 65537, 32792}, {32768, 32768, 32792, 32791}, {32768, 32768, 32792, 32793},
		{32768, 32768, 32792, 4294967295}} {
		cases = append(cases, testCase{valid, bounds[0], bounds[1], bounds[2], bounds[3], -2})
	}
	add(scalarTransport(program.TypeString, 1, 0, "x"), -2)
	cases[len(cases)-1].End--
	var got struct {
		Statuses []int32
		Pure     bool
	}
	runExpressionModule(t, m, `
  const api = instance.exports, statuses = [], allocation = api.cursor();
  let pure = true;
  for (const item of data) {
    memory.fill(0,32768,65536);
    memory.set(Buffer.from(item.Document,'base64'),32768);
    const before = Buffer.from(memory);
    statuses.push(api.wireValue(item.Record,item.Base,item.End,item.Cursor));
    pure = pure&&before.equals(Buffer.from(memory))&&api.status()===0&&api.cursor()===allocation;
  }
  process.stdout.write(JSON.stringify({Statuses:statuses,Pure:pure}));`, cases, &got)
	if len(got.Statuses) != len(cases) || !got.Pure {
		t.Fatal("wire validation skipped records or wrote memory or transaction state")
	}
	for i, item := range cases {
		if got.Statuses[i] != item.Want {
			t.Fatalf("wire case %d: %d want %d", i, got.Statuses[i], item.Want)
		}
	}
}

func TestLinkedWireValuesRejectStringAliasingAndPadding(t *testing.T) {
	l, err := buildLinkedLayout([]Unit{staticUnit(t)}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	c, err := linkProgramCode(l)
	if err != nil {
		t.Fatal(err)
	}
	m := checkpointValueTestModule(c)
	m.Exports = append(m.Exports, wasmgen.Export{Name: "wireValue", Function: c.wireScalar})
	packet := scalarTransport(program.TypeString, 1, 0, "same")
	document := checkpointTestBytes(t, l.inputSetSHA, 0, []checkpointTestFrame{{Locals: []checkpointTestValue{{0, packet}, {1, packet}}}}, nil)
	var cases []string
	for _, pointer := range []uint32{144, 145, 149} {
		modified := append([]byte{}, document...)
		binary.LittleEndian.PutUint32(modified[120+16:], pointer)
		cases = append(cases, base64.StdEncoding.EncodeToString(modified))
	}
	var got []int32
	runExpressionModule(t, m, `
  const api = instance.exports, statuses = [];
  for (const packet of data) {
    const document = Buffer.from(packet,'base64');
    memory.set(document,32768);
    const next = api.wireValue(32768+92,32768,32768+document.length,32768+144);
    statuses.push(next,api.wireValue(32768+120,32768,32768+document.length,next));
  }
  process.stdout.write(JSON.stringify(statuses));`, cases, &got)
	if !reflect.DeepEqual(got, []int32{32916, -2, 32916, -2, 32916, -2}) {
		t.Fatalf("wire strings accepted overlap or per-string padding: %v", got)
	}
}

func TestLinkedWireValuesAcceptEmptyPayloadsAtExactIOEnd(t *testing.T) {
	l, err := buildLinkedLayout([]Unit{staticUnit(t)}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	c, err := linkProgramCode(l)
	if err != nil {
		t.Fatal(err)
	}
	m := checkpointValueTestModule(c)
	m.Exports = append(m.Exports, wasmgen.Export{Name: "wireValue", Function: c.wireScalar})
	packets := []string{scalarTransport(program.TypeString, 0, 0, ""), scalarTransport(program.TypeString, 1, 0, ""),
		scalarTransport(program.TypeInt, 0, -2147483648, ""), scalarTransport(program.TypeBool, 2, 0, ""), scalarTransport(program.TypeAny, 0, 0, "")}
	var got []int32
	runExpressionModule(t, m, `
  const statuses = [], api = instance.exports;
  for (const packet of data) {
    memory.set(Buffer.from(packet,'base64'),65512);
    statuses.push(api.wireValue(65512,65512,65536,65536));
    statuses.push(api.wireValue(65513,65512,65536,65536));
    statuses.push(api.wireValue(65512,65512,65535,65535));
  }
  process.stdout.write(JSON.stringify(statuses));`, packets, &got)
	var want []int32
	for range packets {
		want = append(want, 65536, -2, -2)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wire records at the IO boundary: %v want %v", got, want)
	}
}

func TestLinkedCheckpointValuesPreserveScalarBytesAndDenseStringOrder(t *testing.T) {
	l, err := buildLinkedLayout([]Unit{layoutUnit(t, "CheckpointValues", 1, 0, 0)}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	c, err := linkProgramCode(l)
	if err != nil {
		t.Fatal(err)
	}
	m := checkpointValueTestModule(c)
	packets := []string{
		scalarTransport(program.TypeString, 0, 0, ""), scalarTransport(program.TypeString, 1, 0, ""),
		scalarTransport(program.TypeString, 1, 0, "héllo\x00🌴e\u0301"), scalarTransport(program.TypeString, 1, 0, "héllo\x00🌴e\u0301"),
		scalarTransport(program.TypeInt, 0, -2147483648, ""), scalarTransport(program.TypeInt, 0, 2147483647, ""),
		scalarTransport(program.TypeBool, 0, 0, ""), scalarTransport(program.TypeBool, 2, 0, ""),
		scalarTransport(program.TypeAny, 0, 0, ""), scalarTransport(program.TypeString, 1, 0, strings.Repeat("x", 4096)),
	}
	var got []struct {
		Records, Strings string
		Cursor           uint32
		Pure             bool
	}
	runExpressionModule(t, m, `
  const results = [], api = instance.exports, allocation = api.cursor();
  for (const arena of [65536,131072]) {
    memory.fill(0xa5,32768,65536);
    let cursor = 34000;
    for (let i=0;i<data.length;i++) {
      const packet = Buffer.from(data[i],'base64');
      if (view.getUint32(arena+20,true)!==0) memory.fill(0,arena,arena+8192);
      memory.set(packet,arena);
      if (packet.length>24) view.setUint32(arena+16,arena+24,true);
      const before = Buffer.from(memory.slice(arena,arena+8192));
      cursor = api.encodeValue(arena,32840+i*24,cursor,32768,arena);
      if (!before.equals(Buffer.from(memory.slice(arena,arena+8192)))) throw new Error('source changed');
    }
    results.push({Records:Buffer.from(memory.slice(32840,32840+data.length*24)).toString('base64'),
      Strings:Buffer.from(memory.slice(34000,cursor)).toString('base64'),Cursor:cursor,
      Pure:api.status()===0&&api.cursor()===allocation&&api.pending()===0});
  }
  process.stdout.write(JSON.stringify(results));`, packets, &got)
	var records, tail []byte
	for _, packet := range packets {
		raw, _ := base64.StdEncoding.DecodeString(packet)
		value := append([]byte{}, raw[:24]...)
		if len(raw) > 24 {
			binary.LittleEndian.PutUint32(value[16:], uint32(34000-32768+len(tail)))
			tail = append(tail, raw[24:]...)
		}
		records = append(records, value...)
	}
	if len(got) != 2 {
		t.Fatalf("source arena results: %v", got)
	}
	for _, result := range got {
		if result.Records != base64.StdEncoding.EncodeToString(records) || result.Strings != base64.StdEncoding.EncodeToString(tail) || result.Cursor != uint32(34000+len(tail)) || !result.Pure {
			t.Fatal("checkpoint changed tags, zero flags, relative offsets, dense strings or transaction state")
		}
	}
}

func TestLinkedCheckpointValueRejectsMalformedRecordsAndBoundsBeforeWriting(t *testing.T) {
	l, err := buildLinkedLayout([]Unit{layoutUnit(t, "InvalidCheckpointValues", 0, 0, 0)}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	c, err := linkProgramCode(l)
	if err != nil {
		t.Fatal(err)
	}
	m := checkpointValueTestModule(c)
	type testCase struct {
		Packet                                   string
		Source, Destination, Cursor, Base, Arena uint32
		CorruptPointer                           bool
	}
	var cases []testCase
	add := func(packet string) {
		cases = append(cases, testCase{Packet: packet, Source: 65536, Destination: 32840, Cursor: 33000, Base: 32768, Arena: 65536})
	}
	for _, item := range []struct {
		tag    program.ExprType
		flags  uint32
		number int64
		text   string
	}{{program.TypeString, 0, 0, "x"}, {program.TypeString, 1, 0, "\xff"}, {program.TypeString, 1, 0, strings.Repeat("x", 4097)},
		{program.TypeInt, 0, 2147483648, ""}, {program.TypeInt, 1, 0, ""}, {program.TypeBool, 2, 1, ""},
		{program.TypeAny, 1, 0, ""}, {program.TypeFloat, 0, 0, ""}} {
		add(scalarTransport(item.tag, item.flags, item.number, item.text))
	}
	valid := scalarTransport(program.TypeString, 1, 0, "x")
	for _, bounds := range [][5]uint32{
		{65535, 32840, 33000, 32768, 65536}, {131049, 32840, 33000, 32768, 65536},
		{4294967295, 32840, 33000, 32768, 65536}, {65536, 32767, 33000, 32768, 65536},
		{65536, 32840, 32863, 32768, 65536}, {65536, 65513, 65536, 32768, 65536},
		{65536, 4294967295, 65536, 32768, 65536}, {65536, 32840, 65536, 32768, 65536},
		{65536, 32840, 33000, 32767, 65536}, {65536, 32840, 33000, 32841, 65536},
		{65536, 32840, 33000, 32768, 0}, {65536, 32840, 33000, 32768, 196608},
	} {
		cases = append(cases, testCase{valid, bounds[0], bounds[1], bounds[2], bounds[3], bounds[4], false})
	}
	add(valid)
	cases[len(cases)-1].CorruptPointer = true
	var got []bool
	runExpressionModule(t, m, `
  const results = [], api = instance.exports, allocation = api.cursor();
  for (const item of data) {
    memory.fill(0xa5,32768,65536);
    memory.fill(0,65536,131072);
    const packet = Buffer.from(item.Packet,'base64');
    memory.set(packet,65536);
    if (packet.length>24) view.setUint32(65552,item.CorruptPointer?0xffffffff:65560,true);
    const before = Buffer.from(memory);
    const result = api.encodeValue(item.Source,item.Destination,item.Cursor,item.Base,item.Arena);
    results.push(result===0&&before.equals(Buffer.from(memory))&&api.status()===0&&api.cursor()===allocation);
  }
  process.stdout.write(JSON.stringify(results));`, cases, &got)
	for i, unchanged := range got {
		if !unchanged {
			t.Fatalf("invalid checkpoint value case %d changed memory or returned success", i)
		}
	}
	if len(got) != len(cases) {
		t.Fatal("not all checkpoint bounds were exercised")
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
			"render": source.dom.render,
		} {
			e.module.Exports = append(e.module.Exports, wasmgen.Export{Name: name + strconv.Itoa(p), Function: c.indices[p][index]})
		}
		for handler, index := range source.handlers {
			name := "handler" + strconv.Itoa(p)
			if handler != 0 {
				name = "other" + strconv.Itoa(p)
			}
			e.module.Exports = append(e.module.Exports, wasmgen.Export{Name: name, Function: c.indices[p][index]})
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
	if len(c.module.Imports) != 3 || len(c.module.Exports) != 0 || len(c.module.Globals) != 26 || !bytes.Equal(c.module.Data, l.data) {
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
	if _, err := linkProgramCode(l); err != nil {
		t.Fatalf("shared set without computed rows: %v", err)
	}
}

func linkedSharedUnit(t *testing.T, name string, diamond, swap bool) (Unit, []program.ExprID) {
	u, reads := computedBatchUnit(t, diamond)
	u.Component, u.Contract.Component, u.Program.Name = "example/components."+name, "example/components."+name, name
	u.Program.Signals[0].Name, u.Contract.Signals[0].Name = "$count", "$count"
	for i := range u.Program.Exprs {
		expr := &u.Program.Exprs[i]
		if (expr.Op == program.OpSignalGet || expr.Op == program.OpSignalSet) && expr.Value == "count" {
			expr.Value = "$count"
		}
	}
	if swap {
		u.Program.Signals[0], u.Program.Signals[1] = u.Program.Signals[1], u.Program.Signals[0]
		u.Contract.Signals[0], u.Contract.Signals[1] = u.Contract.Signals[1], u.Contract.Signals[0]
		for i := range u.Contract.Signals {
			u.Contract.Signals[i].Slot = uint32(i)
		}
	}
	return refreshUnit(t, u), reads
}

func TestLinkedSharedNotificationsMatchNativeBatchTraceAndAbort(t *testing.T) {
	a, ar := linkedSharedUnit(t, "SharedFirst", false, false)
	b, br := linkedSharedUnit(t, "SharedSecond", true, true)
	l, err := buildLinkedLayout([]Unit{b, layoutUnit(t, "Unrelated", 0, 0, 0, "$Count", "$$count"), a}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	c, err := linkProgramCode(l)
	if err != nil {
		t.Fatal(err)
	}
	m := exportLinkedTestModule(c)
	reads := map[string][]program.ExprID{"SharedFirst": ar, "SharedSecond": br}
	data := struct {
		Owners     []uint32
		Roots      []string
		LocalRoots []uint32
		Steps      []struct {
			Frame, Handler, Sequence uint32
			Read                     bool
		}
		FrameTable, SharedRoot         uint32
		Versions, ComputedStride, Rows uint32
	}{Owners: make([]uint32, 16), FrameTable: l.frameTable, ComputedStride: l.computedStride, Rows: l.roots}
	for i := range data.Owners {
		data.Owners[i] = NoBindingName
	}
	for id, e := range c.programs {
		for i, expr := range reads[e.unit.Program.Name] {
			m.Exports = append(m.Exports, wasmgen.Export{Name: "read" + strconv.Itoa(id) + "_" + strconv.Itoa(i), Function: c.indices[id][e.functions[expr]]})
		}
		switch e.unit.Program.Name {
		case "SharedFirst":
			data.Owners[0], data.Owners[15] = uint32(id), uint32(id)
		case "SharedSecond":
			data.Owners[1] = uint32(id)
		case "Unrelated":
			data.Owners[9] = uint32(id)
		}
	}
	for _, frame := range []uint32{0, 1, 15} {
		p := &l.programs[data.Owners[frame]]
		data.LocalRoots = append(data.LocalRoots, p.state.rows[frame*uint32(len(p.unit.Program.Signals))+p.state.signals["observed"]])
	}
	for _, shared := range l.shared {
		if shared.name == "$count" {
			data.SharedRoot, data.Versions = shared.root, l.sharedVersions+shared.id*16
		}
	}
	for root := uint32(0); root < l.computedBase; root++ {
		value := int64(0)
		if root == data.SharedRoot {
			value = 10
		}
		data.Roots = append(data.Roots, scalarTransport(program.TypeInt, 0, value, ""))
	}
	for i, frame := range []uint32{0, 1, 15, 1, 0, 15} {
		data.Steps = append(data.Steps, struct {
			Frame, Handler, Sequence uint32
			Read                     bool
		}{frame, uint32(i % 3 / 2), []uint32{1, 2, 5, 9, 10, 20}[i], i == 2 || i == 4})
	}
	var mask instructions
	mask.index(0x23, linkedRenderMaskGlobal)
	mask.op(0x0b)
	m.Exports = append(m.Exports, wasmgen.Export{Name: "mask", Function: uint32(len(m.Imports) + len(m.Functions))}, wasmgen.Export{Name: "notify", Function: c.notify})
	m.Functions = append(m.Functions, wasmgen.Function{Signature: i32Signature(0), Body: mask})
	var got struct {
		States            [][]int64
		Masks, Versions   []uint32
		Statuses, Fault   []uint32
		Aborted, Restored bool
	}
	runExpressionModule(t, m, `
  const api = instance.exports, statuses = [], states = [], masks = [], versions = [];
  for (const [frame,owner] of data.Owners.entries()) {
    if (owner === 4294967295) continue;
    view.setUint32(data.FrameTable+frame*16,owner,true); view.setUint32(data.FrameTable+frame*16+4,1,true);
  }
  statuses.push(api.begin(0,0,1));
  for (const [root,value] of data.Roots.entries()) {
    memory.set(Buffer.from(value,'base64'),32768); statuses.push(api.store(root,32768));
  }
  for (const [frame,owner] of data.Owners.entries()) if (owner !== 4294967295) statuses.push(api['initialize'+owner](frame));
  statuses.push(api.commit(0,0));
  for (const step of data.Steps) {
    statuses.push(api.begin(step.Sequence,0,0),api[(step.Handler?'other':'handler')+data.Owners[step.Frame]](step.Frame));
    if (step.Read) for (const frame of [0,1,15]) api['read'+data.Owners[frame]+'_2'](frame);
    states.push([...data.LocalRoots,data.SharedRoot].map(root => view.getInt32(api.working()+root*24+8,true)));
    masks.push(api.mask()); statuses.push(api.commit(step.Sequence,0));
    versions.push(view.getUint32(data.Versions,true));
  }
  const before = Buffer.from(memory.slice(api.committed(),api.committed()+data.Rows*24));
  statuses.push(api.begin(30,0,0),api['handler'+data.Owners[0]](0));
  const staged = view.getUint32(data.Versions+8,true) === 30 && view.getUint32(data.Versions,true) === 20;
  statuses.push(api.abort());
  const aborted = staged && !api.mask() && before.equals(Buffer.from(memory.slice(api.committed(),api.committed()+data.Rows*24)));
  statuses.push(api.begin(31,0,0));
  const restored = view.getUint32(data.Versions+8,true) === 20;
  statuses.push(api.abort());
  const fault = [];
  for (const [i,row] of [3*data.ComputedStride,9*data.ComputedStride,16*data.ComputedStride,-1].entries()) {
    statuses.push(api.begin(100+i,0,0)); fault.push(api.notify(row),api.commit(100+i,0)); statuses.push(api.abort());
  }
  process.stdout.write(JSON.stringify({States:states,Masks:masks,Versions:versions,Statuses:statuses,Fault:fault,Aborted:aborted,Restored:restored}));`, data, &got)
	for _, status := range got.Statuses {
		if status != 0 {
			t.Fatalf("shared transaction: %+v", got)
		}
	}
	shared := signal.New(vm.IntVal(10))
	models := map[uint32]*vm.VM{}
	for _, frame := range []uint32{0, 1, 15} {
		p := &l.programs[data.Owners[frame]]
		model := vm.NewVM(p.unit.Program, nil)
		vm.InitSignals(model, p.unit.Program)
		model.SetSignal("$count", shared)
		models[frame] = model
	}
	var want [][]int64
	for i, step := range data.Steps {
		p := &l.programs[data.Owners[step.Frame]]
		signal.Batch(func() {
			for _, expr := range p.unit.Program.Handlers[step.Handler].Body {
				models[step.Frame].Eval(expr)
			}
		})
		if step.Read {
			for _, frame := range []uint32{0, 1, 15} {
				models[frame].Eval(reads[l.programs[data.Owners[frame]].unit.Program.Name][2])
			}
		}
		var values []int64
		for _, frame := range []uint32{0, 1, 15} {
			values = append(values, int64(models[frame].Eval(reads[l.programs[data.Owners[frame]].unit.Program.Name][1]).Number()))
		}
		want = append(want, append(values, int64(shared.Get().Number())))
		if got.Masks[i] != 1|2|1<<15 || got.Versions[i] != step.Sequence {
			t.Fatalf("affected peers or equal-write version: %+v", got)
		}
	}
	if !reflect.DeepEqual(got.States, want) || !got.Aborted || !got.Restored {
		t.Fatalf("linked shared trace: %+v want %v", got, want)
	}
	for _, status := range got.Fault {
		if status != statusBadInput {
			t.Fatalf("invalid callback admitted: %+v", got)
		}
	}
}

func TestLinkedSharedStringsWithoutComputedsPreserveVersionsAndOwnership(t *testing.T) {
	var units []Unit
	for i, name := range []string{"PlainFirst", "PlainSecond"} {
		u := layoutUnit(t, name, 0, 0, 0, "$text")
		empty := addExpression(&u, program.OpLitString, program.TypeString, String, "")
		text := addExpression(&u, program.OpLitString, program.TypeString, String, []string{"héllo\x00🌴", "e\u0301"}[i])
		u.Program.Signals[0].Type, u.Program.Signals[0].Init, u.Contract.Signals[0].Kind = program.TypeString, empty, String
		write := addExpression(&u, program.OpSignalSet, program.TypeAny, AnyZero, "$text", text)
		u.Contract.Expressions[write].Pure = false
		u.Program.Handlers = []program.Handler{{Name: "write", Body: []program.ExprID{write, write}}}
		units = append(units, refreshUnit(t, u))
	}
	l, err := buildLinkedLayout(units, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	c, err := linkProgramCode(l)
	if err != nil {
		t.Fatal(err)
	}
	data := struct {
		FrameTable, Root, Versions uint32
		Initial                    string
	}{l.frameTable, l.shared[0].root, l.sharedVersions, scalarTransport(program.TypeString, 1, 0, "")}
	var got struct {
		Texts    []string
		Versions [][]uint32
		Statuses []uint32
		Owned    bool
	}
	runExpressionModule(t, exportLinkedTestModule(c), `
  const api = instance.exports, statuses = [], texts = [], versions = [];
  for (const [owner,frame] of [0,15].entries()) {
    view.setUint32(data.FrameTable+frame*16,owner,true); view.setUint32(data.FrameTable+frame*16+4,1,true);
  }
  statuses.push(api.begin(0,0,1)); memory.set(Buffer.from(data.Initial,'base64'),32768);
  statuses.push(api.store(data.Root,32768),api.initialize0(0),api.initialize1(15),api.commit(0,0));
  let owned = true;
  for (const [owner,frame] of [0,15].entries()) {
    statuses.push(api.begin(1,owner+1,0),api['handler'+owner](frame),api.commit(1,owner+1));
    const record = api.committed()+data.Root*24, start = view.getUint32(record+16,true), length = view.getUint32(record+20,true);
    owned = owned && start >= api.committed() && start+length <= api.committed()+65536;
    texts.push(Buffer.from(memory.subarray(start,start+length)).toString('utf8'));
    versions.push([view.getUint32(data.Versions,true),view.getUint32(data.Versions+4,true)]);
  }
  process.stdout.write(JSON.stringify({Texts:texts,Versions:versions,Statuses:statuses,Owned:owned}));`, data, &got)
	for _, status := range got.Statuses {
		if status != 0 {
			t.Fatalf("shared string transaction: %+v", got)
		}
	}
	shared := signal.New(vm.StringVal(""))
	for id, p := range l.programs {
		model := vm.NewVM(p.unit.Program, nil)
		vm.InitSignals(model, p.unit.Program)
		model.SetSignal("$text", shared)
		signal.Batch(func() {
			for _, expr := range p.unit.Program.Handlers[0].Body {
				model.Eval(expr)
			}
		})
		if got.Texts[id] != shared.Get().Text() || !reflect.DeepEqual(got.Versions[id], []uint32{1, uint32(id + 1)}) || !got.Owned {
			t.Fatalf("shared string/version identity: %+v", got)
		}
	}
}

func TestLinkedScalarValidationIsBoundedPureAndPreservesDefaults(t *testing.T) {
	l, err := buildLinkedLayout([]Unit{linkedComputedUnit(t, "Validation", 1, 1, 4)}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	c, err := linkProgramCode(l)
	if err != nil {
		t.Fatal(err)
	}
	m := exportLinkedTestModule(c)
	m.Exports = append(m.Exports, wasmgen.Export{Name: "validate", Function: c.scalar})
	type testCase struct {
		Packet                   string
		Pointer, Base, End, Want uint32
	}
	var cases []testCase
	add := func(packet string, want uint32) { cases = append(cases, testCase{packet, 32768, 32768, 65536, want}) }
	for _, packet := range []string{
		scalarTransport(program.TypeString, 0, 0, ""), scalarTransport(program.TypeString, 1, 0, ""),
		scalarTransport(program.TypeString, 1, 0, "héllo\x00🌴e\u0301"), scalarTransport(program.TypeString, 1, 0, strings.Repeat("x", 4096)),
		scalarTransport(program.TypeInt, 0, -2147483648, ""), scalarTransport(program.TypeInt, 0, 2147483647, ""),
		scalarTransport(program.TypeBool, 0, 0, ""), scalarTransport(program.TypeBool, 2, 0, ""), scalarTransport(program.TypeAny, 0, 0, ""),
	} {
		add(packet, 0)
	}
	for _, item := range []struct {
		Tag    program.ExprType
		Flags  uint32
		Number int64
		Text   string
		Want   uint32
	}{
		{program.TypeInt, 0, -2147483649, "", 3}, {program.TypeInt, 0, 2147483648, "", 3},
		{program.TypeString, 1, 0, strings.Repeat("x", 4097), 4}, {program.TypeString, 1, 0, "\xff", 2}, {program.TypeString, 0, 0, "x", 2},
		{program.TypeString, 2, 0, "", 2}, {program.TypeString, 1, 1, "", 2}, {program.TypeBool, 1, 0, "", 2}, {program.TypeBool, 2, 1, "", 2},
		{program.TypeInt, 1, 0, "", 2}, {program.TypeInt, 0, 0, "x", 2}, {program.TypeFloat, 0, 0, "", 2}, {program.TypeNode, 0, 0, "", 2},
		{program.TypeAny, 2, 0, "", 2}, {program.TypeAny, 0, 1, "", 2},
	} {
		add(scalarTransport(item.Tag, item.Flags, item.Number, item.Text), item.Want)
	}
	for _, fields := range [][2]uint32{{16, 1}, {16, 65536}, {16, 4294967295}} {
		packet, _ := base64.StdEncoding.DecodeString(scalarTransport(program.TypeString, 1, 0, "x"))
		binary.LittleEndian.PutUint32(packet[fields[0]:], fields[1])
		add(base64.StdEncoding.EncodeToString(packet), 2)
	}
	packet := scalarTransport(program.TypeInt, 0, 0, "")
	cases = append(cases, testCase{packet, 65512, 32768, 65536, 0})
	for _, bounds := range [][3]uint32{{0, 32768, 65536}, {65513, 32768, 65536}, {4294967295, 32768, 65536}, {32768, 65536, 32768}, {32768, 32768, 196609}} {
		cases = append(cases, testCase{packet, bounds[0], bounds[1], bounds[2], 2})
	}
	var got struct {
		Statuses []uint32
		Pure     bool
	}
	runExpressionModule(t, m, `
  const before = Buffer.concat([Buffer.from(memory.slice(0,32768)),Buffer.from(memory.slice(65536))]);
  const statuses = [];
  for (const item of data) {
    memory.set(Buffer.from(item.Packet,'base64'),32768);
    statuses.push(instance.exports.validate(item.Pointer,item.Base,item.End));
  }
  const after = Buffer.concat([Buffer.from(memory.slice(0,32768)),Buffer.from(memory.slice(65536))]);
  process.stdout.write(JSON.stringify({Statuses:statuses,Pure:before.equals(after)&&instance.exports.status()===0}));`, cases, &got)
	for i, item := range cases {
		if got.Statuses[i] != item.Want {
			t.Fatalf("scalar case %d: %d want %d", i, got.Statuses[i], item.Want)
		}
	}
	if !got.Pure {
		t.Fatal("validation changed memory or transaction error state")
	}
}

func TestLinkedDisposeClearsOwnedBanksAndKeepsPeersAndSharedValues(t *testing.T) {
	u := boolDOMUnit(t)
	u.Component, u.Contract.Component, u.Program.Name = "example/components.Disposable", "example/components.Disposable", "Disposable"
	u.Program.Signals[1].Name, u.Contract.Signals[1].Name = "$text", "$text"
	var read program.ExprID
	for i := range u.Program.Exprs {
		expr := &u.Program.Exprs[i]
		if (expr.Op == program.OpSignalGet || expr.Op == program.OpSignalSet) && expr.Value == "text" {
			expr.Value = "$text"
			if expr.Op == program.OpSignalGet {
				read = program.ExprID(i)
			}
		}
	}
	addComputed(&u, "copy", String, read)
	derived := addExpression(&u, program.OpSignalGet, program.TypeString, String, "copy")
	for i := range u.Program.Nodes {
		node := &u.Program.Nodes[i]
		if node.Kind == program.NodeExpr && node.Expr == read {
			node.Expr = derived
		}
		for j := range node.Attrs {
			if node.Attrs[j].Expr == read && node.Attrs[j].Kind == program.AttrExpr {
				node.Attrs[j].Expr = derived
			}
		}
	}
	u = refreshBindingUnit(t, u)
	input, _ := scalarInputUnit(t, "prop", Int, false)
	input.Component, input.Contract.Component, input.Program.Name = "example/components.Inputs", "example/components.Inputs", "Inputs"
	input = refreshUnit(t, input)
	l, err := buildLinkedLayout([]Unit{input, u}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	c, err := linkProgramCode(l)
	if err != nil {
		t.Fatal(err)
	}
	data := struct {
		Bindings                                                                   BindingSet
		ProgramID, InputID, FrameTable, MetaBase, MetaStride, SharedRoot, PropRoot uint32
		Owned, Peer                                                                []uint32
		Initial                                                                    [3]string
	}{FrameTable: l.frameTable, MetaBase: computedMetaBase, MetaStride: l.computedStride * computedMetaBytes, SharedRoot: l.shared[0].root, PropRoot: l.inputBase + 9*l.inputStride,
		Initial: [3]string{scalarTransport(program.TypeBool, 2, 0, ""), scalarTransport(program.TypeString, 1, 0, "héllo\x00🌴"), scalarTransport(program.TypeInt, 0, 7, "")}}
	for id, p := range l.programs {
		if p.unit.Program.Name == "Disposable" {
			data.ProgramID = uint32(id)
			data.Bindings = p.dom.bindings
		} else {
			data.InputID = uint32(id)
		}
	}
	for _, bank := range [][2]uint32{{0, l.localStride}, {l.inputBase, l.inputStride}, {l.computedBase, l.computedStride}, {l.baselineBase, l.baselineStride}} {
		for i := uint32(0); i < bank[1]; i++ {
			data.Owned = append(data.Owned, bank[0]+i)
			data.Peer = append(data.Peer, bank[0]+15*bank[1]+i)
		}
	}
	m := exportLinkedTestModule(c)
	m.Exports = append(m.Exports, wasmgen.Export{Name: "dispose", Function: c.dispose})
	var live instructions
	live.index(0x23, committedStringsGlobal)
	live.op(0x0b)
	m.Exports = append(m.Exports, wasmgen.Export{Name: "live", Function: uint32(len(m.Imports) + len(m.Functions))})
	m.Functions = append(m.Functions, wasmgen.Function{Signature: i32Signature(0), Body: live})
	var got struct {
		Statuses                                                 []uint32
		Busy, Invalid, Corrupt                                   []uint32
		Cleared, Preserved, Atomic, Live, Metadata, Prop, Shared bool
	}
	runExpressionModule(t, m, `
  const api = instance.exports, bound = [], patches = [], statuses = [];
  for (const frame of [0,15,9]) {
    view.setUint32(data.FrameTable+frame*16,frame===9?data.InputID:data.ProgramID,true);
    view.setUint32(data.FrameTable+frame*16+4,1,true);
  }
  statuses.push(api.begin(0,0,1));
  for (const frame of [0,15]) { memory.set(Buffer.from(data.Initial[0],'base64'),32768); statuses.push(api.store(frame,32768)); }
  memory.set(Buffer.from(data.Initial[1],'base64'),32768); statuses.push(api.store(data.SharedRoot,32768));
  memory.set(Buffer.from(data.Initial[2],'base64'),32768); statuses.push(api.store(data.PropRoot,32768));
  for (const frame of [0,15]) statuses.push(api['initialize'+data.ProgramID](frame),api['bind'+data.ProgramID](frame),api['render'+data.ProgramID](frame,1));
  statuses.push(api['initialize'+data.InputID](9),api.commit(0,0));
  const sharedBefore = Buffer.from(memory.slice(api.committed()+data.SharedRoot*24,api.committed()+data.SharedRoot*24+24));
  const peerBefore = Buffer.concat(data.Peer.map(root=>Buffer.from(memory.slice(api.committed()+root*24,api.committed()+root*24+24))));
  let released = data.Owned.reduce((sum,root)=>sum+view.getUint32(api.committed()+root*24+20,true),0);
  const liveBefore = api.live();
  statuses.push(api.begin(1,0,0)); const busy = [api.dispose(0),api.dispose(15)]; statuses.push(api.abort());
  const invalid = [api.dispose(-1),api.dispose(16)];
  const pointer = api.committed()+data.Owned.at(-1)*24;
  const flags = view.getUint32(pointer+4,true); view.setUint32(pointer+4,128,true);
  const beforeFailure = Buffer.from(memory.slice(api.committed(),api.committed()+65536));
  const corrupt = [api.dispose(0)];
  const atomic = beforeFailure.equals(Buffer.from(memory.slice(api.committed(),api.committed()+65536))) && view.getUint32(data.FrameTable+4,true)===1;
  view.setUint32(pointer+4,flags,true);
  statuses.push(api.dispose(0),api.dispose(0));
  const cleared = data.Owned.every(root=>memory.slice(api.committed()+root*24,api.committed()+root*24+24).every(byte=>byte===0));
  const preserved = peerBefore.equals(Buffer.concat(data.Peer.map(root=>Buffer.from(memory.slice(api.committed()+root*24,api.committed()+root*24+24))))) && sharedBefore.equals(Buffer.from(memory.slice(api.committed()+data.SharedRoot*24,api.committed()+data.SharedRoot*24+24)));
  const metadata = memory.slice(data.MetaBase,data.MetaBase+data.MetaStride).every(byte=>byte===0);
  const live = api.live()===liveBefore-released;
  statuses.push(api.dispose(9),api.dispose(9));
  const prop = memory.slice(api.committed()+data.PropRoot*24,api.committed()+data.PropRoot*24+24).every(byte=>byte===0);
  statuses.push(api.begin(2,0,0),api['handler'+data.ProgramID](15),api['render'+data.ProgramID](15,0),api.commit(2,0));
  const record = api.committed()+data.SharedRoot*24, start = view.getUint32(record+16,true), length = view.getUint32(record+20,true);
  const shared = Buffer.from(memory.slice(start,start+length)).toString('utf8')==='e\u0301';
  process.stdout.write(JSON.stringify({Statuses:statuses,Busy:busy,Invalid:invalid,Corrupt:corrupt,Cleared:cleared,Preserved:preserved,Atomic:atomic,Live:live,Metadata:metadata,Prop:prop,Shared:shared}));`, data, &got, domTestImports)
	for _, status := range got.Statuses {
		if status != 0 {
			t.Fatalf("dispose transaction: %+v", got)
		}
	}
	if !reflect.DeepEqual(got.Busy, []uint32{7, 7}) || !reflect.DeepEqual(got.Invalid, []uint32{2, 2}) || !reflect.DeepEqual(got.Corrupt, []uint32{2}) || !got.Cleared || !got.Preserved || !got.Atomic || !got.Live || !got.Metadata || !got.Prop || !got.Shared {
		t.Fatalf("dispose isolation: %+v", got)
	}
}

func TestLinkedPageOperationsMatchNativePatchOrderAndSequenceGenerations(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(strconv.FormatBool(shared), func(t *testing.T) {
			units := []Unit{linkedComputedUnit(t, "Small", 1, 1, 4), linkedComputedUnit(t, "Larger", 2, 2, 9)}
			if shared {
				for i := range units {
					u := &units[i]
					u.Program.Exprs[0].Value = "10"
					u.Program.Signals[0].Name, u.Contract.Signals[0].Name = "$count", "$count"
					for j := range u.Program.Exprs {
						expr := &u.Program.Exprs[j]
						if (expr.Op == program.OpSignalGet || expr.Op == program.OpSignalSet) && expr.Value == "local0" {
							expr.Value = "$count"
						}
					}
					*u = refreshBindingUnit(t, *u)
				}
			}
			l, err := buildLinkedLayout(units, DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			c, err := linkProgramCode(l)
			if err != nil {
				t.Fatal(err)
			}
			m := exportLinkedTestModule(c)
			for _, item := range []struct {
				name string
				fn   uint32
			}{{"initializeAll", c.initialize}, {"bindAll", c.bind}, {"renderAll", c.render}, {"dispatch", c.dispatch}} {
				m.Exports = append(m.Exports, wasmgen.Export{Name: item.name, Function: item.fn})
			}
			data := struct {
				Bindings              []BindingSet
				Owners, LocalRoots    []uint32
				Roots                 []string
				FrameTable, Sequences uint32
			}{Owners: make([]uint32, 16), FrameTable: l.frameTable, Sequences: l.frameSequences}
			for i := range data.Owners {
				data.Owners[i] = NoBindingName
			}
			data.Owners[0], data.Owners[7], data.Owners[15] = 0, 1, 0
			for _, p := range l.programs {
				data.Bindings = append(data.Bindings, p.dom.bindings)
			}
			for _, frame := range []uint32{0, 7, 15} {
				p := &l.programs[data.Owners[frame]]
				initial, _ := strconv.ParseInt(p.unit.Program.Exprs[0].Value, 10, 32)
				data.LocalRoots = append(data.LocalRoots, p.state.rows[frame])
				data.Roots = append(data.Roots, scalarTransport(program.TypeInt, 0, initial, ""))
			}
			type observedPatch struct {
				Frame uint32
				Op    vm.PatchOp
			}
			var got struct {
				Statuses, InitialSequences []uint32
				Patches                    [][]observedPatch
				Sequences                  [][]uint32
				Bound                      [][]uint32
			}
			runExpressionModule(t, m, `
  const api = instance.exports, bound = [], patches = [], statuses = [], batches = [], sequences = [];
  const frames = [0,7,15];
  for (const frame of frames) {
    view.setUint32(data.FrameTable+frame*16,data.Owners[frame],true); view.setUint32(data.FrameTable+frame*16+4,1,true);
  }
  statuses.push(api.begin(100,0,1));
  for (const [i,frame] of frames.entries()) {
    memory.set(Buffer.from(data.Roots[i],'base64'),32768); statuses.push(api.store(data.LocalRoots[i],32768));
    view.setUint32(data.Sequences+frame*8,i+1,true);
  }
  statuses.push(api.initializeAll(),api.bindAll(),api.renderAll(1),api.commit(100,0));
  if (patches.length) throw new Error('initial patches');
  const initialSequences = frames.map(frame=>view.getUint32(data.FrameTable+frame*16+8,true));
  for (const [i,frame] of [0,7,15,7,0].entries()) {
    statuses.push(api.begin(101+i,0,0),api.dispatch(frame,0),api.renderAll(0),api.commit(101+i,0));
    batches.push(patches.splice(0)); sequences.push(frames.map(frame=>view.getUint32(data.FrameTable+frame*16+8,true)));
  }
  process.stdout.write(JSON.stringify({Statuses:statuses,InitialSequences:initialSequences,Patches:batches,Sequences:sequences,Bound:bound}));`, data, &got, `{input: unexpected,
  bind: (id,binding,kind,tag) => { bound.push([id,binding,kind,tag>>>0]); return 0; },
  patch: (id,kind,binding,attribute,pointer) => {
    const descriptor = data.Bindings[data.Owners[id]].bindings[binding];
    if (kind!==0 || attribute!==-1 || view.getUint32(pointer,true)!==0 || view.getUint32(pointer+4,true)!==1) throw new Error('patch shape');
    const start = view.getUint32(pointer+16,true), length = view.getUint32(pointer+20,true);
    patches.push({frame:id,op:{kind,path:descriptor.path,text:Buffer.from(memory.subarray(start,start+length)).toString('utf8')}});
    return 0;
  }}`)
			for _, status := range got.Statuses {
				if status != 0 {
					t.Fatalf("page operation: %+v", got)
				}
			}
			if !reflect.DeepEqual(got.InitialSequences, []uint32{1, 2, 3}) {
				t.Fatalf("initial renderer replaced checkpoint sequences: %v", got.InitialSequences)
			}
			var bindings [][]uint32
			models, previous := map[uint32]*vm.VM{}, map[uint32]*vm.ResolvedTree{}
			sharedSignal := signal.New(vm.IntVal(10))
			for _, frame := range []uint32{0, 7, 15} {
				p := &l.programs[data.Owners[frame]]
				model := vm.NewVM(p.unit.Program, nil)
				vm.InitSignals(model, p.unit.Program)
				if shared {
					model.SetSignal("$count", sharedSignal)
				}
				models[frame], previous[frame] = model, model.EvalTree()
				for _, binding := range p.dom.bindings.Bindings {
					bindings = append(bindings, []uint32{frame, binding.ID, uint32(binding.Kind), binding.TagID})
				}
			}
			if !reflect.DeepEqual(got.Bound, bindings) {
				t.Fatal("binding calls did not follow manifest and physical preorder")
			}
			last := []uint32{1, 2, 3}
			for step, frame := range []uint32{0, 7, 15, 7, 0} {
				p := &l.programs[data.Owners[frame]]
				signal.Batch(func() {
					for _, expr := range p.unit.Program.Handlers[0].Body {
						models[frame].Eval(expr)
					}
				})
				var want []observedPatch
				for i, peer := range []uint32{0, 7, 15} {
					if !shared && peer != frame {
						continue
					}
					next := models[peer].EvalTree()
					for _, op := range vm.ReconcileTrees(previous[peer], next, l.programs[data.Owners[peer]].unit.Program.StaticMask) {
						want = append(want, observedPatch{peer, op})
					}
					previous[peer], last[i] = next, uint32(101+step)
				}
				if !reflect.DeepEqual(got.Patches[step], want) || !reflect.DeepEqual(got.Sequences[step], last) {
					t.Fatalf("patch/sequence step %d: %+v %v want %+v %v", step, got.Patches[step], got.Sequences[step], want, last)
				}
			}
		})
	}
}

func TestLinkedPageOperationFailuresPreserveRootsAndInstanceSequences(t *testing.T) {
	l, err := buildLinkedLayout([]Unit{linkedComputedUnit(t, "Failure", 1, 1, 4)}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	c, err := linkProgramCode(l)
	if err != nil {
		t.Fatal(err)
	}
	m := exportLinkedTestModule(c)
	for _, item := range []struct {
		name string
		fn   uint32
	}{{"dispatch", c.dispatch}, {"initializeAll", c.initialize}, {"bindAll", c.bind}, {"renderAll", c.render}} {
		m.Exports = append(m.Exports, wasmgen.Export{Name: item.name, Function: item.fn})
	}
	data := struct {
		domTestData
		FrameTable, Sequences, Rows uint32
		Initial                     string
	}{domData(c.programs[0]), l.frameTable, l.frameSequences, l.roots, scalarTransport(program.TypeInt, 0, 4, "")}
	var got struct {
		Statuses, Idle, Failures, Commits []uint32
		Preserved, Aborted                bool
	}
	runExpressionModule(t, m, `
  const api = instance.exports, bound = [], patches = [], statuses = [], failures = [], commits = [];
  const idle = [api.dispatch(0,0),api.initializeAll(),api.bindAll(),api.renderAll(0)];
  view.setUint32(data.FrameTable+4,1,true);
  statuses.push(api.begin(0,0,1)); memory.set(Buffer.from(data.Initial,'base64'),32768);
  statuses.push(api.store(0,32768),api.initializeAll(),api.bindAll(),api.renderAll(1),api.commit(0,0));
  const before = Buffer.from(memory.slice(api.committed(),api.committed()+data.Rows*24));
  const metadata = Buffer.from(memory.slice(data.FrameTable,data.FrameTable+256));
  const operations = [()=>api.dispatch(1,0),()=>api.dispatch(16,0),()=>api.dispatch(-1,0),()=>api.dispatch(0,1),()=>api.dispatch(0,-1),()=>api.renderAll(2),()=>api.initializeAll(),()=>api.bindAll(),
    ()=>{view.setUint32(data.FrameTable,1,true);const result=api.dispatch(0,0);view.setUint32(data.FrameTable,0,true);return result;},
    ()=>{view.setUint32(data.FrameTable+4,2,true);const result=api.renderAll(1);view.setUint32(data.FrameTable+4,1,true);return result;}];
  for (const [i,operation] of operations.entries()) {
    statuses.push(api.begin(i+1,0,0)); failures.push(operation()); commits.push(api.commit(i+1,0)); statuses.push(api.abort());
  }
  data.PatchStatus = 6;
  statuses.push(api.begin(20,0,0),api.dispatch(0,0)); failures.push(api.renderAll(0)); commits.push(api.commit(20,0)); statuses.push(api.abort());
  const preserved = before.equals(Buffer.from(memory.slice(api.committed(),api.committed()+data.Rows*24))) && metadata.equals(Buffer.from(memory.slice(data.FrameTable,data.FrameTable+256)));
  statuses.push(api.begin(21,0,0));
  const aborted = view.getUint32(data.Sequences,true)===0 && view.getUint32(data.FrameTable+8,true)===0;
  statuses.push(api.abort());
  process.stdout.write(JSON.stringify({Statuses:statuses,Idle:idle,Failures:failures,Commits:commits,Preserved:preserved,Aborted:aborted}));`, data, &got, domTestImports)
	for _, status := range got.Statuses {
		if status != 0 {
			t.Fatalf("page operation setup: %+v", got)
		}
	}
	want := []uint32{2, 2, 2, 2, 2, 2, 8, 8, 2, 2, 6}
	if !reflect.DeepEqual(got.Idle, []uint32{7, 7, 7, 7}) || !reflect.DeepEqual(got.Failures, want) || !reflect.DeepEqual(got.Commits, want) || !got.Preserved || !got.Aborted {
		t.Fatalf("failed operation published roots or sequences: %+v", got)
	}
}
