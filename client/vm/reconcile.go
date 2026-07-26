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

// childPath builds a slash-separated path from the island root.
//
// Called once per reconcile walk step — dozens of times per tree diff.
// strconv.Itoa + explicit concatenation avoids the fmt.Sprintf
// format-state scratch (2-3 allocs) that the previous implementation paid
// on every call.
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

// ReconcileTrees diffs the previous and next resolved trees and returns patch ops.
func ReconcileTrees(prev, next *ResolvedTree, staticMask []bool) []PatchOp {
	if prev == nil || next == nil || len(prev.Nodes) == 0 || len(next.Nodes) == 0 {
		return nil
	}
	var ops []PatchOp
	reconcileNodePair(&ops, prev, next, 0, 0, "", staticMask)
	return ops
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

func reconcileNodePair(ops *[]PatchOp, prev, next *ResolvedTree, prevIdx, nextIdx int, path string, staticMask []bool) {
	pair, ok := resolveNodePair(prev, next, prevIdx, nextIdx)
	if !ok || shouldSkipReconcileNode(pair.next, nextIdx, staticMask) {
		return
	}
	if isLeafNodePair(pair.prev, pair.next) {
		reconcileLeafNodePair(ops, pair.prev, pair.next, next, nextIdx, path)
		return
	}
	if pair.prev.Tag != pair.next.Tag {
		appendReplaceSubtree(ops, next, nextIdx, path)
		return
	}
	reconcileElementNodePair(ops, prev, next, pair.prev, pair.next, path, staticMask)
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

func reconcileLeafNodePair(ops *[]PatchOp, pn, nn *ResolvedNode, next *ResolvedTree, nextIdx int, path string) {

	switch {
	case pn.Tag == "" && nn.Tag == "":
		if pn.Text != nn.Text {
			appendTextPatch(ops, path, nn.Text)
		}
	case pn.Tag != "" && nn.Tag == "":
		appendTextPatch(ops, path, nn.Text)
	default:
		appendReplaceSubtree(ops, next, nextIdx, path)
	}
}

func reconcileElementNodePair(ops *[]PatchOp, prev, next *ResolvedTree, pn, nn *ResolvedNode, path string, staticMask []bool) {
	if pn.Text != nn.Text && (pn.Text != "" || nn.Text != "") {
		appendTextPatch(ops, path, nn.Text)
	}
	reconcileAttrs(ops, pn, nn, path)
	reconcileChildren(ops, prev, next, pn, nn, path, staticMask)
}

func reconcileChildren(ops *[]PatchOp, prev, next *ResolvedTree, prevNode, nextNode *ResolvedNode, path string, staticMask []bool) {
	if childrenAreFullyKeyed(prev, prevNode) && childrenAreFullyKeyed(next, nextNode) {
		reconcileKeyedChildren(ops, prev, next, prevNode, nextNode, path, staticMask)
		return
	}
	reconcilePositionalChildren(ops, prev, next, prevNode, nextNode, path, staticMask)
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

func reconcileKeyedChildren(ops *[]PatchOp, prev, next *ResolvedTree, pn, nn *ResolvedNode, path string, staticMask []bool) {
	plan, ok := buildKeyedChildrenPlan(prev, next, pn, nn)
	if !ok {
		reconcilePositionalChildren(ops, prev, next, pn, nn, path, staticMask)
		return
	}

	removeMissingKeyedChildren(ops, prev, pn, plan.nextKeys, path)
	plan.currentOrder = appendMissingKeyedChildren(ops, next, plan, path)
	appendKeyedReorderOp(ops, path, plan.currentOrder, plan.desiredOrder)
	reconcileExistingKeyedChildren(ops, prev, next, plan, path, staticMask)
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

func removeMissingKeyedChildren(ops *[]PatchOp, prev *ResolvedTree, node *ResolvedNode, nextKeys map[string]struct{}, path string) {
	for i := len(node.Children) - 1; i >= 0; i-- {
		child := resolvedNodeAt(prev, node.Children[i])
		if child == nil || child.Key == "" {
			continue
		}
		if _, ok := nextKeys[child.Key]; ok {
			continue
		}
		appendRemoveChild(ops, childPath(path, i))
	}
}

func appendMissingKeyedChildren(ops *[]PatchOp, next *ResolvedTree, plan keyedChildrenPlan, path string) []string {
	currentOrder := plan.currentOrder
	for _, child := range plan.nextChildren {
		if _, exists := plan.prevByKey[child.key]; exists {
			continue
		}
		appendCreateSubtree(ops, next, child.nodeIdx, path, child.elementIdx)
		currentOrder = insertKey(currentOrder, child.elementIdx, child.key)
	}
	return currentOrder
}

func appendKeyedReorderOp(ops *[]PatchOp, path string, currentOrder, desiredOrder []string) {
	if order := reorderIndices(currentOrder, desiredOrder); order != nil {
		*ops = append(*ops, PatchOp{Kind: PatchReorder, Path: path, Children: order})
	}
}

func reconcileExistingKeyedChildren(ops *[]PatchOp, prev, next *ResolvedTree, plan keyedChildrenPlan, path string, staticMask []bool) {
	for _, child := range plan.nextChildren {
		prevChild, ok := plan.prevByKey[child.key]
		if !ok {
			continue
		}
		reconcileNodePair(ops, prev, next, prevChild.nodeIdx, child.nodeIdx, childPath(path, child.elementIdx), staticMask)
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
func reconcilePositionalChildren(ops *[]PatchOp, prev, next *ResolvedTree, pn, nn *ResolvedNode, path string, staticMask []bool) {
	pc, nc := pn.Children, nn.Children
	if !childIdentitiesUnique(prev, pc) || !childIdentitiesUnique(next, nc) {
		reconcileIndexPositional(ops, prev, next, pc, nc, path, staticMask)
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
			reconcileNodePair(ops, prev, next, pc[i], nc[j], childPath(path, d), staticMask)
			i, j, d = i+1, j+1, d+1
		case !nextLookup.has(pid):
			appendRemoveChild(ops, childPath(path, d)) // prev child gone; successor shifts to d
			i++
		case !prevLookup.has(nid):
			appendCreateSubtree(ops, next, nc[j], path, d) // next child is new
			j, d = j+1, d+1
		default:
			reconcileNodePair(ops, prev, next, pc[i], nc[j], childPath(path, d), staticMask)
			i, j, d = i+1, j+1, d+1
		}
	}
	for ; i < len(pc); i++ {
		appendRemoveChild(ops, childPath(path, d))
	}
	for ; j < len(nc); j++ {
		appendCreateSubtree(ops, next, nc[j], path, d)
		d++
	}
}

// reconcileIndexPositional is the index-by-index fallback used when identities
// are not unique: reconcile the shared prefix in place, remove the prev tail
// last-to-first (so removals don't shift later indices), then append the next
// tail.
func reconcileIndexPositional(ops *[]PatchOp, prev, next *ResolvedTree, pc, nc []int, path string, staticMask []bool) {
	common := len(pc)
	if len(nc) < common {
		common = len(nc)
	}
	for i := 0; i < common; i++ {
		reconcileNodePair(ops, prev, next, pc[i], nc[i], childPath(path, i), staticMask)
	}
	for i := len(pc) - 1; i >= len(nc); i-- {
		appendRemoveChild(ops, childPath(path, i))
	}
	for i := len(pc); i < len(nc); i++ {
		appendCreateSubtree(ops, next, nc[i], path, i)
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
	for i, idx := range indices {
		id, ok := childIdentity(tree, idx)
		if !ok {
			return false
		}
		for _, prior := range indices[:i] {
			if other, priorOK := childIdentity(tree, prior); priorOK && other == id {
				return false
			}
		}
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

func appendReplaceSubtree(ops *[]PatchOp, tree *ResolvedTree, nodeIdx int, path string) {
	node := resolvedNodeAt(tree, nodeIdx)
	if node == nil {
		return
	}
	if node.Tag == "" {
		appendTextPatch(ops, path, node.Text)
		return
	}

	*ops = append(*ops, PatchOp{Kind: PatchReplaceElement, Path: path, Tag: node.Tag})
	appendNodeAttrOps(ops, node, path)
	if node.Text != "" && len(node.Children) == 0 {
		appendTextPatch(ops, path, node.Text)
	}
	for i, childIdx := range node.Children {
		appendCreateSubtree(ops, tree, childIdx, path, i)
	}
}

func appendCreateSubtree(ops *[]PatchOp, tree *ResolvedTree, nodeIdx int, parentPath string, insertIdx int) {
	node := resolvedNodeAt(tree, nodeIdx)
	if node == nil {
		return
	}
	if node.Tag == "" {
		*ops = append(*ops, PatchOp{
			Kind:     PatchCreateText,
			Path:     parentPath,
			Text:     node.Text,
			Children: []int{insertIdx},
		})
		return
	}

	*ops = append(*ops, PatchOp{
		Kind:     PatchCreateElement,
		Path:     parentPath,
		Tag:      node.Tag,
		Children: []int{insertIdx},
	})

	nodePath := childPath(parentPath, insertIdx)
	appendNodeAttrOps(ops, node, nodePath)
	if node.Text != "" && len(node.Children) == 0 {
		appendTextPatch(ops, nodePath, node.Text)
	}
	for i, childIdx := range node.Children {
		appendCreateSubtree(ops, tree, childIdx, nodePath, i)
	}
}

func reconcileAttrs(ops *[]PatchOp, pn, nn *ResolvedNode, path string) {
	valueElem := isValueElement(nn.Tag)
	prevAttrs := pn.effectiveDOMAttrs()
	nextAttrs := nn.effectiveDOMAttrs()
	prevValues := attrValueMap(prevAttrs)
	nextNames := attrPresenceMap(nextAttrs)

	appendAttrSetOps(ops, nextAttrs, prevValues, valueElem, path)
	appendAttrRemoveOps(ops, prevAttrs, nextNames, path)
}

func appendNodeAttrOps(ops *[]PatchOp, node *ResolvedNode, path string) {
	valueElem := isValueElement(node.Tag)
	for _, attr := range node.effectiveDOMAttrs() {
		if appendValueAttrSetOp(ops, attr, valueElem, path) {
			continue
		}
		appendAttrSetOp(ops, attr, path)
	}
}

func appendTextPatch(ops *[]PatchOp, path, text string) {
	*ops = append(*ops, PatchOp{
		Kind: PatchSetText,
		Path: path,
		Text: text,
	})
}

func appendRemoveChild(ops *[]PatchOp, path string) {
	*ops = append(*ops, PatchOp{Kind: PatchRemoveElement, Path: path})
}

func attrValueMap(attrs []ResolvedAttr) map[string]string {
	values := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		values[attr.Name] = attr.Value
	}
	return values
}

func attrPresenceMap(attrs []ResolvedAttr) map[string]struct{} {
	values := make(map[string]struct{}, len(attrs))
	for _, attr := range attrs {
		values[attr.Name] = struct{}{}
	}
	return values
}

func appendAttrSetOps(ops *[]PatchOp, attrs []ResolvedAttr, prevAttrs map[string]string, valueElem bool, path string) {
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

func attrNeedsUpdate(attr ResolvedAttr, prevAttrs map[string]string) bool {
	prevVal, existed := prevAttrs[attr.Name]
	return !existed || prevVal != attr.Value
}

func appendValueAttrSetOp(ops *[]PatchOp, attr ResolvedAttr, valueElem bool, path string) bool {
	if !valueElem || attr.Name != "value" {
		return false
	}
	*ops = append(*ops, PatchOp{
		Kind:     PatchSetValue,
		Path:     path,
		Text:     attr.Value,
		AttrName: "value",
	})
	return true
}

func appendAttrSetOp(ops *[]PatchOp, attr ResolvedAttr, path string) {
	*ops = append(*ops, PatchOp{
		Kind:     PatchSetAttr,
		Path:     path,
		AttrName: attr.Name,
		Text:     attr.Value,
	})
}

func appendAttrRemoveOps(ops *[]PatchOp, prevAttrs []ResolvedAttr, nextAttrs map[string]struct{}, path string) {
	for _, attr := range prevAttrs {
		if _, ok := nextAttrs[attr.Name]; ok {
			continue
		}
		*ops = append(*ops, PatchOp{
			Kind:     PatchRemoveAttr,
			Path:     path,
			AttrName: attr.Name,
		})
	}
}
