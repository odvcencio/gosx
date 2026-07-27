package vm

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// TestPatchPathJoinsLikeChildPath pins the lazy builder against the reference
// join. childPath is the definition of a patch path; patchPath only avoids
// building one per node visited, so the two must agree at every depth.
func TestPatchPathJoinsLikeChildPath(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	var path patchPath

	for trial := 0; trial < 200; trial++ {
		path.reset()
		want := ""
		depth := rng.Intn(7)
		for level := 0; level < depth; level++ {
			idx := rng.Intn(300)
			path.push(idx)
			want = childPath(want, idx)
			if got := path.String(); got != want {
				t.Fatalf("trial %d level %d: path = %q, want %q", trial, level, got, want)
			}
			// Asking twice must not change the answer: String memoizes.
			if got := path.String(); got != want {
				t.Fatalf("trial %d level %d: second call = %q, want %q", trial, level, got, want)
			}
		}
	}
}

// TestPatchPathChildDoesNotDescend pins that child() leaves the builder where
// it was. A leaked level would shift every later path by one segment.
func TestPatchPathChildDoesNotDescend(t *testing.T) {
	var path patchPath
	path.push(2)
	path.push(7)

	if got, want := path.child(4), "2/7/4"; got != want {
		t.Fatalf("child = %q, want %q", got, want)
	}
	if got, want := path.String(), "2/7"; got != want {
		t.Fatalf("path after child = %q, want %q", got, want)
	}
}

// TestPatchPathResetClearsTheStack pins that a reused builder starts a new
// reconcile at the island root and not part-way down the last tree.
func TestPatchPathResetClearsTheStack(t *testing.T) {
	var path patchPath
	path.push(3)
	path.push(1)
	path.reset()

	if got := path.String(); got != "" {
		t.Fatalf("path after reset = %q, want the island root %q", got, "")
	}
}

// TestReconcileBalancesThePathStack pins the bug this refactor can introduce:
// a descent that pushes a level and returns without popping. Every later path
// in the same reconcile would then carry a stale segment. The check runs over
// generated tree shapes, because the leak may live on a branch no hand-written
// case reaches.
func TestReconcileBalancesThePathStack(t *testing.T) {
	rng := rand.New(rand.NewSource(23))
	for trial := 0; trial < 60; trial++ {
		prog := fuzzBuildIslandProgram(rng, rng.Intn(fuzzReuseMaxNodes))
		island := NewIsland(prog, `{"name":"n","items":["a","b","c"]}`)
		for step := 0; step < 4; step++ {
			island.Dispatch(fuzzReuseHandlers[rng.Intn(len(fuzzReuseHandlers))], "{}")
			if depth := pathDepth(island); depth != 0 {
				t.Fatalf("trial %d step %d: the path stack ended %d levels deep, want 0",
					trial, step, depth)
			}
		}
	}
}

// TestReconcilePathsAddressTheRightNode is the end-to-end oracle for the path
// refactor. It diffs two trees of identical shape whose text differs deep in
// the tree, then walks each emitted path back down the child lists and checks
// it lands on the node the op describes.
//
// Shape-identical trees are the case where a path resolves the same way in
// both trees. Inserts and removals shift the live DOM index on purpose, and
// TestReconcileDeepInsertAndRemovePaths covers those.
func TestReconcilePathsAddressTheRightNode(t *testing.T) {
	rng := rand.New(rand.NewSource(37))
	for trial := 0; trial < 50; trial++ {
		prev, next, changed := buildDeepTreePair(rng)
		ops := ReconcileTrees(prev, next, nil)
		if len(ops) != changed {
			t.Fatalf("trial %d: ops = %d, want %d (one per changed text node)", trial, len(ops), changed)
		}
		for _, op := range ops {
			node, ok := nodeAtPatchPath(next, op.Path)
			if !ok {
				t.Fatalf("trial %d: path %q does not resolve in the next tree", trial, op.Path)
			}
			if op.Kind != PatchSetText {
				t.Fatalf("trial %d: unexpected op kind %v", trial, op.Kind)
			}
			if node.Text != op.Text {
				t.Fatalf("trial %d: path %q holds %q, but the op sets %q",
					trial, op.Path, node.Text, op.Text)
			}
		}
	}
}

// TestReconcileDeepInsertAndRemovePaths pins the paths of a create and a
// remove three levels down, where an off-by-one level would be invisible to a
// shallow test.
func TestReconcileDeepInsertAndRemovePaths(t *testing.T) {
	// root > a > b > [c, d]; the next tree drops d and adds e after c.
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "root", Source: 0, HasSource: true, Children: []int{1}},
		{Tag: "a", Source: 1, HasSource: true, Children: []int{2}},
		{Tag: "b", Source: 2, HasSource: true, Children: []int{3, 4}},
		{Tag: "c", Source: 3, HasSource: true},
		{Tag: "d", Source: 4, HasSource: true},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "root", Source: 0, HasSource: true, Children: []int{1}},
		{Tag: "a", Source: 1, HasSource: true, Children: []int{2}},
		{Tag: "b", Source: 2, HasSource: true, Children: []int{3, 4}},
		{Tag: "c", Source: 3, HasSource: true},
		{Tag: "e", Source: 5, HasSource: true},
	}}

	ops := ReconcileTrees(prev, next, nil)
	var got []string
	for _, op := range ops {
		got = append(got, fmt.Sprintf("%v@%s", op.Kind, op.Path))
	}
	joined := strings.Join(got, " ")

	// The removed child sits at DOM index 1 under root/a/b, which is "0/0/1".
	// The created child is inserted under root/a/b, which is "0/0".
	if !strings.Contains(joined, fmt.Sprintf("%v@0/0/1", PatchRemoveElement)) {
		t.Errorf("ops %v: want a remove at 0/0/1", joined)
	}
	if !strings.Contains(joined, fmt.Sprintf("%v@0/0", PatchCreateElement)) {
		t.Errorf("ops %v: want a create under 0/0", joined)
	}
}

// buildDeepTreePair builds two trees of the same shape, four levels deep, and
// changes the text of some leaves in the second. It returns the number of
// changed leaves so the caller can pin the op count.
func buildDeepTreePair(rng *rand.Rand) (prev, next *ResolvedTree, changed int) {
	prev = &ResolvedTree{}
	next = &ResolvedTree{}

	var build func(depth int) int
	build = func(depth int) int {
		idx := len(prev.Nodes)
		prev.Nodes = append(prev.Nodes, ResolvedNode{Tag: "div", Source: idx, HasSource: true})
		next.Nodes = append(next.Nodes, ResolvedNode{Tag: "div", Source: idx, HasSource: true})
		if depth == 0 {
			text := fmt.Sprintf("leaf%d", idx)
			prev.Nodes[idx].Tag = ""
			next.Nodes[idx].Tag = ""
			prev.Nodes[idx].Text = text
			if rng.Intn(2) == 0 {
				next.Nodes[idx].Text = text + "!"
				changed++
			} else {
				next.Nodes[idx].Text = text
			}
			return idx
		}
		count := 1 + rng.Intn(3)
		children := make([]int, 0, count)
		for i := 0; i < count; i++ {
			children = append(children, build(depth-1))
		}
		prev.Nodes[idx].Children = children
		next.Nodes[idx].Children = children
		return idx
	}
	build(3)
	return prev, next, changed
}

// nodeAtPatchPath walks a tree by a patch path and returns the node it names.
func nodeAtPatchPath(tree *ResolvedTree, path string) (*ResolvedNode, bool) {
	node := resolvedNodeAt(tree, 0)
	if node == nil {
		return nil, false
	}
	if path == "" {
		return node, true
	}
	for _, segment := range strings.Split(path, "/") {
		var childIdx int
		if _, err := fmt.Sscanf(segment, "%d", &childIdx); err != nil {
			return nil, false
		}
		if childIdx < 0 || childIdx >= len(node.Children) {
			return nil, false
		}
		node = resolvedNodeAt(tree, node.Children[childIdx])
		if node == nil {
			return nil, false
		}
	}
	return node, true
}

// pathDepth reports how many levels the island's path builder still holds. A
// balanced walk ends at zero.
func pathDepth(island *Island) int {
	if island == nil || island.diff == nil {
		return 0
	}
	return len(island.diff.path.idx)
}
