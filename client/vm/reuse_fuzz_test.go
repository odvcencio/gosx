// FuzzIslandReuseMatchesFullEval pins the one property subtree reuse must
// never break: the tree a reconcile produces with reuse switched on has to
// equal the tree a full walk produces.
//
// That property is worth fuzzing because the failure mode is silent. A read
// set that misses one input, a span that points at the wrong range, or an
// index that is not shifted, all produce a tree that still renders. It is
// just stale, or subtly mis-linked, and no assertion in a hand-written test
// happens to look at that node.
//
// The generator emits only expressions the reuse plan calls pure, because a
// full re-walk is the oracle here and an impure expression would move the
// oracle. Lowered island node content is drawn from that same set, so the
// restriction costs no coverage of the code under test. Handlers do the
// writing: each one sets a signal, which is what makes the next reconcile
// interesting.
//
// The seed corpus runs as a normal test under `go test`, so the shapes below
// are checked on every build even without -fuzz.

package vm

import (
	"math/rand"
	"testing"

	"m31labs.dev/gosx/island/program"
)

// fuzzReuseMaxNodes bounds the generated node table. Reuse decisions are per
// node, so a few dozen nodes already cover deep nesting, sibling shifts and
// loop bodies. A bigger table only makes each execution slower.
const fuzzReuseMaxNodes = 24

// fuzzReuseMaxSteps bounds the dispatch sequence per execution.
const fuzzReuseMaxSteps = 6

// fuzzReuseExprs is the fixed expression table every generated program
// shares. Keeping it fixed means the fuzzer explores tree SHAPES, which is
// where the span and index bookkeeping lives, instead of re-exploring the
// expression evaluator that FuzzVMEvalNeverPanics already covers.
//
// Index map:
//
//	0  count signal          6  count + 1
//	1  label signal          7  label + name prop
//	2  name prop             8  row prop (bound by a forEach)
//	3  items prop            9  row + label
//	4  "x" literal          10  count > 1        (conditional test)
//	5  1 literal            11  label + "x"
func fuzzReuseExprs() []program.Expr {
	return []program.Expr{
		{Op: program.OpSignalGet, Value: "count", Type: program.TypeInt},                                    // 0
		{Op: program.OpSignalGet, Value: "label", Type: program.TypeString},                                 // 1
		{Op: program.OpPropGet, Value: "name", Type: program.TypeString},                                    // 2
		{Op: program.OpPropGet, Value: "items", Type: program.TypeAny},                                      // 3
		{Op: program.OpLitString, Value: "x", Type: program.TypeString},                                     // 4
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},                                           // 5
		{Op: program.OpAdd, Operands: []program.ExprID{0, 5}, Type: program.TypeInt},                        // 6
		{Op: program.OpConcat, Operands: []program.ExprID{1, 2}, Type: program.TypeString},                  // 7
		{Op: program.OpPropGet, Value: "row", Type: program.TypeString},                                     // 8
		{Op: program.OpConcat, Operands: []program.ExprID{8, 1}, Type: program.TypeString},                  // 9
		{Op: program.OpGt, Operands: []program.ExprID{0, 5}, Type: program.TypeBool},                        // 10
		{Op: program.OpConcat, Operands: []program.ExprID{1, 4}, Type: program.TypeString},                  // 11
		{Op: program.OpSignalSet, Operands: []program.ExprID{6}, Value: "count", Type: program.TypeInt},     // 12
		{Op: program.OpSignalSet, Operands: []program.ExprID{11}, Value: "label", Type: program.TypeString}, // 13
		{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},                                           // 14
	}
}

// fuzzReuseContentExprs are the expression IDs a text-producing node may use.
var fuzzReuseContentExprs = []program.ExprID{0, 1, 2, 4, 6, 7, 8, 9, 11}

// fuzzReuseHandlers name the two writers the dispatch loop picks between.
var fuzzReuseHandlers = []string{"bump", "relabel"}

func FuzzIslandReuseMatchesFullEval(f *testing.F) {
	f.Add(int64(1), 6, 3)
	f.Add(int64(2), 12, 5)
	f.Add(int64(3), 1, 1)
	f.Add(int64(4), 24, 6)
	f.Add(int64(5), 3, 0)
	f.Add(int64(6), 18, 4)
	f.Add(int64(7), 9, 2)

	f.Fuzz(func(t *testing.T, seed int64, rawNodeCount int, rawSteps int) {
		rng := rand.New(rand.NewSource(seed ^ int64(rawNodeCount)))
		prog := fuzzBuildIslandProgram(rng, rawNodeCount)

		island := NewIsland(prog, `{"name":"n","items":["a","b","c"]}`)
		if island == nil {
			t.Fatal("NewIsland returned nil")
		}

		steps := boundedFuzzCount(rawSteps, fuzzReuseMaxSteps)
		for step := 0; step <= steps; step++ {
			island.Dispatch(fuzzReuseHandlers[rng.Intn(len(fuzzReuseHandlers))], "{}")
			want := fullEvalTree(island)
			if !treesEqual(island.prev, want) {
				t.Fatalf("step %d: the reused tree differs from a full evaluation\n"+
					" reused: %s\n   full: %s", step,
					describeFuzzTree(island.prev), describeFuzzTree(want))
			}
		}
	})
}

// boundedFuzzCount folds an arbitrary int into [0, limit].
func boundedFuzzCount(raw, limit int) int {
	if raw < 0 {
		raw = -raw
	}
	if raw < 0 { // math.MinInt negates to itself
		return 0
	}
	return raw % (limit + 1)
}

// fuzzBuildIslandProgram builds a random node tree over the fixed expression
// table. Children always reference strictly higher node IDs, so the graph is
// acyclic by construction and every node is reached at most once.
func fuzzBuildIslandProgram(rng *rand.Rand, rawNodeCount int) *program.Program {
	count := boundedFuzzCount(rawNodeCount, fuzzReuseMaxNodes) + 1
	nodes := make([]program.Node, count)

	// Walk backwards so a node can only adopt children built after it.
	for id := count - 1; id >= 0; id-- {
		nodes[id] = fuzzRandomIslandNode(rng, id, count)
	}
	// The root must be able to hold children, otherwise most generated
	// trees collapse to a single node and the shape coverage is wasted.
	if nodes[0].Kind != program.NodeElement {
		nodes[0] = program.Node{Kind: program.NodeElement, Tag: "div", Children: nodes[0].Children}
	}

	return &program.Program{
		Name:  "fuzz-island",
		Nodes: nodes,
		Root:  0,
		Exprs: fuzzReuseExprs(),
		Signals: []program.SignalDef{
			{Name: "count", Type: program.TypeInt, Init: program.ExprID(14)},
			{Name: "label", Type: program.TypeString, Init: program.ExprID(4)},
		},
		Handlers: []program.Handler{
			{Name: "bump", Body: []program.ExprID{12}},
			{Name: "relabel", Body: []program.ExprID{13}},
		},
	}
}

func fuzzRandomIslandNode(rng *rand.Rand, id, count int) program.Node {
	children := fuzzRandomChildren(rng, id, count)
	switch rng.Intn(6) {
	case 0:
		return program.Node{Kind: program.NodeText, Text: "t"}
	case 1:
		return program.Node{
			Kind: program.NodeExpr,
			Expr: fuzzReuseContentExprs[rng.Intn(len(fuzzReuseContentExprs))],
		}
	case 2:
		return program.Node{Kind: program.NodeFragment, Children: children}
	case 3:
		return program.Node{
			Kind:     program.NodeConditional,
			Expr:     10,
			Attrs:    []program.Attr{{Kind: program.AttrExpr, Name: "fallback", Expr: 4}},
			Children: children,
		}
	case 4:
		return program.Node{
			Kind:     program.NodeForEach,
			Expr:     3,
			Attrs:    []program.Attr{{Kind: program.AttrStatic, Name: "as", Value: "row"}},
			Children: children,
		}
	default:
		node := program.Node{Kind: program.NodeElement, Tag: "div", Children: children}
		switch rng.Intn(3) {
		case 0:
			node.Attrs = []program.Attr{{Kind: program.AttrStatic, Name: "class", Value: "c"}}
		case 1:
			node.Attrs = []program.Attr{
				{Kind: program.AttrExpr, Name: "title", Expr: fuzzReuseContentExprs[rng.Intn(len(fuzzReuseContentExprs))]},
				{Kind: program.AttrEvent, Name: "click", Event: "bump"},
			}
		}
		return node
	}
}

// fuzzRandomChildren picks up to three children from the IDs after id.
func fuzzRandomChildren(rng *rand.Rand, id, count int) []program.NodeID {
	available := count - id - 1
	if available <= 0 {
		return nil
	}
	n := rng.Intn(4)
	if n > available {
		n = available
	}
	if n == 0 {
		return nil
	}
	children := make([]program.NodeID, 0, n)
	next := id + 1
	for i := 0; i < n && next < count; i++ {
		children = append(children, program.NodeID(next))
		next += 1 + rng.Intn(2)
	}
	return children
}

// describeFuzzTree renders a tree compactly so a failure message shows the
// shape that broke instead of a page of struct dumps.
func describeFuzzTree(tree *ResolvedTree) string {
	if tree == nil {
		return "<nil>"
	}
	out := make([]byte, 0, len(tree.Nodes)*16)
	for i := range tree.Nodes {
		node := &tree.Nodes[i]
		out = append(out, '[')
		out = append(out, node.Tag...)
		out = append(out, '"')
		out = append(out, node.Text...)
		out = append(out, '"')
		for _, child := range node.Children {
			out = append(out, ' ')
			out = append(out, byte('0'+child%10))
		}
		out = append(out, ']')
	}
	return string(out)
}
