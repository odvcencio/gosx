package ir

import (
	"fmt"
	"html"
	"path"
	"strconv"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// Lower converts a parsed GoSX CST into the component IR.
func Lower(root *gotreesitter.Node, source []byte, lang *gotreesitter.Language) (*Program, error) {
	l := &lowerer{
		src:           source,
		srcStr:        string(source),
		lang:          lang,
		prog:          &Program{},
		signalImports: make(map[string]struct{}),
	}

	l.lowerSourceFile(root)

	if len(l.errs) > 0 {
		return nil, NewDiagnosticsError("lower", l.errs)
	}
	return l.prog, nil
}

type lowerer struct {
	src           []byte
	srcStr        string
	lang          *gotreesitter.Language
	prog          *Program
	errs          []Diagnostic
	signalImports map[string]struct{}
	signalDot     bool
}

// text returns the source text covered by node n. It substrings the
// pre-converted srcStr instead of reallocating per call — Go strings
// share their backing array, so this is a 16-byte slice header copy
// instead of a fresh byte allocation + copy.
func (l *lowerer) text(n *gotreesitter.Node) string {
	return l.srcStr[n.StartByte():n.EndByte()]
}

func (l *lowerer) nodeType(n *gotreesitter.Node) string {
	return n.Type(l.lang)
}

func (l *lowerer) childByField(n *gotreesitter.Node, name string) *gotreesitter.Node {
	return n.ChildByFieldName(name, l.lang)
}

func (l *lowerer) errorf(n *gotreesitter.Node, format string, args ...any) {
	l.errs = append(l.errs, Diagnostic{
		Span:    l.span(n),
		Message: fmt.Sprintf(format, args...),
	})
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
	for _, line := range l.precedingCommentLines(n) {
		if strings.TrimSpace(line) == "//gosx:island" {
			return true
		}
	}
	return false
}

// parseEngineDirective checks for //gosx:engine and extracts the kind.
// Returns ("worker"|"surface"|"video", true) or ("", false).
func (l *lowerer) parseEngineDirective(n *gotreesitter.Node) (string, bool) {
	for _, line := range l.precedingCommentLines(n) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//gosx:engine ") {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "//gosx:engine "))
			fields := strings.Fields(rest)
			if len(fields) == 0 {
				return "worker", true
			}
			if kind := fields[0]; kind == "worker" || kind == "surface" || kind == "video" {
				return kind, true
			}
			continue
		}
		if trimmed == "//gosx:engine" {
			return "worker", true // default to worker
		}
	}
	return "", false
}

// parseCapabilities extracts //gosx:capabilities from preceding comments.
func (l *lowerer) parseCapabilities(n *gotreesitter.Node) []string {
	for _, line := range l.precedingCommentLines(n) {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "//gosx:capabilities ") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "//gosx:capabilities "))
		if rest == "" {
			return nil
		}
		return strings.Fields(rest)
	}
	return nil
}

func engineDirectiveCapabilities(kind string, declared []string) []string {
	if kind != "video" {
		return declared
	}

	seen := make(map[string]struct{}, len(declared)+3)
	out := make([]string, 0, len(declared)+3)
	for _, cap := range []string{"video", "fetch", "audio"} {
		seen[cap] = struct{}{}
		out = append(out, cap)
	}
	for _, cap := range declared {
		cap = strings.TrimSpace(cap)
		if cap == "" {
			continue
		}
		if _, ok := seen[cap]; ok {
			continue
		}
		seen[cap] = struct{}{}
		out = append(out, cap)
	}
	return out
}

// precedingCommentLines walks backwards from n.StartByte() through srcStr
// collecting the contiguous block of // comment lines that immediately
// precede the node (skipping blank-line padding before the block starts).
//
// The previous implementation did `strings.Split(string(l.src[:start]), "\n")`
// which allocated a string of size `start` plus a slice of every line in the
// file — wasteful for islands declared late in a file. This walk operates
// directly on srcStr and only allocates strings for the few lines actually
// returned (zero allocations when there are no preceding comments).
func (l *lowerer) precedingCommentLines(n *gotreesitter.Node) []string {
	end := int(n.StartByte())
	if end <= 0 {
		return nil
	}

	src := l.srcStr[:end]
	var block []string
	collecting := false

	// Walk lines from the bottom of the prefix up.
	for end > 0 {
		// Find the start of the previous line.
		lineStart := strings.LastIndexByte(src[:end], '\n') + 1
		line := src[lineStart:end]
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			if collecting {
				break
			}
		} else if strings.HasPrefix(trimmed, "//") {
			collecting = true
			block = append(block, trimmed)
		} else {
			break
		}

		if lineStart == 0 {
			break
		}
		end = lineStart - 1 // step before the '\n'
	}

	// Block was collected bottom-up — reverse in place to restore source order.
	for i, j := 0, len(block)-1; i < j; i, j = i+1, j-1 {
		block[i], block[j] = block[j], block[i]
	}
	return block
}

// analyzeBody walks a function body CST node and extracts signal declarations,
// computed values, and handler functions by pattern matching on the syntax tree.
//
// Recognized patterns:
//
//	count := signal.New(0)                  → SignalInfo{Name: "count", InitExpr: "0"}
//	state := signal.NewShared("app", ...)  → SignalInfo{Name: "$app", InitExpr: "..."}
//	doubled := signal.Derive(...)          → ComputedInfo{Name: "doubled", BodyExpr: "..."}
//	increment := func() { ... }            → HandlerInfo{Name: "increment", Statements: [...]}
func (l *lowerer) analyzeBody(bodyNode *gotreesitter.Node) *ComponentScope {
	scope := &ComponentScope{
		Locals: make(map[string]string),
	}

	stmtList := l.statementListNode(bodyNode)
	if stmtList == nil {
		stmtList = bodyNode
	}

	// Walk all named statements looking for declarations that produce signals,
	// computeds, or handlers.
	for i := 0; i < int(stmtList.NamedChildCount()); i++ {
		child := stmtList.NamedChild(i)
		if child == nil {
			continue
		}
		switch l.nodeType(child) {
		case "short_var_declaration":
			l.analyzeShortVarDecl(child, scope)
		case "var_declaration":
			l.analyzeVarDecl(child, scope)
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

	names := l.extractAssignedNames(leftNode)
	exprs := l.extractAssignedExprs(rightNode)
	l.analyzeAssignments(names, exprs, scope)
}

func (l *lowerer) analyzeVarDecl(n *gotreesitter.Node, scope *ComponentScope) {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		if child == nil {
			continue
		}
		switch l.nodeType(child) {
		case "var_spec":
			l.analyzeVarSpec(child, scope)
		case "var_spec_list":
			for j := 0; j < int(child.NamedChildCount()); j++ {
				spec := child.NamedChild(j)
				if spec != nil && l.nodeType(spec) == "var_spec" {
					l.analyzeVarSpec(spec, scope)
				}
			}
		}
	}
}

func (l *lowerer) analyzeVarSpec(n *gotreesitter.Node, scope *ComponentScope) {
	names := l.extractAssignedNames(n)
	var values *gotreesitter.Node
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		if child != nil && l.nodeType(child) == "expression_list" {
			values = child
			break
		}
	}
	l.analyzeAssignments(names, l.extractAssignedExprs(values), scope)
}

func (l *lowerer) analyzeAssignments(names []string, exprs []*gotreesitter.Node, scope *ComponentScope) {
	for idx, varName := range names {
		if idx >= len(exprs) {
			return
		}
		l.analyzeAssignedExpr(varName, exprs[idx], scope)
	}
}

func (l *lowerer) extractAssignedNames(n *gotreesitter.Node) []string {
	if n == nil {
		return nil
	}
	if l.nodeType(n) == "identifier" {
		return []string{l.text(n)}
	}
	var names []string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		if child == nil {
			continue
		}
		if l.nodeType(child) == "identifier" {
			names = append(names, l.text(child))
		}
	}
	return names
}

func (l *lowerer) extractAssignedExprs(n *gotreesitter.Node) []*gotreesitter.Node {
	if n == nil {
		return nil
	}
	if l.nodeType(n) != "expression_list" {
		return []*gotreesitter.Node{n}
	}
	exprs := make([]*gotreesitter.Node, 0, n.NamedChildCount())
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		if child != nil {
			exprs = append(exprs, child)
		}
	}
	return exprs
}

func (l *lowerer) analyzeAssignedExpr(varName string, rightExpr *gotreesitter.Node, scope *ComponentScope) {
	if varName == "" || rightExpr == nil {
		return
	}

	if sig, ok := l.signalInfoForAssignedExpr(varName, rightExpr); ok {
		scope.Signals = append(scope.Signals, sig)
		scope.Locals[varName] = "signal"
		return
	}

	if computed, ok := l.computedInfoForAssignedExpr(varName, rightExpr); ok {
		scope.Computeds = append(scope.Computeds, computed)
		scope.Locals[varName] = "computed"
		return
	}

	if handler, ok := l.handlerInfoForAssignedExpr(varName, rightExpr); ok {
		scope.Handlers = append(scope.Handlers, handler)
		scope.Locals[varName] = "handler"
		return
	}
}

func (l *lowerer) signalInfoForAssignedExpr(varName string, rightExpr *gotreesitter.Node) (SignalInfo, bool) {
	callKind, argsNode, ok := l.signalCallExpr(rightExpr)
	if !ok || argsNode == nil {
		return SignalInfo{}, false
	}
	switch callKind {
	case signalCallNew:
		initExpr := l.extractArg(argsNode, 0)
		return SignalInfo{
			Name:     varName,
			Local:    varName,
			InitExpr: initExpr,
			TypeHint: l.inferTypeHint(initExpr),
		}, true
	case signalCallNewShared, signalCallShared:
		sharedName := l.normalizeSharedSignalName(l.extractArg(argsNode, 0))
		initExpr := l.extractArg(argsNode, 1)
		if sharedName == "" || initExpr == "" {
			return SignalInfo{}, false
		}
		return SignalInfo{
			Name:     sharedName,
			Local:    varName,
			InitExpr: initExpr,
			TypeHint: l.inferTypeHint(initExpr),
		}, true
	default:
		return SignalInfo{}, false
	}
}

func (l *lowerer) computedInfoForAssignedExpr(varName string, rightExpr *gotreesitter.Node) (ComputedInfo, bool) {
	callKind, argsNode, ok := l.signalCallExpr(rightExpr)
	if !ok || callKind != signalCallDerive || argsNode == nil {
		return ComputedInfo{}, false
	}
	return ComputedInfo{
		Name:     varName,
		BodyExpr: l.extractDeriveBody(argsNode),
	}, true
}

func (l *lowerer) handlerInfoForAssignedExpr(varName string, rightExpr *gotreesitter.Node) (HandlerInfo, bool) {
	if l.nodeType(rightExpr) != "func_literal" {
		return HandlerInfo{}, false
	}
	body := l.funcLiteralBody(rightExpr)
	if body == nil {
		return HandlerInfo{}, false
	}
	return HandlerInfo{
		Name:       varName,
		Statements: l.extractStatements(body),
	}, true
}

type signalCall int

const (
	signalCallUnknown signalCall = iota
	signalCallNew
	signalCallNewShared
	signalCallShared
	signalCallDerive
)

func (l *lowerer) signalCallKind(funcNode *gotreesitter.Node) signalCall {
	if funcNode == nil {
		return signalCallUnknown
	}
	pkgName, funcName := l.callName(funcNode)
	if pkgName == "" {
		if !l.signalDot {
			return signalCallUnknown
		}
	} else {
		if pkgName != "signal" {
			if _, ok := l.signalImports[pkgName]; !ok {
				return signalCallUnknown
			}
		}
	}
	switch funcName {
	case "New":
		return signalCallNew
	case "NewShared":
		return signalCallNewShared
	case "Shared":
		return signalCallShared
	case "Derive":
		return signalCallDerive
	default:
		return signalCallUnknown
	}
}

func (l *lowerer) signalCallExpr(n *gotreesitter.Node) (signalCall, *gotreesitter.Node, bool) {
	if n == nil || l.nodeType(n) != "call_expression" {
		return signalCallUnknown, nil, false
	}
	funcNode := l.childByField(n, "function")
	if funcNode == nil {
		return signalCallUnknown, nil, false
	}
	return l.signalCallKind(funcNode), l.childByField(n, "arguments"), true
}

func (l *lowerer) callName(funcNode *gotreesitter.Node) (string, string) {
	switch l.nodeType(funcNode) {
	case "identifier":
		return "", l.text(funcNode)
	case "selector_expression":
		if funcNode.NamedChildCount() < 2 {
			return "", ""
		}
		return l.text(funcNode.NamedChild(0)), l.text(funcNode.NamedChild(1))
	default:
		return "", ""
	}
}

// extractArg gets the source text of the argument at the given position in an
// argument_list.
func (l *lowerer) extractArg(argsNode *gotreesitter.Node, index int) string {
	if index < 0 {
		return ""
	}
	for i := 0; i < int(argsNode.NamedChildCount()); i++ {
		child := argsNode.NamedChild(i)
		if i == index {
			return l.text(child)
		}
	}
	return ""
}

func (l *lowerer) normalizeSharedSignalName(expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return ""
	}
	name, err := strconv.Unquote(expr)
	if err != nil {
		name = expr
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "$") {
		return name
	}
	return "$" + name
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
	body := l.funcLiteralBody(funcLit)
	if body == nil {
		return ""
	}

	ret := l.firstReturnStatement(body)
	if ret == nil {
		return ""
	}
	return l.firstNonEmptyNodeText(l.returnExprNodes(ret))
}

// extractStatements gets the source text of each statement in a block.
func (l *lowerer) extractStatements(bodyNode *gotreesitter.Node) []string {
	if bodyNode == nil {
		return nil
	}
	if stmtList := l.statementListNode(bodyNode); stmtList != nil {
		bodyNode = stmtList
	}
	var stmts []string
	for i := 0; i < int(bodyNode.NamedChildCount()); i++ {
		child := bodyNode.NamedChild(i)
		text := strings.TrimSpace(l.text(child))
		if text == "" {
			continue
		}
		stmts = append(stmts, text)
	}
	return stmts
}

func (l *lowerer) statementListNode(n *gotreesitter.Node) *gotreesitter.Node {
	if n == nil {
		return nil
	}
	if l.nodeType(n) == "statement_list" {
		return n
	}
	return l.firstNamedChildByType(n, "statement_list")
}

func (l *lowerer) funcLiteralBody(n *gotreesitter.Node) *gotreesitter.Node {
	if n == nil {
		return nil
	}
	if body := l.childByField(n, "body"); body != nil {
		return body
	}
	return l.firstChildByType(n, "block")
}

func (l *lowerer) firstReturnStatement(n *gotreesitter.Node) *gotreesitter.Node {
	stmtList := l.statementListNode(n)
	if stmtList == nil {
		stmtList = n
	}
	for i := 0; i < int(stmtList.NamedChildCount()); i++ {
		child := stmtList.NamedChild(i)
		if child != nil && l.nodeType(child) == "return_statement" {
			return child
		}
	}
	return nil
}

func (l *lowerer) returnExprNodes(returnStmt *gotreesitter.Node) []*gotreesitter.Node {
	if returnStmt == nil {
		return nil
	}
	var exprs []*gotreesitter.Node
	for i := 0; i < int(returnStmt.NamedChildCount()); i++ {
		child := returnStmt.NamedChild(i)
		if child == nil {
			continue
		}
		if l.nodeType(child) == "expression_list" {
			for j := 0; j < int(child.NamedChildCount()); j++ {
				if expr := child.NamedChild(j); expr != nil {
					exprs = append(exprs, expr)
				}
			}
			continue
		}
		exprs = append(exprs, child)
	}
	if len(exprs) > 0 {
		return exprs
	}
	for i := 0; i < int(returnStmt.ChildCount()); i++ {
		child := returnStmt.Child(i)
		if child == nil {
			continue
		}
		text := strings.TrimSpace(l.text(child))
		if text == "" || text == "return" {
			continue
		}
		exprs = append(exprs, child)
	}
	return exprs
}

func (l *lowerer) firstNonEmptyNodeText(nodes []*gotreesitter.Node) string {
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if text := strings.TrimSpace(l.text(node)); text != "" {
			return text
		}
	}
	return ""
}

func (l *lowerer) firstNamedChildByType(n *gotreesitter.Node, typ string) *gotreesitter.Node {
	if n == nil {
		return nil
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		if child != nil && l.nodeType(child) == typ {
			return child
		}
	}
	return nil
}

func (l *lowerer) firstChildByType(n *gotreesitter.Node, typ string) *gotreesitter.Node {
	if n == nil {
		return nil
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child != nil && l.nodeType(child) == typ {
			return child
		}
	}
	return nil
}

// inferTypeHint guesses the type from a literal expression.
func (l *lowerer) inferTypeHint(expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return ""
	}
	switch {
	case expr == "true" || expr == "false":
		return "bool"
	case isStringLiteral(expr):
		return "string"
	case isFloatLiteral(expr):
		return "float"
	case isIntLiteral(expr):
		return "int"
	case isArrayLiteral(expr):
		return "array"
	default:
		return ""
	}
}

func isStringLiteral(expr string) bool {
	if expr == "" {
		return false
	}
	switch expr[0] {
	case '"', '\'', '`':
		return true
	default:
		return false
	}
}

func isFloatLiteral(expr string) bool {
	normalized := normalizeNumericLiteral(expr)
	if normalized == "" {
		return false
	}
	if !strings.ContainsAny(normalized, ".eEpP") {
		return false
	}
	_, err := strconv.ParseFloat(normalized, 64)
	return err == nil
}

func isIntLiteral(expr string) bool {
	normalized := normalizeNumericLiteral(expr)
	if normalized == "" {
		return false
	}
	if strings.ContainsAny(normalized, ".eEpP") {
		return false
	}
	if strings.HasPrefix(normalized, "+") {
		normalized = normalized[1:]
	}
	if normalized == "" || normalized == "-" {
		return false
	}
	if strings.HasPrefix(normalized, "-") {
		normalized = normalized[1:]
	}
	if normalized == "" {
		return false
	}
	_, err := strconv.ParseUint(normalized, 0, 64)
	return err == nil
}

func normalizeNumericLiteral(expr string) string {
	return strings.ReplaceAll(strings.TrimSpace(expr), "_", "")
}

func isArrayLiteral(expr string) bool {
	expr = strings.TrimSpace(expr)
	switch {
	case strings.HasPrefix(expr, "make([]"), strings.HasPrefix(expr, "make(["):
		return true
	case strings.HasPrefix(expr, "[]") && strings.Contains(expr, "{"):
		return true
	case strings.HasPrefix(expr, "[") && strings.Contains(expr, "]") && strings.Contains(expr, "{"):
		return true
	default:
		return false
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
	if imp.Alias == "" {
		for i := 0; i < int(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			switch l.nodeType(child) {
			case "package_identifier", "dot":
				imp.Alias = l.text(child)
			case "interpreted_string_literal":
				imp.Path = strings.Trim(l.text(child), `"`)
			}
		}
	}
	l.prog.Imports = append(l.prog.Imports, imp)
	l.recordSignalImport(imp)
}

func (l *lowerer) recordSignalImport(imp Import) {
	if strings.TrimSpace(imp.Path) != "github.com/odvcencio/gosx/signal" {
		return
	}
	alias := strings.TrimSpace(imp.Alias)
	switch alias {
	case "":
		l.signalImports[path.Base(imp.Path)] = struct{}{}
	case ".":
		l.signalDot = true
	case "_":
		return
	default:
		l.signalImports[alias] = struct{}{}
	}
}

// lowerFunctionDecl checks if a function returns Node and contains GSX,
// making it a GoSX component.
func (l *lowerer) lowerFunctionDecl(n *gotreesitter.Node) {
	nameNode := l.childByField(n, "name")
	if nameNode == nil {
		return
	}
	name := l.text(nameNode)

	// Check if this function contains GSX by scanning for tag nodes in the body
	bodyNode := l.childByField(n, "body")
	if bodyNode == nil {
		return
	}

	// Find the return statement with GSX
	gsxRoot := l.findGSXReturn(bodyNode)
	if gsxRoot == nil {
		return // Not a GoSX component
	}

	// Extract props type from parameters
	propsType := l.extractPropsType(n)

	// Lower the GSX tree
	rootID := l.lowerGSXNode(gsxRoot)

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
		comp.EngineCapabilities = engineDirectiveCapabilities(engineKind, l.parseCapabilities(n))
	}

	l.prog.Components = append(l.prog.Components, comp)
}

// findGSXReturn searches a function body for a return statement containing GSX.
func (l *lowerer) findGSXReturn(n *gotreesitter.Node) *gotreesitter.Node {
	if n == nil || l.nodeType(n) == "func_literal" {
		return nil
	}
	if l.nodeType(n) == "return_statement" {
		return l.gsxNodeInReturn(n)
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		if found := l.findGSXReturn(child); found != nil {
			return found
		}
	}
	return nil
}

func (l *lowerer) gsxNodeInReturn(returnStmt *gotreesitter.Node) *gotreesitter.Node {
	for _, expr := range l.returnExprNodes(returnStmt) {
		if l.isGSXNode(expr) {
			return expr
		}
	}
	return nil
}

func (l *lowerer) isGSXNode(n *gotreesitter.Node) bool {
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

// lowerGSXNode converts a GSX CST node into IR nodes.
func (l *lowerer) lowerGSXNode(n *gotreesitter.Node) NodeID {
	switch l.nodeType(n) {
	case "jsx_element":
		return l.lowerGSXElement(n)
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

func (l *lowerer) lowerGSXElement(n *gotreesitter.Node) NodeID {
	openNode := l.childByField(n, "open")
	if openNode == nil {
		l.errorf(n, "element missing opening tag")
		return l.prog.AddNode(Node{Kind: NodeText, Text: ""})
	}

	tag := l.extractTagName(openNode)
	attrs := l.extractAttrs(openNode)
	children := l.extractChildren(n)

	// <script> and <style> contain raw text, not HTML-parsed content.
	// Convert any text children to NodeRawHTML so the renderer won't escape
	// operators like && or CSS selectors containing >.
	if tag == "script" || tag == "style" {
		for _, childID := range children {
			child := &l.prog.Nodes[childID]
			if child.Kind == NodeText {
				child.Kind = NodeRawHTML
			}
		}
	}

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

	// Check if the expression itself is GSX
	if l.isGSXNode(exprNode) {
		return l.lowerGSXNode(exprNode)
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
	// Decode HTML entities (e.g. &rarr; → →) so the IR stores real UTF-8
	// characters. The renderer's html.EscapeString pass will re-escape only
	// the characters that actually need escaping (<, >, &, ").
	text = html.UnescapeString(text)
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
	count := int(n.NamedChildCount())
	if count == 0 {
		return nil
	}
	attrs := make([]Attr, 0, count)
	for i := 0; i < count; i++ {
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
		expr := stripGSXAttributeExpressionText(l.text(valueNode))
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

func stripGSXAttributeExpressionText(text string) string {
	if len(text) >= 2 && text[0] == '{' && text[len(text)-1] == '}' {
		return text[1 : len(text)-1]
	}
	return text
}

func (l *lowerer) extractChildren(n *gotreesitter.Node) []NodeID {
	count := int(n.NamedChildCount())
	if count == 0 {
		return nil
	}
	children := make([]NodeID, 0, count)
	for i := 0; i < count; i++ {
		child := n.NamedChild(i)
		typ := l.nodeType(child)
		// Skip opening/closing tags
		if typ == "jsx_opening_element" || typ == "jsx_closing_element" {
			continue
		}
		if typ == "jsx_element" || typ == "jsx_self_closing_element" ||
			typ == "jsx_expression_container" || typ == "jsx_fragment" ||
			typ == "jsx_text" {
			children = append(children, l.lowerGSXNode(child))
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
