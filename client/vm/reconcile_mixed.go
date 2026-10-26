package vm

import (
	"strconv"
	"strings"
)

// Mixed lists retain explicit keys without assigning identity to arbitrary
// unkeyed insertions. Match unkeyed anchors only when their source/tag sequence
// is unchanged; structural changes use the established positional fallback.
// Internal key domains distinguish user keys from source-order anchors.
func mixedChildKeys(prev, next *ResolvedTree, pn, nn *ResolvedNode) ([]string, []string, bool) {
	i, j := 0, 0
	keyed := false
	leadingZeros := 0
	for {
		for i < len(pn.Children) {
			n := resolvedNodeAt(prev, pn.Children[i])
			if n == nil {
				return nil, nil, false
			}
			if n.Key == "" {
				break
			}
			keyed = true
			leadingZeros = max(leadingZeros, keyLeadingZeros(n.Key))
			i++
		}
		for j < len(nn.Children) {
			n := resolvedNodeAt(next, nn.Children[j])
			if n == nil {
				return nil, nil, false
			}
			if n.Key == "" {
				break
			}
			keyed = true
			leadingZeros = max(leadingZeros, keyLeadingZeros(n.Key))
			j++
		}
		if i == len(pn.Children) || j == len(nn.Children) {
			if i != len(pn.Children) || j != len(nn.Children) || !keyed {
				return nil, nil, false
			}
			// This prefix cannot begin any user key, including binary strings.
			// Reuse user strings directly instead of allocating one per row.
			prefix := strings.Repeat("\x00", leadingZeros+1)
			return mixedChildKeyList(prev, pn, prefix), mixedChildKeyList(next, nn, prefix), true
		}
		left, right := resolvedNodeAt(prev, pn.Children[i]), resolvedNodeAt(next, nn.Children[j])
		if !left.HasSource || !right.HasSource || left.Source != right.Source || left.Tag != right.Tag {
			return nil, nil, false
		}
		i++
		j++
	}
}

func keyLeadingZeros(key string) int {
	i := 0
	for i < len(key) && key[i] == 0 {
		i++
	}
	return i
}

func mixedChildKeyList(tree *ResolvedTree, node *ResolvedNode, prefix string) []string {
	keys := make([]string, len(node.Children))
	anchor := 0
	for i, child := range node.Children {
		key := tree.Nodes[child].Key
		if key != "" {
			keys[i] = key
		} else {
			keys[i] = prefix + strconv.Itoa(anchor)
			anchor++
		}
	}
	return keys
}

func childReconcileKey(child *ResolvedNode, index int, keys []string) string {
	if keys != nil {
		return keys[index]
	}
	if child != nil {
		return child.Key
	}
	return ""
}
