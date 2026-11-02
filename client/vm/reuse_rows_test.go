package vm

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// Rules for row reuse, each with the test that catches a row that stops
// updating if the rule is broken:
//
//  1. A row is copied only when its item, key and index match
//     (TestRowReuseCopiesUnchangedRowsOnly, TestRowItemEditedInPlaceIsEvaluatedAgain,
//     TestRowMatchIsByPositionNotByContent, TestRowKeyDecidesForObjectEntries).
//  2. A row that reads another input is dirty when that input is
//     (TestRowReadingAnotherInputFollowsIt).
//  3. A forced walk copies no row (TestForcedWalkCopiesNoRow).
//  4. Only rows whose roots are tagged elements are copied
//     (TestRowsWithTextRootsAreNeverCopied).
//  5. A copy at the index the row already held is skipped by the diff, and a
//     copy at another index is not (TestRowCopyIsSkippedByTheDiffOnlyAtTheSameIndex,
//     TestRowsAfterAGrowingRowShiftAndStayCorrect).
//  6. Row records survive a walk that copies the subtree around them
//     (TestRowRecordsSurviveACopiedParent).
//  7. Whatever the writes, the tree equals a full walk's
//     (TestRowReuseMatchesFullEvalOnRandomWrites).

type rowOpts struct {
	suffix    bool // each row also reads s.suffix
	textRoot  bool // the body is bare text instead of an element
	extraNode bool // the row's <li> holds a conditional <b>
	byObject  bool // iterate s.rows as an object (keys decide)
}

// rowProgram is
//
//	<section>
//	  <h1>{s.title}</h1>
//	  <ul>
//	    <ForEach s.rows as row>
//	      <li>{row.name}[ {s.suffix}][<b>{row.note}</b> if row.note]</li>
//	    </ForEach>
//	  </ul>
//	</section>
func rowProgram(o rowOpts) *program.Program {
	prog := &program.Program{Name: "Rows", Root: 0}
	expr := func(e program.Expr) program.ExprID {
		prog.Exprs = append(prog.Exprs, e)
		return program.ExprID(len(prog.Exprs) - 1)
	}
	node := func(n program.Node) program.NodeID {
		prog.Nodes = append(prog.Nodes, n)
		return program.NodeID(len(prog.Nodes) - 1)
	}
	lit := func(s string) program.ExprID {
		return expr(program.Expr{Op: program.OpLitString, Value: s, Type: program.TypeString})
	}
	idx := func(base program.ExprID, field string) program.ExprID {
		return expr(program.Expr{Op: program.OpIndex, Operands: []program.ExprID{base, lit(field)}, Type: program.TypeAny})
	}
	sig := expr(program.Expr{Op: program.OpSignalGet, Value: "s", Type: program.TypeAny})
	row := expr(program.Expr{Op: program.OpPropGet, Value: "row", Type: program.TypeAny})
	zero := expr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
	prog.Signals = []program.SignalDef{{Name: "s", Type: program.TypeAny, Init: zero}}

	root := node(program.Node{Kind: program.NodeElement, Tag: "section"})
	h1 := node(program.Node{Kind: program.NodeElement, Tag: "h1"})
	h1Text := node(program.Node{Kind: program.NodeExpr, Expr: idx(sig, "title")})
	prog.Nodes[h1].Children = []program.NodeID{h1Text}
	ul := node(program.Node{Kind: program.NodeElement, Tag: "ul"})
	each := node(program.Node{Kind: program.NodeForEach, Expr: idx(sig, "rows"),
		Attrs: []program.Attr{{Kind: program.AttrStatic, Name: "as", Value: "row"}}})
	name := node(program.Node{Kind: program.NodeExpr, Expr: idx(row, "name")})
	if o.textRoot {
		prog.Nodes[each].Children = []program.NodeID{name}
	} else {
		li := node(program.Node{Kind: program.NodeElement, Tag: "li", Children: []program.NodeID{name}})
		if o.suffix {
			sfx := node(program.Node{Kind: program.NodeExpr, Expr: idx(sig, "suffix")})
			prog.Nodes[li].Children = append(prog.Nodes[li].Children, sfx)
		}
		if o.extraNode {
			b := node(program.Node{Kind: program.NodeElement, Tag: "b"})
			note := node(program.Node{Kind: program.NodeExpr, Expr: idx(row, "note")})
			prog.Nodes[b].Children = []program.NodeID{note}
			cond := node(program.Node{Kind: program.NodeConditional, Expr: idx(row, "note"), Children: []program.NodeID{b}})
			prog.Nodes[li].Children = append(prog.Nodes[li].Children, cond)
		}
		prog.Nodes[each].Children = []program.NodeID{li}
	}
	prog.Nodes[ul].Children = []program.NodeID{each}
	prog.Nodes[root].Children = []program.NodeID{h1, ul}
	return prog
}

type rowItem struct{ name, note string }

func rowState(title, suffix string, items ...rowItem) Value {
	rows := make([]Value, len(items))
	for i, it := range items {
		rows[i] = obj("name", StringVal(it.name), "note", StringVal(it.note))
	}
	return obj("title", StringVal(title), "suffix", StringVal(suffix), "rows", ArrayVal(rows))
}

func names(n int) []rowItem {
	items := make([]rowItem, n)
	for i := range items {
		items[i] = rowItem{name: fmt.Sprintf("r%d", i)}
	}
	return items
}

func newRowIsland(t *testing.T, o rowOpts, initial Value) (*Island, *signal.Signal[Value]) {
	t.Helper()
	island := NewIsland(rowProgram(o), `{}`)
	sig := signal.New(initial)
	island.vm.SetSignal("s", sig)
	island.Reconcile()
	if island.reuse == nil {
		t.Fatal("rows program should get a reuse plan")
	}
	if !island.reuse.plan.rows[4].ok && !island.reuse.plan.rows[5].ok {
		t.Fatal("the forEach should be eligible for row reuse")
	}
	return island, sig
}

func rowHits(island *Island) int { return island.reuse.ctx.rowHits }

func TestRowReuseCopiesUnchangedRowsOnly(t *testing.T) {
	island, sig := newRowIsland(t, rowOpts{}, rowState("t", "", names(6)...))
	items := names(6)
	items[3].name = "changed"
	sig.Set(rowState("t", "", items...))
	patches := island.Reconcile()
	mustMatchFullEval(t, island, "one row changed")
	if got := rowHits(island); got != 5 {
		t.Errorf("rows copied = %d, want 5", got)
	}
	if len(patches) != 1 || patches[0].Kind != PatchSetText || patches[0].Text != "changed" {
		t.Errorf("patches = %#v, want one SetText(changed)", patches)
	}
	// Nothing changes in the list; the heading does.
	sig.Set(rowState("t2", "", items...))
	island.Reconcile()
	mustMatchFullEval(t, island, "heading changed")
	if got := reuseHits(island); got != 1 {
		t.Errorf("subtrees copied after a heading-only write = %d, want 1 (the whole list)", got)
	}
}

func TestRowItemEditedInPlaceIsEvaluatedAgain(t *testing.T) {
	state := rowState("t", "", names(4)...)
	island, _ := newRowIsland(t, rowOpts{}, state)
	rowsValue := state.dict()["rows"]
	row := rowsValue.list()[2]
	row.dict()["name"] = StringVal("edited")
	island.Reconcile()
	mustMatchFullEval(t, island, "in-place row edit")
	if island.prev.Nodes[findText(island.prev, "edited")].Text != "edited" {
		t.Fatal("edited row is missing")
	}
	if got := rowHits(island); got != 3 {
		t.Errorf("rows copied = %d, want 3", got)
	}
}

func findText(tree *ResolvedTree, text string) int {
	for i, n := range tree.Nodes {
		if n.Tag == "" && n.Text == text {
			return i
		}
	}
	return 0
}

func TestRowMatchIsByPositionNotByContent(t *testing.T) {
	island, sig := newRowIsland(t, rowOpts{}, rowState("t", "", names(5)...))
	// A row is inserted at the front: every position holds a different item.
	items := append([]rowItem{{name: "new"}}, names(5)...)
	sig.Set(rowState("t", "", items...))
	island.Reconcile()
	mustMatchFullEval(t, island, "insert at front")
	if got := rowHits(island); got != 0 {
		t.Errorf("rows copied after an insert at the front = %d, want 0", got)
	}
	// The last row is removed: the rest keep their positions.
	sig.Set(rowState("t", "", items[:5]...))
	island.Reconcile()
	mustMatchFullEval(t, island, "remove last")
	if got := rowHits(island); got != 5 {
		t.Errorf("rows copied after removing the last = %d, want 5", got)
	}
	// Reordering swaps two positions.
	items[0], items[1] = items[1], items[0]
	sig.Set(rowState("t", "", items[:5]...))
	island.Reconcile()
	mustMatchFullEval(t, island, "swap")
	if got := rowHits(island); got != 3 {
		t.Errorf("rows copied after a swap = %d, want 3", got)
	}
}

func TestRowKeyDecidesForObjectEntries(t *testing.T) {
	prog := rowProgram(rowOpts{})
	island := NewIsland(prog, `{}`)
	rows := func(kv ...string) Value {
		f := map[string]Value{}
		for i := 0; i < len(kv); i += 2 {
			f[kv[i]] = obj("name", StringVal(kv[i+1]))
		}
		return obj("title", StringVal("t"), "rows", ObjectVal(f))
	}
	sig := signal.New(rows("a", "x", "b", "y"))
	island.vm.SetSignal("s", sig)
	island.Reconcile()
	// Same items at the same positions under different keys: not the same rows.
	sig.Set(rows("c", "x", "d", "y"))
	island.Reconcile()
	mustMatchFullEval(t, island, "keys renamed")
	if got := rowHits(island); got != 0 {
		t.Errorf("rows copied after the keys changed = %d, want 0", got)
	}
	sig.Set(rows("c", "x", "d", "z"))
	island.Reconcile()
	mustMatchFullEval(t, island, "one item changed")
	if got := rowHits(island); got != 1 {
		t.Errorf("rows copied = %d, want 1", got)
	}
}

func TestRowReadingAnotherInputFollowsIt(t *testing.T) {
	island, sig := newRowIsland(t, rowOpts{suffix: true}, rowState("t", "-a", names(4)...))
	sig.Set(rowState("t", "-b", names(4)...))
	island.Reconcile()
	mustMatchFullEval(t, island, "suffix changed")
	if got := rowHits(island); got != 0 {
		t.Errorf("rows copied after the suffix every row reads changed = %d, want 0", got)
	}
	if island.prev.Nodes[findText(island.prev, "-b")].Text != "-b" {
		t.Fatal("rows did not pick up the new suffix")
	}
	// A write to a field no row reads copies them all.
	sig.Set(rowState("t3", "-b", names(4)...))
	island.Reconcile()
	mustMatchFullEval(t, island, "title changed")
	if got := reuseHits(island); got != 1 {
		t.Errorf("subtrees copied after a title-only write = %d, want 1 (the whole list)", got)
	}
}

func TestForcedWalkCopiesNoRow(t *testing.T) {
	island, _ := newRowIsland(t, rowOpts{}, rowState("t", "", names(4)...))
	// An auto-key prop moving forces a full walk.
	island.vm.props["_index"] = IntVal(7)
	island.Reconcile()
	mustMatchFullEval(t, island, "forced walk")
	if got := rowHits(island); got != 0 {
		t.Errorf("rows copied by a forced walk = %d, want 0", got)
	}
	delete(island.vm.props, "_index")
	island.Reconcile()
	mustMatchFullEval(t, island, "forced walk ends")
}

func TestRowsWithTextRootsAreNeverCopied(t *testing.T) {
	island, sig := newRowIsland(t, rowOpts{textRoot: true}, rowState("t", "", names(5)...))
	for i := 0; i < 4; i++ {
		items := names(5)
		items[i].name = fmt.Sprintf("n%d", i)
		sig.Set(rowState("t", "", items...))
		island.Reconcile()
		mustMatchFullEval(t, island, fmt.Sprintf("text rows step %d", i))
		if got := rowHits(island); got != 0 {
			t.Errorf("text-root rows copied = %d, want 0", got)
		}
	}
}

func TestRowCopyIsSkippedByTheDiffOnlyAtTheSameIndex(t *testing.T) {
	island, sig := newRowIsland(t, rowOpts{}, rowState("t", "", names(6)...))
	items := names(6)
	items[4].name = "changed"
	sig.Set(rowState("t", "", items...))
	prev := island.prev
	next := walkOnly(t, island, "")
	marks, gen := island.verbatimRows()
	if gen == 0 {
		t.Fatal("no generation")
	}
	marked := []int{}
	for i, g := range marks {
		if g == gen {
			marked = append(marked, i)
		}
	}
	if len(marked) != 5 {
		t.Fatalf("marked rows = %d, want 5", len(marked))
	}
	mask := island.program.StaticMask
	honest := reconcileTreesInto(&diffWalk{}, prev, next, mask, nil)
	// Lie inside a marked row: a diff that walks it emits one more op.
	prev.Nodes[marked[0]+1].Text = "sentinel the diff must not see"
	skip := &diffWalk{rowMark: marks, rowGen: gen}
	withSkip := reconcileTreesInto(skip, prev, next, mask, nil)
	without := reconcileTreesInto(&diffWalk{}, prev, next, mask, nil)
	if !reflect.DeepEqual(withSkip, honest) {
		t.Fatalf("the diff compared a row it was told to skip\n with: %#v\n honest: %#v", withSkip, honest)
	}
	if reflect.DeepEqual(without, honest) {
		t.Fatal("without the skip the sentinel is invisible, so the test proves nothing")
	}
}

func TestRowsAfterAGrowingRowShiftAndStayCorrect(t *testing.T) {
	o := rowOpts{extraNode: true}
	items := names(6)
	island, sig := newRowIsland(t, o, rowState("t", "", items...))
	// Row 1 gains a <b>: every later row moves by two nodes. They are copied
	// to a new index, so the diff must compare them, and their children must
	// point at the new positions.
	items[1].note = "note"
	sig.Set(rowState("t", "", items...))
	skipOff := NewIsland(rowProgram(o), `{}`)
	skipOff.skipOff = true
	skipSig := signal.New(rowState("t", "", names(6)...))
	skipOff.vm.SetSignal("s", skipSig)
	skipOff.Reconcile()
	skipSig.Set(rowState("t", "", items...))

	got := island.Reconcile()
	want := skipOff.Reconcile()
	mustMatchFullEval(t, island, "row grew")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ops differ with the row skip on and off\n on: %#v\noff: %#v", got, want)
	}
	if got := rowHits(island); got != 5 {
		t.Errorf("rows copied = %d, want 5", got)
	}
	// And back, then another change after the shift.
	items[1].note = ""
	items[5].name = "last"
	sig.Set(rowState("t", "", items...))
	island.Reconcile()
	mustMatchFullEval(t, island, "row shrank")
}

func TestRowRecordsSurviveACopiedParent(t *testing.T) {
	island, sig := newRowIsland(t, rowOpts{}, rowState("t", "", names(6)...))
	// Nothing the island reads changes: the root is copied in one step, and the
	// rows inside it are not walked.
	current := island.vm.signals["s"].Get()
	sig.Set(obj("title", StringVal("t"), "suffix", StringVal("unread"), "rows", current.dict()["rows"]))
	island.Reconcile()
	if reuseHits(island) != 1 {
		t.Fatalf("root hits = %d, want 1", reuseHits(island))
	}
	// Now one row changes. The records must have been carried.
	items := names(6)
	items[2].name = "x"
	sig.Set(rowState("t", "", items...))
	island.Reconcile()
	mustMatchFullEval(t, island, "row after a copied parent")
	if got := rowHits(island); got != 5 {
		t.Errorf("rows copied after a copied parent = %d, want 5", got)
	}
}

func TestRowReuseMatchesFullEvalOnRandomWrites(t *testing.T) {
	for _, o := range []rowOpts{{}, {suffix: true}, {extraNode: true}, {suffix: true, extraNode: true}, {textRoot: true}} {
		for seed := int64(0); seed < 30; seed++ {
			rng := rand.New(rand.NewSource(seed))
			island, sig := newRowIsland(t, o, rowState("t", "s", names(4)...))
			skipOff := NewIsland(rowProgram(o), `{}`)
			skipOff.skipOff = true
			skipSig := signal.New(rowState("t", "s", names(4)...))
			skipOff.vm.SetSignal("s", skipSig)
			skipOff.Reconcile()
			title, suffix := "t", "s"
			items := names(4)
			for step := 0; step < 30; step++ {
				switch rng.Intn(6) {
				case 0:
					title = string(rune('t' + rng.Intn(3)))
				case 1:
					suffix = string(rune('s' + rng.Intn(3)))
				case 2:
					if len(items) > 0 {
						items[rng.Intn(len(items))].name = string(rune('a' + rng.Intn(4)))
					}
				case 3:
					if len(items) > 0 {
						items[rng.Intn(len(items))].note = []string{"", "n1", "n2"}[rng.Intn(3)]
					}
				case 4:
					items = append(items, rowItem{name: string(rune('a' + rng.Intn(4)))})
				case 5:
					if len(items) > 0 {
						i := rng.Intn(len(items))
						items = append(items[:i:i], items[i+1:]...)
					}
				}
				state := rowState(title, suffix, items...)
				sig.Set(state)
				skipSig.Set(rowState(title, suffix, items...))
				got := island.Reconcile()
				want := skipOff.Reconcile()
				mustMatchFullEval(t, island, fmt.Sprintf("%+v seed %d step %d", o, seed, step))
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%+v seed %d step %d: ops differ with the row skip on and off\n on: %#v\noff: %#v", o, seed, step, got, want)
				}
			}
		}
	}
}
