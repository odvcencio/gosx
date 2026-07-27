package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

// handlerCall is one dispatch: a handler name and the event payload it reads.
type handlerCall struct {
	name  string
	event string
}

// islandShape names one program fixture and the handler cycle a benchmark
// fires at it. The counter benchmarks in bench_danmuji_test.go measure the
// smallest island the VM ever runs. These shapes measure denser trees — a tab
// strip, a keyed list, a form with validation and a text editor — so a change
// that only helps a six-node tree cannot look like a general win.
//
// Each cycle returns the island to its starting state. A handler that grows a
// signal without bound, such as ListProgram.addItem on its own, would measure
// string growth instead of the VM.
type islandShape struct {
	name  string
	build func() *program.Program
	cycle []handlerCall
}

var islandShapes = []islandShape{
	{name: "Tabs", build: program.TabsProgram, cycle: []handlerCall{
		{name: "showFeatures", event: `{}`},
		{name: "showAbout", event: `{}`},
	}},
	{name: "Toggle", build: program.ToggleProgram, cycle: []handlerCall{
		{name: "toggle", event: `{}`},
		{name: "toggle", event: `{}`},
	}},
	{name: "Todo", build: program.TodoProgram, cycle: []handlerCall{
		{name: "addItem", event: `{"value":"milk"}`},
		{name: "clearAll", event: `{}`},
	}},
	{name: "Form", build: program.FormProgram, cycle: []handlerCall{
		{name: "fillName", event: `{}`},
		{name: "validateForm", event: `{}`},
		{name: "updateName", event: `{"value":""}`},
		{name: "validateForm", event: `{}`},
	}},
	{name: "Derived", build: program.DerivedProgram, cycle: []handlerCall{
		{name: "toggleDiscount", event: `{}`},
		{name: "toggleDiscount", event: `{}`},
	}},
	{name: "List", build: program.ListProgram, cycle: []handlerCall{
		{name: "addItem", event: `{"value":"milk"}`},
		{name: "clearItems", event: `{}`},
	}},
	{name: "Editor", build: program.EditorProgram, cycle: []handlerCall{
		{name: "onInput", event: `{"value":"hello"}`},
		{name: "clear", event: `{}`},
	}},
}

// runCycle fires one full handler cycle and reports whether any dispatch
// produced a patch.
func runCycle(island *Island, shape islandShape) bool {
	patched := false
	for _, call := range shape.cycle {
		if len(island.Dispatch(call.name, call.event)) > 0 {
			patched = true
		}
	}
	return patched
}

// primeShape builds the island and runs the cycle twice, so the reuse analysis
// and its span snapshot are warm before the timer starts. It fails the caller
// when the second cycle produces no patch: a benchmark that measures an island
// nothing ever changes measures nothing.
func primeShape(tb testing.TB, shape islandShape) *Island {
	tb.Helper()
	island := NewIsland(shape.build(), `{}`)
	runCycle(island, shape)
	if !runCycle(island, shape) {
		tb.Fatalf("shape %s produced no patches in steady state: the benchmark would measure an idle island", shape.name)
	}
	return island
}

// BenchmarkShapeDispatch fires the handler cycle per iteration, which is the
// whole browser event path: read signals, run the handler body, walk the node
// tree and diff it.
func BenchmarkShapeDispatch(b *testing.B) {
	for _, shape := range islandShapes {
		b.Run(shape.name, func(b *testing.B) {
			island := primeShape(b, shape)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, call := range shape.cycle {
					_ = island.Dispatch(call.name, call.event)
				}
			}
		})
	}
}

// BenchmarkShapeReconcile measures a reconcile with no state change, which is
// the path a shared-signal broadcast or a parent re-render takes.
func BenchmarkShapeReconcile(b *testing.B) {
	for _, shape := range islandShapes {
		b.Run(shape.name, func(b *testing.B) {
			island := primeShape(b, shape)
			island.Reconcile()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = island.Reconcile()
			}
		})
	}
}

// TestShapeCyclesReturnToStart pins the property the benchmarks depend on: one
// full cycle leaves the island in the state it started from. A cycle that
// drifts would make the benchmark measure growth, not steady-state work.
func TestShapeCyclesReturnToStart(t *testing.T) {
	for _, shape := range islandShapes {
		t.Run(shape.name, func(t *testing.T) {
			island := primeShape(t, shape)
			before := snapshotTreeText(island.CurrentTree())
			runCycle(island, shape)
			after := snapshotTreeText(island.CurrentTree())
			if before != after {
				t.Fatalf("cycle %v drifts:\n before=%q\n  after=%q", shape.cycle, before, after)
			}
		})
	}
}

// snapshotTreeText flattens a tree to the text and tags a reconcile compares,
// which is enough to detect a cycle that does not return to its start.
func snapshotTreeText(tree *ResolvedTree) string {
	if tree == nil {
		return ""
	}
	out := make([]byte, 0, 256)
	for i := range tree.Nodes {
		node := &tree.Nodes[i]
		out = append(out, node.Tag...)
		out = append(out, '|')
		out = append(out, node.Text...)
		out = append(out, '\n')
	}
	return string(out)
}
