//go:build !tinygo

package gosx

import (
	"fmt"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// validateMarkup uses CST tags, including tags left outside declarations by
// parser recovery. Strings, comments, Go comparisons and raw-text bodies are
// not tags. The grammar alone cannot compare opening and closing names.
func validateMarkup(root *gotreesitter.Node, source []byte, lang *gotreesitter.Language) error {
	type opening struct {
		name  string
		point gotreesitter.Point
		end   uint32
	}
	var stack []opening
	closeTag := func(name string, point gotreesitter.Point, raw bool) error {
		if len(stack) == 0 {
			return markupError(point, source, fmt.Sprintf("unexpected closing tag </%s>", name), "remove the closing tag or add its opening tag")
		}
		open := stack[len(stack)-1]
		matches := open.name == name || raw && strings.EqualFold(open.name, name)
		if !matches {
			return markupError(point, source, fmt.Sprintf("mismatched closing tag </%s>; expected </%s> for <%s> opened at %d:%d", name, open.name, open.name, open.point.Row+1, open.point.Column+1), fmt.Sprintf("replace </%s> with </%s>, or close <%s> before this tag", name, open.name, open.name))
		}
		stack = stack[:len(stack)-1]
		return nil
	}
	var walk func(*gotreesitter.Node) error
	walk = func(n *gotreesitter.Node) error {
		if n == nil {
			return nil
		}
		switch n.Type(lang) {
		case "jsx_opening_element", "jsx_raw_opening_element":
			// Recovery can label a closing tag as an opening element with
			// an ERROR child for '/'. Leave that syntax error to the parser.
			if strings.HasPrefix(n.Text(source), "</") {
				return nil
			}
			name := n.ChildByFieldName("name", lang)
			if name != nil {
				stack = append(stack, opening{strings.TrimPrefix(name.Text(source), "<"), n.StartPoint(), n.EndByte()})
			}
			return nil
		case "jsx_closing_element":
			name := n.ChildByFieldName("name", lang)
			if name != nil && !n.IsMissing() {
				return closeTag(name.Text(source), n.StartPoint(), false)
			}
			return nil
		case "jsx_raw_text":
			// The scanner includes the raw element's closing tag in this token.
			text := n.Text(source)
			if at := strings.LastIndex(text, "</"); at >= 0 {
				name := strings.TrimSpace(strings.TrimSuffix(text[at+2:], ">"))
				prefix := source[:int(n.StartByte())+at]
				point := gotreesitter.Point{Row: uint32(strings.Count(string(prefix), "\n")), Column: uint32(len(prefix) - strings.LastIndex(string(prefix), "\n") - 1)}
				return closeTag(name, point, true)
			}
			return nil
		}
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			// Fragments have anonymous punctuation rather than named tag nodes.
			// This also catches fragment punctuation retained by recovery.
			if child.Type(lang) == "<" && i+1 < n.ChildCount() {
				next := n.Child(i + 1).Type(lang)
				if next == ">" {
					stack = append(stack, opening{"", child.StartPoint(), n.Child(i + 1).EndByte()})
					i++
					continue
				}
				if next == "/" && i+2 < n.ChildCount() && n.Child(i+2).Type(lang) == ">" {
					if err := closeTag("", child.StartPoint(), false); err != nil {
						return err
					}
					i += 2
					continue
				}
			}
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return err
	}
	if len(stack) > 0 {
		open := stack[len(stack)-1]
		// An invalid expression can hide a present closing tag from recovery.
		// Preserve the syntax diagnostic instead of calling that tag unclosed.
		if strings.Contains(string(source[open.end:]), "</"+open.name+">") {
			if err := DescribeParseError(root, source, lang); err != nil {
				return err
			}
			return markupError(open.point, source, fmt.Sprintf("unexpected syntax inside <%s>; could not parse its closing tag", open.name), "check expressions and nested tags before </"+open.name+">")
		}
		return markupError(open.point, source, fmt.Sprintf("unclosed tag <%s>; expected </%s>", open.name, open.name), fmt.Sprintf("add </%s> before the enclosing tag or the end of the return expression", open.name))
	}
	return nil
}

func markupError(point gotreesitter.Point, source []byte, message, hint string) *ParseError {
	return &ParseError{Line: int(point.Row) + 1, Column: int(point.Column) + 1, Message: message, Snippet: sourceLine(source, point.Row), Hint: hint}
}

// DescribeMissingComponents reports markup in a file where the component
// checker found no components. Transpilation also supports markup in ordinary
// Go declarations, so this additional check belongs to gosx check.
func DescribeMissingComponents(root *gotreesitter.Node, source []byte, lang *gotreesitter.Language) error {
	if markup := firstMarkupNode(root, lang); markup != nil {
		return markupError(markup.StartPoint(), source, "markup produced zero components", "put markup in a component with an explicit return")
	}
	return nil
}

func firstMarkupNode(n *gotreesitter.Node, lang *gotreesitter.Language) *gotreesitter.Node {
	if n == nil {
		return nil
	}
	switch n.Type(lang) {
	case "jsx_element", "jsx_self_closing_element", "jsx_fragment", "jsx_opening_element", "jsx_raw_text_element", "jsx_raw_opening_element":
		return n
	}
	for i := 0; i < n.NamedChildCount(); i++ {
		if found := firstMarkupNode(n.NamedChild(i), lang); found != nil {
			return found
		}
	}
	return nil
}
