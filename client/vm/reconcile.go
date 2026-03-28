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
	position int
	nodeIdx  int
}

func reconcileNodePair(ops *[]PatchOp, prev, next *ResolvedTree, prevIdx, nextIdx int, path string, staticMask []bool) {
	pn := resolvedNodeAt(prev, prevIdx)
	nn := resolvedNodeAt(next, nextIdx)
	if pn == nil || nn == nil {
		return
	}
	if isStaticNode(nn, nextIdx, staticMask) {
		return
	}

	if pn.Tag == "" || nn.Tag == "" {
		reconcileLeafNodePair(ops, prev, next, prevIdx, nextIdx, path)
		return
	}

	if pn.Tag != nn.Tag {
		appendReplaceSubtree(ops, next, nextIdx, path)
		return
	}

	if pn.Text != nn.Text && (pn.Text != "" || nn.Text != "") {
		appendTextPatch(ops, path, nn.Text)
	}

	reconcileAttrs(ops, pn, nn, path)
	reconcileChildren(ops, prev, next, pn, nn, path, staticMask)
}

func resolvedNodeAt(tree *ResolvedTree, idx int) *ResolvedNode {
	if tree == nil || idx < 0 || idx >= len(tree.Nodes) {
		return nil
	}
	return &tree.Nodes[idx]
}

func isStaticNode(node *ResolvedNode, nodeIdx int, staticMask []bool) bool {
	if node == nil || !node.HasSource || node.Source < 0 {
		return nodeIdx >= 0 && nodeIdx < len(staticMask) && staticMask[nodeIdx]
	}
	return node.Source < len(staticMask) && staticMask[node.Source]
}

func reconcileLeafNodePair(ops *[]PatchOp, prev, next *ResolvedTree, prevIdx, nextIdx int, path string) {
	pn := resolvedNodeAt(prev, prevIdx)
	nn := resolvedNodeAt(next, nextIdx)
	if pn == nil || nn == nil {
		return
	}

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
	prevByKey := buildPrevKeyIndex(prev, pn)
	nextKeys := make(map[string]bool, len(nn.Children))
	desiredOrder := make([]string, 0, len(nn.Children))

	for _, childIdx := range nn.Children {
		child := resolvedNodeAt(next, childIdx)
		if child == nil || child.Key == "" {
			reconcilePositionalChildren(ops, prev, next, pn, nn, path, staticMask)
			return
		}
		nextKeys[child.Key] = true
		desiredOrder = append(desiredOrder, child.Key)
	}

	currentOrder := make([]string, 0, len(pn.Children))
	for _, childIdx := range pn.Children {
		child := resolvedNodeAt(prev, childIdx)
		if child == nil || child.Key == "" {
			reconcilePositionalChildren(ops, prev, next, pn, nn, path, staticMask)
			return
		}
		if nextKeys[child.Key] {
			currentOrder = append(currentOrder, child.Key)
		}
	}

	removeMissingKeyedChildren(ops, prev, pn, nextKeys, path)

	for elemIdx, childIdx := range nn.Children {
		child := resolvedNodeAt(next, childIdx)
		if child == nil {
			continue
		}
		if _, ok := prevByKey[child.Key]; ok {
			continue
		}
		appendCreateSubtree(ops, next, childIdx, path, elemIdx)
		currentOrder = insertKey(currentOrder, elemIdx, child.Key)
	}

	if order := reorderIndices(currentOrder, desiredOrder); order != nil {
		*ops = append(*ops, PatchOp{Kind: PatchReorder, Path: path, Children: order})
	}

	for elemIdx, childIdx := range nn.Children {
		child := resolvedNodeAt(next, childIdx)
		if child == nil {
			continue
		}
		prevChild, ok := prevByKey[child.Key]
		if !ok {
			continue
		}
		reconcileNodePair(ops, prev, next, prevChild.nodeIdx, childIdx, childPath(path, elemIdx), staticMask)
	}
}

func buildPrevKeyIndex(prev *ResolvedTree, node *ResolvedNode) map[string]keyedChildIndex {
	byKey := make(map[string]keyedChildIndex, len(node.Children))
	for i, childIdx := range node.Children {
		child := resolvedNodeAt(prev, childIdx)
		if child == nil || child.Key == "" {
			continue
		}
		byKey[child.Key] = keyedChildIndex{position: i, nodeIdx: childIdx}
	}
	return byKey
}

func removeMissingKeyedChildren(ops *[]PatchOp, prev *ResolvedTree, node *ResolvedNode, nextKeys map[string]bool, path string) {
	for i := len(node.Children) - 1; i >= 0; i-- {
		child := resolvedNodeAt(prev, node.Children[i])
		if child == nil || child.Key == "" {
			continue
		}
		if nextKeys[child.Key] {
			continue
		}
		appendRemoveChild(ops, childPath(path, i))
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

	unchanged := true
	for i := range current {
		if current[i] != desired[i] {
			unchanged = false
			break
		}
	}
	if unchanged {
		return nil
	}

	indexByKey := make(map[string]int, len(current))
	for i, key := range current {
		indexByKey[key] = i
	}

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
	prevAttrs := resolvedDOMAttrs(pn, "", false)
	nextAttrs := resolvedDOMAttrs(nn, "", false)
	prevValues := attrValueMap(prevAttrs)
	nextNames := attrPresenceMap(nextAttrs)

	appendAttrSetOps(ops, nextAttrs, prevValues, valueElem, path)
	appendAttrRemoveOps(ops, prevAttrs, nextNames, path)
}

func appendNodeAttrOps(ops *[]PatchOp, node *ResolvedNode, path string) {
	valueElem := isValueElement(node.Tag)
	for _, attr := range resolvedDOMAttrs(node, path, false) {
		if appendValueAttrSetOp(ops, attr, valueElem, path) {
			continue
		}
		appendAttrSetOp(ops, attr, path)
	}
}

func resolvedDOMAttrs(node *ResolvedNode, path string, includeStablePath bool) []ResolvedAttr {
	if node == nil {
		return nil
	}

	attrs := make([]ResolvedAttr, 0, len(node.Attrs)+(len(node.Events)*2)+1)
	attrs = append(attrs, node.Attrs...)
	for _, event := range node.Events {
		eventType := eventAttrType(event.Name)
		attrs = append(attrs, ResolvedAttr{
			Name:  "data-gosx-on-" + eventType,
			Value: event.Handler,
		})
		if eventType == "click" {
			attrs = append(attrs, ResolvedAttr{
				Name:  "data-gosx-handler",
				Value: event.Handler,
			})
		}
	}
	if includeStablePath && len(node.Events) > 0 {
		attrs = append(attrs, ResolvedAttr{Name: "data-gosx-path", Value: path})
	}
	return attrs
}

func eventAttrType(name string) string {
	switch name {
	case "onClick":
		return "click"
	case "onInput":
		return "input"
	case "onChange":
		return "change"
	case "onSubmit":
		return "submit"
	case "onKeyDown":
		return "keydown"
	case "onKeyUp":
		return "keyup"
	case "onFocus":
		return "focus"
	case "onBlur":
		return "blur"
	default:
		if strings.HasPrefix(name, "on") && len(name) > 2 {
			return strings.ToLower(name[2:3]) + name[3:]
		}
		return strings.ToLower(name)
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
