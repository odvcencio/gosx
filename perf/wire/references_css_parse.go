package wire

import (
	"bytes"
	"unicode/utf8"

	ts "github.com/odvcencio/gotreesitter"
)

func firstInvalidCSSByte(body []byte) int64 {
	for offset := 0; offset < len(body); {
		r, size := utf8.DecodeRune(body[offset:])
		if r == utf8.RuneError && size == 1 {
			return int64(offset)
		}
		offset += size
	}
	return 0
}

func (out *referenceScanner) cssParse(offset int64) {
	out.drop(dropCSSParse)
	label := out.sourceLabel
	if label == "" {
		label = "stylesheet"
	}
	out.Drops = append(out.Drops, ReferenceDrop{Reason: "css-parse", File: label, Offset: offset})
}

// The pinned CSS parser can recover a rule's block while rejecting nested
// pseudo-class selector arguments. Reparse only those preludes as opaque text;
// declarations, at-rules, strings and their byte positions remain untouched.
// Coverage stays incomplete even when the projected declaration tree is clean.
// The incoming tree is always released; callers own the returned tree.
func recoverCSSReferences(tree *ts.Tree, lang *ts.Language, body []byte, out *referenceScanner) (*ts.Tree, error) {
	defer tree.Release()
	type span struct{ start, end uint32 }
	type pending struct {
		node    *ts.Node
		depth   int
		prelude span
	}
	stack := []pending{{node: tree.RootNode()}}
	preludes := map[span]bool{}
	first := int64(len(body))
	errors, recoverable, nodes := 0, true, 0
	for len(stack) > 0 {
		entry := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		nodes++
		if nodes > maxReferenceASTNodes {
			return nil, referenceLimit("ast-nodes", maxReferenceASTNodes)
		}
		if entry.depth > maxReferenceDepth {
			return nil, referenceLimit("ast-depth", maxReferenceDepth)
		}
		n := entry.node
		if n.Type(lang) == "rule_set" {
			entry.prelude = span{}
			for i := 0; i < n.NamedChildCount(); i++ {
				child := n.NamedChild(i)
				if child.Type(lang) == "block" {
					entry.prelude = span{n.StartByte(), child.StartByte()}
					break
				}
			}
		}
		if n.Type(lang) == "ERROR" || n.IsMissing() {
			errors++
			first = min(first, int64(n.StartByte()))
			if entry.prelude.end > entry.prelude.start && n.StartByte() >= entry.prelude.start && n.EndByte() <= entry.prelude.end {
				preludes[entry.prelude] = true
			} else {
				recoverable = false
			}
		}
		for i := n.NamedChildCount() - 1; i >= 0; i-- {
			stack = append(stack, pending{n.NamedChild(i), entry.depth + 1, entry.prelude})
		}
	}
	if errors == 0 {
		first = 0
		recoverable = false
	}
	out.cssParse(first)
	if !recoverable {
		return nil, nil
	}
	projected := bytes.Clone(body)
	for prelude := range preludes {
		for i := prelude.start; i < prelude.end; i++ {
			if projected[i] != '\n' && projected[i] != '\r' {
				projected[i] = ' '
			}
		}
		projected[prelude.start] = 'x'
	}
	recovered, err := parseReferenceSyntax(ts.NewParser(lang), projected)
	if err != nil {
		out.acceptLimit(err)
		return nil, nil
	}
	if recovered.RootNode().HasErrorOrMissing() {
		recovered.Release()
		return nil, nil
	}
	return recovered, nil
}
