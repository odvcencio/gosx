// Package syntax contains source-level GSX validation shared by every GoSX
// source consumer. It deliberately depends only on the parser tree and source
// bytes, so the compiler, formatter, transpiler, and editor cannot grow
// subtly different recovered-tree rules.
package syntax

import (
	"fmt"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ValidateTree rejects a recovered or structurally inconsistent GSX tree.
//
// Tree-sitter's error flag is necessary but not sufficient for GSX: the
// external text scanner can leave an otherwise clean tree containing an
// opening and closing tag owned by different recovered elements. We collect
// the ordered markup events from the CST and validate them as one stack. The
// source spans remain the authority for diagnostics; no formatter-specific
// reconstruction is involved.
func ValidateTree(root *gotreesitter.Node, source []byte, lang *gotreesitter.Language) error {
	if root == nil || lang == nil {
		return nil
	}
	if root.HasError() {
		if problem := firstParseProblem(root); problem != nil {
			kind := problem.Type(lang)
			if problem.IsMissing() {
				return locatedError(problem, source, fmt.Sprintf("recovered syntax tree contains missing %s", kind))
			}
			return locatedError(problem, source, fmt.Sprintf("recovered syntax tree contains %s", kind))
		}
		return locatedError(root, source, "recovered syntax tree contains an error")
	}

	events := make([]markupEvent, 0, 16)
	var walk func(*gotreesitter.Node) error
	walk = func(n *gotreesitter.Node) error {
		if n == nil {
			return nil
		}
		typ := n.Type(lang)
		switch typ {
		case "jsx_opening_element":
			if name := fieldText(n, "name", source, lang); name != "" {
				events = append(events, markupEvent{kind: eventOpen, name: name, node: n})
			}
		case "jsx_closing_element":
			if name := fieldText(n, "name", source, lang); name != "" {
				events = append(events, markupEvent{kind: eventClose, name: name, node: n})
			}
		case "jsx_self_closing_element":
			// Self-closing elements do not participate in the stack. Walking
			// their children still finds expression-contained GSX, if any.
		case "jsx_raw_text_element":
			// The raw scanner folds the matching close tag into the raw-body
			// token. Emit an explicit event for that close so raw and ordinary
			// tags share the same validation path.
			open := n.ChildByFieldName("open", lang)
			if open == nil {
				return locatedError(n, source, "raw-text element is missing its opening tag")
			}
			name := rawTagName(fieldText(open, "name", source, lang))
			if name == "" {
				return locatedError(open, source, "raw-text element has no tag name")
			}
			events = append(events, markupEvent{kind: eventOpen, name: name, node: open})
			// Raw elements with an expression child own an explicit ordinary
			// close node. The generic walk below will collect that close. A raw
			// body token owns its close, so append its event at node end after
			// walking the opening tag and body.
			body := n.ChildByFieldName("children", lang)
			if body != nil && strings.HasPrefix(body.Type(lang), "jsx_") && body.Type(lang) != "jsx_expression_container" {
				if closeName, ok := rawBodyCloseName(text(body, source), name); ok {
					events = append(events, markupEvent{kind: eventRawClose, name: closeName, node: body})
				} else {
					return locatedError(n, source, fmt.Sprintf("raw-text element is missing closing </%s>", name))
				}
			}
		}
		for i := 0; i < n.ChildCount(); i++ {
			if err := walk(n.Child(i)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return err
	}

	// Re-run the inexpensive raw-body check so a malformed body is diagnosed
	// even when the scanner produced a partially recovered node.
	if err := validateRawBodies(root, source, lang); err != nil {
		return err
	}

	// CST traversal is depth-first, not source order: a parent element is
	// visited before its descendants. Sorting by span gives the actual lexical
	// stream. Raw-body closes use the body end, which is after all content and
	// before any following sibling.
	sortEvents(events)
	stack := make([]markupEvent, 0, len(events))
	for _, event := range events {
		switch event.kind {
		case eventOpen:
			stack = append(stack, event)
		case eventClose, eventRawClose:
			if len(stack) == 0 {
				return locatedError(event.node, source, fmt.Sprintf("unexpected closing tag </%s>", event.name))
			}
			top := stack[len(stack)-1]
			// HTML raw-text element names are case-insensitive, including the
			// explicit close used by an expression-bodied raw element. Ordinary
			// GSX/component tags remain case-sensitive by design.
			matched := top.name == event.name
			if top.name == "script" || top.name == "style" {
				matched = strings.EqualFold(top.name, event.name)
			}
			if !matched {
				return locatedError(event.node, source, fmt.Sprintf("mismatched closing tag: expected </%s>, got </%s>", top.name, event.name))
			}
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) > 0 {
		top := stack[len(stack)-1]
		return locatedError(top.node, source, fmt.Sprintf("missing closing tag for <%s>", top.name))
	}
	return nil
}

func firstParseProblem(n *gotreesitter.Node) *gotreesitter.Node {
	if n == nil {
		return nil
	}
	if n.IsError() || n.IsMissing() {
		return n
	}
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		if child == nil || !child.HasError() && !child.IsError() && !child.IsMissing() {
			continue
		}
		if problem := firstParseProblem(child); problem != nil {
			return problem
		}
	}
	return nil
}

type eventKind uint8

const (
	eventOpen eventKind = iota
	eventClose
	eventRawClose
)

type markupEvent struct {
	kind       eventKind
	name       string
	node       *gotreesitter.Node
	start, end uint32
}

func sortEvents(events []markupEvent) {
	for i := range events {
		events[i].start = events[i].node.StartByte()
		events[i].end = events[i].node.EndByte()
	}
	// Stable insertion sort keeps duplicate source positions deterministic.
	for i := 1; i < len(events); i++ {
		v := events[i]
		j := i
		for j > 0 && (events[j-1].start > v.start || events[j-1].start == v.start && events[j-1].end > v.end) {
			events[j] = events[j-1]
			j--
		}
		events[j] = v
	}
}

func fieldText(n *gotreesitter.Node, field string, source []byte, lang *gotreesitter.Language) string {
	if n == nil {
		return ""
	}
	child := n.ChildByFieldName(field, lang)
	if child == nil || child.StartByte() > child.EndByte() || int(child.EndByte()) > len(source) {
		return ""
	}
	return string(source[child.StartByte():child.EndByte()])
}

func text(n *gotreesitter.Node, source []byte) string {
	if n == nil || n.StartByte() > n.EndByte() || int(n.EndByte()) > len(source) {
		return ""
	}
	return string(source[n.StartByte():n.EndByte()])
}

func rawTagName(name string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "<"))
}

func rawBodyCloseName(raw, expected string) (string, bool) {
	idx := strings.LastIndex(strings.ToLower(raw), "</")
	if idx < 0 {
		return "", false
	}
	tail := raw[idx+2:]
	tail = strings.TrimLeft(tail, " \t\r\n")
	end := 0
	for end < len(tail) {
		c := tail[end]
		if !isASCIIAlpha(c) {
			break
		}
		end++
	}
	if end == 0 || !strings.EqualFold(tail[:end], expected) {
		return "", false
	}
	closeTail := strings.TrimSpace(tail[end:])
	if closeTail != ">" {
		return "", false
	}
	return strings.ToLower(tail[:end]), true
}

func isASCIIAlpha(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func validateRawBodies(root *gotreesitter.Node, source []byte, lang *gotreesitter.Language) error {
	var walk func(*gotreesitter.Node) error
	walk = func(n *gotreesitter.Node) error {
		if n == nil {
			return nil
		}
		if n.Type(lang) == "jsx_raw_text_element" {
			open := n.ChildByFieldName("open", lang)
			if open == nil {
				return locatedError(n, source, "raw-text element is missing its opening tag")
			}
			name := rawTagName(fieldText(open, "name", source, lang))
			if name == "" {
				return locatedError(open, source, "raw-text element has no tag name")
			}
			body := n.ChildByFieldName("children", lang)
			if body != nil && body.Type(lang) != "jsx_expression_container" {
				if _, ok := rawBodyCloseName(text(body, source), name); !ok {
					return locatedError(n, source, fmt.Sprintf("raw-text element has no matching closing </%s>", name))
				}
			}
		}
		for i := 0; i < n.ChildCount(); i++ {
			if err := walk(n.Child(i)); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root)
}

func locatedError(n *gotreesitter.Node, source []byte, message string) error {
	if n == nil {
		return fmt.Errorf("1:1: %s", message)
	}
	point := n.StartPoint()
	line := int(point.Row) + 1
	column := int(point.Column) + 1
	lines := strings.Split(string(source), "\n")
	snippet := ""
	if int(point.Row) < len(lines) {
		snippet = strings.TrimRight(lines[point.Row], "\r")
	}
	return &LocatedError{Line: line, Column: column, Message: message, Snippet: snippet}
}

// LocatedError is intentionally compatible with gosx.ParseError while living
// below the root package to keep the shared syntax layer acyclic.
type LocatedError struct {
	Line, Column int
	Message      string
	Snippet      string
}

func (e *LocatedError) Error() string {
	if e.Snippet == "" {
		return fmt.Sprintf("%d:%d: %s", e.Line, e.Column, e.Message)
	}
	return fmt.Sprintf("%d:%d: %s\n    %s", e.Line, e.Column, e.Message, e.Snippet)
}
