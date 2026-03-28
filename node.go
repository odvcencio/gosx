package gosx

import (
	"fmt"
	"html"
	"strings"
)

// Node is the runtime representation of a GoSX component tree node.
// Components are Go functions that return Node values.
type Node struct {
	kind     nodeKind
	tag      string
	text     string
	attrs    []nodeAttr
	children []Node
}

type nodeKind uint8

const (
	kindElement nodeKind = iota
	kindText
	kindExpr
	kindFragment
	kindRawHTML
)

type nodeAttr struct {
	name  string
	value any
}

// El creates an element node. The variadic args can be AttrList or Node children.
func El(tag string, args ...any) Node {
	n := Node{kind: kindElement, tag: tag}
	for _, arg := range args {
		switch v := arg.(type) {
		case AttrList:
			for _, a := range v {
				n.attrs = append(n.attrs, a)
			}
		case Node:
			n.children = append(n.children, v)
		}
	}
	return n
}

// Text creates a static text node.
func Text(s string) Node {
	return Node{kind: kindText, text: s}
}

// Expr creates an expression node from a value.
func Expr(v any) Node {
	return Node{kind: kindExpr, text: fmt.Sprint(v)}
}

// Fragment creates a fragment node containing children.
func Fragment(children ...Node) Node {
	return Node{kind: kindFragment, children: children}
}

// RawHTML creates a node that renders pre-escaped HTML.
func RawHTML(s string) Node {
	return Node{kind: kindRawHTML, text: s}
}

// AttrList is a list of attributes for element construction.
type AttrList []nodeAttr

// Attrs creates an attribute list from key-value pairs.
func Attrs(pairs ...any) AttrList {
	var attrs AttrList
	for _, p := range pairs {
		switch v := p.(type) {
		case nodeAttr:
			attrs = append(attrs, v)
		}
	}
	return attrs
}

// Attr creates a single attribute.
func Attr(name string, value any) nodeAttr {
	return nodeAttr{name: name, value: value}
}

// BoolAttr creates a boolean attribute (e.g., disabled, checked).
func BoolAttr(name string) nodeAttr {
	return nodeAttr{name: name, value: true}
}

// Spread merges attributes from a map.
func Spread(attrs map[string]any) AttrList {
	var list AttrList
	for k, v := range attrs {
		list = append(list, nodeAttr{name: k, value: v})
	}
	return list
}

// Props is an alias for Attrs used when passing props to components.
func Props(pairs ...any) AttrList {
	return Attrs(pairs...)
}

// RenderHTML renders a Node tree to an HTML string.
// This is the server-side rendering entry point.
func RenderHTML(n Node) string {
	var b strings.Builder
	renderNodeHTML(&b, n)
	return b.String()
}

// PlainText walks a node tree and returns the concatenated text content.
// RawHTML nodes are ignored because their text may include markup.
func PlainText(n Node) string {
	var b strings.Builder
	writePlainText(&b, n)
	return b.String()
}

func renderNodeHTML(b *strings.Builder, n Node) {
	switch n.kind {
	case kindElement:
		safeTag := html.EscapeString(n.tag)
		b.WriteByte('<')
		b.WriteString(safeTag)
		for _, attr := range n.attrs {
			renderAttrHTML(b, attr)
		}
		if isVoidElement(n.tag) && len(n.children) == 0 {
			b.WriteString(" />")
			return
		}
		b.WriteByte('>')
		for _, child := range n.children {
			renderNodeHTML(b, child)
		}
		b.WriteString("</")
		b.WriteString(safeTag)
		b.WriteByte('>')

	case kindText:
		b.WriteString(html.EscapeString(n.text))

	case kindExpr:
		b.WriteString(html.EscapeString(n.text))

	case kindFragment:
		for _, child := range n.children {
			renderNodeHTML(b, child)
		}

	case kindRawHTML:
		b.WriteString(n.text)
	}
}

func renderAttrHTML(b *strings.Builder, attr nodeAttr) {
	safeName := html.EscapeString(attr.name)
	switch v := attr.value.(type) {
	case bool:
		if v {
			b.WriteByte(' ')
			b.WriteString(safeName)
		}
	case string:
		fmt.Fprintf(b, ` %s="%s"`, safeName, html.EscapeString(v))
	default:
		fmt.Fprintf(b, ` %s="%s"`, safeName, html.EscapeString(fmt.Sprint(v)))
	}
}

func writePlainText(b *strings.Builder, n Node) {
	switch n.kind {
	case kindText, kindExpr:
		b.WriteString(n.text)
	case kindElement, kindFragment:
		for _, child := range n.children {
			writePlainText(b, child)
		}
	}
}

// IsZero returns true if the node is the zero value (uninitialized).
func (n Node) IsZero() bool {
	return n.kind == 0 && n.tag == "" && n.text == "" && n.attrs == nil && n.children == nil
}

func isVoidElement(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}
