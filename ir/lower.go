package ir

import (
	"fmt"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// Lower converts a parsed GoSX CST into the component IR.
func Lower(root *gotreesitter.Node, source []byte, lang *gotreesitter.Language) (*Program, error) {
	l := &lowerer{
		src:  source,
		lang: lang,
		prog: &Program{},
	}

	l.lowerSourceFile(root)

	if len(l.errs) > 0 {
		return nil, fmt.Errorf("lowering errors:\n%s", strings.Join(l.errs, "\n"))
	}
	return l.prog, nil
}

type lowerer struct {
	src  []byte
	lang *gotreesitter.Language
	prog *Program
	errs []string
}

func (l *lowerer) text(n *gotreesitter.Node) string {
	return string(l.src[n.StartByte():n.EndByte()])
}

func (l *lowerer) nodeType(n *gotreesitter.Node) string {
	return n.Type(l.lang)
}

func (l *lowerer) childByField(n *gotreesitter.Node, name string) *gotreesitter.Node {
	return n.ChildByFieldName(name, l.lang)
}

func (l *lowerer) errorf(n *gotreesitter.Node, format string, args ...any) {
	pos := n.StartPoint()
	msg := fmt.Sprintf("%d:%d: %s", pos.Row+1, pos.Column+1, fmt.Sprintf(format, args...))
	l.errs = append(l.errs, msg)
}

func (l *lowerer) span(n *gotreesitter.Node) Span {
	start := n.StartPoint()
	end := n.EndPoint()
	return Span{
		StartLine: int(start.Row) + 1,
		StartCol:  int(start.Column) + 1,
		EndLine:   int(end.Row) + 1,
		EndCol:    int(end.Column) + 1,
	}
}

// lowerSourceFile processes the root source_file node.
func (l *lowerer) lowerSourceFile(root *gotreesitter.Node) {
	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		switch l.nodeType(child) {
		case "package_clause":
			l.lowerPackageClause(child)
		case "import_declaration":
			l.lowerImportDecl(child)
		case "function_declaration":
			l.lowerFunctionDecl(child)
		}
	}
}

func (l *lowerer) lowerPackageClause(n *gotreesitter.Node) {
	// package_clause has a package_identifier child (not a named field)
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		if l.nodeType(child) == "package_identifier" {
			l.prog.Package = l.text(child)
			return
		}
	}
}

func (l *lowerer) lowerImportDecl(n *gotreesitter.Node) {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		switch l.nodeType(child) {
		case "import_spec":
			l.lowerImportSpec(child)
		case "import_spec_list":
			for j := 0; j < int(child.NamedChildCount()); j++ {
				spec := child.NamedChild(j)
				if l.nodeType(spec) == "import_spec" {
					l.lowerImportSpec(spec)
				}
			}
		}
	}
}

func (l *lowerer) lowerImportSpec(n *gotreesitter.Node) {
	imp := Import{}
	nameNode := l.childByField(n, "name")
	if nameNode != nil {
		imp.Alias = l.text(nameNode)
	}
	pathNode := l.childByField(n, "path")
	if pathNode != nil {
		imp.Path = strings.Trim(l.text(pathNode), `"`)
	}
	l.prog.Imports = append(l.prog.Imports, imp)
}

// lowerFunctionDecl checks if a function returns Node and contains JSX,
// making it a GoSX component.
func (l *lowerer) lowerFunctionDecl(n *gotreesitter.Node) {
	nameNode := l.childByField(n, "name")
	if nameNode == nil {
		return
	}
	name := l.text(nameNode)

	// Check if this function contains JSX by scanning for jsx_ nodes in the body
	bodyNode := l.childByField(n, "body")
	if bodyNode == nil {
		return
	}

	// Find the return statement with JSX
	jsxRoot := l.findJSXReturn(bodyNode)
	if jsxRoot == nil {
		return // Not a GoSX component
	}

	// Extract props type from parameters
	propsType := l.extractPropsType(n)

	// Lower the JSX tree
	rootID := l.lowerJSXNode(jsxRoot)

	comp := Component{
		Name:      name,
		PropsType: propsType,
		Root:      rootID,
		Span:      l.span(n),
	}
	l.prog.Components = append(l.prog.Components, comp)
}

// findJSXReturn searches a function body for a return statement containing JSX.
func (l *lowerer) findJSXReturn(n *gotreesitter.Node) *gotreesitter.Node {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		typ := l.nodeType(child)

		if typ == "return_statement" {
			// Check expression list for JSX
			for j := 0; j < int(child.NamedChildCount()); j++ {
				expr := child.NamedChild(j)
				if l.isJSXNode(expr) {
					return expr
				}
				// Check inside expression_list
				if l.nodeType(expr) == "expression_list" {
					for k := 0; k < int(expr.NamedChildCount()); k++ {
						inner := expr.NamedChild(k)
						if l.isJSXNode(inner) {
							return inner
						}
					}
				}
			}
		}

		// Recurse into blocks
		if found := l.findJSXReturn(child); found != nil {
			return found
		}
	}
	return nil
}

func (l *lowerer) isJSXNode(n *gotreesitter.Node) bool {
	typ := l.nodeType(n)
	return typ == "jsx_element" || typ == "jsx_self_closing_element" || typ == "jsx_fragment"
}

func (l *lowerer) extractPropsType(funcDecl *gotreesitter.Node) string {
	params := l.childByField(funcDecl, "parameters")
	if params == nil {
		return ""
	}
	for i := 0; i < int(params.NamedChildCount()); i++ {
		param := params.NamedChild(i)
		if l.nodeType(param) == "parameter_declaration" {
			typeNode := l.childByField(param, "type")
			if typeNode != nil {
				return l.text(typeNode)
			}
		}
	}
	return ""
}

// lowerJSXNode converts a JSX CST node into IR nodes.
func (l *lowerer) lowerJSXNode(n *gotreesitter.Node) NodeID {
	switch l.nodeType(n) {
	case "jsx_element":
		return l.lowerJSXElement(n)
	case "jsx_self_closing_element":
		return l.lowerSelfClosing(n)
	case "jsx_fragment":
		return l.lowerFragment(n)
	case "jsx_expression_container":
		return l.lowerExprContainer(n)
	case "jsx_text":
		return l.lowerText(n)
	default:
		// Treat unknown nodes as expression holes
		return l.prog.AddNode(Node{
			Kind: NodeExpr,
			Text: l.text(n),
			Span: l.span(n),
		})
	}
}

func (l *lowerer) lowerJSXElement(n *gotreesitter.Node) NodeID {
	openNode := l.childByField(n, "open")
	if openNode == nil {
		l.errorf(n, "element missing opening tag")
		return l.prog.AddNode(Node{Kind: NodeText, Text: ""})
	}

	tag := l.extractTagName(openNode)
	attrs := l.extractAttrs(openNode)
	children := l.extractChildren(n)

	kind := NodeElement
	if IsComponent(tag) {
		kind = NodeComponent
	}

	node := Node{
		Kind:     kind,
		Tag:      tag,
		Attrs:    attrs,
		Children: children,
		IsStatic: l.isStaticNode(attrs, children),
		Span:     l.span(n),
	}
	return l.prog.AddNode(node)
}

func (l *lowerer) lowerSelfClosing(n *gotreesitter.Node) NodeID {
	tag := l.extractTagName(n)
	attrs := l.extractAttrs(n)

	kind := NodeElement
	if IsComponent(tag) {
		kind = NodeComponent
	}

	node := Node{
		Kind:     kind,
		Tag:      tag,
		Attrs:    attrs,
		IsStatic: l.isStaticAttrs(attrs),
		Span:     l.span(n),
	}
	return l.prog.AddNode(node)
}

func (l *lowerer) lowerFragment(n *gotreesitter.Node) NodeID {
	children := l.extractChildren(n)
	node := Node{
		Kind:     NodeFragment,
		Children: children,
		Span:     l.span(n),
	}
	return l.prog.AddNode(node)
}

func (l *lowerer) lowerExprContainer(n *gotreesitter.Node) NodeID {
	exprNode := l.childByField(n, "expression")
	if exprNode == nil {
		l.errorf(n, "expression container missing expression")
		return l.prog.AddNode(Node{Kind: NodeText, Text: ""})
	}

	// Check if the expression itself is JSX
	if l.isJSXNode(exprNode) {
		return l.lowerJSXNode(exprNode)
	}

	return l.prog.AddNode(Node{
		Kind: NodeExpr,
		Text: l.text(exprNode),
		Span: l.span(n),
	})
}

func (l *lowerer) lowerText(n *gotreesitter.Node) NodeID {
	text := l.text(n)
	// Trim whitespace-only text nodes to just a space
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return l.prog.AddNode(Node{
			Kind:     NodeText,
			Text:     " ",
			IsStatic: true,
			Span:     l.span(n),
		})
	}
	return l.prog.AddNode(Node{
		Kind:     NodeText,
		Text:     text,
		IsStatic: true,
		Span:     l.span(n),
	})
}

func (l *lowerer) extractTagName(n *gotreesitter.Node) string {
	nameNode := l.childByField(n, "name")
	if nameNode == nil {
		return ""
	}
	return l.text(nameNode)
}

func (l *lowerer) extractAttrs(n *gotreesitter.Node) []Attr {
	var attrs []Attr
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		switch l.nodeType(child) {
		case "jsx_attribute":
			attrs = append(attrs, l.lowerAttr(child))
		case "jsx_spread_attribute":
			attrs = append(attrs, l.lowerSpreadAttr(child))
		}
	}
	return attrs
}

func (l *lowerer) lowerAttr(n *gotreesitter.Node) Attr {
	nameNode := l.childByField(n, "name")
	name := ""
	if nameNode != nil {
		name = l.text(nameNode)
	}

	valueNode := l.childByField(n, "value")
	if valueNode == nil {
		// Boolean attribute: <input disabled />
		return Attr{Kind: AttrBool, Name: name}
	}

	switch l.nodeType(valueNode) {
	case "jsx_string_literal":
		val := l.text(valueNode)
		// Strip quotes
		if len(val) >= 2 {
			val = val[1 : len(val)-1]
		}
		return Attr{Kind: AttrStatic, Name: name, Value: val}

	case "jsx_expression_container":
		exprNode := l.childByField(valueNode, "expression")
		expr := ""
		if exprNode != nil {
			expr = l.text(exprNode)
		}
		isEvent := strings.HasPrefix(name, "on") && len(name) > 2 && name[2] >= 'A' && name[2] <= 'Z'
		return Attr{Kind: AttrExpr, Name: name, Expr: expr, IsEvent: isEvent}
	}

	return Attr{Kind: AttrStatic, Name: name, Value: l.text(valueNode)}
}

func (l *lowerer) lowerSpreadAttr(n *gotreesitter.Node) Attr {
	exprNode := l.childByField(n, "expression")
	expr := ""
	if exprNode != nil {
		expr = l.text(exprNode)
	}
	return Attr{Kind: AttrSpread, Expr: expr}
}

func (l *lowerer) extractChildren(n *gotreesitter.Node) []NodeID {
	var children []NodeID
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		typ := l.nodeType(child)
		// Skip opening/closing tags
		if typ == "jsx_opening_element" || typ == "jsx_closing_element" {
			continue
		}
		if typ == "jsx_element" || typ == "jsx_self_closing_element" ||
			typ == "jsx_expression_container" || typ == "jsx_fragment" ||
			typ == "jsx_text" {
			children = append(children, l.lowerJSXNode(child))
		}
	}
	return children
}

func (l *lowerer) isStaticNode(attrs []Attr, children []NodeID) bool {
	if !l.isStaticAttrs(attrs) {
		return false
	}
	for _, childID := range children {
		if !l.prog.Nodes[childID].IsStatic {
			return false
		}
	}
	return true
}

func (l *lowerer) isStaticAttrs(attrs []Attr) bool {
	for _, a := range attrs {
		if a.Kind != AttrStatic && a.Kind != AttrBool {
			return false
		}
	}
	return true
}
