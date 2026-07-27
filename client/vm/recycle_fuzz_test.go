// FuzzIslandRecycleMatchesFreshTree pins two properties the reconcile path
// must never break. It drives three islands through the same handler sequence:
//
//   - recycling, with tree double buffering and the diff skip both on;
//   - fresh, with recycleOff set, which allocates a tree per reconcile;
//   - noSkip, with skipOff set, which diffs the copied subtrees.
//
// All three must produce the same tree and the same patch ops at every step.
//
// Both properties are worth fuzzing because both fail silently. A stale field
// left over in a recycled node, a subtree copied out of the array the walk is
// writing into, or a shrunk tree whose tail still holds last generation's
// text, all produce a tree that still renders and is wrong in one node. A
// wrong diff skip is worse: it emits NOTHING, the browser keeps the previous
// generation, and a test that lists the expected ops still passes because the
// ops it lists are the ops that were emitted.
//
// The generator is the one FuzzIslandReuseMatchesFullEval uses, so both
// targets explore the same tree shapes. Shapes are what matters here: a
// conditional that collapses shrinks the tree, and a forEach that grows it
// forces the array to grow, which are the two cases where a recycled buffer
// can differ from a fresh one.
//
// The seed corpus runs as a normal test under `go test`, so the shapes below
// are checked on every build even without -fuzz.

package vm

import (
	"math/rand"
	"reflect"
	"testing"
	"unsafe"

	"m31labs.dev/gosx/island/program"
)

// counterLikeProgram is a three-node island whose "bump" handler changes the
// text of one node. It shares the expression table the reuse fuzz target
// uses, so expression IDs 12 and 13 are the two signal writers.
func counterLikeProgram() *program.Program {
	return &program.Program{
		Name: "recycle-fixture",
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1, 2}},
			{Kind: program.NodeExpr, Expr: 0},
			{Kind: program.NodeElement, Tag: "span", Attrs: []program.Attr{
				{Kind: program.AttrStatic, Name: "class", Value: "c"},
			}},
		},
		Root:  0,
		Exprs: fuzzReuseExprs(),
		Signals: []program.SignalDef{
			{Name: "count", Type: program.TypeInt, Init: program.ExprID(14)},
			{Name: "label", Type: program.TypeString, Init: program.ExprID(4)},
		},
		Handlers: []program.Handler{
			{Name: "bump", Body: []program.ExprID{12}},
		},
	}
}

// recycleFuzzMaxSteps bounds the dispatch sequence per execution. Three steps
// are the minimum that exercises recycling at all: the first reconcile has no
// spare, the second retires one, and the third writes into it.
const recycleFuzzMaxSteps = 8

func FuzzIslandRecycleMatchesFreshTree(f *testing.F) {
	f.Add(int64(1), 6, 4)
	f.Add(int64(2), 12, 6)
	f.Add(int64(3), 1, 3)
	f.Add(int64(4), 24, 8)
	f.Add(int64(5), 3, 5)
	f.Add(int64(6), 18, 7)
	f.Add(int64(7), 9, 3)

	f.Fuzz(func(t *testing.T, seed int64, rawNodeCount int, rawSteps int) {
		rng := rand.New(rand.NewSource(seed ^ int64(rawNodeCount)))
		prog := fuzzBuildIslandProgram(rng, rawNodeCount)

		const props = `{"name":"n","items":["a","b","c"]}`
		recycling := NewIsland(prog, props)
		fresh := NewIsland(prog, props)
		noSkip := NewIsland(prog, props)
		if recycling == nil || fresh == nil || noSkip == nil {
			t.Fatal("NewIsland returned nil")
		}
		fresh.recycleOff = true
		noSkip.skipOff = true

		steps := boundedFuzzCount(rawSteps, recycleFuzzMaxSteps) + 3
		for step := 0; step < steps; step++ {
			handler := fuzzReuseHandlers[rng.Intn(len(fuzzReuseHandlers))]
			gotOps := recycling.Dispatch(handler, "{}")
			wantOps := fresh.Dispatch(handler, "{}")
			noSkipOps := noSkip.Dispatch(handler, "{}")

			if !treesEqual(recycling.prev, fresh.prev) {
				t.Fatalf("step %d: the recycled tree differs from a freshly allocated one\n"+
					" recycled: %s\n    fresh: %s", step,
					describeFuzzTree(recycling.prev), describeFuzzTree(fresh.prev))
			}
			if !reflect.DeepEqual(gotOps, wantOps) {
				t.Fatalf("step %d: patch ops differ\n recycled: %#v\n    fresh: %#v",
					step, gotOps, wantOps)
			}
			// The third arm is the guard a wrong diff skip needs. A skip
			// that goes stale emits NO op, so comparing the emitted ops
			// against a hand-written list can never see it. Only a run of
			// the same handler sequence with the skip switched off can.
			if !treesEqual(recycling.prev, noSkip.prev) {
				t.Fatalf("step %d: the tree differs with the diff skip off\n"+
					" skip on: %s\nskip off: %s", step,
					describeFuzzTree(recycling.prev), describeFuzzTree(noSkip.prev))
			}
			if !reflect.DeepEqual(gotOps, noSkipOps) {
				t.Fatalf("step %d: patch ops differ with the diff skip off\n"+
					" skip on: %#v\nskip off: %#v", step, gotOps, noSkipOps)
			}
		}

		// The buffers must stay distinct. Writing the walk into the tree
		// the reuse context copies out of would read and overwrite one
		// array, which is the one aliasing bug this design can have.
		if recycling.spare != nil && recycling.spare == recycling.prev {
			t.Fatal("the spare tree aliases the current tree")
		}
	})
}

// TestRecycleKeepsCurrentTreeReadableForOneReconcile pins the lifetime
// CurrentTree documents. A caller may read the tree it received across the
// next reconcile; the second reconcile after that is free to overwrite it.
func TestRecycleKeepsCurrentTreeReadableForOneReconcile(t *testing.T) {
	island := NewIsland(counterLikeProgram(), `{}`)
	island.Dispatch("bump", "{}")
	island.Dispatch("bump", "{}")

	held := island.CurrentTree()
	before := describeFuzzTree(held)

	island.Dispatch("bump", "{}")
	if after := describeFuzzTree(held); after != before {
		t.Fatalf("the held tree changed after one reconcile:\n before=%s\n  after=%s", before, after)
	}
}

// TestRecycleReusesTheRetiredBuffer pins that double buffering actually
// happens. Without it the reconcile path would still be correct and would
// still allocate, and every benchmark gain in this change would be gone with
// nothing failing.
func TestRecycleReusesTheRetiredBuffer(t *testing.T) {
	island := NewIsland(counterLikeProgram(), `{}`)
	island.Dispatch("bump", "{}")
	island.Dispatch("bump", "{}")

	retired := island.spare
	if retired == nil {
		t.Fatal("no tree was retired after two reconciles")
	}
	island.Dispatch("bump", "{}")
	if island.prev != retired {
		t.Fatalf("the walk allocated a new tree instead of reusing the retired one")
	}
	if island.spare == island.prev {
		t.Fatal("the spare aliases the current tree")
	}
}

// TestReleaseTailDropsTheRetiredGeneration pins that a tree which shrank stops
// holding the strings and slices of the generation before it. Without this the
// recycled array would pin dead text for as long as the island lives, and no
// output test would ever notice.
func TestReleaseTailDropsTheRetiredGeneration(t *testing.T) {
	tree := &ResolvedTree{}
	tree.reset(3)
	tree.Nodes = append(tree.Nodes,
		ResolvedNode{Tag: "a", Text: "one", Children: []int{1}},
		ResolvedNode{Tag: "b", Text: "two", Key: "k", Attrs: []ResolvedAttr{{Name: "class"}}},
		ResolvedNode{Tag: "c", Text: "three"},
	)
	tree.releaseTail()

	// The next generation is one node shorter, so two entries retire.
	tree.reset(3)
	tree.Nodes = append(tree.Nodes, ResolvedNode{Tag: "a", Text: "fresh"})
	tree.releaseTail()

	tail := tree.Nodes[len(tree.Nodes):cap(tree.Nodes)]
	if len(tail) != 2 {
		t.Fatalf("tail length = %d, want 2; the array was reallocated and the test proves nothing", len(tail))
	}
	for i := range tail {
		node := &tail[i]
		if node.Tag != "" || node.Text != "" || node.Key != "" {
			t.Errorf("tail node %d still holds strings: %+v", i, *node)
		}
		if node.Attrs != nil || node.DOMAttrs != nil || node.Events != nil || node.Children != nil {
			t.Errorf("tail node %d still holds slices: %+v", i, *node)
		}
	}
}

// TestResetKeepsTheArrayWhenLargeEnough pins the other half of the contract:
// reset must not allocate when the array already fits, or double buffering
// buys nothing.
func TestResetKeepsTheArrayWhenLargeEnough(t *testing.T) {
	tree := &ResolvedTree{}
	tree.reset(8)
	tree.Nodes = append(tree.Nodes, ResolvedNode{Tag: "a"})
	first := &tree.Nodes[:1][0]

	tree.reset(8)
	tree.Nodes = append(tree.Nodes, ResolvedNode{Tag: "b"})
	if &tree.Nodes[:1][0] != first {
		t.Fatal("reset reallocated an array that was already large enough")
	}

	tree.reset(64)
	if cap(tree.Nodes) < 64 {
		t.Fatalf("reset did not grow for a bigger program: cap = %d, want at least 64", cap(tree.Nodes))
	}
}

// TestRecycleOffAllocatesEveryWalk pins the reference path the differential
// fuzz target compares against. If recycleOff stopped disabling the spare,
// the fuzz target would compare an island with itself and prove nothing.
func TestRecycleOffAllocatesEveryWalk(t *testing.T) {
	island := NewIsland(counterLikeProgram(), `{}`)
	island.recycleOff = true
	island.Dispatch("bump", "{}")
	island.Dispatch("bump", "{}")

	retired := island.spare
	island.Dispatch("bump", "{}")
	if island.prev == retired {
		t.Fatal("recycleOff still reused the retired tree")
	}
}

// TestIslandStaysWithinOneSizeClass pins the cost double buffering adds to an
// island that never receives an event, which is the server-side render.
//
// The island grew from 80 to 96 bytes: one word for the retired tree, one for
// the path builder, and two flags that share a word of padding. Both pointers
// stay nil until the first Reconcile, so a render-once island pays the 16
// bytes and nothing more. A field added carelessly here would push the struct
// into the next size class and charge every server render for it.
func TestIslandStaysWithinOneSizeClass(t *testing.T) {
	const want = 96
	if got := unsafe.Sizeof(Island{}); got != want {
		t.Fatalf("sizeof(Island) = %d, want %d; a new field crossed a size class", got, want)
	}

	island := NewIsland(counterLikeProgram(), `{}`)
	if island.spare != nil || island.diff != nil {
		t.Fatal("a render-once island allocated reconcile scratch it never uses")
	}
}
