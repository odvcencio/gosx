package aot

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
)

func refreshBindingUnit(t *testing.T, u Unit) Unit {
	t.Helper()
	var err error
	u.Contract.Bindings, err = ContractBindings(u.Program)
	if err != nil {
		t.Fatal(err)
	}
	u.Program.StaticMask = make([]bool, len(u.Program.Nodes))
	var walk func(program.NodeID) bool
	walk = func(id program.NodeID) bool {
		n := u.Program.Nodes[id]
		static := n.Kind != program.NodeExpr
		for _, attr := range n.Attrs {
			static = static && attr.Kind != program.AttrExpr && attr.Kind != program.AttrEvent
		}
		for _, child := range n.Children {
			if !walk(child) {
				static = false
			}
		}
		u.Program.StaticMask[id] = static
		return static
	}
	walk(u.Program.Root)
	return refreshUnit(t, u)
}

func fixedBindingUnit(t *testing.T) Unit {
	u := stateCounterUnit(t)
	boolID := addExpression(&u, program.OpLitBool, program.TypeBool, Bool, "false")
	textID := addExpression(&u, program.OpLitString, program.TypeString, String, "title")
	u.Program.Nodes = []program.Node{
		{Kind: program.NodeElement, Tag: "div", Attrs: []program.Attr{{Kind: program.AttrExpr, Name: "title", Expr: textID}}, Children: []program.NodeID{1, 2, 3, 4, 6, 7}},
		{Kind: program.NodeText, Text: "count: "},
		{Kind: program.NodeExpr, Expr: 3},
		{Kind: program.NodeText, Text: "!"},
		{Kind: program.NodeElement, Tag: "button", Attrs: []program.Attr{{Kind: program.AttrStatic, Name: "type", Value: "button"}, {Kind: program.AttrEvent, Name: "onClick", Event: "increment"}}, Children: []program.NodeID{5}},
		{Kind: program.NodeText, Text: "increase"},
		{Kind: program.NodeElement, Tag: "input", Attrs: []program.Attr{{Kind: program.AttrExpr, Name: "value", Expr: 3}, {Kind: program.AttrExpr, Name: "disabled", Expr: boolID}, {Kind: program.AttrExpr, Name: "data-count", Expr: 3}}},
		{Kind: program.NodeText, Text: "\n"},
	}
	return refreshBindingUnit(t, u)
}

func TestBindingsPhysicalPathsMatchNativeVM(t *testing.T) {
	u := fixedBindingUnit(t)
	set, err := BuildBindings(u)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set.Tags, []string{"button", "div", "input"}) {
		t.Fatalf("name tables: %+v", set)
	}
	want := []Binding{
		{ID: 0, Path: "", Kind: program.NodeElement, TagID: 1, SourceNodes: []program.NodeID{0}, Attributes: []string{"title"}, Events: []BindingEvent{}},
		{ID: 1, Path: "0", Kind: program.NodeText, TagID: NoBindingName, SourceNodes: []program.NodeID{1, 2, 3}, Attributes: []string{}, Events: []BindingEvent{}},
		{ID: 2, Path: "1", Kind: program.NodeElement, TagID: 0, SourceNodes: []program.NodeID{4}, Attributes: []string{"type"}, Events: []BindingEvent{{Type: "click", Handler: 0}}},
		{ID: 3, Path: "1/0", Kind: program.NodeText, TagID: NoBindingName, SourceNodes: []program.NodeID{5}, Attributes: []string{}, Events: []BindingEvent{}},
		{ID: 4, Path: "2", Kind: program.NodeElement, TagID: 2, SourceNodes: []program.NodeID{6}, Attributes: []string{"value", "disabled", "data-count"}, Events: []BindingEvent{}},
		{ID: 5, Path: "3", Kind: program.NodeText, TagID: NoBindingName, SourceNodes: []program.NodeID{7}, Attributes: []string{}, Events: []BindingEvent{}},
	}
	if !reflect.DeepEqual(set.Bindings, want) {
		t.Fatalf("bindings: %+v", set.Bindings)
	}
	tree := vm.ResolveInitialTree(u.Program, "")
	var paths, tags []string
	var walk func(int, string)
	walk = func(index int, path string) {
		node := tree.Nodes[index]
		paths = append(paths, path)
		tags = append(tags, node.Tag)
		for i, child := range node.Children {
			childPath := strconv.Itoa(i)
			if path != "" {
				childPath = path + "/" + childPath
			}
			walk(child, childPath)
		}
	}
	walk(0, "")
	for i, binding := range set.Bindings {
		tag := ""
		if binding.TagID != NoBindingName {
			tag = set.Tags[binding.TagID]
		}
		if paths[i] != binding.Path || tags[i] != tag {
			t.Fatalf("VM path %d: %v/%v binding %+v", i, paths, tags, binding)
		}
	}
	if len(paths) != len(set.Bindings) || tree.Nodes[tree.Nodes[0].Children[0]].Text != "count: 0!" {
		t.Fatalf("physical VM tree: %+v", tree)
	}
}

func TestBindingsNestedTextGroupsKeepPhysicalIndices(t *testing.T) {
	u := literalUnit(t)
	u.Program.Nodes = []program.Node{
		{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1, 2, 4, 5}},
		{Kind: program.NodeText, Text: "a"},
		{Kind: program.NodeElement, Tag: "span", Children: []program.NodeID{3, 6}},
		{Kind: program.NodeText, Text: "b"},
		{Kind: program.NodeText, Text: "c"},
		{Kind: program.NodeExpr, Expr: 0},
		{Kind: program.NodeExpr, Expr: 0},
	}
	u = refreshBindingUnit(t, u)
	set, err := BuildBindings(u)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, binding := range set.Bindings {
		paths = append(paths, binding.Path)
	}
	if !reflect.DeepEqual(paths, []string{"", "0", "1", "1/0", "2"}) || !reflect.DeepEqual(set.Bindings[3].SourceNodes, []program.NodeID{3, 6}) || !reflect.DeepEqual(set.Bindings[4].SourceNodes, []program.NodeID{4, 5}) {
		t.Fatalf("nested groups: %+v", set.Bindings)
	}
}

func TestBindingsEventAliasesUseDeclaredHandlerIDs(t *testing.T) {
	for _, name := range []string{"click", "input", "change", "keydown", "keyup", "focus", "blur", "onClick", "onInput", "onChange", "onKeyDown", "onKeyUp", "onFocus", "onBlur"} {
		u := staticUnit(t)
		u.Program.Handlers = []program.Handler{{Name: "first"}, {Name: "second"}}
		u.Program.Nodes[0].Attrs = []program.Attr{{Kind: program.AttrEvent, Name: name, Event: "second"}}
		u = refreshBindingUnit(t, u)
		set, err := BuildBindings(u)
		if err != nil {
			t.Fatal(err)
		}
		if len(set.Bindings[0].Events) != 1 || set.Bindings[0].Events[0].Handler != 1 {
			t.Fatalf("event %s: %+v", name, set)
		}
		island := vm.NewIsland(u.Program, "")
		marker := island.CurrentTree().Nodes[0].DOMAttrs[0].Name
		if marker != "data-gosx-on-"+set.Bindings[0].Events[0].Type {
			t.Fatalf("event identity: %+v", set)
		}
	}
}

func TestBindingsRevalidateRetainedUnitIdentity(t *testing.T) {
	u := fixedBindingUnit(t)
	u.Program.Nodes[1].Text = "different"
	if set, err := BuildBindings(u); err == nil || len(set.Bindings) != 0 {
		t.Fatal("accepted changed program with retained proof identity")
	}
	u = fixedBindingUnit(t)
	u.Contract.Bindings[0].Attributes[0] = "class"
	if set, err := BuildBindings(u); err == nil || len(set.Bindings) != 0 {
		t.Fatal("accepted changed binding contract")
	}
}

func TestBindingsRejectUnsafeOrUnprovedTopology(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Unit)
	}{
		{"parser reparenting", func(u *Unit) { u.Program.Nodes[0].Tag = "p"; u.Program.Nodes[4].Tag = "div" }},
		{"void children", func(u *Unit) { u.Program.Nodes[4].Tag = "br" }},
		{"structural node", func(u *Unit) { u.Program.Nodes[4].Kind = program.NodeConditional }},
		{"dynamic URL", func(u *Unit) { u.Program.Nodes[0].Attrs[0].Name = "href" }},
		{"event attribute text", func(u *Unit) { u.Program.Nodes[0].Attrs[0].Name = "onclick" }},
		{"action marker", func(u *Unit) { u.Program.Nodes[0].Attrs[0].Name = "data-action" }},
		{"reserved marker", func(u *Unit) { u.Program.Nodes[0].Attrs[0].Name = "data-gosx-handler" }},
		{"dynamic key", func(u *Unit) { u.Program.Nodes[0].Attrs[0].Name = "key" }},
		{"unproved scalar", func(u *Unit) { u.Contract.Expressions[3].Kind = ScalarKind("uint") }},
		{"unknown handler", func(u *Unit) { u.Program.Nodes[4].Attrs[1].Event = "unknown" }},
		{"submit button", func(u *Unit) { u.Program.Nodes[4].Attrs[0].Value = "submit" }},
		{"dangling group", func(u *Unit) { u.Contract.Bindings[1].Nodes = []program.NodeID{1, 2} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := fixedBindingUnit(t)
			tc.change(&u)
			if set, err := BuildBindings(u); err == nil || len(set.Bindings) != 0 {
				t.Fatalf("accepted invalid bindings: %+v", set)
			}
		})
	}
}

func TestBindingsTablesAndJSONAreDeterministic(t *testing.T) {
	u := fixedBindingUnit(t)
	first, err := BuildBindings(u)
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		set, err := BuildBindings(u)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(set)
		if string(raw) != string(before) {
			t.Fatal("binding descriptor bytes changed")
		}
	}
	if strings.Contains(string(before), "null") {
		t.Fatalf("descriptor arrays are absent: %s", before)
	}
	first.Bindings[1].SourceNodes[0] = 255
	first.Bindings[4].Attributes[0] = "changed"
	first.Bindings[2].Events[0].Type = "changed"
	second, err := BuildBindings(u)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(second)
	if string(after) != string(before) {
		t.Fatal("descriptor mutation changed the unit or later output")
	}
	static, err := BuildBindings(staticUnit(t))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(static)
	if string(raw) != `{"tags":["div"],"bindings":[{"id":0,"path":"","kind":0,"tagId":0,"sourceNodes":[0],"attributes":[],"events":[]}]}` {
		t.Fatalf("static descriptor: %s", raw)
	}
}

func TestBindingsAcceptProfileNodeLimitAndDeepPaths(t *testing.T) {
	for _, count := range []int{64, 256} {
		u := staticUnit(t)
		u.Program.Nodes = make([]program.Node, count)
		for i := range u.Program.Nodes {
			u.Program.Nodes[i] = program.Node{Kind: program.NodeElement, Tag: "div"}
			if count == 64 && i+1 < count {
				u.Program.Nodes[i].Children = []program.NodeID{program.NodeID(i + 1)}
			}
		}
		if count == 256 {
			for i := 1; i < count; i++ {
				u.Program.Nodes[0].Children = append(u.Program.Nodes[0].Children, program.NodeID(i))
			}
		}
		u = refreshBindingUnit(t, u)
		set, err := BuildBindings(u)
		if err != nil {
			t.Fatal(err)
		}
		if len(set.Bindings) != count {
			t.Fatalf("node limit: %d", len(set.Bindings))
		}
		last := set.Bindings[count-1].Path
		if count == 64 && last != strings.TrimSuffix(strings.Repeat("0/", 63), "/") || count == 256 && last != "254" {
			t.Fatalf("last physical path: %q", last)
		}
	}
}

func TestBindingsAttributeIDsAreLocalDeclarationIndices(t *testing.T) {
	u := fixedBindingUnit(t)
	u.Program.Nodes[4].Attrs = append(u.Program.Nodes[4].Attrs,
		program.Attr{Kind: program.AttrStatic, Name: "title", Value: "button title"})
	u = refreshBindingUnit(t, u)
	set, err := BuildBindings(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		binding int
		names   []string
	}{
		{0, []string{"title"}}, {2, []string{"type", "title"}}, {4, []string{"value", "disabled", "data-count"}},
	} {
		if !reflect.DeepEqual(set.Bindings[item.binding].Attributes, item.names) {
			t.Fatalf("binding-local attribute IDs: %+v", set.Bindings[item.binding])
		}
	}
}
