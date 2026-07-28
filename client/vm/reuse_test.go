package vm

import (
	"reflect"
	"testing"

	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// treesEqual compares two resolved trees by content. Subtree reuse must
// produce exactly the tree a full evaluation produces, so every test that
// exercises reuse checks the result against a fresh full walk.
func treesEqual(left, right *ResolvedTree) bool {
	if left == nil || right == nil {
		return left == right
	}
	if len(left.Nodes) != len(right.Nodes) {
		return false
	}
	for i := range left.Nodes {
		if !nodesEqual(&left.Nodes[i], &right.Nodes[i]) {
			return false
		}
	}
	return true
}

func nodesEqual(left, right *ResolvedNode) bool {
	return left.Source == right.Source &&
		left.HasSource == right.HasSource &&
		left.Tag == right.Tag &&
		left.Text == right.Text &&
		left.Key == right.Key &&
		reflect.DeepEqual(left.Attrs, right.Attrs) &&
		reflect.DeepEqual(left.Events, right.Events) &&
		reflect.DeepEqual(left.Children, right.Children)
}

// fullEvalTree walks the program with reuse switched off, which is the
// oracle every reuse test compares against.
func fullEvalTree(island *Island) *ResolvedTree {
	saved := island.vm.reuse
	island.vm.reuse = nil
	tree := island.vm.EvalTree()
	island.vm.reuse = saved
	return tree
}

func reuseHits(island *Island) int {
	if island.reuse == nil {
		return 0
	}
	return island.reuse.ctx.hits
}

func TestReconcileReusesUnchangedElementSubtrees(t *testing.T) {
	island := NewIsland(program.CounterProgram(), `{}`)
	// The analysis is built on the first reconcile and the snapshot is
	// primed by it, so the first event still evaluates the whole tree.
	island.Dispatch("increment", "{}")
	if island.reuse == nil {
		t.Fatal("counter program should get a reuse plan")
	}

	patches := island.Dispatch("increment", "{}")

	if hits := reuseHits(island); hits != 2 {
		t.Errorf("reused subtrees = %d, want 2 (both buttons); the counter's "+
			"root and its display node read the count signal, the buttons do not", hits)
	}
	if got := counterDisplayText(island.prev); got != "2" {
		t.Fatalf("display after two increments = %q, want %q", got, "2")
	}
	if !treesEqual(island.prev, fullEvalTree(island)) {
		t.Fatal("reused tree differs from a full evaluation")
	}
	if len(patches) != 1 || patches[0].Kind != PatchSetText || patches[0].Text != "2" {
		t.Fatalf("patches = %#v, want one SetText(\"2\")", patches)
	}
}

func TestReconcileWithoutChangeReusesWholeTree(t *testing.T) {
	island := NewIsland(program.CounterProgram(), `{}`)
	island.Reconcile() // builds the analysis and primes the snapshot

	patches := island.Reconcile()

	if len(patches) != 0 {
		t.Fatalf("patches from an unchanged reconcile = %#v, want none", patches)
	}
	if hits := reuseHits(island); hits != 1 {
		t.Errorf("reused subtrees = %d, want 1 (the whole root copies in one step)", hits)
	}
	if !treesEqual(island.prev, fullEvalTree(island)) {
		t.Fatal("reused tree differs from a full evaluation")
	}
}

func TestReconcileReuseMatchesFullEvalOverManyDispatches(t *testing.T) {
	island := NewIsland(program.CounterProgram(), `{}`)
	for step := 0; step < 8; step++ {
		if step%3 == 0 {
			island.Dispatch("decrement", "{}")
		} else {
			island.Dispatch("increment", "{}")
		}
		if !treesEqual(island.prev, fullEvalTree(island)) {
			t.Fatalf("step %d: reused tree differs from a full evaluation", step)
		}
	}
	if got := counterDisplayText(island.prev); got != "2" {
		t.Fatalf("display after 8 dispatches = %q, want %q", got, "2")
	}
}

// listSuffixProgram builds the shape that breaks a careless read-set walk.
//
//	<ul>                        element, reached once, so a reuse candidate
//	  <ForEach items as row>    collection is a PROP, so it never changes
//	    <li>{row + suffix}</li> content reads a SIGNAL through the rebound prop
//
// The <ul> holds no expression of its own. Its read set has to come from the
// forEach body, one scope down, or the <ul> looks unchanged when the suffix
// signal moves and the whole list silently stops updating.
func listSuffixProgram() *program.Program {
	return &program.Program{
		Name: "ListSuffix",
		Nodes: []program.Node{
			{ // 0: <ul>
				Kind:     program.NodeElement,
				Tag:      "ul",
				Children: []program.NodeID{1},
			},
			{ // 1: forEach over the "items" prop
				Kind: program.NodeForEach,
				Expr: 0,
				Attrs: []program.Attr{
					{Kind: program.AttrStatic, Name: "as", Value: "row"},
				},
				Children: []program.NodeID{2},
			},
			{ // 2: <li>
				Kind:     program.NodeElement,
				Tag:      "li",
				Children: []program.NodeID{3},
			},
			{ // 3: {row + suffix}
				Kind: program.NodeExpr,
				Expr: 1,
			},
		},
		Root: 0,
		Exprs: []program.Expr{
			{Op: program.OpPropGet, Value: "items", Type: program.TypeAny},                                      // 0
			{Op: program.OpConcat, Operands: []program.ExprID{2, 3}},                                            // 1
			{Op: program.OpPropGet, Value: "row", Type: program.TypeString},                                     // 2
			{Op: program.OpSignalGet, Value: "suffix", Type: program.TypeString},                                // 3
			{Op: program.OpLitString, Value: "!", Type: program.TypeString},                                     // 4
			{Op: program.OpLitString, Value: "?", Type: program.TypeString},                                     // 5
			{Op: program.OpSignalSet, Operands: []program.ExprID{5}, Value: "suffix", Type: program.TypeString}, // 6
		},
		Signals: []program.SignalDef{
			{Name: "suffix", Type: program.TypeString, Init: program.ExprID(4)},
		},
		Handlers: []program.Handler{
			{Name: "swap", Body: []program.ExprID{6}},
		},
	}
}

func listItemTexts(tree *ResolvedTree) []string {
	if tree == nil || len(tree.Nodes) == 0 {
		return nil
	}
	var texts []string
	for _, itemIdx := range tree.Nodes[0].Children {
		item := tree.Nodes[itemIdx]
		if len(item.Children) == 0 {
			continue
		}
		texts = append(texts, tree.Nodes[item.Children[0]].Text)
	}
	return texts
}

// TestForEachNodeUpdatesThroughReboundProp is the regression guard route (b)
// needs. The forEach collection never changes, so only the read set carried
// up from the loop body can tell the walk that the list is stale.
func TestForEachNodeUpdatesThroughReboundProp(t *testing.T) {
	island := NewIsland(listSuffixProgram(), `{"items": ["a", "b"]}`)

	before := listItemTexts(island.prev)
	if !reflect.DeepEqual(before, []string{"a!", "b!"}) {
		t.Fatalf("initial list = %v, want [a! b!]", before)
	}

	island.Dispatch("swap", "{}")

	after := listItemTexts(island.prev)
	if !reflect.DeepEqual(after, []string{"a?", "b?"}) {
		t.Fatalf("list after the suffix signal changed = %v, want [a? b?]; the "+
			"<ul> was reused even though its forEach body reads the signal "+
			"through the rebound `row` prop", after)
	}
	if !treesEqual(island.prev, fullEvalTree(island)) {
		t.Fatal("reused tree differs from a full evaluation")
	}
}

// TestForEachDeletesScopedPropsItCreated pins the scope contract a forEach
// owes every node after it.
//
// FuzzIslandReuseMatchesFullEval found this. restoreProps used to put back
// only the names that already existed, so `_item`, `_index`, `_key` and the
// `as` name survived the loop. autoKey reads `_index`, which meant a plain
// element rendered after a list picked up a key built from the list's last
// index, and the key changed again the next time the list length changed.
func TestForEachDeletesScopedPropsItCreated(t *testing.T) {
	island := NewIsland(listSuffixProgram(), `{"items": ["a", "b"]}`)

	for _, name := range []string{"_item", "_index", "_key", "row", "rowKey"} {
		if _, ok := island.vm.props[name]; ok {
			t.Errorf("prop %q survived the forEach that bound it; a name absent "+
				"before the loop must be absent after it", name)
		}
	}
	if island.prev.Nodes[0].Key != "" {
		t.Errorf("root key = %q, want empty; the root is not inside the list",
			island.prev.Nodes[0].Key)
	}
}

// TestForEachBodyIsNeverReusable pins the gate that makes the case above
// safe. A loop body resolves once per iteration, so it has no single home in
// the tree to copy from.
func TestForEachBodyIsNeverReusable(t *testing.T) {
	plan := newReusePlan(listSuffixProgram())
	if plan == nil {
		t.Fatal("list program should get a reuse plan")
	}
	if !plan.nodes[0].reusable {
		t.Error("node 0 <ul> should be reusable: it is reached once, outside any loop")
	}
	if plan.nodes[2].reusable {
		t.Error("node 2 <li> must not be reusable: it sits inside a forEach body")
	}
	if plan.nodes[0].mask == 0 {
		t.Error("node 0 <ul> read set is empty; it must carry the suffix signal " +
			"that its forEach body reads")
	}
	if plan.nodes[0].mask != plan.nodes[2].mask|plan.nodes[1].mask {
		t.Errorf("node 0 read set %b should cover its whole subtree", plan.nodes[0].mask)
	}
}

// objectSignalProgram reads one field of an object-valued signal. Handlers
// mutate that object in place, which no snapshot can detect by comparison.
func objectSignalProgram() *program.Program {
	return &program.Program{
		Name: "ObjectSignal",
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1}},
			{Kind: program.NodeExpr, Expr: 0},
		},
		Root: 0,
		Exprs: []program.Expr{
			{Op: program.OpIndex, Operands: []program.ExprID{1, 2}, Type: program.TypeString}, // 0
			{Op: program.OpSignalGet, Value: "state", Type: program.TypeAny},                  // 1
			{Op: program.OpLitString, Value: "label", Type: program.TypeString},               // 2
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},                         // 3
		},
		Signals: []program.SignalDef{
			{Name: "state", Type: program.TypeAny, Init: program.ExprID(3)},
		},
	}
}

// TestReuseTreatsCompositeSignalAsAlwaysDirty covers the in-place mutation
// contract. OpFieldSet writes through the same map every alias holds, so a
// value snapshot of an object signal always compares equal to itself.
func TestReuseTreatsCompositeSignalAsAlwaysDirty(t *testing.T) {
	island := NewIsland(objectSignalProgram(), `{}`)
	fields := map[string]Value{"label": StringVal("first")}
	island.vm.SetSignal("state", signal.New(ObjectVal(fields)))
	island.Reconcile()

	if got := island.prev.Nodes[1].Text; got != "first" {
		t.Fatalf("text = %q, want %q", got, "first")
	}

	// Mutate through the shared map, exactly as OpFieldSet does.
	fields["label"] = StringVal("second")
	island.Reconcile()

	if got := island.prev.Nodes[1].Text; got != "second" {
		t.Fatalf("text after an in-place field write = %q, want %q; a composite "+
			"signal must count as dirty on every reconcile", got, "second")
	}
}

// TestReuseFollowsPropChanges covers the second watched namespace. Props are
// set once at construction for most islands, but SetProp is a public seam.
func TestReuseFollowsPropChanges(t *testing.T) {
	prog := &program.Program{
		Name: "PropText",
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1}},
			{Kind: program.NodeExpr, Expr: 0},
		},
		Root: 0,
		Exprs: []program.Expr{
			{Op: program.OpPropGet, Value: "label", Type: program.TypeString},
		},
	}
	island := NewIsland(prog, `{"label": "before"}`)
	if got := island.prev.Nodes[1].Text; got != "before" {
		t.Fatalf("initial text = %q, want %q", got, "before")
	}

	island.vm.SetProp("label", StringVal("after"))
	patches := island.Reconcile()

	if got := island.prev.Nodes[1].Text; got != "after" {
		t.Fatalf("text after the prop changed = %q, want %q", got, "after")
	}
	if len(patches) != 1 {
		t.Fatalf("patches = %#v, want one", patches)
	}
}

// conditionalShiftProgram places a conditional ahead of a reusable sibling,
// so toggling the condition moves the sibling to a different index.
func conditionalShiftProgram() *program.Program {
	return &program.Program{
		Name: "ConditionalShift",
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1, 3}},
			{ // 1: conditional on the "shown" signal
				Kind:     program.NodeConditional,
				Expr:     0,
				Children: []program.NodeID{2},
			},
			{Kind: program.NodeElement, Tag: "b", Children: []program.NodeID{5}}, // 2
			{Kind: program.NodeElement, Tag: "span", Children: []program.NodeID{4}},
			{Kind: program.NodeText, Text: "tail"}, // 4
			{Kind: program.NodeText, Text: "head"}, // 5
		},
		Root: 0,
		Exprs: []program.Expr{
			{Op: program.OpSignalGet, Value: "shown", Type: program.TypeBool},                                // 0
			{Op: program.OpLitBool, Value: "true", Type: program.TypeBool},                                   // 1
			{Op: program.OpLitBool, Value: "false", Type: program.TypeBool},                                  // 2
			{Op: program.OpSignalSet, Operands: []program.ExprID{2}, Value: "shown", Type: program.TypeBool}, // 3
			{Op: program.OpSignalSet, Operands: []program.ExprID{1}, Value: "shown", Type: program.TypeBool}, // 4
		},
		Signals: []program.SignalDef{
			{Name: "shown", Type: program.TypeBool, Init: program.ExprID(1)},
		},
		Handlers: []program.Handler{
			{Name: "hide", Body: []program.ExprID{3}},
			{Name: "show", Body: []program.ExprID{4}},
		},
	}
}

// TestReuseRemapsChildrenWhenIndicesShift exercises the offset path. The
// copied subtree carries child indices from the previous tree, so a subtree
// that lands somewhere else has to rebuild them.
func TestReuseRemapsChildrenWhenIndicesShift(t *testing.T) {
	island := NewIsland(conditionalShiftProgram(), `{}`)
	if len(island.prev.Nodes) != 5 {
		t.Fatalf("initial tree size = %d, want 5 (div, b, head, span, tail)", len(island.prev.Nodes))
	}

	island.Dispatch("hide", "{}")

	if !treesEqual(island.prev, fullEvalTree(island)) {
		t.Fatal("tree after the conditional collapsed differs from a full evaluation")
	}
	root := island.prev.Nodes[0]
	if len(root.Children) != 1 {
		t.Fatalf("root children = %v, want one (the span)", root.Children)
	}
	span := island.prev.Nodes[root.Children[0]]
	if span.Tag != "span" || len(span.Children) != 1 {
		t.Fatalf("reused span = %#v, want a span with one child", span)
	}
	if got := island.prev.Nodes[span.Children[0]].Text; got != "tail" {
		t.Fatalf("reused span child text = %q, want %q; the copied child index "+
			"was not shifted to the new tree", got, "tail")
	}

	island.Dispatch("show", "{}")
	if !treesEqual(island.prev, fullEvalTree(island)) {
		t.Fatal("tree after the conditional reopened differs from a full evaluation")
	}
}

// TestReusePlanRejectsImpureSubtrees pins the purity contract. An opcode
// whose result depends on state the plan does not watch must switch reuse
// off for every element above it.
func TestReusePlanRejectsImpureSubtrees(t *testing.T) {
	prog := &program.Program{
		Name: "EventText",
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1}},
			{Kind: program.NodeExpr, Expr: 0},
		},
		Root: 0,
		Exprs: []program.Expr{
			{Op: program.OpEventGet, Value: "key", Type: program.TypeString},
		},
	}
	if plan := newReusePlan(prog); plan != nil {
		t.Fatalf("a program whose only element reads event data must not get a "+
			"reuse plan; got reusable=%v", plan.nodes[0].reusable)
	}
}

func TestIsPureOpDefaultsToImpure(t *testing.T) {
	impure := []program.OpCode{
		program.OpSignalSet, program.OpSignalUpdate, program.OpAssign,
		program.OpLocalGet, program.OpLocalSet, program.OpLocalDecl,
		program.OpFieldSet, program.OpIndexSet,
		program.OpCall, program.OpIndirectCall, program.OpHostCall, program.OpClosure,
		program.OpEventGet, program.OpMake, program.OpSeq,
		program.OpFor, program.OpForRange, program.OpReturn, program.OpBreak, program.OpContinue,
		program.OpMap, program.OpFilter, program.OpFind, program.OpComposite, program.OpRange,
	}
	for _, op := range impure {
		if isPureOp(op) {
			t.Errorf("opcode %v is listed as pure; it writes state, rebinds a scope, "+
				"or reads state the reuse plan does not watch", op)
		}
	}
	if !isPureOp(program.OpAdd) || !isPureOp(program.OpConcat) {
		t.Error("arithmetic and concatenation must stay pure")
	}
}

func TestReusePlanRejectsTooManyWatchedInputs(t *testing.T) {
	prog := &program.Program{
		Name:  "WideRead",
		Nodes: []program.Node{{Kind: program.NodeElement, Tag: "div"}},
		Root:  0,
	}
	// One element whose text concatenates more distinct signals than the
	// read-set bitmask can hold.
	concat := program.Expr{Op: program.OpConcat}
	for i := 0; i <= maxWatchedInputs; i++ {
		name := "s" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpSignalGet, Value: name, Type: program.TypeString})
		concat.Operands = append(concat.Operands, program.ExprID(i))
	}
	prog.Exprs = append(prog.Exprs, concat)
	prog.Nodes = append(prog.Nodes, program.Node{Kind: program.NodeExpr, Expr: program.ExprID(len(prog.Exprs) - 1)})
	prog.Nodes[0].Children = []program.NodeID{1}

	if plan := newReusePlan(prog); plan != nil {
		t.Fatal("a program reading more inputs than the bitmask holds must fall " +
			"back to full evaluation")
	}
}

func TestReusePlanSurvivesCyclicNodeGraph(t *testing.T) {
	prog := &program.Program{
		Name: "Cyclic",
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1}},
			{Kind: program.NodeElement, Tag: "span", Children: []program.NodeID{0}},
		},
		Root: 0,
	}
	// The walks must terminate. The result only has to be safe, not clever.
	_ = newReusePlan(prog)
}

func TestSwapProgramDropsStaleReuseState(t *testing.T) {
	island := NewIsland(program.CounterProgram(), `{}`)
	island.Dispatch("increment", "{}")

	island.SwapProgram(program.FormProgram())

	if !treesEqual(island.prev, fullEvalTree(island)) {
		t.Fatal("tree after a program swap differs from a full evaluation")
	}
	if hits := reuseHits(island); hits != 0 {
		t.Errorf("reused subtrees right after a swap = %d, want 0; the previous "+
			"tree belongs to the old node table", hits)
	}
}
