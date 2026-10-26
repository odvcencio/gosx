package vm

import (
	"math/rand"
	"reflect"
	"strconv"
	"testing"
)

func TestSourceAnchoredKeysReplayMixedEdits(t *testing.T) {
	rng := rand.New(rand.NewSource(743))
	pool := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "\x00", "\x00\x00", "\x00anchor:0"}
	anchor := func(items []reconcilerKeyedItem) []reconcilerKeyedItem {
		out := append([]reconcilerKeyedItem{{Key: "head", Text: "header"}}, items...)
		return append(out, reconcilerKeyedItem{Key: "tail", Text: "footer"})
	}
	for i := 0; i < 300; i++ {
		items := randomPrevItems(rng, pool)
		nextItems := randomNextItems(rng, pool, items)
		if i%8 == 0 {
			nextItems = nil
		}
		before, after := anchor(items), anchor(nextItems)
		ops := ReconcileTrees(mixedKeyFixture(before), mixedKeyFixture(after), nil)
		got, err := applyRootKeyedListPatches(before, after, ops)
		if err != nil || !keyedItemsEqual(got, after) {
			t.Fatalf("case %d: replay=%+v want=%+v error=%v ops=%+v", i, got, after, err, ops)
		}
		if hasPatchKind(ops, PatchReplaceElement) {
			t.Fatalf("case %d replaced retained identities: %+v", i, ops)
		}
		if got, want := countPatchKind(ops, PatchCreateElement), len(keysOnly(after))-intersectionSize(before, after); got != want {
			t.Fatalf("case %d: created %d want %d", i, got, want)
		}
		if got, want := countPatchKind(ops, PatchRemoveElement), len(keysOnly(before))-intersectionSize(before, after); got != want {
			t.Fatalf("case %d: removed %d want %d", i, got, want)
		}
	}
}

func mixedKeyFixture(items []reconcilerKeyedItem) *ResolvedTree {
	tree := &ResolvedTree{Nodes: []ResolvedNode{{Tag: "ul"}}}
	for _, item := range items {
		node := ResolvedNode{Tag: "li", Key: item.Key, Text: item.Text, HasSource: true, Source: 2}
		switch item.Key {
		case "head":
			node.Key = ""
			node.Source = 10
		case "tail":
			node.Key = ""
			node.Source = 11
		}
		tree.Nodes = append(tree.Nodes, node)
		tree.Nodes[0].Children = append(tree.Nodes[0].Children, len(tree.Nodes)-1)
	}
	return tree
}

func TestReconcileKeysAroundSourceAnchors(t *testing.T) {
	for _, tc := range []struct {
		name          string
		before, after []string
	}{
		{"reorder", []string{"head", "a", "b", "tail"}, []string{"head", "b", "a", "tail"}},
		{"grow", []string{"head", "tail"}, []string{"head", "a", "tail"}},
		{"empty", []string{"head", "a", "tail"}, []string{"head", "tail"}},
		{"insert-remove-reorder", []string{"head", "a", "c", "tail"}, []string{"head", "c", "a", "b", "tail"}},
		{"key-domain", []string{"head", "\x00anchor:0", "a", "tail"}, []string{"head", "a", "\x00anchor:0", "tail"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items := func(keys []string) []reconcilerKeyedItem {
				out := make([]reconcilerKeyedItem, len(keys))
				for i, key := range keys {
					out[i] = reconcilerKeyedItem{Key: key, Text: key}
				}
				return out
			}
			before, after := items(tc.before), items(tc.after)
			prev, next := mixedKeyFixture(before), mixedKeyFixture(after)
			ops := ReconcileTrees(prev, next, nil)
			got, err := applyRootKeyedListPatches(before, after, ops)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, after) {
				t.Fatalf("patch replay: got %+v want %+v; ops=%+v", got, after, ops)
			}
			retained := make(map[string]bool)
			for _, item := range before {
				retained[item.Key] = true
			}
			for _, op := range ops {
				index, err := strconv.Atoi(op.Path)
				if op.Kind == PatchSetText && err == nil && index >= 0 && index < len(after) && retained[after[index].Key] {
					t.Fatalf("a retained keyed row must move instead of acquiring another row's text: %+v", ops)
				}
			}
		})
	}
}

func TestMixedKeysRequireStableSourceAnchors(t *testing.T) {
	before := []reconcilerKeyedItem{{Key: "head", Text: "head"}, {Key: "a", Text: "a"}, {Key: "tail", Text: "tail"}}
	for _, change := range []func(*ResolvedTree){func(t *ResolvedTree) { t.Nodes[1].HasSource = false }, func(t *ResolvedTree) { t.Nodes[1].Source++ }, func(t *ResolvedTree) { t.Nodes[1].Tag = "span" }} {
		prev, next := mixedKeyFixture(before), mixedKeyFixture(before)
		change(next)
		if _, _, ok := mixedChildKeys(prev, next, &prev.Nodes[0], &next.Nodes[0]); ok {
			t.Fatal("matched an unproven anchor")
		}
	}
}

var benchmarkMixedOps []PatchOp

func BenchmarkSourceAnchoredRows(b *testing.B) {
	for _, count := range []int{100, 2000} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			rows := []reconcilerKeyedItem{{Key: "head", Text: "header"}}
			for i := 0; i < count; i++ {
				key := strconv.Itoa(i)
				rows = append(rows, reconcilerKeyedItem{Key: key, Text: key})
			}
			rows = append(rows, reconcilerKeyedItem{Key: "tail", Text: "footer"})
			after := append([]reconcilerKeyedItem(nil), rows...)
			after[count/2], after[count/2+1] = after[count/2+1], after[count/2]
			prev, next := mixedKeyFixture(rows), mixedKeyFixture(after)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkMixedOps = ReconcileTrees(prev, next, nil)
			}
		})
	}
}
