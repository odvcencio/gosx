//go:build !tinygo && !js

package ir_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
)

func TestAuthoredIslandHelperCallsDispatch(t *testing.T) {
	const source = `package app
import "m31labs.dev/gosx/signal"
//gosx:island
component Helpers() {
 count := signal.New(0)
 label := signal.New("initial")
 put := func() {
  if value == "" { return }
  label.Set(value)
  count.Set(count.Get() + 1)
 }
 save := func() { put(); count.Set(count.Get() + 10) }
 run := func() { save(); count.Set(count.Get() + 100) }
 return <section><button onClick={run}>Run</button><p>{label.Get()}</p><output>{count.Get()}</output></section>
}`
	for _, compiled := range guardPrograms(t, source) {
		island := vm.NewIsland(compiled, `{}`)
		t.Cleanup(island.Dispose)
		for _, step := range []struct{ handler, event, label, count string }{
			{"run", `{}`, "initial", "110"},
			{"run", `{"value":"Selena 🌊"}`, "Selena 🌊", "221"},
			{"put", `{"value":"direct"}`, "direct", "222"},
		} {
			island.Dispatch(step.handler, step.event)
			if got := guardText(island, "output"); !slices.Equal(got, []string{step.count}) {
				t.Fatalf("count after %s %s = %q, want %s", step.handler, step.event, got, step.count)
			}
			if got := guardText(island, "p"); !slices.Equal(got, []string{step.label}) {
				t.Fatalf("event snapshot after %s = %q, want %q", step.handler, got, step.label)
			}
		}
		var names []string
		for _, fn := range compiled.Funcs {
			names = append(names, fn.Name)
		}
		if !slices.Equal(names, []string{"put", "save"}) {
			t.Fatalf("only called helpers need function descriptors: %q", names)
		}
	}
}

func TestAuthoredIslandHelperCallsShadowBuiltins(t *testing.T) {
	for _, name := range []string{"append", "len"} {
		t.Run(name, func(t *testing.T) {
			source := fmt.Sprintf(`package app
//gosx:island
component Helpers() {
 count := signal.New(0)
 %s := func() { count.Set(count.Get() + 1) }
 run := func() { %s() }
 return <section><button onClick={run}>Run</button><output>{count.Get()}</output></section>
}`, name, name)
			for _, compiled := range guardPrograms(t, source) {
				island := vm.NewIsland(compiled, `{}`)
				t.Cleanup(island.Dispose)
				island.Dispatch("run", `{}`)
				if got := guardText(island, "output"); !slices.Equal(got, []string{"1"}) {
					t.Fatalf("builtin replaced the declared %s helper: %q", name, got)
				}
			}
		})
	}
}

func TestAuthoredIslandHelperCallsRejectInvalidUses(t *testing.T) {
	for _, tc := range []struct{ name, declaration, body, view string }{
		{"arguments", `helper := func() {}`, `helper(1)`, `{count.Get()}`},
		{"parameter declaration", `helper := func(n int) {}`, `helper()`, `{count.Get()}`},
		{"result declaration", `helper := func() int { count.Set(1) }`, `helper()`, `{count.Get()}`},
		{"condition", `helper := func() {}`, `if helper() { count.Set(1) }`, `{count.Get()}`},
		{"signal value", `helper := func() {}`, `count.Set(helper())`, `{count.Get()}`},
		{"render value", `helper := func() {}`, `count.Set(1)`, `{helper()}`},
		{"signal initializer", `helper := func() {}; other := signal.New(helper())`, `count.Set(1)`, `{count.Get()}`},
		{"computed initializer", `helper := func() {}; other := signal.Derive(func() int { return helper() })`, `count.Set(1)`, `{count.Get()}`},
		{"missing declaration", `helper := func() {}`, `ghost()`, `<button onClick={ghost}>Ghost</button>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := fmt.Sprintf("package app\n//gosx:island\ncomponent Helpers() {\ncount := signal.New(0)\n%s\nrun := func() { %s }\nreturn <section><button onClick={run}>Run</button><output>%s</output></section>\n}", tc.declaration, tc.body, tc.view)
			p, err := parse(t, []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			_, err = ir.LowerIsland(p, 0)
			if err == nil || !strings.Contains(err.Error(), "helper") {
				t.Fatalf("invalid helper use accepted or lost context: %v", err)
			}
		})
	}
}

func TestAuthoredIslandHelperCallsFromLegacyInlineEvent(t *testing.T) {
	const source = `package app
//gosx:island
func Legacy() Node {
 count := signal.New(0)
 helper := func() { count.Set(count.Get() + 1) }
 return <section><button data-on-click="helper(); count.Set(count.Get() + 10)">Run</button><output>{count.Get()}</output></section>
}`
	for _, compiled := range guardPrograms(t, source) {
		island := vm.NewIsland(compiled, `{}`)
		t.Cleanup(island.Dispose)
		var handler string
		for _, node := range compiled.Nodes {
			for _, attr := range node.Attrs {
				if attr.Name == "click" {
					handler = attr.Event
				}
			}
		}
		if handler == "" {
			t.Fatal("missing legacy inline event")
		}
		island.Dispatch(handler, `{}`)
		if got := guardText(island, "output"); !slices.Equal(got, []string{"11"}) {
			t.Fatalf("inline event lost its helper call: %q", got)
		}
	}
}
