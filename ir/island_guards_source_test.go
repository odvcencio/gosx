//go:build !tinygo && !js

package ir_test

import (
	"slices"
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/program"
)

type guardBrowser struct{ calls []string }

func (b *guardBrowser) Call(method string, _ []vm.Value) (vm.Value, error) {
	b.calls = append(b.calls, method)
	return vm.Value{}, nil
}

func guardPrograms(t *testing.T, source string) []*program.Program {
	t.Helper()
	p, err := parse(t, []byte(source))
	if err != nil {
		t.Fatal(err)
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
	return []*program.Program{lowered, decoded}
}

func guardText(island *vm.Island, tag string) []string {
	var result []string
	tree := island.CurrentTree()
	for _, node := range tree.Nodes {
		if node.Tag == tag {
			var text string
			for _, child := range node.Children {
				text += tree.Nodes[child].Text
			}
			result = append(result, text)
		}
	}
	return result
}

func TestAuthoredIslandGuardClausesDispatch(t *testing.T) {
	const source = `package app
import "m31labs.dev/gosx/signal"
import "m31labs.dev/gosx/browser"
//gosx:island
component Collection() {
 items := signal.New([]string{"Go"})
 draft := signal.New("")
 edit := func() { draft.Set(value) }
 add := func() {
  browser.PreventDefault()
  if draft.Get() == "" || len(items.Get()) >= 3 { return }
  items.Set(append(items.Get(), draft.Get()))
  draft.Set("")
  browser.Focus("#draft")
 }
 return <section><input id="draft" value={draft.Get()} onInput={edit}/><button onClick={add}>Add</button><ul><Each of={items.Get()} as="item"><li>{item}</li></Each></ul></section>
}`
	for _, compiled := range guardPrograms(t, source) {
		island := vm.NewIsland(compiled, `{}`)
		t.Cleanup(island.Dispose)
		browser := &guardBrowser{}
		island.BindHost("browser", browser)
		island.Dispatch("add", `{}`)
		if got := guardText(island, "li"); !slices.Equal(got, []string{"Go"}) {
			t.Fatalf("empty submission mutated items: %q", got)
		}
		if !slices.Equal(browser.calls, []string{"PreventDefault"}) {
			t.Fatalf("rejected submission ran later effects: %q", browser.calls)
		}
		for _, event := range []string{`{"value":"Selena 🌊"}`, `{"value":"GoSX"}`, `{"value":"over limit"}`} {
			island.Dispatch("edit", event)
			island.Dispatch("add", `{}`)
		}
		if got := guardText(island, "li"); !slices.Equal(got, []string{"Go", "Selena 🌊", "GoSX"}) {
			t.Fatalf("handler did not enforce its own limit: %q", got)
		}
		if !slices.Equal(browser.calls, []string{"PreventDefault", "PreventDefault", "Focus", "PreventDefault", "Focus", "PreventDefault"}) {
			t.Fatalf("guard/effect ordering = %q", browser.calls)
		}
	}
}

func TestAuthoredIslandGuardClausesNestedBranches(t *testing.T) {
	const source = `package app
import "m31labs.dev/gosx/signal"
//gosx:island
component Branches() {
 label := signal.New("initial")
 count := signal.New(0)
 choose := func() {
  if checked {
   if value == "" { return }
   label.Set(value)
  } else if value == "clear" {
   label.Set("cleared")
   return
  } else {
   label.Set("fallback")
  }
  count.Set(count.Get() + 1)
 }
 return <section><button onClick={choose}>Choose</button><p>{label.Get()}</p><output>{count.Get()}</output></section>
}`
	for _, compiled := range guardPrograms(t, source) {
		island := vm.NewIsland(compiled, `{}`)
		t.Cleanup(island.Dispose)
		for _, step := range []struct{ event, label, count string }{
			{`{"checked":true,"value":""}`, "initial", "0"},
			{`{"checked":true,"value":"coast"}`, "coast", "1"},
			{`{"value":"clear"}`, "cleared", "1"},
			{`{"value":"other"}`, "fallback", "2"},
		} {
			island.Dispatch("choose", step.event)
			if got := guardText(island, "p"); !slices.Equal(got, []string{step.label}) {
				t.Fatalf("label after %s = %q", step.event, got)
			}
			if got := guardText(island, "output"); !slices.Equal(got, []string{step.count}) {
				t.Fatalf("count after %s = %q", step.event, got)
			}
		}
	}
}

func TestAuthoredIslandGuardClausesRejectUnsupportedStatements(t *testing.T) {
	for _, body := range []string{
		`if n := 1; n > 0 { return }`,
		`if true { return 1 }`,
		`if true { for {} }`,
		`if true { count = 1 }`,
		`if true { defer count.Set(1) }`,
	} {
		t.Run(body, func(t *testing.T) {
			source := "package app\n//gosx:island\ncomponent Guard() {\ncount := signal.New(0)\nrun := func() { " + body + " }\nreturn <button onClick={run}>{count.Get()}</button>\n}"
			p, err := parse(t, []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			_, err = ir.LowerIsland(p, 0)
			if err == nil || !strings.Contains(err.Error(), "handler run") {
				t.Fatalf("unsupported statement accepted or lost handler context: %v", err)
			}
		})
	}
}
