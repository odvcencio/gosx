package vm

import (
	"strconv"
	"strings"
)

// isValueElement reports whether the tag is an element whose "value" attribute
// should be patched with PatchSetValue instead of PatchSetAttr.
func isValueElement(tag string) bool {
	t := strings.ToLower(tag)
	return t == "input" || t == "textarea" || t == "select"
}

// childPath builds a slash-separated path from the island root. It is the
// reference definition of a patch path; the walk uses patchPath, which
// produces the same strings without building one per node visited.
func childPath(parentPath string, childIdx int) string {
	idxStr := strconv.Itoa(childIdx)
	if parentPath == "" {
		return idxStr
	}
	var b strings.Builder
	b.Grow(len(parentPath) + 1 + len(idxStr))
	b.WriteString(parentPath)
	b.WriteByte('/')
	b.WriteString(idxStr)
	return b.String()
}

// patchPath addresses one node in the DOM tree the patch ops describe.
//
// The walk descends through many nodes that emit no patch at all. Joining the
// slash-separated string at every level charged one allocation per node
// visited, which a CPU profile of the island benchmarks put at 40% of every
// allocation the package made. This type carries the child index of each
// level instead, and joins them only when an op is emitted.
//
// One instance serves a whole reconcile. Both its slices keep their arrays
// across calls, so a steady-state reconcile that emits nothing allocates
// nothing here.
type patchPath struct {
	// idx holds the child index of each level below the island root. The
	// root itself is the empty path.
	idx []int
	// buf is the scratch the joined string is assembled in.
	buf []byte
	// built is the last joined value, and valid says it still matches idx.
	// A node that emits several ops therefore joins once.
	built string
	valid bool
}

func (p *patchPath) reset() {
	p.idx = p.idx[:0]
	p.built = ""
	p.valid = false
}

func (p *patchPath) push(childIdx int) {
	p.idx = append(p.idx, childIdx)
	p.valid = false
}

func (p *patchPath) pop() {
	p.idx = p.idx[:len(p.idx)-1]
	p.valid = false
}

// String joins the current levels. It returns "" for the island root, which
// is the path the DOM patch applier reads as "the island element itself".
func (p *patchPath) String() string {
	if p.valid {
		return p.built
	}
	p.buf = p.buf[:0]
	for level, childIdx := range p.idx {
		if level > 0 {
			p.buf = append(p.buf, '/')
		}
		p.buf = strconv.AppendInt(p.buf, int64(childIdx), 10)
	}
	p.built = string(p.buf)
	p.valid = true
	return p.built
}

// child joins the path of the child at childIdx without descending into it.
// Use it where an op names a child the walk does not enter.
func (p *patchPath) child(childIdx int) string {
	p.push(childIdx)
	joined := p.String()
	p.pop()
	return joined
}

// diffWalk carries the four read-only inputs every level of the tree diff
// reads: the two trees, the path builder, the static mask and the span table
// the tree walk just filled.
//
// They used to travel as separate arguments. The static mask alone is three
// machine words, so the recursive walk passed nine words per level and adding
// one more would have spilled the call to the stack. A live island keeps one
// diffWalk across events, so bundling them costs no allocation.
type diffWalk struct {
	path patchPath
	prev *ResolvedTree
	next *ResolvedTree
	// static is Program.StaticMask, indexed by program node.
	static []bool
	// spans is the reuse span table of the walk that produced next, indexed
	// by program node. nil switches the subtree skip off. See skipsSubtree.
	spans []nodeSpan
}

// ReconcileTrees diffs the previous and next resolved trees and returns patch ops.
func ReconcileTrees(prev, next *ResolvedTree, staticMask []bool) []PatchOp {
	var w diffWalk
	return reconcileTreesInto(&w, prev, next, staticMask, nil)
}

// reconcileTreesInto is ReconcileTrees against a caller-owned walk, so a live
// island keeps one across events instead of starting from an empty one.
//
// spans must come from the walk that produced next, against prev. Pass nil
// when the caller cannot promise that, and the diff compares every node.
func reconcileTreesInto(w *diffWalk, prev, next *ResolvedTree, staticMask []bool, spans []nodeSpan) []PatchOp {
	if prev == nil || next == nil || len(prev.Nodes) == 0 || len(next.Nodes) == 0 {
		return nil
	}
	w.path.reset()
	w.prev, w.next, w.static, w.spans = prev, next, staticMask, spans
	var ops []PatchOp
	reconcileNodePair(w, &ops, 0, 0)
	// Drop the borrowed references. A retired tree must not stay reachable
	// through the walk after the island released it.
	w.prev, w.next, w.static, w.spans = nil, nil, nil, nil
	return ops
}

// skipsSubtree reports whether the diff may stop at the pair of nodes at
// prevIdx and nextIdx without emitting anything.
//
// Two facts carry the proof, and both are needed:
//
//   - The tree walk copied the subtree at nextIdx out of the previous tree
//     into the index range it already held, so both trees hold equal nodes at
//     equal indices through the whole subtree.
//   - The pair being diffed is that pair. The record says "next.Nodes[i] came
//     from prev.Nodes[i]", so it licenses skipping only the comparison of
//     those two nodes. A sibling inserted ahead of the subtree pairs the same
//     next node against a DIFFERENT prev node, and that pair carries a real
//     change.
//
// The span table is indexed by program node, so the resolved node's Source
// field is the way in. A node whose Source was rewritten after the span was
// recorded, which mergeAdjacentText does, then reads a span whose start names
// another index and falls through to the full comparison.
func (w *diffWalk) skipsSubtree(prevIdx, nextIdx int) bool {
	if prevIdx != nextIdx || len(w.spans) == 0 || nextIdx < 0 || nextIdx >= len(w.next.Nodes) {
		return false
	}
	node := &w.next.Nodes[nextIdx]
	if !node.HasSource || node.Source < 0 || node.Source >= len(w.spans) {
		return false
	}
	span := w.spans[node.Source]
	return span.verbatim && int(span.start) == nextIdx
}

type keyedChildIndex struct {
	nodeIdx int
}

type keyedNextChild struct {
	elementIdx int
	nodeIdx    int
	key        string
}

type keyedChildrenPlan struct {
	prevByKey    map[string]keyedChildIndex
	nextKeys     map[string]struct{}
	desiredOrder []string
	currentOrder []string
	nextChildren []keyedNextChild
}

func reconcileNodePair(w *diffWalk, ops *[]PatchOp, prevIdx, nextIdx int) {
	if w.skipsSubtree(prevIdx, nextIdx) {
		return
	}
	pair, ok := resolveNodePair(w.prev, w.next, prevIdx, nextIdx)
	if !ok || shouldSkipReconcileNode(pair.next, nextIdx, w.static) {
		return
	}
	if isLeafNodePair(pair.prev, pair.next) {
		reconcileLeafNodePair(w, ops, pair.prev, pair.next, nextIdx)
		return
	}
	if pair.prev.Tag != pair.next.Tag {
		appendReplaceSubtree(w, ops, nextIdx)
		return
	}
	reconcileElementNodePair(w, ops, pair.prev, pair.next)
}

type resolvedNodePair struct {
	prev *ResolvedNode
	next *ResolvedNode
}

func resolveNodePair(prev, next *ResolvedTree, prevIdx, nextIdx int) (resolvedNodePair, bool) {
	pair := resolvedNodePair{
		prev: resolvedNodeAt(prev, prevIdx),
		next: resolvedNodeAt(next, nextIdx),
	}
	return pair, pair.prev != nil && pair.next != nil
}

func resolvedNodeAt(tree *ResolvedTree, idx int) *ResolvedNode {
	if tree == nil || idx < 0 || idx >= len(tree.Nodes) {
		return nil
	}
	return &tree.Nodes[idx]
}

func shouldSkipReconcileNode(node *ResolvedNode, nodeIdx int, staticMask []bool) bool {
	return staticMaskAt(staticMask, staticMaskIndex(node, nodeIdx))
}

func staticMaskIndex(node *ResolvedNode, nodeIdx int) int {
	if node != nil && node.HasSource && node.Source >= 0 {
		return node.Source
	}
	return nodeIdx
}

func staticMaskAt(staticMask []bool, idx int) bool {
	return idx >= 0 && idx < len(staticMask) && staticMask[idx]
}

func isLeafNodePair(prev, next *ResolvedNode) bool {
	return prev == nil || next == nil || prev.Tag == "" || next.Tag == ""
}

func reconcileLeafNodePair(w *diffWalk, ops *[]PatchOp, pn, nn *ResolvedNode, nextIdx int) {
	switch {
	case pn.Tag == "" && nn.Tag == "":
		if pn.Text != nn.Text {
			appendTextPatch(ops, &w.path, nn.Text)
		}
	case pn.Tag != "" && nn.Tag == "":
		appendTextPatch(ops, &w.path, nn.Text)
	default:
		appendReplaceSubtree(w, ops, nextIdx)
	}
}

func reconcileElementNodePair(w *diffWalk, ops *[]PatchOp, pn, nn *ResolvedNode) {
	if pn.Text != nn.Text && (pn.Text != "" || nn.Text != "") {
		appendTextPatch(ops, &w.path, nn.Text)
	}
	reconcileAttrs(ops, pn, nn, &w.path)
	reconcileChildren(w, ops, pn, nn)
}

func reconcileChildren(w *diffWalk, ops *[]PatchOp, prevNode, nextNode *ResolvedNode) {
	if childrenAreFullyKeyed(w.prev, prevNode) && childrenAreFullyKeyed(w.next, nextNode) {
		reconcileKeyedChildren(w, ops, prevNode, nextNode)
		return
	}
	reconcilePositionalChildren(w, ops, prevNode, nextNode)
}

func childrenAreFullyKeyed(tree *ResolvedTree, node *ResolvedNode) bool {
	if node == nil || len(node.Children) == 0 {
		return false
	}
	for _, idx := range node.Children {
		child := resolvedNodeAt(tree, idx)
		if child == nil || child.Key == "" {
			return false
		}
	}
	return true
}

func reconcileKeyedChildren(w *diffWalk, ops *[]PatchOp, pn, nn *ResolvedNode) {
	plan, ok := buildKeyedChildrenPlan(w.prev, w.next, pn, nn)
	if !ok {
		reconcilePositionalChildren(w, ops, pn, nn)
		return
	}

	removeMissingKeyedChildren(ops, w.prev, pn, plan.nextKeys, &w.path)
	plan.currentOrder = appendMissingKeyedChildren(w, ops, plan)
	appendKeyedReorderOp(ops, &w.path, plan.currentOrder, plan.desiredOrder)
	reconcileExistingKeyedChildren(w, ops, plan)
}

func buildKeyedChildrenPlan(prev, next *ResolvedTree, pn, nn *ResolvedNode) (keyedChildrenPlan, bool) {
	prevByKey, prevKeysUnique := buildPrevKeyIndex(prev, pn)
	if !prevKeysUnique {
		return keyedChildrenPlan{}, false
	}
	nextChildren, nextKeys, desiredOrder, nextKeysUnique := collectNextKeyedChildren(next, nn)
	if !nextKeysUnique {
		return keyedChildrenPlan{}, false
	}
	currentOrder, prevKeysComplete := currentKeyOrder(prev, pn, nextKeys)
	if !prevKeysComplete {
		return keyedChildrenPlan{}, false
	}
	return keyedChildrenPlan{
		prevByKey:    prevByKey,
		nextKeys:     nextKeys,
		desiredOrder: desiredOrder,
		currentOrder: currentOrder,
		nextChildren: nextChildren,
	}, true
}

func collectNextKeyedChildren(next *ResolvedTree, node *ResolvedNode) ([]keyedNextChild, map[string]struct{}, []string, bool) {
	children := make([]keyedNextChild, 0, len(node.Children))
	keys := make(map[string]struct{}, len(node.Children))
	order := make([]string, 0, len(node.Children))
	for elemIdx, childIdx := range node.Children {
		child := resolvedNodeAt(next, childIdx)
		if child == nil || child.Key == "" {
			return nil, nil, nil, false
		}
		if _, exists := keys[child.Key]; exists {
			return nil, nil, nil, false
		}
		keys[child.Key] = struct{}{}
		children = append(children, keyedNextChild{
			elementIdx: elemIdx,
			nodeIdx:    childIdx,
			key:        child.Key,
		})
		order = append(order, child.Key)
	}
	return children, keys, order, true
}

func currentKeyOrder(prev *ResolvedTree, node *ResolvedNode, nextKeys map[string]struct{}) ([]string, bool) {
	order := make([]string, 0, len(node.Children))
	for _, childIdx := range node.Children {
		child := resolvedNodeAt(prev, childIdx)
		if child == nil || child.Key == "" {
			return nil, false
		}
		if _, ok := nextKeys[child.Key]; ok {
			order = append(order, child.Key)
		}
	}
	return order, true
}

func buildPrevKeyIndex(prev *ResolvedTree, node *ResolvedNode) (map[string]keyedChildIndex, bool) {
	byKey := make(map[string]keyedChildIndex, len(node.Children))
	for _, childIdx := range node.Children {
		child := resolvedNodeAt(prev, childIdx)
		if child == nil || child.Key == "" {
			continue
		}
		if _, exists := byKey[child.Key]; exists {
			return nil, false
		}
		byKey[child.Key] = keyedChildIndex{nodeIdx: childIdx}
	}
	return byKey, true
}

func removeMissingKeyedChildren(ops *[]PatchOp, prev *ResolvedTree, node *ResolvedNode, nextKeys map[string]struct{}, path *patchPath) {
	for i := len(node.Children) - 1; i >= 0; i-- {
		child := resolvedNodeAt(prev, node.Children[i])
		if child == nil || child.Key == "" {
			continue
		}
		if _, ok := nextKeys[child.Key]; ok {
			continue
		}
		appendRemoveChild(ops, path.child(i))
	}
}

func appendMissingKeyedChildren(w *diffWalk, ops *[]PatchOp, plan keyedChildrenPlan) []string {
	currentOrder := plan.currentOrder
	for _, child := range plan.nextChildren {
		if _, exists := plan.prevByKey[child.key]; exists {
			continue
		}
		appendCreateSubtree(w, ops, child.nodeIdx, child.elementIdx)
		currentOrder = insertKey(currentOrder, child.elementIdx, child.key)
	}
	return currentOrder
}

func appendKeyedReorderOp(ops *[]PatchOp, path *patchPath, currentOrder, desiredOrder []string) {
	if order := reorderIndices(currentOrder, desiredOrder); order != nil {
		*ops = append(*ops, PatchOp{Kind: PatchReorder, Path: path.String(), Children: order})
	}
}

func reconcileExistingKeyedChildren(w *diffWalk, ops *[]PatchOp, plan keyedChildrenPlan) {
	for _, child := range plan.nextChildren {
		prevChild, ok := plan.prevByKey[child.key]
		if !ok {
			continue
		}
		w.path.push(child.elementIdx)
		reconcileNodePair(w, ops, prevChild.nodeIdx, child.nodeIdx)
		w.path.pop()
	}
}

func insertKey(keys []string, index int, value string) []string {
	if index < 0 {
		index = 0
	}
	if index > len(keys) {
		index = len(keys)
	}
	keys = append(keys, "")
	copy(keys[index+1:], keys[index:])
	keys[index] = value
	return keys
}

func reorderIndices(current, desired []string) []int {
	if len(current) != len(desired) {
		return nil
	}
	if stringSlicesEqual(current, desired) {
		return nil
	}

	indexByKey := stringIndexMap(current)
	return reorderedIndices(indexByKey, desired)
}

// stringSlicesEqual reports whether two string slices hold the same values in
// the same order. It checks the lengths itself: the only caller guards them
// today, but a future caller must not be able to turn this into an index panic.
func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func stringIndexMap(values []string) map[string]int {
	indexByKey := make(map[string]int, len(values))
	for i, key := range values {
		indexByKey[key] = i
	}
	return indexByKey
}

func reorderedIndices(indexByKey map[string]int, desired []string) []int {
	order := make([]int, 0, len(desired))
	for _, key := range desired {
		idx, ok := indexByKey[key]
		if !ok {
			return nil
		}
		order = append(order, idx)
	}
	return order
}

// reconcilePositionalChildren diffs unkeyed children. When every child has a
// distinct identity (its Key, else its program Source — both unique among
// siblings) it uses an identity-aware two-pointer walk so an insert or removal
// ANYWHERE in the list (e.g. a conditional toggling between siblings) patches
// the right positions instead of cascading mismatches down the tail. `d` tracks
// the live DOM child index as inserts/removals shift it. When identities repeat
// (unkeyed/duplicate-keyed list rows that fell through from the keyed differ) it
// falls back to a plain index-positional diff.
func reconcilePositionalChildren(w *diffWalk, ops *[]PatchOp, pn, nn *ResolvedNode) {
	prev, next := w.prev, w.next
	pc, nc := pn.Children, nn.Children
	if !childIdentitiesUnique(prev, pc) || !childIdentitiesUnique(next, nc) {
		reconcileIndexPositional(w, ops, pc, nc)
		return
	}

	prevLookup := newChildIdentityLookup(prev, pc)
	nextLookup := newChildIdentityLookup(next, nc)

	i, j, d := 0, 0, 0
	for i < len(pc) && j < len(nc) {
		pid, _ := childIdentity(prev, pc[i])
		nid, _ := childIdentity(next, nc[j])
		switch {
		case pid == nid:
			reconcileChildPair(w, ops, pc[i], nc[j], d)
			i, j, d = i+1, j+1, d+1
		case !nextLookup.has(pid):
			appendRemoveChild(ops, w.path.child(d)) // prev child gone; successor shifts to d
			i++
		case !prevLookup.has(nid):
			appendCreateSubtree(w, ops, nc[j], d) // next child is new
			j, d = j+1, d+1
		default:
			reconcileChildPair(w, ops, pc[i], nc[j], d)
			i, j, d = i+1, j+1, d+1
		}
	}
	for ; i < len(pc); i++ {
		appendRemoveChild(ops, w.path.child(d))
	}
	for ; j < len(nc); j++ {
		appendCreateSubtree(w, ops, nc[j], d)
		d++
	}
}

// reconcileChildPair diffs one matched child pair at DOM child index d.
//
// It tests the reuse skip before it pushes the path level. A skipped subtree
// then costs one array read: no push, no pop, and no invalidation of the
// joined path the parent already built.
func reconcileChildPair(w *diffWalk, ops *[]PatchOp, prevIdx, nextIdx, d int) {
	if w.skipsSubtree(prevIdx, nextIdx) {
		return
	}
	w.path.push(d)
	reconcileNodePair(w, ops, prevIdx, nextIdx)
	w.path.pop()
}

// reconcileIndexPositional is the index-by-index fallback used when identities
// are not unique: reconcile the shared prefix in place, remove the prev tail
// last-to-first (so removals don't shift later indices), then append the next
// tail.
func reconcileIndexPositional(w *diffWalk, ops *[]PatchOp, pc, nc []int) {
	common := len(pc)
	if len(nc) < common {
		common = len(nc)
	}
	for i := 0; i < common; i++ {
		reconcileChildPair(w, ops, pc[i], nc[i], i)
	}
	for i := len(pc) - 1; i >= len(nc); i-- {
		appendRemoveChild(ops, w.path.child(i))
	}
	for i := len(pc); i < len(nc); i++ {
		appendCreateSubtree(w, ops, nc[i], i)
	}
}

// childID identifies one sibling for identity-aware diffing.
//
// A non-empty key identifies the node; otherwise the program node index does.
// The struct is comparable, so identity checks need no string building. The
// previous shape returned "k:"+Key or "s:"+itoa(Source), which allocated one
// string per child, per side, on every event.
type childID struct {
	key    string
	source int
}

// childIdentity returns the identity of a sibling plus whether it has one.
func childIdentity(tree *ResolvedTree, idx int) (childID, bool) {
	n := resolvedNodeAt(tree, idx)
	if n == nil {
		return childID{}, false
	}
	if n.Key != "" {
		return childID{key: n.Key}, true
	}
	if n.HasSource && n.Source >= 0 {
		return childID{source: n.source1()}, true
	}
	return childID{}, false
}

// source1 shifts the program node index by one so that source 0 never collides
// with the zero childID, which stands for "no identity".
func (node *ResolvedNode) source1() int {
	return node.Source + 1
}

// childIdentityScanLimit is the sibling count above which building a map beats
// scanning the child list. Below it the scan wins, because it allocates nothing.
//
// Sibling groups with distinct identities are handwritten markup — a handful of
// elements. A forEach row set shares one program source, so its identities
// repeat and childIdentitiesUnique rejects it on the second child. A list long
// enough to reach the map branch is therefore rare.
const childIdentityScanLimit = 16

// childIdentitiesUnique reports whether every child in indices carries a
// distinct, non-empty identity. That is the precondition for identity-aware
// diffing; a false answer sends the caller to the index-positional walk.
func childIdentitiesUnique(tree *ResolvedTree, indices []int) bool {
	// An empty list is trivially unique. A single child is NOT: it may carry no
	// identity at all, and that must still send the caller to the index walk,
	// because the identity walk would treat the missing identity as absent from
	// the other side and emit a remove plus a create instead of an in-place
	// reconcile.
	if len(indices) == 0 {
		return true
	}
	if len(indices) > childIdentityScanLimit {
		seen := make(map[childID]struct{}, len(indices))
		for _, idx := range indices {
			id, ok := childIdentity(tree, idx)
			if !ok {
				return false
			}
			if _, exists := seen[id]; exists {
				return false
			}
			seen[id] = struct{}{}
		}
		return true
	}
	// Identities are read once into a stack array and compared from there.
	// Reading them again inside the inner loop charged the quadratic pass a
	// bounds check, a nil check and a string compare per pair, which a CPU
	// profile put above the whole expression interpreter.
	var ids [childIdentityScanLimit]childID
	for i, idx := range indices {
		id, ok := childIdentity(tree, idx)
		if !ok {
			return false
		}
		for j := 0; j < i; j++ {
			if ids[j] == id {
				return false
			}
		}
		ids[i] = id
	}
	return true
}

// childIdentityLookup answers "does this sibling list contain that identity".
//
// Short lists scan the tree directly and allocate nothing. Long lists build a map
// once so the merge walk stays linear. The struct holds no inline array, so it
// returns through the caller's frame with no heap traffic.
type childIdentityLookup struct {
	tree    *ResolvedTree
	indices []int
	set     map[childID]struct{} // nil in scanning mode
}

// newChildIdentityLookup prepares membership answers for one sibling list. Call
// it only after childIdentitiesUnique has passed.
func newChildIdentityLookup(tree *ResolvedTree, indices []int) childIdentityLookup {
	if len(indices) <= childIdentityScanLimit {
		return childIdentityLookup{tree: tree, indices: indices}
	}
	set := make(map[childID]struct{}, len(indices))
	for _, idx := range indices {
		if id, ok := childIdentity(tree, idx); ok {
			set[id] = struct{}{}
		}
	}
	return childIdentityLookup{tree: tree, indices: indices, set: set}
}

// has reports whether the sibling list carries id.
func (l childIdentityLookup) has(id childID) bool {
	if l.set != nil {
		_, ok := l.set[id]
		return ok
	}
	for _, idx := range l.indices {
		if other, ok := childIdentity(l.tree, idx); ok && other == id {
			return true
		}
	}
	return false
}

func appendReplaceSubtree(w *diffWalk, ops *[]PatchOp, nodeIdx int) {
	node := resolvedNodeAt(w.next, nodeIdx)
	if node == nil {
		return
	}
	path := &w.path
	if node.Tag == "" {
		appendTextPatch(ops, path, node.Text)
		return
	}

	*ops = append(*ops, PatchOp{Kind: PatchReplaceElement, Path: path.String(), Tag: node.Tag})
	appendNodeAttrOps(ops, node, path)
	if node.Text != "" && len(node.Children) == 0 {
		appendTextPatch(ops, path, node.Text)
	}
	for i, childIdx := range node.Children {
		appendCreateSubtree(w, ops, childIdx, i)
	}
}

func appendCreateSubtree(w *diffWalk, ops *[]PatchOp, nodeIdx, insertIdx int) {
	node := resolvedNodeAt(w.next, nodeIdx)
	if node == nil {
		return
	}
	parentPath := &w.path
	if node.Tag == "" {
		*ops = append(*ops, PatchOp{
			Kind:     PatchCreateText,
			Path:     parentPath.String(),
			Text:     node.Text,
			Children: []int{insertIdx},
		})
		return
	}

	*ops = append(*ops, PatchOp{
		Kind:     PatchCreateElement,
		Path:     parentPath.String(),
		Tag:      node.Tag,
		Children: []int{insertIdx},
	})

	parentPath.push(insertIdx)
	appendNodeAttrOps(ops, node, parentPath)
	if node.Text != "" && len(node.Children) == 0 {
		appendTextPatch(ops, parentPath, node.Text)
	}
	for i, childIdx := range node.Children {
		appendCreateSubtree(w, ops, childIdx, i)
	}
	parentPath.pop()
}

func reconcileAttrs(ops *[]PatchOp, pn, nn *ResolvedNode, path *patchPath) {
	prevAttrs := pn.effectiveDOMAttrs()
	nextAttrs := nn.effectiveDOMAttrs()
	// Subtree reuse copies a resolved node whole, so an element nothing
	// touched hands the reconciler the identical attribute array on both
	// sides. That is the common case on a live island, and it needs no
	// comparison at all.
	if attrsAlias(prevAttrs, nextAttrs) {
		return
	}

	valueElem := isValueElement(nn.Tag)
	prevValues := newAttrIndex(prevAttrs)
	nextNames := newAttrIndex(nextAttrs)

	appendAttrSetOps(ops, nextAttrs, prevValues, valueElem, path)
	appendAttrRemoveOps(ops, prevAttrs, nextNames, path)
}

// attrsAlias reports whether two attribute lists are the same array.
func attrsAlias(left, right []ResolvedAttr) bool {
	if len(left) != len(right) {
		return false
	}
	if len(left) == 0 {
		return true
	}
	return &left[0] == &right[0]
}

func appendNodeAttrOps(ops *[]PatchOp, node *ResolvedNode, path *patchPath) {
	valueElem := isValueElement(node.Tag)
	for _, attr := range node.effectiveDOMAttrs() {
		if appendValueAttrSetOp(ops, attr, valueElem, path) {
			continue
		}
		appendAttrSetOp(ops, attr, path)
	}
}

func appendTextPatch(ops *[]PatchOp, path *patchPath, text string) {
	*ops = append(*ops, PatchOp{
		Kind: PatchSetText,
		Path: path.String(),
		Text: text,
	})
}

// appendRemoveChild takes the joined path, because every caller names a
// child the walk does not enter.
func appendRemoveChild(ops *[]PatchOp, path string) {
	*ops = append(*ops, PatchOp{Kind: PatchRemoveElement, Path: path})
}

// attrScanLimit is the list length up to which attrIndex answers by scanning
// the slice instead of building a map. An element carries a handful of
// attributes, so the scan wins: building a map of four entries costs four
// hashes and an allocation, while four string compares that mostly fail on
// length cost neither. The map path stays for the wide element a component
// spread can produce.
const attrScanLimit = 8

// attrIndex answers "what did this attribute hold before?" and "is this name
// still present?" for one element pair.
//
// Both questions used to be answered by a freshly built map per element pair.
// A CPU profile of the island benchmarks charged those two maps 11% of all
// samples, which is more than the whole expression interpreter costs.
type attrIndex struct {
	attrs []ResolvedAttr
	// byName is built only above attrScanLimit. nil means "scan attrs".
	byName map[string]string
}

func newAttrIndex(attrs []ResolvedAttr) attrIndex {
	if len(attrs) <= attrScanLimit {
		return attrIndex{attrs: attrs}
	}
	byName := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		byName[attr.Name] = attr.Value
	}
	return attrIndex{attrs: attrs, byName: byName}
}

// lookup returns the value recorded for name and whether the name is present.
//
// The scan runs backwards because a map keeps the LAST value written for a
// duplicated name. Scanning forwards would return the first, which is a
// different answer for an element that lists one name twice.
func (index attrIndex) lookup(name string) (string, bool) {
	if index.byName != nil {
		value, ok := index.byName[name]
		return value, ok
	}
	for i := len(index.attrs) - 1; i >= 0; i-- {
		if index.attrs[i].Name == name {
			return index.attrs[i].Value, true
		}
	}
	return "", false
}

// has reports whether name appears in the list.
func (index attrIndex) has(name string) bool {
	if index.byName != nil {
		_, ok := index.byName[name]
		return ok
	}
	for i := range index.attrs {
		if index.attrs[i].Name == name {
			return true
		}
	}
	return false
}

func appendAttrSetOps(ops *[]PatchOp, attrs []ResolvedAttr, prevAttrs attrIndex, valueElem bool, path *patchPath) {
	for _, attr := range attrs {
		if !attrNeedsUpdate(attr, prevAttrs) {
			continue
		}
		if appendValueAttrSetOp(ops, attr, valueElem, path) {
			continue
		}
		appendAttrSetOp(ops, attr, path)
	}
}

func attrNeedsUpdate(attr ResolvedAttr, prevAttrs attrIndex) bool {
	prevVal, existed := prevAttrs.lookup(attr.Name)
	return !existed || prevVal != attr.Value
}

func appendValueAttrSetOp(ops *[]PatchOp, attr ResolvedAttr, valueElem bool, path *patchPath) bool {
	if !valueElem || attr.Name != "value" {
		return false
	}
	*ops = append(*ops, PatchOp{
		Kind:     PatchSetValue,
		Path:     path.String(),
		Text:     attr.Value,
		AttrName: "value",
	})
	return true
}

func appendAttrSetOp(ops *[]PatchOp, attr ResolvedAttr, path *patchPath) {
	*ops = append(*ops, PatchOp{
		Kind:     PatchSetAttr,
		Path:     path.String(),
		AttrName: attr.Name,
		Text:     attr.Value,
	})
}

func appendAttrRemoveOps(ops *[]PatchOp, prevAttrs []ResolvedAttr, nextAttrs attrIndex, path *patchPath) {
	for _, attr := range prevAttrs {
		if nextAttrs.has(attr.Name) {
			continue
		}
		*ops = append(*ops, PatchOp{
			Kind:     PatchRemoveAttr,
			Path:     path.String(),
			AttrName: attr.Name,
		})
	}
}
