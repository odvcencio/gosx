// Package transpile converts GoSX source files into standard Go code.
//
// The transpiler follows a two-phase pattern (collect → emit) consistent
// with Danmuji and Ferrous Wheel:
//
//  1. Parse GoSX source using the extended grammar.
//  2. Walk the CST, emitting standard Go code. JSX expressions are
//     converted into gosx.Node-building function calls.
package transpile

import (
	"fmt"
	"strings"

	"github.com/odvcencio/gosx"
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// Options controls transpiler behavior.
type Options struct {
	SourceFile string
	Debug      bool
}

// Transpile converts GoSX source into valid Go code that uses the gosx/node package.
func Transpile(source []byte, opts Options) (string, error) {
	tree, lang, err := gosx.Parse(source)
	if err != nil {
		return "", err
	}

	root := tree.RootNode()
	if root.HasError() {
		return "", fmt.Errorf("parse errors in source")
	}

	t := &transpiler{
		src:        source,
		lang:       lang,
		sourceFile: opts.SourceFile,
		imports:    make(map[string]string),
	}

	result := t.emit(root)
	if len(t.errs) > 0 {
		return "", fmt.Errorf("transpile errors:\n%s", strings.Join(t.errs, "\n"))
	}

	return result, nil
}

type transpiler struct {
	src        []byte
	lang       *gotreesitter.Language
	sourceFile string
	imports    map[string]string // path -> alias
	errs       []string
}

func (t *transpiler) text(n *gotreesitter.Node) string {
	return string(t.src[n.StartByte():n.EndByte()])
}

func (t *transpiler) nodeType(n *gotreesitter.Node) string {
	return n.Type(t.lang)
}

func (t *transpiler) childByField(n *gotreesitter.Node, name string) *gotreesitter.Node {
	return n.ChildByFieldName(name, t.lang)
}

func (t *transpiler) errorf(n *gotreesitter.Node, format string, args ...any) {
	pos := n.StartPoint()
	msg := fmt.Sprintf("%d:%d: %s", pos.Row+1, pos.Column+1, fmt.Sprintf(format, args...))
	t.errs = append(t.errs, msg)
}

// emit dispatches on node type, returning Go source code.
func (t *transpiler) emit(n *gotreesitter.Node) string {
	switch t.nodeType(n) {
	case "source_file":
		return t.emitSourceFile(n)
	case "jsx_element":
		return t.emitJSXElement(n)
	case "jsx_self_closing_element":
		return t.emitSelfClosing(n)
	case "jsx_fragment":
		return t.emitFragment(n)
	case "jsx_expression_container":
		return t.emitExprContainer(n)
	case "jsx_text":
		return t.emitJSXText(n)
	default:
		return t.emitDefault(n)
	}
}

func (t *transpiler) emitSourceFile(n *gotreesitter.Node) string {
	var b strings.Builder

	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		b.WriteString(t.emit(child))
		b.WriteByte('\n')
	}

	return b.String()
}

// emitDefault passes through non-JSX nodes by re-emitting their source,
// but recursively processes any JSX children within.
func (t *transpiler) emitDefault(n *gotreesitter.Node) string {
	if n.NamedChildCount() == 0 {
		return t.text(n)
	}

	var b strings.Builder
	lastEnd := n.StartByte()

	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)

		// Emit any source text between the previous child and this one
		if child.StartByte() > lastEnd {
			b.Write(t.src[lastEnd:child.StartByte()])
		}

		childType := t.nodeType(child)
		if childType == "jsx_element" || childType == "jsx_self_closing_element" || childType == "jsx_fragment" {
			b.WriteString(t.emit(child))
		} else {
			b.WriteString(t.emitDefault(child))
		}

		lastEnd = child.EndByte()
	}

	// Emit trailing source after last child
	if lastEnd < n.EndByte() {
		b.Write(t.src[lastEnd:n.EndByte()])
	}

	return b.String()
}

func (t *transpiler) emitJSXElement(n *gotreesitter.Node) string {
	openNode := t.childByField(n, "open")
	if openNode == nil {
		t.errorf(n, "element missing opening tag")
		return ""
	}

	tag := t.extractTagName(openNode)
	attrs := t.emitAttrs(openNode)
	children := t.emitChildren(n)

	if isComponent(tag) {
		return t.emitComponentCall(tag, attrs, children)
	}
	return t.emitElementCall(tag, attrs, children)
}

func (t *transpiler) emitSelfClosing(n *gotreesitter.Node) string {
	tag := t.extractTagName(n)
	attrs := t.emitAttrs(n)

	if isComponent(tag) {
		return t.emitComponentCall(tag, attrs, nil)
	}
	return t.emitElementCall(tag, attrs, nil)
}

func (t *transpiler) emitFragment(n *gotreesitter.Node) string {
	children := t.emitChildren(n)
	var b strings.Builder
	b.WriteString("gosx.Fragment(")
	for i, child := range children {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(child)
	}
	b.WriteByte(')')
	return b.String()
}

func (t *transpiler) emitExprContainer(n *gotreesitter.Node) string {
	exprNode := t.childByField(n, "expression")
	if exprNode == nil {
		return ""
	}

	// If the expression contains JSX, transpile it
	exprType := t.nodeType(exprNode)
	if exprType == "jsx_element" || exprType == "jsx_self_closing_element" || exprType == "jsx_fragment" {
		return t.emit(exprNode)
	}

	return fmt.Sprintf("gosx.Expr(%s)", t.text(exprNode))
}

func (t *transpiler) emitJSXText(n *gotreesitter.Node) string {
	text := t.text(n)
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	return fmt.Sprintf("gosx.Text(%q)", text)
}

func (t *transpiler) emitElementCall(tag string, attrs []string, children []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "gosx.El(%q", tag)

	if len(attrs) > 0 {
		b.WriteString(", gosx.Attrs(")
		b.WriteString(strings.Join(attrs, ", "))
		b.WriteByte(')')
	}

	for _, child := range children {
		if child != "" {
			b.WriteString(", ")
			b.WriteString(child)
		}
	}

	b.WriteByte(')')
	return b.String()
}

func (t *transpiler) emitComponentCall(tag string, attrs []string, children []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s(", tag)

	if len(attrs) > 0 || len(children) > 0 {
		b.WriteString("gosx.Props(")
		b.WriteString(strings.Join(attrs, ", "))
		b.WriteByte(')')
	}

	for _, child := range children {
		if child != "" {
			b.WriteString(", ")
			b.WriteString(child)
		}
	}

	b.WriteByte(')')
	return b.String()
}

func (t *transpiler) emitAttrs(n *gotreesitter.Node) []string {
	var attrs []string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		switch t.nodeType(child) {
		case "jsx_attribute":
			attr := t.emitAttr(child)
			if attr != "" {
				attrs = append(attrs, attr)
			}
		case "jsx_spread_attribute":
			exprNode := t.childByField(child, "expression")
			if exprNode != nil {
				attrs = append(attrs, fmt.Sprintf("gosx.Spread(%s)", t.text(exprNode)))
			}
		}
	}
	return attrs
}

func (t *transpiler) emitAttr(n *gotreesitter.Node) string {
	nameNode := t.childByField(n, "name")
	if nameNode == nil {
		return ""
	}
	name := t.text(nameNode)

	valueNode := t.childByField(n, "value")
	if valueNode == nil {
		// Boolean attribute
		return fmt.Sprintf("gosx.BoolAttr(%q)", name)
	}

	switch t.nodeType(valueNode) {
	case "jsx_string_literal":
		val := t.text(valueNode)
		return fmt.Sprintf("gosx.Attr(%q, %s)", name, val) // already quoted
	case "jsx_expression_container":
		exprNode := t.childByField(valueNode, "expression")
		if exprNode != nil {
			return fmt.Sprintf("gosx.Attr(%q, %s)", name, t.text(exprNode))
		}
	}

	return ""
}

func (t *transpiler) emitChildren(n *gotreesitter.Node) []string {
	var children []string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		typ := t.nodeType(child)
		if typ == "jsx_opening_element" || typ == "jsx_closing_element" {
			continue
		}
		if typ == "jsx_element" || typ == "jsx_self_closing_element" ||
			typ == "jsx_expression_container" || typ == "jsx_fragment" ||
			typ == "jsx_text" {
			result := t.emit(child)
			if result != "" {
				children = append(children, result)
			}
		}
	}
	return children
}

func (t *transpiler) extractTagName(n *gotreesitter.Node) string {
	nameNode := t.childByField(n, "name")
	if nameNode == nil {
		return ""
	}
	return t.text(nameNode)
}

func isComponent(tag string) bool {
	return len(tag) > 0 && tag[0] >= 'A' && tag[0] <= 'Z'
}
