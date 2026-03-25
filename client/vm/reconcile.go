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

	// Walk children. Every child gets a DOM index (elemIdx) because in the DOM,
	// both element children and text nodes are addressable via childNodes[].
	elemIdx := 0
	prevLen := len(pn.Children)
	nextLen := len(nn.Children)
	maxLen := prevLen
	if nextLen > maxLen {
		maxLen = nextLen
	}

	for i := 0; i < maxLen; i++ {
		if i >= nextLen {
			*ops = append(*ops, PatchOp{Kind: PatchRemoveElement, Path: childPath(path, elemIdx)})
			elemIdx++
			continue
		}
		if i >= prevLen {
			childIdx := nn.Children[i]
			if childIdx < len(next.Nodes) {
				cn := &next.Nodes[childIdx]
				if cn.Tag != "" {
					*ops = append(*ops, PatchOp{
						Kind: PatchCreateElement,
						Path: path,
						Tag:  cn.Tag,
						Text: cn.Text,
					})
				}
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

		nextChild := &next.Nodes[nextChildIdx]
		prevChild := &prev.Nodes[prevChildIdx]

		if nextChild.Tag != "" {
			// Element child — recurse
			cp := childPath(path, elemIdx)
			reconcileNode(ops, prev, next, nextChildIdx, cp, staticMask)
		} else {
			// Text/Expr child — compare and emit SetText at this child's DOM index
			if prevChild.Text != nextChild.Text {
				*ops = append(*ops, PatchOp{
					Kind: PatchSetText,
					Path: childPath(path, elemIdx),
					Text: nextChild.Text,
				})
			}
		}
		elemIdx++
	}
}

// reconcileAttrs diffs the attributes between a prev and next node.
func reconcileAttrs(ops *[]PatchOp, pn, nn *ResolvedNode, path string) {
	valueElem := isValueElement(nn.Tag)

	prevAttrs := make(map[string]string, len(pn.Attrs))
	for _, a := range pn.Attrs {
		prevAttrs[a.Name] = a.Value
	}

	nextAttrs := make(map[string]struct{}, len(nn.Attrs))
	for _, a := range nn.Attrs {
		nextAttrs[a.Name] = struct{}{}
		prevVal, existed := prevAttrs[a.Name]
		if existed && prevVal == a.Value {
			continue
		}
		if valueElem && a.Name == "value" {
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
