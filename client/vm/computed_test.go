package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// computedProgram models the shape emitted for:
//
//	count := signal.New(init)
//	scaled := signal.Derive(func() int { return count.Get() * factor })
//	label := signal.Derive(func() int { return scaled.Get() + 1 })
//
// The second derived value deliberately chains through the first so the tests
// exercise computed-to-computed dependency tracking, not only a direct signal
// read.
func computedProgram(init, factor string) *program.Program {
	return &program.Program{
		Name: "ComputedCounter",
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: init, Type: program.TypeInt},                                       // 0 count init
			{Op: program.OpSignalGet, Value: "count", Type: program.TypeInt},                                 // 1
			{Op: program.OpLitInt, Value: factor, Type: program.TypeInt},                                     // 2
			{Op: program.OpMul, Operands: []program.ExprID{1, 2}, Type: program.TypeInt},                     // 3 scaled
			{Op: program.OpSignalGet, Value: "scaled", Type: program.TypeInt},                                // 4
			{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},                                        // 5
			{Op: program.OpAdd, Operands: []program.ExprID{4, 5}, Type: program.TypeInt},                     // 6 label
			{Op: program.OpSignalGet, Value: "label", Type: program.TypeInt},                                 // 7 render
			{Op: program.OpSignalGet, Value: "count", Type: program.TypeInt},                                 // 8
			{Op: program.OpAdd, Operands: []program.ExprID{8, 5}, Type: program.TypeInt},                     // 9
			{Op: program.OpSignalSet, Value: "count", Operands: []program.ExprID{9}, Type: program.TypeInt},  // 10 increment
			{Op: program.OpLitInt, Value: "2", Type: program.TypeInt},                                        // 11
			{Op: program.OpSignalSet, Value: "count", Operands: []program.ExprID{11}, Type: program.TypeInt}, // 12
			{Op: program.OpLitInt, Value: "3", Type: program.TypeInt},                                        // 13
			{Op: program.OpSignalSet, Value: "count", Operands: []program.ExprID{13}, Type: program.TypeInt}, // 14
		},
		Signals: []program.SignalDef{
			{Name: "count", Type: program.TypeInt, Init: 0},
		},
		Computeds: []program.ComputedDef{
			{Name: "scaled", Type: program.TypeInt, Expr: 3},
			{Name: "label", Type: program.TypeInt, Expr: 6},
		},
		Handlers: []program.Handler{
			{Name: "increment", Body: []program.ExprID{10}},
			{Name: "setTwice", Body: []program.ExprID{12, 14}},
		},
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "output", Children: []program.NodeID{1}},
			{Kind: program.NodeExpr, Expr: 7},
		},
		Root:       0,
		StaticMask: []bool{false, false},
	}
}

func computedText(tree *ResolvedTree) string {
	if tree == nil || len(tree.Nodes) == 0 || len(tree.Nodes[0].Children) == 0 {
		return ""
	}
	child := tree.Nodes[0].Children[0]
	if child < 0 || child >= len(tree.Nodes) {
		return ""
	}
	return tree.Nodes[child].Text
}

func TestProgramComputedsRenderReactivelyAndChain(t *testing.T) {
	island := NewIsland(computedProgram("1", "2"), `{}`)
	if got := computedText(island.CurrentTree()); got != "3" {
		t.Fatalf("initial computed text = %q, want 3", got)
	}

	patches := island.Dispatch("increment", `{}`)
	if got := computedText(island.CurrentTree()); got != "5" {
		t.Fatalf("computed text after increment = %q, want 5", got)
	}
	if len(patches) != 1 || patches[0].Kind != PatchSetText || patches[0].Text != "5" {
		t.Fatalf("patches after increment = %#v, want one SetText(5)", patches)
	}
}

func TestProgramComputedsObserveBatchedHandlerResult(t *testing.T) {
	island := NewIsland(computedProgram("1", "2"), `{}`)
	patches := island.Dispatch("setTwice", `{}`)

	if got := computedText(island.CurrentTree()); got != "7" {
		t.Fatalf("computed text after batched writes = %q, want 7", got)
	}
	if len(patches) != 1 || patches[0].Kind != PatchSetText || patches[0].Text != "7" {
		t.Fatalf("patches after batched writes = %#v, want one final SetText(7)", patches)
	}
}

func TestProgramComputedRebuildsAfterExternalPropChange(t *testing.T) {
	prog := &program.Program{
		Name: "ComputedProp",
		Exprs: []program.Expr{
			{Op: program.OpPropGet, Value: "factor", Type: program.TypeInt},
			{Op: program.OpLitInt, Value: "2", Type: program.TypeInt},
			{Op: program.OpMul, Operands: []program.ExprID{0, 1}, Type: program.TypeInt},
			{Op: program.OpSignalGet, Value: "scaled", Type: program.TypeInt},
		},
		Computeds: []program.ComputedDef{{Name: "scaled", Type: program.TypeInt, Expr: 2}},
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "output", Children: []program.NodeID{1}},
			{Kind: program.NodeExpr, Expr: 3},
		},
		Root:       0,
		StaticMask: []bool{false, false},
	}
	island := NewIsland(prog, `{"factor":3}`)
	if got := computedText(island.CurrentTree()); got != "6" {
		t.Fatalf("initial prop-derived text = %q, want 6", got)
	}

	island.vm.SetProp("factor", IntVal(5))
	island.Reconcile()
	if got := computedText(island.CurrentTree()); got != "10" {
		t.Fatalf("prop-derived text after SetProp = %q, want 10", got)
	}

	island.vm.DeleteProp("factor")
	island.Reconcile()
	if got := computedText(island.CurrentTree()); got != "0" {
		t.Fatalf("prop-derived text after DeleteProp = %q, want 0", got)
	}
}

func TestBulkPropMutationRebuildsComputedGraphOnce(t *testing.T) {
	prog := &program.Program{
		Exprs:     []program.Expr{{Op: program.OpHostCall, Value: "probe.Read", Type: program.TypeInt}},
		Computeds: []program.ComputedDef{{Name: "derived", Type: program.TypeInt, Expr: 0}},
	}
	machine := NewVM(prog, nil)
	recorder := NewHostRecorder()
	recorder.ReturnFor = map[string]Value{"Read": IntVal(1)}
	machine.BindHost("probe", recorder)
	InitSignals(machine, prog)
	if got := len(recorder.Calls); got != 1 {
		t.Fatalf("initial computed builds = %d, want 1", got)
	}

	machine.ApplyPropMutations([]PropMutation{
		{Name: "ev.X", Value: FloatVal(10)},
		{Name: "ev.Y", Value: FloatVal(20)},
	})
	if got := len(recorder.Calls); got != 2 {
		t.Fatalf("two-slot stage rebuilt computed %d times, want one additional build", got-1)
	}
	machine.ApplyPropMutations([]PropMutation{
		{Name: "ev.X", Delete: true},
		{Name: "ev.Y", Delete: true},
	})
	if got := len(recorder.Calls); got != 3 {
		t.Fatalf("two-slot restore rebuilt computed %d times, want one additional build", got-2)
	}
}

func TestHandlerCanReadComputedNotReferencedByTree(t *testing.T) {
	prog := &program.Program{
		Name: "HandlerOnlyComputed",
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: "2", Type: program.TypeInt},
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},
			{Op: program.OpSignalGet, Value: "input", Type: program.TypeInt},
			{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},
			{Op: program.OpAdd, Operands: []program.ExprID{2, 3}, Type: program.TypeInt},
			{Op: program.OpSignalGet, Value: "next", Type: program.TypeInt},
			{Op: program.OpSignalSet, Value: "output", Operands: []program.ExprID{5}, Type: program.TypeInt},
			{Op: program.OpSignalGet, Value: "output", Type: program.TypeInt},
		},
		Signals: []program.SignalDef{
			{Name: "input", Type: program.TypeInt, Init: 0},
			{Name: "output", Type: program.TypeInt, Init: 1},
		},
		Computeds: []program.ComputedDef{{Name: "next", Type: program.TypeInt, Expr: 4}},
		Handlers:  []program.Handler{{Name: "copy", Body: []program.ExprID{6}}},
		Nodes:     []program.Node{{Kind: program.NodeExpr, Expr: 7}},
		Root:      0,
	}
	island := NewIsland(prog, `{}`)
	island.Dispatch("copy", `{}`)
	if got := island.CurrentTree().Nodes[0].Text; got != "3" {
		t.Fatalf("handler-only computed result = %q, want 3", got)
	}
}

func TestForwardComputedReferenceIsBoundedAndZero(t *testing.T) {
	prog := &program.Program{
		Name: "MalformedForwardComputed",
		Exprs: []program.Expr{
			{Op: program.OpSignalGet, Value: "later", Type: program.TypeInt},
			{Op: program.OpLitInt, Value: "7", Type: program.TypeInt},
			{Op: program.OpSignalGet, Value: "first", Type: program.TypeInt},
		},
		Computeds: []program.ComputedDef{
			{Name: "first", Type: program.TypeInt, Expr: 0},
			{Name: "later", Type: program.TypeInt, Expr: 1},
		},
		Nodes: []program.Node{{Kind: program.NodeExpr, Expr: 2}},
		Root:  0,
	}
	island := NewIsland(prog, `{}`)
	if got := island.CurrentTree().Nodes[0].Text; got != "0" {
		t.Fatalf("forward computed reference rendered %q, want bounded zero value", got)
	}
}

func TestComputedReuseSamplesLazyDerivedValue(t *testing.T) {
	island := NewIsland(computedProgram("1", "2"), `{}`)
	island.Reconcile() // build and prime the subtree-reuse plan
	if hits := reuseHits(island); hits != 0 {
		// The first walk primes snapshots and deliberately evaluates in full.
		t.Fatalf("reuse hits on priming walk = %d, want 0", hits)
	}

	island.vm.signals["count"].Set(IntVal(4))
	patches := island.Reconcile()
	if got := computedText(island.CurrentTree()); got != "9" {
		t.Fatalf("computed text after base write = %q, want 9; computed subtree was reused stale", got)
	}
	if len(patches) != 1 || patches[0].Kind != PatchSetText || patches[0].Text != "9" {
		t.Fatalf("patches after base write = %#v, want one SetText(9)", patches)
	}
	if !treesEqual(island.CurrentTree(), fullEvalTree(island)) {
		t.Fatal("computed reuse result differs from a full evaluation")
	}
}

func TestSharedSignalReplacementRebindsComputeds(t *testing.T) {
	island := NewIsland(computedProgram("1", "2"), `{}`)
	retiredBase := island.vm.signals["count"]
	retiredLabel := island.vm.computeds["label"]
	shared := signal.New(IntVal(5))

	island.SetSharedSignal("count", shared)
	if got := computedText(island.CurrentTree()); got != "11" {
		t.Fatalf("computed text after shared install = %q, want 11", got)
	}

	// The replacement rebuild stops the old derived graph. Mutating its base
	// must neither update the retired computed nor affect the live island.
	retiredBase.Set(IntVal(8))
	if got := retiredLabel.Get().Number(); got != 3 {
		t.Fatalf("retired computed changed after shared replacement: got %v, want 3", got)
	}
	if got := computedText(island.CurrentTree()); got != "11" {
		t.Fatalf("live tree changed through retired base: got %q, want 11", got)
	}

	shared.Set(IntVal(7))
	island.Reconcile()
	if got := computedText(island.CurrentTree()); got != "15" {
		t.Fatalf("computed text after shared write = %q, want 15", got)
	}
}

func TestSwapProgramRebuildsComputedsAndPreservesMutableState(t *testing.T) {
	island := NewIsland(computedProgram("1", "2"), `{}`)
	island.Dispatch("increment", `{}`) // count = 2, label = 5
	retired := island.vm.computeds["label"]

	island.SwapProgram(computedProgram("99", "3"))
	if got := island.vm.signals["count"].Get().Number(); got != 2 {
		t.Fatalf("count after swap = %v, want preserved value 2", got)
	}
	if got := computedText(island.CurrentTree()); got != "7" {
		t.Fatalf("computed text after swap = %q, want 7 from new multiplier", got)
	}

	island.vm.signals["count"].Set(IntVal(4))
	island.Reconcile()
	if got := computedText(island.CurrentTree()); got != "13" {
		t.Fatalf("computed text after post-swap write = %q, want 13", got)
	}
	if got := retired.Get().Number(); got != 5 {
		t.Fatalf("retired computed changed after hot swap: got %v, want 5", got)
	}
}

func TestDisposeStopsComputedDependencies(t *testing.T) {
	island := NewIsland(computedProgram("1", "2"), `{}`)
	base := island.vm.signals["count"]
	derived := island.vm.computeds["label"]

	island.Dispose()
	base.Set(IntVal(9))
	if got := derived.Get().Number(); got != 3 {
		t.Fatalf("disposed computed changed to %v, want retained terminal value 3", got)
	}
	if island.vm.computeds != nil {
		t.Fatal("disposed island retained its computed registry")
	}
}
