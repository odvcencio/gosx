package gosx

import "m31labs.dev/gosx/markup"

// The node tree and its HTML renderer are defined in package markup, which
// has no compiler dependency, so browser (js/wasm) code can build and render
// nodes without linking the .gsx compiler. This file re-exports them under
// their long-standing names; a value built through either package is the
// same value.

// Node is the runtime representation of a GoSX component tree node.
// Components are Go functions that return Node values. It is an alias of
// markup.Node.
type Node = markup.Node

// AttrList is a list of attributes for element construction. It is an alias
// of markup.AttrList.
type AttrList = markup.AttrList

// El creates an element node. The variadic args can be AttrList or Node children.
func El(tag string, args ...any) Node { return markup.El(tag, args...) }

// Text creates a static text node.
func Text(s string) Node { return markup.Text(s) }

// Expr creates an expression node from a value. A Node is inlined and a
// []Node becomes a Fragment; any other value renders as its fmt.Sprint text.
// See markup.Expr.
func Expr(v any) Node { return markup.Expr(v) }

// Fragment creates a fragment node containing children.
func Fragment(children ...Node) Node { return markup.Fragment(children...) }

// RawHTML creates a node that renders pre-escaped HTML.
func RawHTML(s string) Node { return markup.RawHTML(s) }

// Attrs creates an attribute list from key-value pairs.
func Attrs(pairs ...any) AttrList { return markup.Attrs(pairs...) }

// Attr creates a single attribute.
func Attr(name string, value any) markup.Attribute { return markup.Attr(name, value) }

// DispatchAttr explicitly binds a trusted handler to a DOM event in handwritten
// RenderIsland content. Use event types such as "click", "input", or
// "document-keydown". Unlike Attr and Spread, this constructor can emit
// framework dispatch metadata.
func DispatchAttr(event, handler string) markup.Attribute {
	return markup.DispatchAttr(event, handler)
}

// BoolAttr creates a boolean attribute (e.g., disabled, checked).
func BoolAttr(name string) markup.Attribute { return markup.BoolAttr(name) }

// Spread merges attributes from a map.
func Spread(attrs map[string]any) AttrList { return markup.Spread(attrs) }

// RenderAttrs renders an attribute list the same way El renders an element's
// own attributes. See markup.RenderAttrs.
func RenderAttrs(attrs AttrList) string { return markup.RenderAttrs(attrs) }

// Props is an alias for Attrs used when passing props to components.
func Props(pairs ...any) AttrList { return markup.Props(pairs...) }

// RenderHTML renders a Node tree to an HTML string.
// This is the server-side rendering entry point.
func RenderHTML(n Node) string { return markup.RenderHTML(n) }

// PlainText walks a node tree and returns the concatenated text content.
// RawHTML nodes are ignored because their text may include markup.
func PlainText(n Node) string { return markup.PlainText(n) }
