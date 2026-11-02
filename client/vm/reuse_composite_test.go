package vm

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// Rules this file pins, each with the test that catches a node that stops
// updating if the rule is broken:
//
//  1. A field-path read is dirty exactly when the digest of that part of the
//     value changes (TestPathReadsAreCleanWhenOtherFieldsChange,
//     TestPathReadFollowsItsOwnField, TestPathDigestSeesTypeChanges,
//     TestPathDigestSeesAbsentAndNestedFields).
//  2. A composite edited in place is seen, because the digest is of content
//     (TestInPlaceEditOfAWatchedFieldIsSeen, TestInPlaceEditOfAnUnwatchedFieldKeepsReuse).
//  3. A read of the whole value, or through a non-literal index, depends on the
//     whole value (TestWholeValueReadSeesEveryField).
//  4. A loop variable is internal to its forEach: it never dirties the
//     elements around the loop, and never hides an outer prop of the same name
//     (TestLoopVariablesDoNotDirtyTheElementAroundTheLoop,
//     TestLoopVariableNameDoesNotShadowAnOuterProp).
//  5. Too many paths merge into their ancestors and stay correct
//     (TestPathsMergeIntoAncestorsWhenTheMaskIsFull).
//  6. Random composite writes never change the tree a full walk produces
//     (TestCompositeReuseMatchesFullEvalOnRandomWrites).

// pathProgram is
//
//	<div>
//	  <p>{s.a}</p>        node 1, reads path s.a
//	  <p>{s.b}</p>        node 3, reads path s.b
//	  <p>{s.c.d}</p>      node 5, reads path s.c.d
//	</div>
func pathProgram() *program.Program {
	return &program.Program{
		Name: "Paths",
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1, 3, 5}},
			{Kind: program.NodeElement, Tag: "p", Children: []program.NodeID{2}},
			{Kind: program.NodeExpr, Expr: 2},
			{Kind: program.NodeElement, Tag: "p", Children: []program.NodeID{4}},
			{Kind: program.NodeExpr, Expr: 4},
			{Kind: program.NodeElement, Tag: "p", Children: []program.NodeID{6}},
			{Kind: program.NodeExpr, Expr: 7},
		},
		Root: 0,
		Exprs: []program.Expr{
			{Op: program.OpSignalGet, Value: "s", Type: program.TypeAny},                   // 0
			{Op: program.OpLitString, Value: "a", Type: program.TypeString},                // 1
			{Op: program.OpIndex, Operands: []program.ExprID{0, 1}, Type: program.TypeAny}, // 2: s.a
			{Op: program.OpLitString, Value: "b", Type: program.TypeString},                // 3
			{Op: program.OpIndex, Operands: []program.ExprID{0, 3}, Type: program.TypeAny}, // 4: s.b
			{Op: program.OpLitString, Value: "c", Type: program.TypeString},                // 5
			{Op: program.OpIndex, Operands: []program.ExprID{0, 5}, Type: program.TypeAny}, // 6: s.c
			{Op: program.OpIndex, Operands: []program.ExprID{6, 8}, Type: program.TypeAny}, // 7: s.c.d
			{Op: program.OpLitString, Value: "d", Type: program.TypeString},                // 8
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},                      // 9
		},
		Signals: []program.SignalDef{{Name: "s", Type: program.TypeAny, Init: 9}},
	}
}

func obj(kv ...any) Value {
	fields := map[string]Value{}
	for i := 0; i < len(kv); i += 2 {
		fields[kv[i].(string)] = kv[i+1].(Value)
	}
	return ObjectVal(fields)
}

func newPathIsland(t *testing.T, prog *program.Program, initial Value) (*Island, *signal.Signal[Value]) {
	t.Helper()
	island := NewIsland(prog, `{}`)
	sig := signal.New(initial)
	island.vm.SetSignal("s", sig)
	island.Reconcile() // builds the plan and primes the snapshot
	if island.reuse == nil {
		t.Fatal("program should get a reuse plan")
	}
	return island, sig
}

func texts(tree *ResolvedTree) string {
	var parts []string
	for _, n := range tree.Nodes {
		if n.Tag == "" && n.Text != "" {
			parts = append(parts, n.Text)
		}
	}
	return strings.Join(parts, "|")
}

func mustMatchFullEval(t *testing.T, island *Island, what string) {
	t.Helper()
	if !treesEqual(island.prev, fullEvalTree(island)) {
		t.Fatalf("%s: reused tree %q differs from a full evaluation %q", what, texts(island.prev), texts(fullEvalTree(island)))
	}
}

func TestPathReadsAreCleanWhenOtherFieldsChange(t *testing.T) {
	island, sig := newPathIsland(t, pathProgram(), obj("a", StringVal("1"), "b", StringVal("2"), "c", obj("d", StringVal("3"))))
	if got := texts(island.prev); got != "1|2|3" {
		t.Fatalf("initial text %q", got)
	}
	// Only b changes. The paragraphs that read a and c.d must be copied, and a
	// field nothing reads must not dirty anything.
	sig.Set(obj("a", StringVal("1"), "b", StringVal("two"), "c", obj("d", StringVal("3")), "unread", IntVal(7)))
	patches := island.Reconcile()
	if got := texts(island.prev); got != "1|two|3" {
		t.Fatalf("text after b changed = %q", got)
	}
	if hits := reuseHits(island); hits != 2 {
		t.Errorf("reused subtrees = %d, want 2 (the paragraphs for a and c.d)", hits)
	}
	if len(patches) != 1 || patches[0].Kind != PatchSetText || patches[0].Text != "two" {
		t.Errorf("patches = %#v, want one SetText(two)", patches)
	}
	mustMatchFullEval(t, island, "b changed")

	// A write that changes only an unread field re-evaluates nothing.
	sig.Set(obj("a", StringVal("1"), "b", StringVal("two"), "c", obj("d", StringVal("3")), "unread", IntVal(8)))
	patches = island.Reconcile()
	if len(patches) != 0 {
		t.Errorf("patches for an unread field = %#v, want none", patches)
	}
	if hits := reuseHits(island); hits != 1 {
		t.Errorf("reused subtrees after an unread-field write = %d, want 1 (the whole root)", hits)
	}
	mustMatchFullEval(t, island, "unread field changed")
}

func TestPathReadFollowsItsOwnField(t *testing.T) {
	island, sig := newPathIsland(t, pathProgram(), obj("a", StringVal("1"), "b", StringVal("2"), "c", obj("d", StringVal("3"))))
	for i, v := range []Value{
		obj("a", StringVal("x"), "b", StringVal("2"), "c", obj("d", StringVal("3"))),
		obj("a", StringVal("x"), "b", StringVal("2"), "c", obj("d", StringVal("y"))),
		obj("a", StringVal("x"), "b", StringVal("z"), "c", obj("d", StringVal("y"))),
		obj("a", StringVal("x"), "b", StringVal("z"), "c", obj("d", StringVal("y")), "e", IntVal(1)),
	} {
		sig.Set(v)
		island.Reconcile()
		mustMatchFullEval(t, island, fmt.Sprintf("step %d", i))
	}
	if got := texts(island.prev); got != "x|z|y" {
		t.Fatalf("final text %q", got)
	}
}

func TestPathDigestSeesTypeChanges(t *testing.T) {
	island, sig := newPathIsland(t, pathProgram(), obj("a", IntVal(1), "b", StringVal("2"), "c", obj("d", StringVal("3"))))
	// Same printed text, different type: a string "1", a float 1, a bool.
	for i, v := range []Value{StringVal("1"), FloatVal(1), IntVal(1), BoolVal(true)} {
		sig.Set(obj("a", v, "b", StringVal("2"), "c", obj("d", StringVal("3"))))
		island.Reconcile()
		mustMatchFullEval(t, island, fmt.Sprintf("type step %d", i))
		if island.prev.Nodes[2].Text != v.String() {
			t.Fatalf("step %d: text %q, want %q", i, island.prev.Nodes[2].Text, v.String())
		}
	}
}

func TestPathDigestSeesAbsentAndNestedFields(t *testing.T) {
	island, sig := newPathIsland(t, pathProgram(), obj("a", StringVal("1"), "c", obj("d", StringVal("3"))))
	steps := []Value{
		obj("a", StringVal("1")),                                    // c removed
		obj("a", StringVal("1"), "c", StringVal("not an object")),   // c.d on a string
		obj("a", StringVal("1"), "c", ArrayVal([]Value{IntVal(1)})), // c.d on a list
		obj("a", StringVal("1"), "c", obj("d", obj("deep", IntVal(1)))),
		obj("a", StringVal("1"), "c", obj("d", obj("deep", IntVal(2)))), // change below the watched value
		IntVal(5), // the whole signal becomes a scalar
		obj("a", StringVal("back"), "c", obj("d", StringVal("3"))),
		ZeroValue(program.TypeAny),
	}
	for i, v := range steps {
		sig.Set(v)
		island.Reconcile()
		mustMatchFullEval(t, island, fmt.Sprintf("shape step %d", i))
	}
}

func TestInPlaceEditOfAWatchedFieldIsSeen(t *testing.T) {
	inner := map[string]Value{"d": StringVal("3")}
	fields := map[string]Value{"a": StringVal("1"), "b": StringVal("2"), "c": ObjectVal(inner)}
	island, _ := newPathIsland(t, pathProgram(), ObjectVal(fields))

	// OpFieldSet writes through the map every alias holds. No Set happens.
	inner["d"] = StringVal("edited")
	island.Reconcile()
	if got := texts(island.prev); got != "1|2|edited" {
		t.Fatalf("text after an in-place nested edit = %q", got)
	}
	fields["a"] = StringVal("also edited")
	island.Reconcile()
	if got := texts(island.prev); got != "also edited|2|edited" {
		t.Fatalf("text after an in-place edit = %q", got)
	}
	mustMatchFullEval(t, island, "in-place edits")
}

func TestInPlaceEditOfAnUnwatchedFieldKeepsReuse(t *testing.T) {
	fields := map[string]Value{"a": StringVal("1"), "b": StringVal("2"), "c": obj("d", StringVal("3"))}
	island, _ := newPathIsland(t, pathProgram(), ObjectVal(fields))
	fields["unread"] = StringVal("x")
	patches := island.Reconcile()
	if len(patches) != 0 || reuseHits(island) != 1 {
		t.Errorf("patches %#v, hits %d: an edit nothing reads must keep the whole root", patches, reuseHits(island))
	}
}

// wholeProgram reads s as a whole (its length) and through a non-literal index.
func wholeProgram() *program.Program {
	return &program.Program{
		Name: "Whole",
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1, 3}},
			{Kind: program.NodeElement, Tag: "p", Children: []program.NodeID{2}},
			{Kind: program.NodeExpr, Expr: 1}, // len(s)
			{Kind: program.NodeElement, Tag: "p", Children: []program.NodeID{4}},
			{Kind: program.NodeExpr, Expr: 4}, // s[label]
		},
		Root: 0,
		Exprs: []program.Expr{
			{Op: program.OpSignalGet, Value: "s", Type: program.TypeAny},                   // 0
			{Op: program.OpLen, Operands: []program.ExprID{0}, Type: program.TypeInt},      // 1
			{Op: program.OpSignalGet, Value: "label", Type: program.TypeString},            // 2
			{Op: program.OpSignalGet, Value: "s", Type: program.TypeAny},                   // 3
			{Op: program.OpIndex, Operands: []program.ExprID{3, 2}, Type: program.TypeAny}, // 4: s[label], not a literal path
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},                      // 5
			{Op: program.OpLitString, Value: "a", Type: program.TypeString},                // 6
		},
		Signals: []program.SignalDef{
			{Name: "s", Type: program.TypeAny, Init: 5},
			{Name: "label", Type: program.TypeString, Init: 6},
		},
	}
}

func TestWholeValueReadSeesEveryField(t *testing.T) {
	island := NewIsland(wholeProgram(), `{}`)
	s := signal.New(obj("a", StringVal("1")))
	label := signal.New(StringVal("a"))
	island.vm.SetSignal("s", s)
	island.vm.SetSignal("label", label)
	island.Reconcile()
	if island.reuse == nil {
		t.Fatal("no plan")
	}
	for i, v := range []Value{
		obj("a", StringVal("1"), "b", StringVal("2")), // len changes
		obj("a", StringVal("9"), "b", StringVal("2")), // only a changes: s[label] must follow
		obj("a", StringVal("9"), "b", StringVal("3")), // only b changes: nothing visible, still correct
	} {
		s.Set(v)
		island.Reconcile()
		mustMatchFullEval(t, island, fmt.Sprintf("whole step %d", i))
	}
	label.Set(StringVal("b"))
	island.Reconcile()
	mustMatchFullEval(t, island, "label changed")
	if got := texts(island.prev); got != "2|3" {
		t.Fatalf("text %q, want 2|3", got)
	}
}

// loopProgram is
//
//	<section>                               node 0, singleton
//	  <h1>{title}</h1>                      node 1: reads the prop "title"
//	  <ul>                                  node 3, singleton: holds the loop
//	    <ForEach s.rows as row>             node 4
//	      <li>{row.name}</li>               node 5: reads the loop variable
//	    </ForEach>
//	  </ul>
//	  <footer>{s.footer}</footer>           node 8
//	</section>
func loopProgram() *program.Program {
	return &program.Program{
		Name: "Loop",
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "section", Children: []program.NodeID{1, 3, 8}},
			{Kind: program.NodeElement, Tag: "h1", Children: []program.NodeID{2}},
			{Kind: program.NodeExpr, Expr: 0}, // title prop
			{Kind: program.NodeElement, Tag: "ul", Children: []program.NodeID{4}},
			{Kind: program.NodeForEach, Expr: 3, Children: []program.NodeID{5}, Attrs: []program.Attr{{Kind: program.AttrStatic, Name: "as", Value: "row"}}},
			{Kind: program.NodeElement, Tag: "li", Children: []program.NodeID{6}},
			{Kind: program.NodeExpr, Expr: 6},
			{Kind: program.NodeExpr, Expr: 9}, // unused
			{Kind: program.NodeElement, Tag: "footer", Children: []program.NodeID{7}},
		},
		Root: 0,
		Exprs: []program.Expr{
			{Op: program.OpPropGet, Value: "title", Type: program.TypeString},              // 0
			{Op: program.OpSignalGet, Value: "s", Type: program.TypeAny},                   // 1
			{Op: program.OpLitString, Value: "rows", Type: program.TypeString},             // 2
			{Op: program.OpIndex, Operands: []program.ExprID{1, 2}, Type: program.TypeAny}, // 3: s.rows
			{Op: program.OpPropGet, Value: "row", Type: program.TypeAny},                   // 4
			{Op: program.OpLitString, Value: "name", Type: program.TypeString},             // 5
			{Op: program.OpIndex, Operands: []program.ExprID{4, 5}, Type: program.TypeAny}, // 6: row.name
			{Op: program.OpLitString, Value: "footer", Type: program.TypeString},           // 7
			{Op: program.OpIndex, Operands: []program.ExprID{1, 7}, Type: program.TypeAny}, // 8: s.footer
			{Op: program.OpLitString, Value: "", Type: program.TypeString},                 // 9
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},                      // 10
		},
		Signals: []program.SignalDef{{Name: "s", Type: program.TypeAny, Init: 10}},
	}
}

func rowsValue(names ...string) Value {
	rows := make([]Value, len(names))
	for i, n := range names {
		rows[i] = obj("name", StringVal(n))
	}
	return ArrayVal(rows)
}

func loopState(footer string, names ...string) Value {
	return obj("rows", rowsValue(names...), "footer", StringVal(footer))
}

func fixLoopProgram(prog *program.Program) {
	// Node 8 (footer) must read expr 8 through node 7's NodeExpr.
	prog.Nodes[7] = program.Node{Kind: program.NodeExpr, Expr: 8}
}

func newLoopIsland(t *testing.T) (*Island, *signal.Signal[Value]) {
	t.Helper()
	prog := loopProgram()
	fixLoopProgram(prog)
	island := NewIsland(prog, `{"title":"T"}`)
	sig := signal.New(loopState("f", "a", "b"))
	island.vm.SetSignal("s", sig)
	island.Reconcile()
	if island.reuse == nil {
		t.Fatal("no plan")
	}
	return island, sig
}

func TestLoopVariablesDoNotDirtyTheElementAroundTheLoop(t *testing.T) {
	island, sig := newLoopIsland(t)
	// The footer changes; the heading and the list do not. Before loop
	// variables were internal to their forEach, the <ul> read the absent prop
	// "row" and was dirty on every walk.
	sig.Set(loopState("g", "a", "b"))
	island.Reconcile()
	mustMatchFullEval(t, island, "footer changed")
	if hits := reuseHits(island); hits != 2 {
		t.Errorf("reused subtrees = %d, want 2 (the heading and the list)", hits)
	}
	// A row changes: the list must follow.
	sig.Set(loopState("g", "a", "B"))
	island.Reconcile()
	mustMatchFullEval(t, island, "row changed")
	// A row is added and removed.
	sig.Set(loopState("g", "a", "B", "c"))
	island.Reconcile()
	mustMatchFullEval(t, island, "row added")
	sig.Set(loopState("g"))
	island.Reconcile()
	mustMatchFullEval(t, island, "rows emptied")
}

func TestLoopVariableNameDoesNotShadowAnOuterProp(t *testing.T) {
	// The heading reads the prop "title". The loop binds "title" for its own
	// body only, so the heading must still follow the prop.
	prog := loopProgram()
	fixLoopProgram(prog)
	prog.Nodes[4].Attrs = []program.Attr{{Kind: program.AttrStatic, Name: "as", Value: "title"}}
	prog.Exprs[4] = program.Expr{Op: program.OpPropGet, Value: "title", Type: program.TypeAny}
	island := NewIsland(prog, `{"title":"T"}`)
	sig := signal.New(loopState("f", "a"))
	island.vm.SetSignal("s", sig)
	island.Reconcile()
	if island.reuse == nil {
		t.Fatal("no plan")
	}
	island.vm.props["title"] = StringVal("T2")
	island.Reconcile()
	mustMatchFullEval(t, island, "outer prop changed")
	if island.prev.Nodes[2].Text != "T2" {
		t.Fatalf("heading %q, want T2", island.prev.Nodes[2].Text)
	}
}

func TestPathsMergeIntoAncestorsWhenTheMaskIsFull(t *testing.T) {
	const fieldCount = 80
	prog := &program.Program{
		Name:    "Wide",
		Nodes:   []program.Node{{Kind: program.NodeElement, Tag: "div"}},
		Root:    0,
		Signals: []program.SignalDef{{Name: "s", Type: program.TypeAny, Init: 1}},
		Exprs: []program.Expr{
			{Op: program.OpSignalGet, Value: "s", Type: program.TypeAny}, // 0
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},    // 1
		},
	}
	// 80 elements, each reading s.group.fN: 80 distinct paths, 1 distinct
	// parent. Depth 2 would need 80 bits; depth 1 needs one.
	prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpLitString, Value: "group", Type: program.TypeString}) // 2
	prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpIndex, Operands: []program.ExprID{0, 2}, Type: program.TypeAny})
	groupExpr := program.ExprID(3)
	for i := 0; i < fieldCount; i++ {
		prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpLitString, Value: fmt.Sprintf("f%d", i), Type: program.TypeString})
		key := program.ExprID(len(prog.Exprs) - 1)
		prog.Exprs = append(prog.Exprs, program.Expr{Op: program.OpIndex, Operands: []program.ExprID{groupExpr, key}, Type: program.TypeAny})
		read := program.ExprID(len(prog.Exprs) - 1)
		prog.Nodes = append(prog.Nodes,
			program.Node{Kind: program.NodeElement, Tag: "p", Children: []program.NodeID{program.NodeID(len(prog.Nodes) + 1)}},
			program.Node{Kind: program.NodeExpr, Expr: read})
		prog.Nodes[0].Children = append(prog.Nodes[0].Children, program.NodeID(len(prog.Nodes)-2))
	}
	plan := newReusePlan(prog)
	if plan == nil {
		t.Fatal("80 paths under one parent should merge into a plan, not disable reuse")
	}
	if len(plan.watch) > maxWatchedInputs {
		t.Fatalf("plan watches %d inputs", len(plan.watch))
	}
	group := map[string]Value{}
	for i := 0; i < fieldCount; i++ {
		group[fmt.Sprintf("f%d", i)] = IntVal(i)
	}
	island := NewIsland(prog, `{}`)
	sig := signal.New(obj("group", ObjectVal(group)))
	island.vm.SetSignal("s", sig)
	island.Reconcile()
	g2 := map[string]Value{}
	for k, v := range group {
		g2[k] = v
	}
	g2["f40"] = StringVal("changed")
	sig.Set(obj("group", ObjectVal(g2)))
	island.Reconcile()
	mustMatchFullEval(t, island, "merged path")
	if island.prev.Nodes[2*40+2].Text != "changed" {
		t.Fatalf("f40 text %q", island.prev.Nodes[2*40+2].Text)
	}
}

func TestValueDigest(t *testing.T) {
	a := obj("x", IntVal(1), "y", ArrayVal([]Value{StringVal("p"), FloatVal(2)}))
	b := obj("y", ArrayVal([]Value{StringVal("p"), FloatVal(2)}), "x", IntVal(1))
	ha, oka := hashValue(a)
	hb, okb := hashValue(b)
	if !oka || !okb || ha != hb {
		t.Fatal("equal maps must digest equal whatever their insertion order")
	}
	differ := []Value{
		obj("x", IntVal(2), "y", ArrayVal([]Value{StringVal("p"), FloatVal(2)})),
		obj("x", IntVal(1), "y", ArrayVal([]Value{StringVal("q"), FloatVal(2)})),
		obj("x", IntVal(1), "y", ArrayVal([]Value{StringVal("p"), FloatVal(3)})),
		obj("x", IntVal(1), "y", ArrayVal([]Value{StringVal("p")})),
		obj("x", IntVal(1), "z", ArrayVal([]Value{StringVal("p"), FloatVal(2)})),
		obj("x", FloatVal(1), "y", ArrayVal([]Value{StringVal("p"), FloatVal(2)})),
		obj("x", IntVal(1)),
		ArrayVal([]Value{}), ObjectVal(map[string]Value{}),
		StringVal(""), IntVal(0), BoolVal(false),
	}
	seen := map[valueHash]int{ha: -1}
	for i, v := range differ {
		h, ok := hashValue(v)
		if !ok {
			t.Fatalf("value %d is not digestible", i)
		}
		if prior, dup := seen[h]; dup {
			t.Errorf("value %d collides with %d", i, prior)
		}
		seen[h] = i
	}
	// A value nested past the evaluator's limit cannot be digested, and the
	// caller treats that as a change.
	deep := IntVal(1)
	for i := 0; i < maxValueRecursionDepth+5; i++ {
		deep = ArrayVal([]Value{deep})
	}
	if _, ok := hashValue(deep); ok {
		t.Error("an over-deep value must report that it cannot be digested")
	}
}

func TestSampleOfADeepValueIsAlwaysDirty(t *testing.T) {
	deep := IntVal(1)
	for i := 0; i < maxValueRecursionDepth+5; i++ {
		deep = ArrayVal([]Value{deep})
	}
	s := sampleValue(deep, true)
	if !sampleDiffers(s, s) {
		t.Error("a value that cannot be digested must count as changed")
	}
}

// TestCompositeReuseMatchesFullEvalOnRandomWrites replaces the signal with
// random shapes, edits them in place, and checks every step against a full
// walk. The loop program covers rows, paths, a prop and a whole-value read.
func TestCompositeReuseMatchesFullEvalOnRandomWrites(t *testing.T) {
	for seed := int64(0); seed < 40; seed++ {
		rng := rand.New(rand.NewSource(seed))
		island, sig := newLoopIsland(t)
		current := map[string]Value{}
		randomRows := func() Value {
			n := rng.Intn(5)
			names := make([]string, n)
			for i := range names {
				names[i] = string(rune('a' + rng.Intn(3)))
			}
			return rowsValue(names...)
		}
		current["rows"] = randomRows()
		current["footer"] = StringVal("f")
		for step := 0; step < 25; step++ {
			switch rng.Intn(5) {
			case 0:
				current["rows"] = randomRows()
			case 1:
				current["footer"] = StringVal(string(rune('f' + rng.Intn(3))))
			case 2:
				current["noise"] = IntVal(rng.Intn(3))
			case 3:
				// In-place edit of a row, no Set.
				rowsValue := current["rows"]
				if rows := rowsValue.list(); len(rows) > 0 {
					row := rows[rng.Intn(len(rows))]
					row.dict()["name"] = StringVal(string(rune('a' + rng.Intn(3))))
					island.Reconcile()
					mustMatchFullEval(t, island, fmt.Sprintf("seed %d step %d in place", seed, step))
					continue
				}
			case 4:
				if rng.Intn(4) == 0 {
					island.vm.props["title"] = StringVal(string(rune('T' + rng.Intn(3))))
					island.Reconcile()
					mustMatchFullEval(t, island, fmt.Sprintf("seed %d step %d prop", seed, step))
					continue
				}
			}
			next := map[string]Value{}
			for k, v := range current {
				next[k] = v
			}
			sig.Set(ObjectVal(next))
			island.Reconcile()
			mustMatchFullEval(t, island, fmt.Sprintf("seed %d step %d", seed, step))
		}
	}
}
