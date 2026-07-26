//go:build !tinygo

package gosx

import (
	"fmt"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ParseError describes a syntax problem in GoSX source.
type ParseError struct {
	Line    int
	Column  int
	Message string
	Snippet string
}

func (e *ParseError) Error() string {
	if e == nil {
		return ""
	}
	if e.Snippet == "" {
		return fmt.Sprintf("%d:%d: %s", e.Line, e.Column, e.Message)
	}
	return fmt.Sprintf("%d:%d: %s\n    %s\n    %s^", e.Line, e.Column, e.Message, e.Snippet, caretPadding(e.Snippet, e.Column))
}

// requirePackageClause rejects a source file that does not open with a
// package clause.
//
// Go requires a package clause in every file. The grammar does not, and the
// parser reports no error node for a file without one. Text such as
// "not a valid gsx file" lexes as a run of bare identifiers and yields a clean
// source_file. Callers read a compile error as the signal that input is bad,
// so check the structure here.
//
// gotreesitter made this check necessary at v0.35.0. Before that release the
// Go grammar approximated automatic semicolon insertion inside the lexer DFA,
// and the approximation produced an error node on such input by accident. The
// external _automatic_semicolon scanner that replaced it is byte-strict and
// accepts the same input.
func requirePackageClause(root *gotreesitter.Node, lang *gotreesitter.Language) error {
	if root == nil || lang == nil {
		return nil
	}
	if root.Type(lang) != "source_file" {
		return nil
	}
	for i := 0; i < root.NamedChildCount(); i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Type(lang) {
		case "comment":
			// Comments are extras. Skip them and keep looking.
			continue
		case "package_clause":
			return nil
		default:
			return &ParseError{
				Line:    int(child.StartPoint().Row) + 1,
				Column:  int(child.StartPoint().Column) + 1,
				Message: "expected a package clause before " + child.Type(lang),
			}
		}
	}
	// A file of only comments or whitespace has no package clause either.
	if root.NamedChildCount() > 0 || root.EndByte() > root.StartByte() {
		return &ParseError{
			Line:    1,
			Column:  1,
			Message: "expected a package clause",
		}
	}
	return nil
}

// DescribeParseError returns the first syntax error in a parse tree, if any.
func DescribeParseError(root *gotreesitter.Node, source []byte, lang *gotreesitter.Language) error {
	if root == nil || !root.HasError() {
		return nil
	}

	node := firstParseProblem(root)
	if node == nil {
		return fmt.Errorf("parse error in source")
	}

	point := node.StartPoint()
	line := int(point.Row) + 1
	column := int(point.Column) + 1
	nodeType := node.Type(lang)
	text := strings.TrimSpace(node.Text(source))
	snippet := sourceLine(source, point.Row)

	if shouldApproximateParsePoint(node, text) {
		line, column, snippet, text = approximateParsePoint(text, source)
	}

	switch {
	case node.IsMissing():
		return &ParseError{
			Line:    line,
			Column:  column,
			Message: fmt.Sprintf("missing %s", nodeType),
			Snippet: snippet,
		}
	case node.IsError():
		msg := "unexpected syntax"
		if text != "" {
			msg = fmt.Sprintf("unexpected syntax near %q", text)
		} else if nodeType != "" {
			msg = fmt.Sprintf("unexpected %s", nodeType)
		}
		return &ParseError{
			Line:    line,
			Column:  column,
			Message: msg,
			Snippet: snippet,
		}
	default:
		return &ParseError{
			Line:    line,
			Column:  column,
			Message: "parse error in source",
			Snippet: snippet,
		}
	}
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

func sourceLine(source []byte, row uint32) string {
	lines := strings.Split(string(source), "\n")
	if int(row) >= len(lines) {
		return ""
	}
	return strings.TrimRight(lines[row], "\r")
}

func shouldApproximateParsePoint(node *gotreesitter.Node, text string) bool {
	if node == nil {
		return false
	}
	point := node.StartPoint()
	return node.IsError() &&
		point.Row == 0 &&
		point.Column == 0 &&
		node.ChildCount() > 0 &&
		strings.Contains(text, "\n")
}

func approximateParsePoint(text string, source []byte) (line int, column int, snippet string, excerpt string) {
	lines := strings.Split(strings.TrimRight(text, "\r\n\t "), "\n")
	if len(lines) == 0 {
		return 1, 1, sourceLine(source, 0), ""
	}

	target := len(lines) - 1
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		if trimmed == "}" || trimmed == "{" || trimmed == ")" || trimmed == "]" {
			continue
		}
		target = i
		break
	}

	snippet = sourceLine(source, uint32(target))
	if snippet == "" {
		snippet = strings.TrimRight(lines[target], "\r")
	}
	trimmedSnippet := strings.TrimRight(snippet, "\r")
	trimmedLine := strings.TrimRight(lines[target], "\r")
	trimmedLine = strings.TrimRight(trimmedLine, " \t")
	column = len([]rune(trimmedLine))
	if column == 0 {
		column = 1
	}

	excerpt = strings.TrimSpace(lines[target])
	if excerpt == "" {
		excerpt = strings.TrimSpace(trimmedSnippet)
	}

	return target + 1, column, trimmedSnippet, excerpt
}

func caretPadding(snippet string, column int) string {
	if column <= 1 || snippet == "" {
		return ""
	}
	width := 0
	for _, r := range snippet {
		if width >= column-1 {
			break
		}
		if r == '\t' {
			width += 4
			continue
		}
		width++
	}
	if width <= 0 {
		return ""
	}
	return strings.Repeat(" ", width)
}
