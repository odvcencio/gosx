package aot

import (
	"reflect"
	"strconv"
	"testing"

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
