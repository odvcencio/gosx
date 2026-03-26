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
	if !canReconcileTrees(prev, next) {
		return nil
	}
	var ops []PatchOp
	reconcileNode(&ops, prev, next, 0, "", staticMask)
	return ops
}

func canReconcileTrees(prev, next *ResolvedTree) bool {
	if prev == nil || next == nil {
		return false
	}
	return len(prev.Nodes) > 0 && len(next.Nodes) > 0
}

type keyedChildIndex struct {
	position int
	nodeIdx  int
}

// reconcileNode recursively diffs a single node and its subtree.
//
// Key design: paths only address Element nodes (things the DOM has as elements).
// Text and Expr nodes don't get their own paths — instead, when they change,
// the reconciler emits a SetText on the nearest parent Element that contains them.
// This avoids the fragile text-node-indexing problem where empty text nodes
// vanish from the DOM and shift child indices.
func reconcileNode(ops *[]PatchOp, prev, next *ResolvedTree, nodeIdx int, path string, staticMask []bool) {
	if !reconcileNodePresent(prev, next, nodeIdx) {
		return
	}
	if isStaticNode(nodeIdx, staticMask) {
		return
	}

	pn := &prev.Nodes[nodeIdx]
	nn := &next.Nodes[nodeIdx]
	if reconcileTextLikeNode(ops, pn, nn, path) {
		return
	}
	appendElementTextPatch(ops, pn, nn, path)
	reconcileAttrs(ops, pn, nn, path)
	reconcileNodeChildren(ops, prev, next, pn, nn, path, staticMask)
}

func reconcileNodePresent(prev, next *ResolvedTree, nodeIdx int) bool {
	return nodeIdx < len(prev.Nodes) && nodeIdx < len(next.Nodes)
}

func isStaticNode(nodeIdx int, staticMask []bool) bool {
	return nodeIdx < len(staticMask) && staticMask[nodeIdx]
}

func reconcileTextLikeNode(ops *[]PatchOp, prevNode, nextNode *ResolvedNode, path string) bool {
	if nextNode.Tag != "" {
		return false
	}
	if prevNode.Text != nextNode.Text {
		appendTextPatch(ops, path, nextNode.Text)
	}
	return true
}

func appendElementTextPatch(ops *[]PatchOp, prevNode, nextNode *ResolvedNode, path string) {
	if prevNode.Text != nextNode.Text && nextNode.Text != "" {
		appendTextPatch(ops, path, nextNode.Text)
	}
}

func appendTextPatch(ops *[]PatchOp, path, text string) {
	*ops = append(*ops, PatchOp{
		Kind: PatchSetText,
		Path: path,
		Text: text,
	})
}

func reconcileNodeChildren(ops *[]PatchOp, prev, next *ResolvedTree, prevNode, nextNode *ResolvedNode, path string, staticMask []bool) {
	if childrenHaveKeys(prev, next, prevNode, nextNode) {
		reconcileKeyedChildren(ops, prev, next, prevNode, nextNode, path, staticMask)
		return
	}
	reconcilePositionalChildren(ops, prev, next, prevNode, nextNode, path, staticMask)
}

// childrenHaveKeys returns true if any child in either list has a key.
func childrenHaveKeys(prev, next *ResolvedTree, pn, nn *ResolvedNode) bool {
	return nodeHasKeyedChildren(next, nn) || nodeHasKeyedChildren(prev, pn)
}

func nodeHasKeyedChildren(tree *ResolvedTree, node *ResolvedNode) bool {
	for _, idx := range node.Children {
		if idx >= len(tree.Nodes) {
			continue
		}
		if tree.Nodes[idx].Key != "" {
			return true
		}
	}
	return false
}

// reconcileKeyedChildren diffs children using stable keys.
// Items with the same key are matched regardless of position, producing
// minimal move/insert/remove operations instead of rewriting everything.
func reconcileKeyedChildren(ops *[]PatchOp, prev, next *ResolvedTree, pn, nn *ResolvedNode, path string, staticMask []bool) {
	prevByKey := buildPrevKeyIndex(prev, pn)
	nextKeys := make(map[string]bool)
	reorderNeeded := false
	lastPrevIdx := -1

	for elemIdx, childIdx := range nn.Children {
		if childIdx >= len(next.Nodes) {
			continue
		}
		nextChild := &next.Nodes[childIdx]
		key := nextChild.Key

		if key == "" {
			reconcileUnkeyedKeyedChild(ops, prev, next, pn, nextChild, childIdx, elemIdx, path, staticMask)
			continue
		}

		nextKeys[key] = true

		if prevChild, ok := prevByKey[key]; ok {
			reconcileMatchedKeyedChild(ops, prev, next, prevChild, childIdx, elemIdx, path, staticMask)
			if prevChild.position < lastPrevIdx {
				reorderNeeded = true
			}
			lastPrevIdx = prevChild.position
		} else {
			appendCreateChild(ops, path, nextChild)
		}
	}

	removeMissingKeyedChildren(ops, prev, pn, nextKeys, path)
	appendReorderOp(ops, nn, path, reorderNeeded)
}

// reconcileNodePair diffs two nodes at arbitrary positions.
func reconcileNodePair(ops *[]PatchOp, prev, next *ResolvedTree, prevIdx, nextIdx int, path string, staticMask []bool) {
	if prevIdx >= len(prev.Nodes) || nextIdx >= len(next.Nodes) {
		return
	}
	pn := &prev.Nodes[prevIdx]
	nn := &next.Nodes[nextIdx]

	appendElementTextPatch(ops, pn, nn, path)
	reconcileAttrs(ops, pn, nn, path)
}

// reconcilePositionalChildren diffs children by position index (no keys).
func reconcilePositionalChildren(ops *[]PatchOp, prev, next *ResolvedTree, pn, nn *ResolvedNode, path string, staticMask []bool) {
	elemIdx := 0
	prevLen := len(pn.Children)
	nextLen := len(nn.Children)
	maxLen := positionalChildCount(prevLen, nextLen)

	for i := 0; i < maxLen; i++ {
		if reconcilePositionalEdge(ops, prev, next, nn, i, elemIdx, path, prevLen, nextLen) {
			elemIdx++
			continue
		}
		reconcilePositionalPair(ops, prev, next, pn, nn, i, elemIdx, path, staticMask)
		elemIdx++
	}
}

func positionalChildCount(prevLen, nextLen int) int {
	if nextLen > prevLen {
		return nextLen
	}
	return prevLen
}

func reconcilePositionalEdge(ops *[]PatchOp, prev, next *ResolvedTree, nn *ResolvedNode, i, elemIdx int, path string, prevLen, nextLen int) bool {
	if i >= nextLen {
		appendRemoveChild(ops, childPath(path, elemIdx))
		return true
	}
	if i >= prevLen {
		appendPositionalCreateChild(ops, next, nn, i, path)
		return true
	}
	return false
}

func appendPositionalCreateChild(ops *[]PatchOp, next *ResolvedTree, node *ResolvedNode, index int, path string) {
	childIdx := node.Children[index]
	if childIdx < len(next.Nodes) {
		appendCreateChild(ops, path, &next.Nodes[childIdx])
	}
}

func reconcilePositionalPair(ops *[]PatchOp, prev, next *ResolvedTree, pn, nn *ResolvedNode, index, elemIdx int, path string, staticMask []bool) {
	nextChildIdx := nn.Children[index]
	prevChildIdx := pn.Children[index]
	if nextChildIdx >= len(next.Nodes) || prevChildIdx >= len(prev.Nodes) {
		return
	}
	reconcilePositionalChild(ops, prev, next, prevChildIdx, nextChildIdx, elemIdx, path, staticMask)
}

// reconcileAttrs diffs the attributes between a prev and next node.
func reconcileAttrs(ops *[]PatchOp, pn, nn *ResolvedNode, path string) {
	valueElem := isValueElement(nn.Tag)
	prevAttrs := attrValueMap(pn.Attrs)
	nextAttrs := attrPresenceMap(nn.Attrs)
	appendAttrSetOps(ops, nn.Attrs, prevAttrs, valueElem, path)
	appendAttrRemoveOps(ops, pn.Attrs, nextAttrs, path)
}

func buildPrevKeyIndex(prev *ResolvedTree, node *ResolvedNode) map[string]keyedChildIndex {
	byKey := make(map[string]keyedChildIndex)
	for i, childIdx := range node.Children {
		if childIdx >= len(prev.Nodes) {
			continue
		}
		key := prev.Nodes[childIdx].Key
		if key == "" {
			continue
		}
		byKey[key] = keyedChildIndex{position: i, nodeIdx: childIdx}
	}
	return byKey
}

func reconcileUnkeyedKeyedChild(ops *[]PatchOp, prev, next *ResolvedTree, pn *ResolvedNode, nextChild *ResolvedNode, childIdx, elemIdx int, path string, staticMask []bool) {
	cp := childPath(path, elemIdx)
	if elemIdx >= len(pn.Children) {
		appendCreateChild(ops, path, nextChild)
		return
	}
	prevChildIdx := pn.Children[elemIdx]
	if prevChildIdx >= len(prev.Nodes) {
		return
	}
	if nextChild.Tag != "" {
		reconcileNode(ops, prev, next, childIdx, cp, staticMask)
		return
	}
	if prev.Nodes[prevChildIdx].Text != nextChild.Text {
		*ops = append(*ops, PatchOp{Kind: PatchSetText, Path: cp, Text: nextChild.Text})
	}
}

func reconcileMatchedKeyedChild(ops *[]PatchOp, prev, next *ResolvedTree, prevChild keyedChildIndex, nextChildIdx, elemIdx int, path string, staticMask []bool) {
	reconcileNodePair(ops, prev, next, prevChild.nodeIdx, nextChildIdx, childPath(path, elemIdx), staticMask)
}

func removeMissingKeyedChildren(ops *[]PatchOp, prev *ResolvedTree, node *ResolvedNode, nextKeys map[string]bool, path string) {
	for i := len(node.Children) - 1; i >= 0; i-- {
		childIdx := node.Children[i]
		if childIdx >= len(prev.Nodes) {
			continue
		}
		key := prev.Nodes[childIdx].Key
		if key != "" && !nextKeys[key] {
			appendRemoveChild(ops, childPath(path, i))
		}
	}
}

func appendReorderOp(ops *[]PatchOp, node *ResolvedNode, path string, reorderNeeded bool) {
	if !reorderNeeded {
		return
	}
	order := make([]int, len(node.Children))
	for i := range order {
		order[i] = i
	}
	*ops = append(*ops, PatchOp{Kind: PatchReorder, Path: path, Children: order})
}

func reconcilePositionalChild(ops *[]PatchOp, prev, next *ResolvedTree, prevChildIdx, nextChildIdx, elemIdx int, path string, staticMask []bool) {
	nextChild := &next.Nodes[nextChildIdx]
	prevChild := &prev.Nodes[prevChildIdx]
	cp := childPath(path, elemIdx)
	if nextChild.Tag != "" {
		reconcileNode(ops, prev, next, nextChildIdx, cp, staticMask)
		return
	}
	if prevChild.Text != nextChild.Text {
		appendTextPatch(ops, cp, nextChild.Text)
	}
}

func appendCreateChild(ops *[]PatchOp, path string, child *ResolvedNode) {
	if child == nil || child.Tag == "" {
		return
	}
	*ops = append(*ops, PatchOp{Kind: PatchCreateElement, Path: path, Tag: child.Tag, Text: child.Text})
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
