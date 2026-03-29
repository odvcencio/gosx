package test

import (
	"testing"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/ir"
)

// TestBodyAnalyzerSignals verifies that signal.New() declarations
// are extracted from .gsx component bodies.
func TestBodyAnalyzerSignals(t *testing.T) {
	source := []byte(`package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	name := signal.New("hello")
	active := signal.New(true)

	return <div>{count}</div>
}
`)
	prog, err := compileGSX(t, source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if len(prog.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(prog.Components))
	}

	comp := prog.Components[0]
	if comp.Scope == nil {
		t.Fatal("expected component scope from body analysis")
	}

	if len(comp.Scope.Signals) != 3 {
		t.Fatalf("expected 3 signals, got %d", len(comp.Scope.Signals))
	}

	// Verify signal details
	sigs := comp.Scope.Signals
	if sigs[0].Name != "count" || sigs[0].InitExpr != "0" || sigs[0].TypeHint != "int" {
		t.Errorf("signal 0: got %+v", sigs[0])
	}
	if sigs[1].Name != "name" || sigs[1].TypeHint != "string" {
		t.Errorf("signal 1: got %+v", sigs[1])
	}
	if sigs[2].Name != "active" || sigs[2].InitExpr != "true" || sigs[2].TypeHint != "bool" {
		t.Errorf("signal 2: got %+v", sigs[2])
	}

	t.Logf("Signals extracted: %+v", sigs)
}

func TestBodyAnalyzerInfersGoLiteralTypeHints(t *testing.T) {
	source := []byte("package main\n\n//gosx:island\nfunc Literals() Node {\n\thexValue := signal.New(0x2a)\n\tratio := signal.New(6.02e23)\n\traw := signal.New(`hello`)\n\titems := signal.New([]string{\"a\", \"b\"})\n\tfixed := signal.New([2]int{1, 2})\n\tbuilt := signal.New(make([]int, 0, 4))\n\n\treturn <div>{hexValue}{ratio}{raw}{items}{fixed}{built}</div>\n}\n")
	prog, err := compileGSX(t, source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	scope := prog.Components[0].Scope
	if scope == nil {
		t.Fatal("expected component scope")
	}

	want := map[string]string{
		"hexValue": "int",
		"ratio":    "float",
		"raw":      "string",
		"items":    "array",
		"fixed":    "array",
		"built":    "array",
	}
	if len(scope.Signals) != len(want) {
		t.Fatalf("expected %d signals, got %d", len(want), len(scope.Signals))
	}
	for _, sig := range scope.Signals {
		if got := sig.TypeHint; got != want[sig.Local] {
			t.Fatalf("signal %s type hint = %q, want %q", sig.Local, got, want[sig.Local])
		}
	}
}

func TestBodyAnalyzerSharedSignals(t *testing.T) {
	source := []byte(`package main

//gosx:island
func Dashboard() Node {
	state := signal.NewShared("dashboard.state", props.State)
	view := signal.NewShared("$dashboard.view", "overview")

	return <div>{view.Get()}</div>
}
`)
	prog, err := compileGSX(t, source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	comp := prog.Components[0]
	if comp.Scope == nil {
		t.Fatal("expected component scope from body analysis")
	}
	if len(comp.Scope.Signals) != 2 {
		t.Fatalf("expected 2 signals, got %d", len(comp.Scope.Signals))
	}

	if comp.Scope.Signals[0].Name != "$dashboard.state" {
		t.Fatalf("expected first shared signal to be $dashboard.state, got %q", comp.Scope.Signals[0].Name)
	}
	if comp.Scope.Signals[1].Name != "$dashboard.view" {
		t.Fatalf("expected second shared signal to be $dashboard.view, got %q", comp.Scope.Signals[1].Name)
	}
	if comp.Scope.Locals["state"] != "signal" || comp.Scope.Locals["view"] != "signal" {
		t.Fatalf("expected shared signal locals to register as signal, got %#v", comp.Scope.Locals)
	}
}

func TestBodyAnalyzerRecognizesAliasedSignalImportsInVarDecls(t *testing.T) {
	source := []byte(`package main

import sig "github.com/odvcencio/gosx/signal"

//gosx:island
func Counter() Node {
	var count = sig.New(0)
	var label = sig.NewShared("counter.label", "ready")
	var doubled = sig.Derive(func() int { return count.Get() * 2 })

	return <div>{label.Get()} {doubled}</div>
}
`)
	prog, err := compileGSX(t, source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	scope := prog.Components[0].Scope
	if scope == nil {
		t.Fatal("expected component scope")
	}
	if len(scope.Signals) != 2 {
		t.Fatalf("expected 2 signals, got %d", len(scope.Signals))
	}
	if scope.Signals[0].Name != "count" || scope.Signals[0].InitExpr != "0" {
		t.Fatalf("unexpected first signal %+v", scope.Signals[0])
	}
	if scope.Signals[1].Name != "$counter.label" || scope.Signals[1].InitExpr != `"ready"` {
		t.Fatalf("unexpected shared signal %+v", scope.Signals[1])
	}
	if len(scope.Computeds) != 1 || scope.Computeds[0].Name != "doubled" {
		t.Fatalf("expected aliased derive to register computed, got %+v", scope.Computeds)
	}
}

func TestBodyAnalyzerRecognizesDotImportedSignalHelpers(t *testing.T) {
	source := []byte(`package main

import . "github.com/odvcencio/gosx/signal"

//gosx:island
func Dashboard() Node {
	title := New("hello")
	shared := Shared("dashboard.mode", "overview")
	refresh := func() {
		title.Set("updated")
	}

	return <button onClick={refresh}>{shared.Get()} {title.Get()}</button>
}
`)
	prog, err := compileGSX(t, source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	scope := prog.Components[0].Scope
	if scope == nil {
		t.Fatal("expected component scope")
	}
	if len(scope.Signals) != 2 {
		t.Fatalf("expected 2 signals, got %d", len(scope.Signals))
	}
	if scope.Signals[0].Name != "title" {
		t.Fatalf("unexpected local signal %+v", scope.Signals[0])
	}
	if scope.Signals[1].Name != "$dashboard.mode" {
		t.Fatalf("unexpected shared signal %+v", scope.Signals[1])
	}
	if scope.Locals["refresh"] != "handler" {
		t.Fatalf("expected refresh handler local, got %#v", scope.Locals)
	}
}

// TestBodyAnalyzerHandlers verifies that func literal assignments
// are extracted as handlers.
func TestBodyAnalyzerHandlers(t *testing.T) {
	source := []byte(`package main

//gosx:island
func Counter() Node {
	count := signal.New(0)

	increment := func() {
		count.Set(count.Get() + 1)
	}

	decrement := func() {
		count.Set(count.Get() - 1)
	}

	return <div>
		<button onClick={decrement}>-</button>
		<span>{count}</span>
		<button onClick={increment}>+</button>
	</div>
}
`)
	prog, err := compileGSX(t, source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	comp := prog.Components[0]
	if comp.Scope == nil {
		t.Fatal("expected component scope")
	}

	if len(comp.Scope.Handlers) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(comp.Scope.Handlers))
	}

	h0 := comp.Scope.Handlers[0]
	h1 := comp.Scope.Handlers[1]

	if h0.Name != "increment" {
		t.Errorf("handler 0: expected 'increment', got %q", h0.Name)
	}
	if h1.Name != "decrement" {
		t.Errorf("handler 1: expected 'decrement', got %q", h1.Name)
	}

	if len(h0.Statements) == 0 {
		t.Error("handler 0 has no statements")
	}

	t.Logf("Handler 0: %s → %v", h0.Name, h0.Statements)
	t.Logf("Handler 1: %s → %v", h1.Name, h1.Statements)
}

func TestBodyAnalyzerMultiStatementHandler(t *testing.T) {
	source := []byte(`package main

//gosx:island
func Dashboard() Node {
	activeFile := signal.New("main.arb")
	inspector := signal.New("overview")

	openDiagnostic := func() {
		activeFile.Set("schema.arb")
		inspector.Set("diagnostics")
	}

	return <button onClick={openDiagnostic}>Open</button>
}
`)
	prog, err := compileGSX(t, source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	comp := prog.Components[0]
	if comp.Scope == nil {
		t.Fatal("expected component scope")
	}
	if len(comp.Scope.Handlers) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(comp.Scope.Handlers))
	}

	got := comp.Scope.Handlers[0].Statements
	if len(got) != 2 {
		t.Fatalf("expected 2 statements, got %d: %#v", len(got), got)
	}
	if got[0] != `activeFile.Set("schema.arb")` {
		t.Fatalf("statement 0: got %q", got[0])
	}
	if got[1] != `inspector.Set("diagnostics")` {
		t.Fatalf("statement 1: got %q", got[1])
	}
}

// TestBodyAnalyzerComputed verifies signal.Derive() extraction.
func TestBodyAnalyzerComputed(t *testing.T) {
	source := []byte(`package main

//gosx:island
func Calculator() Node {
	price := signal.New(100)
	qty := signal.New(1)

	total := signal.Derive(func() int {
		return price.Get() * qty.Get()
	})

	return <div>{total}</div>
}
`)
	prog, err := compileGSX(t, source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	comp := prog.Components[0]
	if comp.Scope == nil {
		t.Fatal("expected component scope")
	}

	if len(comp.Scope.Computeds) != 1 {
		t.Fatalf("expected 1 computed, got %d", len(comp.Scope.Computeds))
	}

	c := comp.Scope.Computeds[0]
	if c.Name != "total" {
		t.Errorf("expected 'total', got %q", c.Name)
	}
	if c.BodyExpr == "" {
		t.Error("computed body expression is empty")
	}

	t.Logf("Computed: %s → %s", c.Name, c.BodyExpr)
}

// TestBodyAnalyzerLocals verifies the locals map is populated correctly.
func TestBodyAnalyzerLocals(t *testing.T) {
	source := []byte(`package main

//gosx:island
func App() Node {
	count := signal.New(0)
	doubled := signal.Derive(func() int { return count.Get() * 2 })
	reset := func() { count.Set(0) }

	return <div>{count}</div>
}
`)
	prog, err := compileGSX(t, source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	scope := prog.Components[0].Scope
	if scope == nil {
		t.Fatal("expected scope")
	}

	if scope.Locals["count"] != "signal" {
		t.Errorf("count: expected 'signal', got %q", scope.Locals["count"])
	}
	if scope.Locals["doubled"] != "computed" {
		t.Errorf("doubled: expected 'computed', got %q", scope.Locals["doubled"])
	}
	if scope.Locals["reset"] != "handler" {
		t.Errorf("reset: expected 'handler', got %q", scope.Locals["reset"])
	}

	t.Logf("Locals: %v", scope.Locals)
}

// TestBodyAnalyzerNoScope verifies that a component without signals/handlers
// gets a nil scope.
func TestBodyAnalyzerNoScope(t *testing.T) {
	source := []byte(`package main

func Static() Node {
	return <div>hello</div>
}
`)
	prog, err := compileGSX(t, source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if prog.Components[0].Scope != nil {
		t.Error("expected nil scope for component with no signals/handlers")
	}
}

func compileGSX(t *testing.T, source []byte) (*ir.Program, error) {
	t.Helper()
	tree, lang, err := gosx.Parse(source)
	if err != nil {
		return nil, err
	}
	root := tree.RootNode()
	return ir.Lower(root, source, lang)
}
