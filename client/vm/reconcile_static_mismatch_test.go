package vm

import "testing"

// TestStaticNodeIsNotSkippedAgainstADifferentProgramNode pins the diff for a
// static node that arrives at an index where the previous tree held another
// program node. A static node may be skipped only against its own previous
// self: the mask says "this program node never changes", not "whatever sat at
// this index before already looks like it".
//
// Children before: [span#1, button#3]. After a conditional <i>#2 mounts between
// them: [span#1, i#2, button#3]. Source 2 is static. Identities repeat (two
// text nodes share source 9) so the diff falls back to index-by-index pairing,
// which pairs button#3 with i#2 at index 3.
func TestStaticNodeIsNotSkippedAgainstADifferentProgramNode(t *testing.T) {
	node := func(tag string, src int, kids ...int) ResolvedNode {
		return ResolvedNode{Tag: tag, Source: src, HasSource: true, Children: kids}
	}
	text := func(s string, src int) ResolvedNode {
		return ResolvedNode{Text: s, Source: src, HasSource: true}
	}
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		node("div", 0, 1, 2, 3, 4),
		text(" ", 9), node("span", 1), text(" ", 9), node("button", 3),
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		node("div", 0, 1, 2, 3, 4, 5),
		text(" ", 9), node("span", 1), text(" ", 9), node("i", 2), node("button", 3),
	}}
	static := make([]bool, 10)
	static[2], static[3], static[9] = true, true, true

	ops := ReconcileTrees(prev, next, static)

	// Replay on a flat child list: tags only, text nodes as "#".
	dom := []string{"#", "span", "#", "button"}
	for _, op := range ops {
		switch op.Kind {
		case PatchCreateElement:
			if op.Path != "" {
				continue
			}
			i := op.Children[0]
			dom = append(dom[:i], append([]string{op.Tag}, dom[i:]...)...)
		case PatchCreateText:
			if op.Path != "" {
				continue
			}
			i := op.Children[0]
			dom = append(dom[:i], append([]string{"#"}, dom[i:]...)...)
		case PatchReplaceElement:
			if op.Path == "3" {
				dom[3] = op.Tag
			}
		}
	}
	want := []string{"#", "span", "#", "i", "button"}
	if len(dom) != len(want) {
		t.Fatalf("children = %v, want %v; ops = %+v", dom, want, ops)
	}
	for i := range want {
		if dom[i] != want[i] {
			t.Fatalf("children = %v, want %v; ops = %+v", dom, want, ops)
		}
	}
}
