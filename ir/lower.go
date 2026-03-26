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

// hasIslandDirective checks if the source text preceding a function declaration
// contains a //gosx:island comment directive. Scans backwards from the function
// start position through preceding whitespace and comment lines.
func (l *lowerer) hasIslandDirective(n *gotreesitter.Node) bool {
	return strings.Contains(l.precedingText(n), "//gosx:island")
}

// parseEngineDirective checks for //gosx:engine and extracts the kind.
// Returns ("worker"|"surface", true) or ("", false).
func (l *lowerer) parseEngineDirective(n *gotreesitter.Node) (string, bool) {
	preceding := l.precedingText(n)
	if idx := strings.Index(preceding, "//gosx:engine "); idx >= 0 {
		rest := preceding[idx+len("//gosx:engine "):]
		kind := strings.Fields(rest)[0]
		if kind == "worker" || kind == "surface" {
			return kind, true
		}
	}
	if strings.Contains(preceding, "//gosx:engine") {
		return "worker", true // default to worker
	}
	return "", false
}

// parseCapabilities extracts //gosx:capabilities from preceding comments.
func (l *lowerer) parseCapabilities(n *gotreesitter.Node) []string {
	preceding := l.precedingText(n)
	if idx := strings.Index(preceding, "//gosx:capabilities "); idx >= 0 {
		rest := preceding[idx+len("//gosx:capabilities "):]
		line := strings.Split(rest, "\n")[0]
		return strings.Fields(line)
	}
	return nil
}

func (l *lowerer) precedingText(n *gotreesitter.Node) string {
	start := int(n.StartByte())
	searchStart := start - 300
	if searchStart < 0 {
		searchStart = 0
	}
	return string(l.src[searchStart:start])
}

// analyzeBody walks a function body CST node and extracts signal declarations,
// computed values, and handler functions by pattern matching on the syntax tree.
//
// Recognized patterns:
//
//	count := signal.New(0)          → SignalInfo{Name: "count", InitExpr: "0"}
//	doubled := signal.Derive(...)   → ComputedInfo{Name: "doubled", BodyExpr: "..."}
//	increment := func() { ... }    → HandlerInfo{Name: "increment", Statements: [...]}
func (l *lowerer) analyzeBody(bodyNode *gotreesitter.Node) *ComponentScope {
	scope := &ComponentScope{
		Locals: make(map[string]string),
	}

	// The function body is a block: { statement_list }
	// Find the statement_list and walk its children.
	stmtList := bodyNode
	for i := 0; i < int(bodyNode.NamedChildCount()); i++ {
		child := bodyNode.NamedChild(i)
		if l.nodeType(child) == "statement_list" {
			stmtList = child
			break
		}
	}

	// Walk all children of the statement list looking for short_var_declaration
	for i := 0; i < int(stmtList.ChildCount()); i++ {
		child := stmtList.Child(i)
		if child == nil {
			continue
		}
		typ := l.nodeType(child)
		if typ == "short_var_declaration" {
			l.analyzeShortVarDecl(child, scope)
		}
	}

	// Only return scope if we found anything
	if len(scope.Signals) == 0 && len(scope.Computeds) == 0 && len(scope.Handlers) == 0 {
		return nil
	}
	return scope
}

// analyzeShortVarDecl checks if a short variable declaration matches
// a signal, computed, or handler pattern.
func (l *lowerer) analyzeShortVarDecl(n *gotreesitter.Node, scope *ComponentScope) {
	// short_var_declaration has "left" (expression_list) and "right" (expression_list)
	leftNode := l.childByField(n, "left")
	rightNode := l.childByField(n, "right")
	if leftNode == nil || rightNode == nil {
		return
	}

	// Get the variable name (first identifier in left side)
	varName := ""
	for j := 0; j < int(leftNode.NamedChildCount()); j++ {
		id := leftNode.NamedChild(j)
		if l.nodeType(id) == "identifier" {
			varName = l.text(id)
			break
		}
	}
	// If left is itself an identifier (single var)
	if varName == "" && l.nodeType(leftNode) == "identifier" {
		varName = l.text(leftNode)
	}
	// Try expression_list → first child
	if varName == "" {
		varName = l.text(leftNode)
		// Clean up if it grabbed too much
		if strings.Contains(varName, ",") {
			varName = strings.TrimSpace(strings.Split(varName, ",")[0])
		}
	}
	if varName == "" {
		return
	}

	// Get the right-side expression
	// It might be inside an expression_list wrapper
	rightExpr := rightNode
	if l.nodeType(rightExpr) == "expression_list" && rightExpr.NamedChildCount() > 0 {
		rightExpr = rightExpr.NamedChild(0)
	}

	rightType := l.nodeType(rightExpr)

	// Pattern: name := signal.New(initExpr)
	if rightType == "call_expression" {
		funcNode := l.childByField(rightExpr, "function")
		if funcNode != nil {
			funcText := l.text(funcNode)
			argsNode := l.childByField(rightExpr, "arguments")

			// signal.New(...)
			if funcText == "signal.New" && argsNode != nil {
				initExpr := l.extractFirstArg(argsNode)
				typeHint := l.inferTypeHint(initExpr)
				scope.Signals = append(scope.Signals, SignalInfo{
					Name:     varName,
					InitExpr: initExpr,
					TypeHint: typeHint,
				})
				scope.Locals[varName] = "signal"
				return
			}

			// signal.Derive(func() T { return expr })
			if funcText == "signal.Derive" && argsNode != nil {
				bodyExpr := l.extractDeriveBody(argsNode)
				scope.Computeds = append(scope.Computeds, ComputedInfo{
					Name:     varName,
					BodyExpr: bodyExpr,
				})
				scope.Locals[varName] = "computed"
				return
			}
		}
	}

	// Pattern: name := func() { ...statements... }
	if rightType == "func_literal" {
		body := l.childByField(rightExpr, "body")
		if body != nil {
			stmts := l.extractStatements(body)
			scope.Handlers = append(scope.Handlers, HandlerInfo{
				Name:       varName,
				Statements: stmts,
			})
			scope.Locals[varName] = "handler"
			return
		}
	}
}

// extractFirstArg gets the source text of the first argument in an argument_list.
func (l *lowerer) extractFirstArg(argsNode *gotreesitter.Node) string {
	for i := 0; i < int(argsNode.NamedChildCount()); i++ {
		child := argsNode.NamedChild(i)
		return l.text(child)
	}
	return ""
}

// extractDeriveBody extracts the return expression from a signal.Derive(func() T { return expr }) call.
func (l *lowerer) extractDeriveBody(argsNode *gotreesitter.Node) string {
	// Walk all children (named and unnamed) looking for func_literal
	for i := 0; i < int(argsNode.ChildCount()); i++ {
		child := argsNode.Child(i)
		if child == nil {
			continue
		}
		if l.nodeType(child) == "func_literal" {
			return l.extractReturnExpr(child)
		}
	}
	return ""
}

// extractReturnExpr finds the return statement inside a func_literal and extracts its expression.
func (l *lowerer) extractReturnExpr(funcLit *gotreesitter.Node) string {
	// func_literal → body (block) → statement_list → return_statement → expression
	body := l.childByField(funcLit, "body")
	if body == nil {
		// Try unnamed child approach
		for i := 0; i < int(funcLit.ChildCount()); i++ {
			child := funcLit.Child(i)
			if child != nil && l.nodeType(child) == "block" {
				body = child
				break
			}
		}
	}
	if body == nil {
		return ""
	}

	// Find statement_list inside the block
	var stmtList *gotreesitter.Node
	for i := 0; i < int(body.ChildCount()); i++ {
		child := body.Child(i)
		if child != nil && l.nodeType(child) == "statement_list" {
			stmtList = child
			break
		}
	}
	if stmtList == nil {
		stmtList = body // try body directly
	}

	// Find return_statement
	for i := 0; i < int(stmtList.ChildCount()); i++ {
		child := stmtList.Child(i)
		if child == nil {
			continue
		}
		if l.nodeType(child) == "return_statement" {
			// Extract the expression(s) after "return"
			// Try named children first
			for j := 0; j < int(child.NamedChildCount()); j++ {
				expr := child.NamedChild(j)
				text := strings.TrimSpace(l.text(expr))
				if text != "" {
					return text
				}
			}
			// Try all children, skip the "return" keyword
			for j := 0; j < int(child.ChildCount()); j++ {
				expr := child.Child(j)
				if expr == nil {
					continue
				}
				text := strings.TrimSpace(l.text(expr))
				if text != "" && text != "return" {
					return text
				}
			}
		}
	}
	return ""
}

// extractStatements gets the source text of each statement in a block.
func (l *lowerer) extractStatements(bodyNode *gotreesitter.Node) []string {
	var stmts []string
	for i := 0; i < int(bodyNode.NamedChildCount()); i++ {
		child := bodyNode.NamedChild(i)
		stmts = append(stmts, l.text(child))
	}
	return stmts
}

// inferTypeHint guesses the type from a literal expression.
func (l *lowerer) inferTypeHint(expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return ""
	}
	if expr == "true" || expr == "false" {
		return "bool"
	}
	if len(expr) > 0 && expr[0] == '"' {
		return "string"
	}
	if len(expr) > 0 && expr[0] == '\'' {
		return "string"
	}
	// Check for float (contains '.')
	if strings.Contains(expr, ".") {
		allDigitsAndDot := true
		for _, c := range expr {
			if (c < '0' || c > '9') && c != '.' && c != '-' {
				allDigitsAndDot = false
				break
			}
		}
		if allDigitsAndDot {
			return "float"
		}
	}
	// Check for int
	allDigits := true
	start := 0
	if len(expr) > 0 && expr[0] == '-' {
		start = 1
	}
	for _, c := range expr[start:] {
		if c < '0' || c > '9' {
			allDigits = false
			break
		}
	}
	if allDigits && len(expr) > start {
		return "int"
	}
	// Arrays/slices
	if strings.HasPrefix(expr, "[]") || strings.HasPrefix(expr, "make([]") {
		return "array"
	}
	return ""
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

	// Analyze the function body for signal/computed/handler declarations.
	// This extracts the component scope needed for island lowering.
	scope := l.analyzeBody(bodyNode)

	comp := Component{
		Name:      name,
		PropsType: propsType,
		Root:      rootID,
		IsIsland:  l.hasIslandDirective(n),
		Scope:     scope,
		Span:      l.span(n),
	}

	// Check for engine directive
	if engineKind, isEngine := l.parseEngineDirective(n); isEngine {
		comp.IsEngine = true
		comp.EngineKind = engineKind
		comp.EngineCapabilities = l.parseCapabilities(n)
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

	case "jsx_attribute_expression":
		expr := stripJSXAttributeExpressionText(l.text(valueNode))
		isEvent := strings.HasPrefix(name, "on") && len(name) > 2 && name[2] >= 'A' && name[2] <= 'Z'
		return Attr{Kind: AttrExpr, Name: name, Expr: expr, IsEvent: isEvent}

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

func stripJSXAttributeExpressionText(text string) string {
	if len(text) >= 2 && text[0] == '{' && text[len(text)-1] == '}' {
		return text[1 : len(text)-1]
	}
	return text
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
