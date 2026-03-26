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
	if prev == nil || next == nil {
		return nil
	}
	if len(next.Nodes) == 0 || len(prev.Nodes) == 0 {
		return nil
	}
	var ops []PatchOp
	reconcileNode(&ops, prev, next, 0, "", staticMask)
	return ops
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
	if nodeIdx >= len(prev.Nodes) || nodeIdx >= len(next.Nodes) {
		return
	}

	if nodeIdx < len(staticMask) && staticMask[nodeIdx] {
		return
	}

	pn := &prev.Nodes[nodeIdx]
	nn := &next.Nodes[nodeIdx]

	// Text/Expr nodes (no tag) — if they changed, emit SetText at current path.
	if nn.Tag == "" {
		if pn.Text != nn.Text {
			*ops = append(*ops, PatchOp{
				Kind: PatchSetText,
				Path: path,
				Text: nn.Text,
			})
		}
		return
	}

	// Element node — diff text (if set directly), attributes, then recurse children.
	// Text on elements handles the case where an element's direct textContent changes
	// (e.g., <span>old</span> → <span>new</span> with no child nodes).
	if pn.Text != nn.Text && nn.Text != "" {
		*ops = append(*ops, PatchOp{
			Kind: PatchSetText,
			Path: path,
			Text: nn.Text,
		})
	}

	reconcileAttrs(ops, pn, nn, path)

	// Check if children are keyed (any child has a Key).
	hasKeys := childrenHaveKeys(prev, next, pn, nn)

	if hasKeys {
		reconcileKeyedChildren(ops, prev, next, pn, nn, path, staticMask)
	} else {
		reconcilePositionalChildren(ops, prev, next, pn, nn, path, staticMask)
	}
}

// childrenHaveKeys returns true if any child in either list has a key.
func childrenHaveKeys(prev, next *ResolvedTree, pn, nn *ResolvedNode) bool {
	for _, idx := range nn.Children {
		if idx < len(next.Nodes) && next.Nodes[idx].Key != "" {
			return true
		}
	}
	for _, idx := range pn.Children {
		if idx < len(prev.Nodes) && prev.Nodes[idx].Key != "" {
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

	if pn.Text != nn.Text && nn.Text != "" {
		*ops = append(*ops, PatchOp{Kind: PatchSetText, Path: path, Text: nn.Text})
	}
	reconcileAttrs(ops, pn, nn, path)
}

// reconcilePositionalChildren diffs children by position index (no keys).
func reconcilePositionalChildren(ops *[]PatchOp, prev, next *ResolvedTree, pn, nn *ResolvedNode, path string, staticMask []bool) {
	elemIdx := 0
	prevLen := len(pn.Children)
	nextLen := len(nn.Children)
	maxLen := prevLen
	if nextLen > maxLen {
		maxLen = nextLen
	}

	for i := 0; i < maxLen; i++ {
		if i >= nextLen {
			appendRemoveChild(ops, childPath(path, elemIdx))
			elemIdx++
			continue
		}
		if i >= prevLen {
			childIdx := nn.Children[i]
			if childIdx < len(next.Nodes) {
				appendCreateChild(ops, path, &next.Nodes[childIdx])
			}
			elemIdx++
			continue
		}

		nextChildIdx := nn.Children[i]
		prevChildIdx := pn.Children[i]

		if nextChildIdx >= len(next.Nodes) || prevChildIdx >= len(prev.Nodes) {
			elemIdx++
			continue
		}

		reconcilePositionalChild(ops, prev, next, prevChildIdx, nextChildIdx, elemIdx, path, staticMask)
		elemIdx++
	}
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
		*ops = append(*ops, PatchOp{Kind: PatchSetText, Path: cp, Text: nextChild.Text})
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
		prevVal, existed := prevAttrs[attr.Name]
		if existed && prevVal == attr.Value {
			continue
		}
		if valueElem && attr.Name == "value" {
			*ops = append(*ops, PatchOp{
				Kind:     PatchSetValue,
				Path:     path,
				Text:     attr.Value,
				AttrName: "value",
			})
			continue
		}
		*ops = append(*ops, PatchOp{
			Kind:     PatchSetAttr,
			Path:     path,
			AttrName: attr.Name,
			Text:     attr.Value,
		})
	}
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
