package ir_test

import (
	"bytes"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/program"
)

const callbackCounterSource = `package app
import "m31labs.dev/gosx/signal"
type ControlProps struct { Count int; OnActivate func() }
component Control(props: ControlProps) {
 return <button type="button" onClick={props.OnActivate}>Count: {props.Count}</button>
}
component Wrapper(props: ControlProps) {
 return <Control Count={props.Count} OnActivate={props.OnActivate} />
}
//gosx:island
component Counter() {
 count := signal.New(0)
 increment := func() { count.Set(count.Get() + 1) }
 return <section><Wrapper Count={count.Get()} OnActivate={increment} /><Control Count={count.Get()} OnActivate={increment} /></section>
}
`

func TestAuthoredIslandCallbackComposition(t *testing.T) {
	p, err := parse(t, []byte(callbackCounterSource))
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics := ir.Validate(p); len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
	index := -1
	for i, component := range p.Components {
		if component.Name == "Counter" {
			index = i
		}
	}
	lowered, err := ir.LowerIsland(p, index)
	if err != nil {
		t.Fatal(err)
	}
	if len(lowered.Handlers) != 1 || lowered.Handlers[0].Name != "increment" {
		t.Fatalf("forwarding duplicated behavior: %+v", lowered.Handlers)
	}
	machine := vm.NewVM(lowered, nil)
	vm.InitSignals(machine, lowered)
	t.Cleanup(func() { machine.SwapProgram(&program.Program{}) })
	for count := 0; count < 3; count++ {
		buttons, readings := 0, 0
		for _, node := range lowered.Nodes {
			if node.Tag == "button" {
				buttons++
				for _, attr := range node.Attrs {
					if attr.Kind == program.AttrEvent && attr.Event != "increment" {
						t.Fatalf("wrong callback target: %+v", attr)
					}
				}
			}
			if node.Kind == program.NodeExpr {
				readings++
				if got := machine.Eval(node.Expr).Number(); got != float64(count) {
					t.Fatalf("control value = %v, want %d", got, count)
				}
			}
		}
		if buttons != 2 || readings != 2 {
			t.Fatalf("buttons/readings = %d/%d, want 2/2", buttons, readings)
		}
		for _, operation := range lowered.Handlers[0].Body {
			machine.Eval(operation)
		}
	}
}

func TestAuthoredIslandCallbackRejectsHandlerArguments(t *testing.T) {
	source := strings.Replace(callbackCounterSource, "increment := func()", "increment := func(unused int)", 1)
	p, err := parse(t, []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	for i, component := range p.Components {
		if component.Name == "Counter" {
			_, err := ir.LowerIsland(p, i)
			if err == nil || !strings.Contains(err.Error(), "signature func()") {
				t.Fatalf("handler with arguments accepted: %v", err)
			}
			return
		}
	}
	t.Fatal("missing Counter")
}

func TestAuthoredIslandCallbackRejectsHandlerResults(t *testing.T) {
	source := strings.Replace(callbackCounterSource, "increment := func() { count.Set(count.Get() + 1) }", "increment := func() int { return 1 }", 1)
	p, err := parse(t, []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	for index, component := range p.Components {
		if component.Name == "Counter" {
			if _, err := ir.LowerIsland(p, index); err == nil || !strings.Contains(err.Error(), "signature func()") {
				t.Fatalf("callback result was accepted: %v", err)
			}
			return
		}
	}
	t.Fatal("missing Counter")
}

func TestAuthoredIslandCallbacksMatchFlatProgram(t *testing.T) {
	flat := `package app
import "m31labs.dev/gosx/signal"
//gosx:island
component Counter() {
 count := signal.New(0)
 increment := func() { count.Set(count.Get() + 1) }
 return <section><button type="button" onClick={increment}>Count: {count.Get()}</button><button type="button" onClick={increment}>Count: {count.Get()}</button></section>
}`
	var artifacts [][]byte
	for _, source := range []string{callbackCounterSource, flat} {
		p, err := parse(t, []byte(source))
		if err != nil {
			t.Fatal(err)
		}
		for index, component := range p.Components {
			if component.Name == "Counter" {
				lowered, err := ir.LowerIsland(p, index)
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := program.EncodeBinary(lowered)
				if err != nil {
					t.Fatal(err)
				}
				artifacts = append(artifacts, encoded)
			}
		}
	}
	if len(artifacts) != 2 || !bytes.Equal(artifacts[0], artifacts[1]) {
		t.Fatal("callback composition adds runtime instructions or changes the flat program")
	}
}

func TestAuthoredIslandRejectsCallbackInputsAndValues(t *testing.T) {
	for name, source := range map[string]string{
		"render callback as text": strings.Replace(callbackCounterSource, "Count: {props.Count}", "{props.OnActivate}", 1),
		"callback from server": `package app
type Props struct { OnActivate func() }
//gosx:island
component Counter(props: Props) {
 return <button onClick={props.OnActivate}>Count</button>
}
`,
	} {
		t.Run(name, func(t *testing.T) {
			tree, language, parseErr := gosx.Parse([]byte(source))
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			if tree.RootNode().HasError() {
				t.Fatal(gosx.DescribeParseError(tree.RootNode(), []byte(source), language))
			}
			tree.Release()
			p, err := parse(t, []byte(source))
			if err != nil {
				return // Host schema rejection is sufficient; nothing can hydrate.
			}
			if diagnostics := ir.Validate(p); len(diagnostics) == 0 {
				t.Fatal("callback data accepted without a diagnostic")
			}
		})
	}
}
