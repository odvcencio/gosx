package vm

import (
	"fmt"
	"strings"
)

// isValueElement reports whether the tag is an element whose "value" attribute
// should be patched with PatchSetValue instead of PatchSetAttr. This preserves
// the cursor position and prevents infinite loops in two-way binding (spec 13.2).
func isValueElement(tag string) bool {
	t := strings.ToLower(tag)
	return t == "input" || t == "textarea" || t == "select"
}

// childPath builds a slash-separated path from the island root.
// The root node itself has path "". Children are indexed within their parent.
func childPath(parentPath string, childIdx int) string {
	if parentPath == "" {
		return fmt.Sprintf("%d", childIdx)
	}
	return fmt.Sprintf("%s/%d", parentPath, childIdx)
}

// ReconcileTrees diffs the previous and next resolved trees and returns patch ops.
// Uses staticMask to skip subtrees that cannot change.
func ReconcileTrees(prev, next *ResolvedTree, staticMask []bool) []PatchOp {
	if prev == nil || next == nil {
		return nil
	}
	if len(next.Nodes) == 0 || len(prev.Nodes) == 0 {
		return nil
	}
	var ops []PatchOp
	// Start from the root (index 0) with empty path
	reconcileNode(&ops, prev, next, 0, "", staticMask)
	return ops
}

// reconcileNode recursively diffs a single node and its subtree.
func reconcileNode(ops *[]PatchOp, prev, next *ResolvedTree, nodeIdx int, path string, staticMask []bool) {
	// Bounds check
	if nodeIdx >= len(prev.Nodes) || nodeIdx >= len(next.Nodes) {
		return
	}

	// 1. Static skip: if the compiler marked this subtree as static, skip it.
	if nodeIdx < len(staticMask) && staticMask[nodeIdx] {
		return
	}

	pn := &prev.Nodes[nodeIdx]
	nn := &next.Nodes[nodeIdx]

	// 2. Text / Expr nodes: compare resolved text.
	if pn.Text != nn.Text {
		*ops = append(*ops, PatchOp{
			Kind: PatchSetText,
			Path: path,
			Text: nn.Text,
		})
	}

	// 3. Attribute diff
	reconcileAttrs(ops, pn, nn, path)

	// 4. Children diff
	prevChildCount := len(pn.Children)
	nextChildCount := len(nn.Children)

	// Find the min count for pairwise comparison
	minCount := prevChildCount
	if nextChildCount < minCount {
		minCount = nextChildCount
	}

	// Recurse into matching children
	for i := 0; i < minCount; i++ {
		prevChildIdx := pn.Children[i]
		nextChildIdx := nn.Children[i]
		cp := childPath(path, i)
		// If both children reference the same node index, recurse normally.
		// If they reference different indices, we still reconcile them pairwise.
		if prevChildIdx == nextChildIdx {
			reconcileNode(ops, prev, next, nextChildIdx, cp, staticMask)
		} else {
			// Different node indices — reconcile the next child's node against the prev child's node.
			// This handles the case where the tree was restructured.
			reconcileCrossNode(ops, prev, next, prevChildIdx, nextChildIdx, cp, staticMask)
		}
	}

	// New children added
	for i := prevChildCount; i < nextChildCount; i++ {
		childIdx := nn.Children[i]
		cp := childPath(path, i)
		if childIdx < len(next.Nodes) {
			*ops = append(*ops, PatchOp{
				Kind: PatchCreateElement,
				Path: path,
				Tag:  next.Nodes[childIdx].Tag,
				Text: next.Nodes[childIdx].Text,
			})
			// Note: the bridge is responsible for creating children of the new element.
			// We still recurse to emit attribute patches, text, and nested children.
			emitCreateSubtree(ops, next, childIdx, cp, staticMask)
		}
	}

	// Children removed
	for i := nextChildCount; i < prevChildCount; i++ {
		cp := childPath(path, i)
		*ops = append(*ops, PatchOp{
			Kind: PatchRemoveElement,
			Path: cp,
		})
	}
}

// reconcileCrossNode diffs two nodes at different indices in the flat array.
func reconcileCrossNode(ops *[]PatchOp, prev, next *ResolvedTree, prevIdx, nextIdx int, path string, staticMask []bool) {
	if prevIdx >= len(prev.Nodes) || nextIdx >= len(next.Nodes) {
		return
	}

	// Check static mask for the next node index
	if nextIdx < len(staticMask) && staticMask[nextIdx] {
		return
	}

	pn := &prev.Nodes[prevIdx]
	nn := &next.Nodes[nextIdx]

	if pn.Text != nn.Text {
		*ops = append(*ops, PatchOp{
			Kind: PatchSetText,
			Path: path,
			Text: nn.Text,
		})
	}

	reconcileAttrs(ops, pn, nn, path)

	// Children
	prevChildCount := len(pn.Children)
	nextChildCount := len(nn.Children)
	minCount := prevChildCount
	if nextChildCount < minCount {
		minCount = nextChildCount
	}
	for i := 0; i < minCount; i++ {
		cp := childPath(path, i)
		reconcileCrossNode(ops, prev, next, pn.Children[i], nn.Children[i], cp, staticMask)
	}
	for i := prevChildCount; i < nextChildCount; i++ {
		childIdx := nn.Children[i]
		cp := childPath(path, i)
		if childIdx < len(next.Nodes) {
			*ops = append(*ops, PatchOp{
				Kind: PatchCreateElement,
				Path: path,
				Tag:  next.Nodes[childIdx].Tag,
				Text: next.Nodes[childIdx].Text,
			})
			emitCreateSubtree(ops, next, childIdx, cp, staticMask)
		}
	}
	for i := nextChildCount; i < prevChildCount; i++ {
		cp := childPath(path, i)
		*ops = append(*ops, PatchOp{
			Kind: PatchRemoveElement,
			Path: cp,
		})
	}
}

// reconcileAttrs diffs the attributes between a prev and next node.
func reconcileAttrs(ops *[]PatchOp, pn, nn *ResolvedNode, path string) {
	valueElem := isValueElement(nn.Tag)

	// Build a map from prev attrs for lookup
	prevAttrs := make(map[string]string, len(pn.Attrs))
	for _, a := range pn.Attrs {
		prevAttrs[a.Name] = a.Value
	}

	// Check next attrs: new or changed
	nextAttrs := make(map[string]struct{}, len(nn.Attrs))
	for _, a := range nn.Attrs {
		nextAttrs[a.Name] = struct{}{}
		prevVal, existed := prevAttrs[a.Name]
		if existed && prevVal == a.Value {
			continue // unchanged
		}
		// For value elements, the "value" attr uses PatchSetValue
		if valueElem && a.Name == "value" {
			// Spec 13.2: if the value hasn't changed, do NOT emit (prevents infinite loop)
			if existed && prevVal == a.Value {
				continue
			}
			*ops = append(*ops, PatchOp{
				Kind:     PatchSetValue,
				Path:     path,
				Text:     a.Value,
				AttrName: "value",
			})
			continue
		}
		*ops = append(*ops, PatchOp{
			Kind:     PatchSetAttr,
			Path:     path,
			AttrName: a.Name,
			Text:     a.Value,
		})
	}

	// Check removed attrs (in prev but not in next)
	for _, a := range pn.Attrs {
		if _, ok := nextAttrs[a.Name]; !ok {
			*ops = append(*ops, PatchOp{
				Kind:     PatchRemoveAttr,
				Path:     path,
				AttrName: a.Name,
			})
		}
	}
}

// emitCreateSubtree emits attribute and text patches for a newly created subtree.
// The PatchCreateElement for the root of the subtree is already emitted by the caller.
func emitCreateSubtree(ops *[]PatchOp, tree *ResolvedTree, nodeIdx int, path string, staticMask []bool) {
	if nodeIdx >= len(tree.Nodes) {
		return
	}
	if nodeIdx < len(staticMask) && staticMask[nodeIdx] {
		return
	}
	node := &tree.Nodes[nodeIdx]
	// Recurse into children of the new element
	for i, childIdx := range node.Children {
		cp := childPath(path, i)
		if childIdx < len(tree.Nodes) {
			child := &tree.Nodes[childIdx]
			*ops = append(*ops, PatchOp{
				Kind: PatchCreateElement,
				Path: path,
				Tag:  child.Tag,
				Text: child.Text,
			})
			emitCreateSubtree(ops, tree, childIdx, cp, staticMask)
		}
	}
}
