package gosx

import (
	"fmt"
	"html"
	"strings"

	"m31labs.dev/gosx/internal/htmlattr"
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
	name     string
	value    any
	presence bool
}

// El creates an element node. The variadic args can be AttrList or Node children.
//
// The children slice is pre-sized to len(args) which is a safe upper bound
// (any AttrList entries will leave a few unused slots but that's cheaper
// than growing the slice 2-3× during construction). When the first AttrList
// is seen and n.attrs is still nil we alias it directly instead of copying
// each entry — AttrList instances are built per-call by Attrs() so there's
// no sharing concern.
func El(tag string, args ...any) Node {
	n := Node{kind: kindElement, tag: tag}
	if len(args) > 0 {
		n.children = make([]Node, 0, len(args))
	}
	for _, arg := range args {
		switch v := arg.(type) {
		case AttrList:
			if n.attrs == nil {
				n.attrs = v
			} else {
				n.attrs = append(n.attrs, v...)
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
//
// The gsx transpiler emits gosx.Expr(x) for every `{x}` interpolation in a
// template, regardless of x's static type (it is a syntactic transform with
// no type information). When x is already a Node — e.g. `{props.Child}` where
// Child is a gosx.Node, the standard way to inject a composed child or island
// into a template — it must be inlined into the tree, not stringified. A
// []Node is wrapped in a Fragment for the same reason. Without this, a Node
// value would be rendered as fmt.Sprint's struct dump and HTML-escaped into
// visible garbage text. All other values keep their text representation.
func Expr(v any) Node {
	switch t := v.(type) {
	case Node:
		return t
	case []Node:
		return Fragment(t...)
	}
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

// Attrs creates an attribute list from key-value pairs. Pre-sized to
// len(pairs) since on the common path every arg is an Attr().
func Attrs(pairs ...any) AttrList {
	if len(pairs) == 0 {
		return nil
	}
	attrs := make(AttrList, 0, len(pairs))
	for _, p := range pairs {
		if v, ok := p.(nodeAttr); ok {
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
	return nodeAttr{name: name, value: true, presence: true}
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
//
// The output Builder is pre-grown based on a rough estimate of the final
// HTML size (tag overhead per element, attribute name+value per attr, text
// length per text/raw node). Over-estimating slightly wastes a few bytes;
// under-estimating is fine because Builder still grows on demand. The goal
// is to eliminate the 3-5 doublings a typical page would otherwise trigger
// during renderNodeHTML.
func RenderHTML(n Node) string {
	var b strings.Builder
	b.Grow(estimateRenderSize(n))
	renderNodeHTML(&b, n)
	return b.String()
}

// estimateRenderSize walks the node tree and returns a rough byte count
// for the HTML output. This is a best-effort heuristic, not an exact size:
// it doesn't account for escape expansion (e.g., `<` → `&lt;`) and it
// assumes attribute values render as-is. That's fine for pre-sizing a
// Builder — if we under-allocate by a few bytes, Builder still grows once.
func estimateRenderSize(n Node) int {
	switch n.kind {
	case kindElement:
		// <tag ...attrs...></tag> = 2*len(tag) + 5 bytes of fixed structure
		// (`<`, `>`, `</`, `>`). Each attribute is ` name="value"` → 4 bytes
		// of structure plus the name+value length.
		size := 2*len(n.tag) + 5
		for _, attr := range n.attrs {
			size += len(attr.name) + 4
			if s, ok := attr.value.(string); ok {
				size += len(s)
			} else {
				size += 8 // rough guess for bool/int/float values
			}
		}
		for _, child := range n.children {
			size += estimateRenderSize(child)
		}
		return size
	case kindText, kindExpr, kindRawHTML:
		return len(n.text)
	case kindFragment:
		size := 0
		for _, child := range n.children {
			size += estimateRenderSize(child)
		}
		return size
	}
	return 0
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
		attrs := n.attrs
		if strings.EqualFold(n.tag, "form") {
			attrs = expandManagedFormAttrs(attrs)
		}
		for _, attr := range attrs {
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

// expandManagedFormAttrs replaces a truthy ManagedFormShorthandAttr on a
// <form> element's attribute list with the ManagedFormAttrs default set
// (state idle, layer bootstrap, fallback native-form). The HTML method
// attribute, if present on the same element, stays authoritative for the
// navigation runtime; the shorthand does not add ManagedFormModeAttr.
//
// Merge rule: any contract attribute the author already wrote (for example
// an explicit data-gosx-form-state) is left exactly as authored, in its
// original position — only the missing contract attributes are inserted,
// in place of the shorthand attribute. Attribute order and values outside
// the contract are untouched. If the shorthand attribute is absent, or
// present but not truthy, attrs is returned unchanged.
func expandManagedFormAttrs(attrs []nodeAttr) []nodeAttr {
	idx := -1
	for i, attr := range attrs {
		if attr.name == ManagedFormShorthandAttr {
			idx = i
			break
		}
	}
	if idx < 0 || !managedFormShorthandTruthy(attrs[idx]) {
		return attrs
	}

	have := make(map[string]bool, len(attrs))
	for _, attr := range attrs {
		have[attr.name] = true
	}

	var fill []nodeAttr
	for _, d := range ManagedFormAttrs(ManagedFormOptions{}) {
		if !have[d.name] {
			fill = append(fill, d)
		}
	}

	out := make([]nodeAttr, 0, len(attrs)-1+len(fill))
	out = append(out, attrs[:idx]...)
	out = append(out, fill...)
	out = append(out, attrs[idx+1:]...)
	return out
}

// managedFormShorthandTruthy reports whether a ManagedFormShorthandAttr
// value opts the element in. It delegates to ManagedFormShorthandTruthy
// (runtime_contract.go), the single shared definition of the truthy rule
// used by both this Node render path and the route package's .gsx render
// path, so `data-gosx-managed` and `data-gosx-managed="true"` opt in and
// `data-gosx-managed="false"` opts out the same way in either surface.
func managedFormShorthandTruthy(attr nodeAttr) bool {
	return ManagedFormShorthandTruthy(attr.presence, attr.value)
}

func renderAttrHTML(b *strings.Builder, attr nodeAttr) {
	switch v := attr.value.(type) {
	case bool:
		if attr.presence || htmlattr.IsBoolean(attr.name) {
			if !v {
				return
			}
			b.WriteByte(' ')
			b.WriteString(html.EscapeString(attr.name))
			return
		}
		b.WriteByte(' ')
		b.WriteString(html.EscapeString(attr.name))
		if v {
			b.WriteString(`="true"`)
		} else {
			b.WriteString(`="false"`)
		}
	case string:
		// Direct byte writes instead of fmt.Fprintf — each Fprintf boxes
		// the two string args into interface{} values (2 allocations)
		// plus allocates a format-state scratch buffer (another 1-2
		// allocations) for every attribute. Rendering a typical page
		// with hundreds of attributes adds up to thousands of avoidable
		// allocations per request. Writing bytes directly drops the
		// per-attr cost from ~100ns / 3 allocs to ~40ns / 0 allocs for
		// already-safe attribute names + HTML-escaped string values.
		b.WriteByte(' ')
		b.WriteString(html.EscapeString(attr.name))
		b.WriteString(`="`)
		b.WriteString(html.EscapeString(v))
		b.WriteByte('"')
	default:
		// Non-string / non-bool values still go through fmt.Sprint for
		// correctness (handles ints, floats, fmt.Stringer, etc.) but
		// the outer write is direct. Rare enough on real pages that the
		// Sprint alloc isn't worth micro-optimizing.
		b.WriteByte(' ')
		b.WriteString(html.EscapeString(attr.name))
		b.WriteString(`="`)
		b.WriteString(html.EscapeString(fmt.Sprint(v)))
		b.WriteByte('"')
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
