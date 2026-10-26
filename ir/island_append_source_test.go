package ir_test

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/program"
)

func TestAuthoredIslandGoAppendSurvivesBinaryRoundTrip(t *testing.T) {
	source := []byte(`package app
import "m31labs.dev/gosx/signal"
//gosx:island
component Collection() {
 items := signal.New([]string{"Go"})
 draft := signal.New("")
 edit := func() { draft.Set(value) }
 add := func() { items.Set(append(items.Get(), draft.Get(), "GoSX")); draft.Set("") }
 clear := func() { items.Set([]string{}) }
 return <section><input value={draft.Get()} onInput={edit}/><button onClick={add}>Add</button><button onClick={clear}>Clear</button><ul><Each of={items.Get()} as="item"><li>{item}</li></Each></ul></section>
}`)
	p, err := parse(t, source)
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics := ir.Validate(p); len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
	lowered, err := ir.LowerIsland(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := program.EncodeBinary(lowered)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := program.DecodeBinary(encoded)
	if err != nil {
		t.Fatal(err)
	}
	for _, compiled := range []*program.Program{lowered, decoded} {
		machine := vm.NewVM(compiled, nil)
		vm.InitSignals(machine, compiled)
		t.Cleanup(func() { machine.SwapProgram(&program.Program{}) })
		dispatch := func(name string, value string) {
			t.Helper()
			machine.SetEventData(map[string]vm.Value{"value": vm.StringVal(value)})
			defer machine.ClearEventData()
			for _, handler := range compiled.Handlers {
				if handler.Name == name {
					for _, operation := range handler.Body {
						machine.Eval(operation)
					}
					return
				}
			}
			t.Fatalf("missing %s handler", name)
		}
		assertItems := func(want ...string) {
			t.Helper()
			tree := machine.EvalTree()
			var got []string
			for _, node := range tree.Nodes {
				if node.Tag == "li" {
					var text string
					for _, child := range node.Children {
						text += tree.Nodes[child].Text
					}
					got = append(got, text)
				}
			}
			if len(got) != len(want) {
				t.Fatalf("items = %q, want %q", got, want)
			}
			for i, value := range want {
				if got[i] != value {
					t.Fatalf("items = %q, want %q", got, want)
				}
			}
		}
		assertItems("Go")
		dispatch("edit", "Selena 🌊")
		dispatch("add", "")
		assertItems("Go", "Selena 🌊", "GoSX")
		dispatch("edit", "Next")
		dispatch("add", "")
		assertItems("Go", "Selena 🌊", "GoSX", "Next", "GoSX")
		dispatch("clear", "")
		assertItems()
	}
}
