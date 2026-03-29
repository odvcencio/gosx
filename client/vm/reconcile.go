package vm

import (
	"fmt"
	"strings"
)

// isValueElement reports whether the tag is an element whose "value" attribute
// should be patched with PatchSetValue instead of PatchSetAttr.
func isValueElement(tag string) bool {
	t := strings.ToLower(tag)
	return t == "input" || t == "textarea" || t == "select"
}

// childPath builds a slash-separated path from the island root.
func childPath(parentPath string, childIdx int) string {
	if parentPath == "" {
		return fmt.Sprintf("%d", childIdx)
	}
	return fmt.Sprintf("%s/%d", parentPath, childIdx)
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
	reconcileElementNodePair(ops, prev, next, pair.prev, pair.next, nextIdx, path, staticMask)
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

func reconcileElementNodePair(ops *[]PatchOp, prev, next *ResolvedTree, pn, nn *ResolvedNode, nextIdx int, path string, staticMask []bool) {
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

func stringSlicesEqual(left, right []string) bool {
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

func reconcilePositionalChildren(ops *[]PatchOp, prev, next *ResolvedTree, pn, nn *ResolvedNode, path string, staticMask []bool) {
	prevLen := len(pn.Children)
	nextLen := len(nn.Children)
	maxLen := prevLen
	if nextLen > maxLen {
		maxLen = nextLen
	}

	for i := 0; i < maxLen; i++ {
		cp := childPath(path, i)
		switch {
		case i >= nextLen:
			appendRemoveChild(ops, cp)
		case i >= prevLen:
			appendCreateSubtree(ops, next, nn.Children[i], path, i)
		default:
			reconcileNodePair(ops, prev, next, pn.Children[i], nn.Children[i], cp, staticMask)
		}
	}
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
