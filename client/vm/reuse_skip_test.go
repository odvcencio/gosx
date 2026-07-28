package vm

import (
	"reflect"
	"testing"

	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// Tests for the diff skip over the subtrees the reuse pass copied verbatim.
//
// A wrong skip is silent. It emits no patch, the browser keeps the previous
// generation on screen, and a test that lists the expected ops still passes,
// because the ops it lists are the ops that were emitted. Every test here is
// therefore a differential or a structural proof, never an op assertion:
//
//   - TestSkipMarksOnlyIdenticalSubtrees walks each marked subtree and proves
//     both trees hold equal nodes at equal indices, which is the property that
//     makes the skip emit nothing.
//   - TestSkipSuppressesTheDiffOfACopiedSubtree lies to the diff between the
//     walk and the diff, so a skip that did not fire shows up as an extra op.
//   - TestSkipIgnoresAShiftedCopy proves a copy that landed elsewhere is not
//     marked.
//   - FuzzIslandRecycleMatchesFreshTree drives a third island with skipOff and
//     compares the ops step by step over generated tree shapes.

// walkOnly runs a handler and the tree walk, and stops before the diff.
//
// It is Dispatch with the reconcile removed, so a test can change the previous
// tree in the window between the walk that copied out of it and the diff that
// reads it. Nothing in production opens that window; the tests use it to prove
// that the skip really suppressed a comparison.
func walkOnly(t *testing.T, island *Island, handler string) *ResolvedTree {
	t.Helper()
	if handler != "" {
		body, ok := island.handlers[handler]
		if !ok {
			t.Fatalf("handler %q is not defined", handler)
		}
		signal.Batch(func() { island.evalHandlerBody(body) })
	}
	island.ensureReuse()
	next := island.nextTreeBuffer()
	island.evalTreeInto(next)
	return next
}

// skipFixtureProgram is the smallest island whose reused subtree is DYNAMIC.
//
//	<div>
//	  {count}                 changes on every bump
//	  <span>{label}</span>    reads another signal, so bump leaves it alone
//	</div>
//
// The counter fixture cannot serve here. Its buttons are the only reusable
// children and Program.StaticMask marks them static, so the diff skips them
// for a second reason and a test built on it would pass with the skip removed.
// This program ships no static mask at all, so the skip is the only thing that
// can stop the walk.
func skipFixtureProgram() *program.Program {
	return &program.Program{
		Name: "SkipFixture",
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1, 2}},
			{Kind: program.NodeExpr, Expr: 0},
			{Kind: program.NodeElement, Tag: "span", Children: []program.NodeID{3}},
			{Kind: program.NodeExpr, Expr: 1},
		},
		Root: 0,
		Exprs: []program.Expr{
			{Op: program.OpSignalGet, Value: "count", Type: program.TypeInt},                                   // 0
			{Op: program.OpSignalGet, Value: "label", Type: program.TypeString},                                // 1
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},                                          // 2
			{Op: program.OpLitString, Value: "L", Type: program.TypeString},                                    // 3
			{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},                                          // 4
			{Op: program.OpAdd, Operands: []program.ExprID{0, 4}, Type: program.TypeInt},                       // 5
			{Op: program.OpSignalSet, Operands: []program.ExprID{5}, Value: "count", Type: program.TypeInt},    // 6
			{Op: program.OpConcat, Operands: []program.ExprID{1, 3}, Type: program.TypeString},                 // 7
			{Op: program.OpSignalSet, Operands: []program.ExprID{7}, Value: "label", Type: program.TypeString}, // 8
		},
		Signals: []program.SignalDef{
			{Name: "count", Type: program.TypeInt, Init: program.ExprID(2)},
			{Name: "label", Type: program.TypeString, Init: program.ExprID(3)},
		},
		Handlers: []program.Handler{
			{Name: "bump", Body: []program.ExprID{6}},
			{Name: "relabel", Body: []program.ExprID{8}},
		},
	}
}

// markedSkips lists the resolved indices the last walk copied verbatim.
func markedSkips(spans []nodeSpan) []int {
	var marked []int
	for _, span := range spans {
		if span.valid && span.verbatim {
			marked = append(marked, int(span.start))
		}
	}
	return marked
}

// firstDifferentNode walks the subtree rooted at idx in both trees and returns
// the first index whose nodes differ, or -1 when the whole subtree matches.
//
// It compares at EQUAL indices on purpose. That is exactly the claim a mark
// makes, and the claim the diff relies on when it stops at the root.
func firstDifferentNode(prev, next *ResolvedTree, idx, depth int) int {
	if depth > 64 {
		return idx
	}
	if idx < 0 || idx >= len(prev.Nodes) || idx >= len(next.Nodes) {
		return idx
	}
	if !nodesEqual(&prev.Nodes[idx], &next.Nodes[idx]) {
		return idx
	}
	for _, child := range next.Nodes[idx].Children {
		if bad := firstDifferentNode(prev, next, child, depth+1); bad >= 0 {
			return bad
		}
	}
	return -1
}

// TestSkipMarksOnlyIdenticalSubtrees pins the invariant the skip rests on.
//
// A mark says "the diff may stop here". That is sound only when the marked
// node, and every node under it, equals the node the diff would compare it
// against, at the same index. The handler sequence drives the fixture through
// both shapes the walk produces: a whole-tree copy, and a copy of the sibling
// beside a node that changed.
func TestSkipMarksOnlyIdenticalSubtrees(t *testing.T) {
	island := NewIsland(skipFixtureProgram(), `{}`)
	island.Dispatch("bump", "{}") // builds the analysis and primes it

	total := 0
	for _, handler := range []string{"bump", "bump", "relabel", "", "bump", ""} {
		prev := island.prev
		next := walkOnly(t, island, handler)
		marked := markedSkips(island.verbatimSpans())
		total += len(marked)
		for _, idx := range marked {
			if bad := firstDifferentNode(prev, next, idx, 0); bad >= 0 {
				t.Fatalf("handler %q: subtree marked at %d differs at node %d\n prev: %+v\n next: %+v",
					handler, idx, bad, prev.Nodes[bad], next.Nodes[bad])
			}
		}
		island.spare, island.prev = island.prev, next
	}
	if total == 0 {
		t.Fatal("no walk copied anything verbatim, so the test proves nothing")
	}
}

// TestSkipSuppressesTheDiffOfACopiedSubtree is the differential that proves
// the skip fires, and that skipOff switches it off.
//
// The test writes a sentinel into the previous tree AFTER the walk copied out
// of it. The two sides then disagree, so a diff that walks the subtree emits a
// patch and a diff that skips it emits none. Without this pin the skipOff arm
// of the fuzz target could compare an island with itself and prove nothing.
func TestSkipSuppressesTheDiffOfACopiedSubtree(t *testing.T) {
	cases := []struct {
		name    string
		handler string
	}{
		// Nothing changed, so the walk copies the whole root in one step.
		{name: "WholeTree", handler: ""},
		// The count changed, so the root is evaluated again and only the
		// <span> beside the changed display node is copied.
		{name: "OneChild", handler: "bump"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			island := NewIsland(skipFixtureProgram(), `{}`)
			island.Dispatch("bump", "{}")
			island.Dispatch("bump", "{}")

			prev := island.prev
			next := walkOnly(t, island, testCase.handler)
			spans := island.verbatimSpans()
			marked := markedSkips(spans)
			if len(marked) == 0 {
				t.Fatal("the walk copied nothing verbatim, so the test proves nothing")
			}

			// Record what an honest diff emits before the tree is changed.
			mask := island.program.StaticMask
			honest := reconcileTreesInto(&diffWalk{}, prev, next, mask, nil)

			// Now lie: change one element's text inside a marked subtree.
			// The two trees disagree from here on, so a diff that walks the
			// subtree must emit one more op than the honest run.
			target := marked[len(marked)-1]
			prev.Nodes[target].Text = "sentinel that the diff must not see"

			withSkip := reconcileTreesInto(&diffWalk{}, prev, next, mask, spans)
			withoutSkip := reconcileTreesInto(&diffWalk{}, prev, next, mask, nil)

			if !reflect.DeepEqual(withSkip, honest) {
				t.Fatalf("the diff compared a subtree it was told to skip\n"+
					" with skip: %#v\n    honest: %#v", withSkip, honest)
			}
			if reflect.DeepEqual(withoutSkip, honest) {
				t.Fatalf("skipOff did not compare the changed subtree either, so the "+
					"sentinel proves nothing and the fuzz arm would compare an "+
					"island with itself: %#v", withoutSkip)
			}
		})
	}
}

// TestSkipOffKeepsTheIslandCorrect pins that the flag changes speed and
// nothing else. It is the counterpart of TestRecycleOffAllocatesEveryWalk.
func TestSkipOffKeepsTheIslandCorrect(t *testing.T) {
	withSkip := NewIsland(skipFixtureProgram(), `{}`)
	withoutSkip := NewIsland(skipFixtureProgram(), `{}`)
	withoutSkip.skipOff = true

	for step := 0; step < 6; step++ {
		handler := "bump"
		if step%3 == 2 {
			handler = "relabel"
		}
		got := withSkip.Dispatch(handler, "{}")
		want := withoutSkip.Dispatch(handler, "{}")
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("step %d: ops differ\n  skip on: %#v\n skip off: %#v", step, got, want)
		}
		if !treesEqual(withSkip.prev, withoutSkip.prev) {
			t.Fatalf("step %d: trees differ", step)
		}
	}
	if withSkip.verbatimSpans() == nil {
		t.Fatal("the island with the skip on reports no span table")
	}
	if withoutSkip.verbatimSpans() != nil {
		t.Fatal("skipOff still handed the span table to the diff")
	}
}

// TestSkipIgnoresAShiftedCopy pins the other half of the mark rule. A subtree
// that landed at a different index carries rebuilt child indices, so the nodes
// no longer match the prev tree position for position and the diff must run.
func TestSkipIgnoresAShiftedCopy(t *testing.T) {
	island := NewIsland(conditionalShiftProgram(), `{}`)
	island.Reconcile() // builds the analysis and primes the snapshot

	// Collapsing the conditional moves the <span> from index 3 to index 1.
	prev := island.prev
	next := walkOnly(t, island, "hide")

	spanBefore := indexOfTag(prev, "span")
	spanAfter := indexOfTag(next, "span")
	if spanBefore < 0 || spanAfter < 0 || spanBefore == spanAfter {
		t.Fatalf("the span did not move: before=%d after=%d; the fixture no longer "+
			"exercises a shifted copy", spanBefore, spanAfter)
	}
	for _, idx := range markedSkips(island.verbatimSpans()) {
		if idx == spanAfter {
			t.Fatalf("index %d was marked even though the subtree moved from %d",
				spanAfter, spanBefore)
		}
	}
}

// TestReusePlanRejectsATaglessElement pins the merge guard the plan carries.
//
// mergeAdjacentText appends a following sibling's text into a text node IN
// PLACE, and it reads any node with no tag and no children as a text node. A
// copied subtree whose root reads that way would be rewritten after the copy:
// the tree would hold last generation's text concatenated with this one's, and
// the diff skip would compare two nodes that no longer agree.
//
// The lowerer always emits a tag, so this is a hand-built program only. The
// plan refuses it anyway, because the cost is one comparison at load time.
func TestReusePlanRejectsATaglessElement(t *testing.T) {
	prog := &program.Program{
		Name: "TaglessElement",
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1, 2}},
			{Kind: program.NodeElement, Tag: ""}, // reads as a text node
			{Kind: program.NodeExpr, Expr: 0},
		},
		Root: 0,
		Exprs: []program.Expr{
			{Op: program.OpSignalGet, Value: "count", Type: program.TypeInt},
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},
		},
		Signals: []program.SignalDef{
			{Name: "count", Type: program.TypeInt, Init: program.ExprID(1)},
		},
	}
	plan := newReusePlan(prog)
	if plan == nil {
		t.Fatal("the program should still get a plan through its tagged root")
	}
	if plan.nodes[1].reusable {
		t.Error("a tagless element must not be reusable: mergeAdjacentText reads it as " +
			"a text node and rewrites it after the copy")
	}
	if !plan.nodes[0].reusable {
		t.Error("the tagged root should stay reusable")
	}
}

// TestSkipDropsSpansTheWalkDidNotVisit pins the invalidation that carries the
// whole span table, and now the diff skip with it.
//
// A collapsing conditional stops resolving the nodes under it, so those nodes
// record no span for that walk. The span buffers alternate, so an entry the
// walk leaves untouched still holds what it held TWO walks ago: a range in a
// tree that is two generations old, and a verbatim flag from a copy that is no
// longer current. begin clears the buffer for exactly this reason.
//
// The three-way comparison is the guard. A stale verbatim flag emits no op, so
// only the tree oracle and the skipOff arm can see it.
func TestSkipDropsSpansTheWalkDidNotVisit(t *testing.T) {
	withSkip := NewIsland(conditionalShiftProgram(), `{}`)
	withoutSkip := NewIsland(conditionalShiftProgram(), `{}`)
	withoutSkip.skipOff = true

	// The repeats matter. A walk that changes nothing copies the whole root
	// in one step and records one span, so every entry below the root is
	// left untouched for that walk. Strict alternation would keep each node
	// on the same buffer and hide the leftovers.
	handlers := []string{"hide", "show", "show", "hide", "hide", "show", "hide", "show", "show", "hide"}
	for step, handler := range handlers {
		got := withSkip.Dispatch(handler, "{}")
		want := withoutSkip.Dispatch(handler, "{}")
		if !treesEqual(withSkip.prev, fullEvalTree(withSkip)) {
			t.Fatalf("step %d (%s): the tree differs from a full evaluation: %s",
				step, handler, describeFuzzTree(withSkip.prev))
		}
		if !treesEqual(withSkip.prev, withoutSkip.prev) {
			t.Fatalf("step %d (%s): the tree differs with the skip off", step, handler)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("step %d (%s): ops differ\n  skip on: %#v\n skip off: %#v",
				step, handler, got, want)
		}
	}
}

// TestSkipNeedsBothTreesAtTheSameIndex pins the second half of the skip rule.
//
// A mark says "next.Nodes[i] was copied from prev.Nodes[i]". It licenses
// skipping only the pair that compares those two nodes. The index-positional
// walk pairs children by their POSITION in the child list, so it can hand the
// diff a marked next node beside a different prev node, and that pair carries a
// real change.
//
// The trees below are built by hand because the tree walk does not produce the
// shape today. The guard is the reason it stays safe if it ever does: a diff
// that trusted the mark alone would drop the patch and leave the browser one
// generation behind, with no op to show for it.
func TestSkipNeedsBothTreesAtTheSameIndex(t *testing.T) {
	// Both children share program source 1, so their identities repeat and
	// reconcileChildren falls through to the index-positional walk.
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", HasSource: true, Source: 0, Children: []int{1, 2}},
		{Tag: "span", HasSource: true, Source: 1, Text: "first"},
		{Tag: "span", HasSource: true, Source: 1, Text: "second"},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", HasSource: true, Source: 0, Children: []int{1, 3}},
		{Tag: "span", HasSource: true, Source: 1, Text: "first"},
		{Tag: "span", HasSource: true, Source: 1, Text: "second"},
		{Tag: "span", HasSource: true, Source: 1, Text: "changed"},
	}}

	// The walk copied the subtree of program node 1 verbatim, and it landed
	// at index 3, which the child list does not name. Position 1 therefore
	// pairs prev index 2 with next index 3, and that pair must still be
	// diffed.
	spans := make([]nodeSpan, 2)
	spans[1] = nodeSpan{start: 3, end: 4, valid: true, verbatim: true}

	ops := reconcileTreesInto(&diffWalk{}, prev, next, nil, spans)
	found := false
	for _, op := range ops {
		if op.Kind == PatchSetText && op.Text == "changed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the diff skipped a pair whose two sides sit at different indices, so "+
			"the change was never patched: %#v", ops)
	}
}

func indexOfTag(tree *ResolvedTree, tag string) int {
	for i := range tree.Nodes {
		if tree.Nodes[i].Tag == tag {
			return i
		}
	}
	return -1
}

// TestSkipStopsAtTheSpanRootOnly pins that the mark addresses one node, not a
// range. skipsSubtree finds the span through the resolved node's Source, so a
// node whose Source names a verbatim span but which sits somewhere else must
// still be diffed.
func TestSkipStopsAtTheSpanRootOnly(t *testing.T) {
	tree := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", HasSource: true, Source: 0},
		{Tag: "span", HasSource: true, Source: 1},
		// Index 2 names the same program node as the span root at index 1.
		// The walk does not produce that today; mergeAdjacentText rewriting
		// a Source is the way it could. Only the node the span actually
		// names may be skipped.
		{Tag: "span", HasSource: true, Source: 1},
		{Tag: "span"}, // no source at all
	}}
	spans := make([]nodeSpan, 2)
	spans[1] = nodeSpan{start: 1, end: 2, valid: true, verbatim: true}
	w := diffWalk{prev: tree, next: tree, spans: spans}

	if !w.skipsSubtree(1, 1) {
		t.Fatal("the span root is not skipped")
	}
	for _, pair := range [][2]int{{0, 0}, {2, 2}, {3, 3}, {0, 1}, {1, 0}, {1, 2}, {-1, -1}, {9, 9}} {
		if w.skipsSubtree(pair[0], pair[1]) {
			t.Errorf("pair %v was skipped; only the span root at its own index qualifies", pair)
		}
	}

	// An invalid span, and a span the walk did not copy, license nothing.
	spans[1] = nodeSpan{start: 1, end: 2, valid: true}
	if w.skipsSubtree(1, 1) {
		t.Fatal("a freshly evaluated subtree was skipped")
	}
	if (&diffWalk{prev: tree, next: tree}).skipsSubtree(1, 1) {
		t.Fatal("a walk with no span table skipped a node")
	}
}
