//go:build !tinygo

// Lower (this file) is the CST-to-IR compiler step: it is host/build-time
// -only tooling (its only callers are compile.go's gosx.Compile and
// lsp/symbols.go — never client/wasm at runtime, which only ever hydrates
// already-lowered IR/bytecode). It is excluded from TinyGo builds for the
// same reason compile.go/grammar.go/grammargen_aliases.go/gsx_attr_scanner.go
// are (see compile_stub_tinygo.go): its gotreesitter dependency is not
// TinyGo-clean. Specifically, gotreesitter transitively pulls in
// encoding/gob (via its embedded-grammar loader), and TinyGo's
// internal/reflectlite has a known, unimplemented gap —
// `AssignableTo`/`Implements` against a non-empty interface panics
// ("reflect: unimplemented: AssignableTo with interface") — that gob's
// type-info machinery (mustGetTypeInfo -> userType -> implementsInterface)
// trips during WASM boot. Before this fix, this file had NO build
// constraint, so importing the `ir` package for its data types (Program,
// etc. — needed by client/wasm's dependency graph via engine/surface) also
// linked gotreesitter into every TinyGo build of client/wasm, regardless of
// whether Lower() was ever called. Combined with the production build's
// `-panic=trap` TinyGo flag, that panic silently traps as a bare WASM
// `unreachable` on every /admin/editor load, before any hydrate call.
// The standard (non-TinyGo) `go build GOOS=js GOARCH=wasm` test/dev build is
// unaffected either way — it keeps the real compiler (64-bit int, so
// grammargen compiles) as client/wasm's own tests rely on genuinely
// compiling .gsx snippets.

package ir

import (
	"fmt"
	"html"
	"path"
	"strconv"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"m31labs.dev/gosx/internal/strictcomponent"
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
	strict        bool
	strictNames   map[string]struct{}
	legacyNames   map[string]struct{}
	strictProps   map[string]string
	structFields  map[string]map[string]string
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

// engineDirectiveKinds are the kinds //gosx:engine accepts. The near-miss
// check reads this so a misspelled directive with a real kind is reported and
// ordinary prose that happens to begin the same way is not.
var engineDirectiveKinds = []string{"worker", "surface", "video"}

// checkDirectiveTypos reports a comment above a declaration that was nearly a
// directive but is not one.
//
// Every directive is matched byte for byte. `//gosx:island` makes an island;
// `// gosx:island` is an ordinary comment, so the component lowers to static
// markup and its handlers never run. Nothing failed, so `gosx check` printed
// "ok" and left the author to work out why the page did nothing. A compile
// error naming the line costs a typo; silence costs an afternoon.
//
// A candidate is normalized by removing whitespace and lowering case, then
// compared against each directive's own shape rather than a shared prefix:
//
//   - island takes no argument, so the whole line must reduce to the token.
//     That is what keeps prose such as "// gosx:island directive on component"
//     out — the trailing words survive normalization.
//   - engine takes a kind, so the remainder must be one this compiler accepts.
//   - capabilities takes a list, so the remainder need only be non-empty.
//
// A line already spelled correctly is never reported.
func (l *lowerer) checkDirectiveTypos(n *gotreesitter.Node) {
	for _, line := range l.precedingCommentLines(n) {
		trimmed := strings.TrimSpace(line)
		if isCanonicalDirective(trimmed) {
			// Spelled the way the parsers read it. Whether the argument is
			// valid is their business, not this check's.
			//
			// This must test the exact accepted forms rather than the "//gosx:"
			// prefix. "//gosx: island" and "//gosx:Island" both carry that
			// prefix and are both typos the parsers ignore.
			continue
		}

		squeezed := normalizeDirectiveCandidate(trimmed)
		switch {
		case squeezed == "//gosx:island":
			l.errorf(n, "comment %q is not the island directive", trimmed)
			l.hintLast("write it exactly as //gosx:island, with no space after // and none around the colon")

		case strings.HasPrefix(squeezed, "//gosx:engine"):
			kind := strings.TrimPrefix(squeezed, "//gosx:engine")
			for _, valid := range engineDirectiveKinds {
				if kind == valid {
					l.errorf(n, "comment %q is not the engine directive", trimmed)
					l.hintLast("write it exactly as //gosx:engine " + valid)
					break
				}
			}

		case strings.HasPrefix(squeezed, "//gosx:capabilities"):
			if strings.TrimPrefix(squeezed, "//gosx:capabilities") != "" {
				l.errorf(n, "comment %q is not the capabilities directive", trimmed)
				l.hintLast("write it exactly as //gosx:capabilities followed by the list")
			}
		}
	}
}

// isCanonicalDirective reports whether a comment is spelled exactly the way
// the directive parsers match it. It mirrors hasIslandDirective,
// parseEngineDirective and parseCapabilities; keep the four in step.
func isCanonicalDirective(trimmed string) bool {
	return trimmed == "//gosx:island" ||
		strings.HasPrefix(trimmed, "//gosx:engine ") ||
		strings.HasPrefix(trimmed, "//gosx:capabilities ")
}

// normalizeDirectiveCandidate removes every space and lowers the case, so the
// three ways a directive is usually mistyped — a space after //, spaces around
// the colon, and a capital letter — all reduce to the canonical token.
func normalizeDirectiveCandidate(line string) string {
	var b strings.Builder
	b.Grow(len(line))
	for _, r := range line {
		if r == ' ' || r == '\t' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// hintLast attaches a hint to the diagnostic just recorded. Diagnostic carries
// a Hint field that String prints in parentheses, and errorf does not set it.
func (l *lowerer) hintLast(hint string) {
	if len(l.errs) == 0 {
		return
	}
	l.errs[len(l.errs)-1].Hint = hint
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
	if kind != "video" && len(declared) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(declared)+6)
	out := make([]string, 0, len(declared)+6)
	appendCap := func(cap string) {
		cap = strings.TrimSpace(cap)
		if cap == "" {
			return
		}
		key := strings.ToLower(cap)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, cap)
	}
	appendDeclared := func(cap string) {
		switch strings.ToLower(strings.TrimSpace(cap)) {
		case "gpu":
			for _, expanded := range []string{"canvas", "webgpu", "webgl", "webgl2", "compute"} {
				appendCap(expanded)
			}
		default:
			appendCap(cap)
		}
	}

	if kind == "video" {
		for _, cap := range []string{"video", "fetch", "audio"} {
			appendCap(cap)
		}
	}
	for _, cap := range declared {
		appendDeclared(cap)
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
	bodyExpr, err := l.extractDeriveBody(argsNode)
	if err != nil {
		l.errorf(rightExpr, "computed %q: %v", varName, err)
	}
	return ComputedInfo{
		Name:     varName,
		BodyExpr: bodyExpr,
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
	case "Derive", "Computed":
		// Derive and Computed are aliases for the same derived-signal
		// construct. Computed is the name gosx-docs document; both lower to a
		// ComputedDef whose body is the func literal's return expression.
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

// extractDeriveBody accepts exactly the VM-computed shape supported by the
// legacy island expression language: signal.Derive(func() T { return expr }).
// Rejecting extra statements here prevents the old first-return extraction
// from silently discarding setup or control-flow statements.
func (l *lowerer) extractDeriveBody(argsNode *gotreesitter.Node) (string, error) {
	var funcLit *gotreesitter.Node
	if argsNode.NamedChildCount() != 1 {
		return "", fmt.Errorf("requires exactly one function literal argument")
	}
	for i := 0; i < int(argsNode.NamedChildCount()); i++ {
		child := argsNode.NamedChild(i)
		if child != nil && l.nodeType(child) == "func_literal" {
			funcLit = child
		}
	}
	if funcLit == nil {
		return "", fmt.Errorf("requires signal.Derive(func() T { return expr })")
	}

	body := l.funcLiteralBody(funcLit)
	if body == nil {
		return "", fmt.Errorf("function literal has no body")
	}
	statements := l.extractStatements(body)
	if len(statements) != 1 {
		return "", fmt.Errorf("body must contain exactly one return statement; multi-statement bodies are not supported")
	}
	ret := l.firstReturnStatement(body)
	if ret == nil {
		return "", fmt.Errorf("body must contain exactly one return statement")
	}
	exprs := l.returnExprNodes(ret)
	if len(exprs) != 1 {
		return "", fmt.Errorf("return must contain exactly one expression")
	}
	bodyExpr := strings.TrimSpace(l.text(exprs[0]))
	if bodyExpr == "" {
		return "", fmt.Errorf("return expression is empty")
	}
	return bodyExpr, nil
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
	l.collectStrictSchemas(root)
	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		switch l.nodeType(child) {
		case "package_clause":
			l.lowerPackageClause(child)
		case "import_declaration":
			l.lowerImportDecl(child)
		case "function_declaration":
			l.lowerFunctionDecl(child)
		case "gosx_component_declaration":
			l.lowerStrictComponentDecl(child)
		}
	}
}

func (l *lowerer) collectStrictSchemas(root *gotreesitter.Node) {
	l.strictNames = make(map[string]struct{})
	l.legacyNames = make(map[string]struct{})
	l.strictProps = make(map[string]string)
	l.structFields = make(map[string]map[string]string)
	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		switch l.nodeType(child) {
		case "function_declaration":
			name := l.childByField(child, "name")
			body := l.childByField(child, "body")
			if name != nil && body != nil && l.findGSXReturn(body) != nil {
				l.legacyNames[l.text(name)] = struct{}{}
			}
		case "gosx_component_declaration":
			name := l.childByField(child, "name")
			_, propsType := l.extractStrictProps(child)
			if name != nil {
				componentName := l.text(name)
				l.strictNames[componentName] = struct{}{}
				if propsType != "" {
					l.strictProps[componentName] = propsType
				}
			}
		case "type_declaration":
			l.collectStructSchemas(child)
		}
	}
}

func (l *lowerer) collectStructSchemas(n *gotreesitter.Node) {
	var walk func(*gotreesitter.Node)
	walk = func(node *gotreesitter.Node) {
		if node == nil {
			return
		}
		if l.nodeType(node) == "type_spec" {
			nameNode := l.childByField(node, "name")
			typeNode := l.childByField(node, "type")
			if nameNode == nil || typeNode == nil || l.nodeType(typeNode) != "struct_type" {
				return
			}
			fields := make(map[string]string)
			var collectFields func(*gotreesitter.Node)
			collectFields = func(current *gotreesitter.Node) {
				if current == nil {
					return
				}
				if l.nodeType(current) == "field_declaration" {
					for i := 0; i < int(current.NamedChildCount()); i++ {
						fieldNode := current.NamedChild(i)
						if l.nodeType(fieldNode) != "field_identifier" {
							continue
						}
						field := l.text(fieldNode)
						if field == "" || field[0] < 'A' || field[0] > 'Z' {
							continue
						}
						fields[field] = field
						alias := lowerCamelInitialism(field)
						if existing, ok := fields[alias]; ok && existing != field {
							fields[alias] = ""
						} else if !ok {
							fields[alias] = field
						}
					}
					return
				}
				for i := 0; i < int(current.NamedChildCount()); i++ {
					collectFields(current.NamedChild(i))
				}
			}
			collectFields(typeNode)
			if len(fields) > 0 {
				l.structFields[l.text(nameNode)] = fields
			}
			return
		}
		for i := 0; i < int(node.NamedChildCount()); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(n)
}

func lowerCamelInitialism(value string) string {
	if value == "" || value[0] < 'A' || value[0] > 'Z' {
		return value
	}
	end := 1
	for end < len(value) && value[end] >= 'A' && value[end] <= 'Z' {
		if end+1 < len(value) && value[end+1] >= 'a' && value[end+1] <= 'z' {
			break
		}
		end++
	}
	return strings.ToLower(value[:end]) + value[end:]
}

func propsBaseType(propsType string) string {
	propsType = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(propsType), "*"))
	if idx := strings.LastIndex(propsType, "."); idx >= 0 {
		propsType = propsType[idx+1:]
	}
	if idx := strings.Index(propsType, "["); idx >= 0 {
		propsType = propsType[:idx]
	}
	return propsType
}

func (l *lowerer) normalizeStrictComponentAttrs(tag string, attrs []Attr) {
	propsType := l.strictProps[tag]
	if propsType == "" {
		return
	}
	aliases := l.structFields[propsBaseType(propsType)]
	for i := range attrs {
		field, known := aliases[attrs[i].Name]
		if known && field == "" {
			l.errs = append(l.errs, Diagnostic{
				Span:    Span{},
				Message: fmt.Sprintf("strict prop %q is ambiguous for %s", attrs[i].Name, propsType),
				Hint:    "use the exact exported Go field spelling",
			})
			continue
		}
		if field != "" {
			attrs[i].Name = field
		}
	}
}

// validateStrictComponentCall keeps the file-local strict component contract
// fail-closed in the IR compiler. Component styles may coexist in a file, but
// calls must remain within one style so legacy bodies cannot bypass strict prop
// checking and strict bodies cannot depend on legacy renderer semantics.
func (l *lowerer) validateStrictComponentCall(n *gotreesitter.Node, tag string, attrs []Attr, children []NodeID) {
	_, strictCallee := l.strictNames[tag]
	_, legacyCallee := l.legacyNames[tag]
	if l.strict && legacyCallee {
		l.errorf(n, "strict component cannot call legacy component %s; component styles may coexist but calls must stay within one style", tag)
		return
	}
	if !l.strict && strictCallee {
		l.errorf(n, "legacy component cannot call strict component %s; component styles may coexist but calls must stay within one style", tag)
		return
	}
	if !strictCallee {
		return
	}
	for _, attr := range attrs {
		if attr.Kind == AttrSpread {
			l.errorf(n, "spread attributes are not supported for strict component %s", tag)
		}
	}
	if _, acceptsProps := l.strictProps[tag]; !acceptsProps && len(attrs) > 0 {
		l.errorf(n, "strict component %s does not accept props", tag)
	}
	if len(children) > 0 {
		l.errorf(n, "strict component %s does not accept positional children", tag)
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
	if strings.TrimSpace(imp.Path) != "m31labs.dev/gosx/signal" {
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
	propsName, propsType := l.extractProps(n)

	// Lower the GSX tree
	rootID := l.lowerGSXNode(gsxRoot)

	// Analyze the function body for signal/computed/handler declarations.
	// This extracts the component scope needed for island lowering.
	scope := l.analyzeBody(bodyNode)

	// Run before reading the directives, so a misspelled one is reported as
	// itself rather than as a component that mysteriously is not an island.
	l.checkDirectiveTypos(n)

	comp := Component{
		Name:      name,
		PropsType: propsType,
		PropsName: propsName,
		Syntax:    ComponentSyntaxLegacy,
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
		// Surface engines require additional lowering: validate root is <canvas>
		// and collect on* handler bindings.
		if engineKind == "surface" {
			l.lowerEngineSurface(&comp)
		}
	}

	l.prog.Components = append(l.prog.Components, comp)
}

// lowerStrictComponentDecl lowers the TSX-like component spelling while
// enforcing the narrower semantics the IR renderer can execute faithfully.
// In particular, arbitrary Go statements are rejected instead of being
// type-checked and then silently ignored by the renderer.
func (l *lowerer) lowerStrictComponentDecl(n *gotreesitter.Node) {
	nameNode := l.childByField(n, "name")
	bodyNode := l.childByField(n, "body")
	if nameNode == nil || bodyNode == nil {
		l.errorf(n, "strict component declaration is incomplete")
		return
	}

	l.checkDirectiveTypos(n)
	isIsland := l.hasIslandDirective(n)
	engineKind, isEngine := l.parseEngineDirective(n)
	propsName, propsType := l.extractStrictProps(n)
	if propsType != "" && propsName != "props" {
		l.errorf(n, "strict component props parameter must be named props; got %q", propsName)
		l.hintLast("use component " + l.text(nameNode) + "(props: " + propsType + ")")
	}

	gsxRoot := l.strictComponentGSXRoot(bodyNode, isIsland || isEngine)
	if gsxRoot == nil {
		return
	}

	wasStrict := l.strict
	l.strict = true
	rootID := l.lowerGSXNode(gsxRoot)
	l.strict = wasStrict
	scope := l.analyzeBody(bodyNode)
	comp := Component{
		Name:      l.text(nameNode),
		PropsType: propsType,
		PropsName: propsName,
		Syntax:    ComponentSyntaxStrict,
		Root:      rootID,
		IsIsland:  isIsland,
		Scope:     scope,
		Span:      l.span(n),
	}
	if isEngine {
		comp.IsEngine = true
		comp.EngineKind = engineKind
		comp.EngineCapabilities = engineDirectiveCapabilities(engineKind, l.parseCapabilities(n))
		if engineKind == "surface" {
			l.lowerEngineSurface(&comp)
		}
	}

	if !isIsland && !isEngine {
		l.validateStrictServerExpressions(comp.Root)
	}
	l.prog.Components = append(l.prog.Components, comp)
}

func (l *lowerer) strictComponentGSXRoot(body *gotreesitter.Node, allowIslandDecls bool) *gotreesitter.Node {
	statements := l.statementListNode(body)
	if statements == nil {
		statements = body
	}
	count := int(statements.NamedChildCount())
	if count == 0 {
		l.errorf(body, "strict component body must end with exactly one top-level GSX return")
		return nil
	}

	for i := 0; i < count-1; i++ {
		stmt := statements.NamedChild(i)
		if allowIslandDecls && l.nodeType(stmt) == "short_var_declaration" && l.isSupportedStrictIslandDeclaration(stmt) {
			continue
		}
		l.errorf(stmt, "strict component body contains a statement the IR renderer cannot execute")
		if allowIslandDecls {
			l.hintLast("only signal/computed/handler short declarations may precede the final GSX return in strict islands")
		} else {
			l.hintLast("strict server components require exactly one top-level GSX return")
		}
	}

	last := statements.NamedChild(count - 1)
	if l.nodeType(last) != "return_statement" {
		l.errorf(last, "strict component body must end with exactly one top-level GSX return")
		return nil
	}
	exprs := l.returnExprNodes(last)
	if len(exprs) != 1 || !l.isGSXNode(exprs[0]) {
		l.errorf(last, "strict component return must contain exactly one GSX element or fragment")
		return nil
	}
	if !allowIslandDecls && count != 1 {
		return nil
	}
	return exprs[0]
}

func (l *lowerer) isSupportedStrictIslandDeclaration(n *gotreesitter.Node) bool {
	scope := &ComponentScope{Locals: make(map[string]string)}
	before := len(l.errs)
	l.analyzeShortVarDecl(n, scope)
	if len(l.errs) != before {
		return true // the declaration was recognized; its own diagnostic is clearer
	}
	namesNode := l.childByField(n, "left")
	names := l.extractAssignedNames(namesNode)
	if len(names) == 0 {
		return false
	}
	for _, name := range names {
		if _, ok := scope.Locals[name]; !ok {
			return false
		}
	}
	return true
}

func (l *lowerer) validateStrictServerExpressions(root NodeID) {
	seen := make(map[NodeID]bool)
	var visit func(NodeID)
	visit = func(id NodeID) {
		if seen[id] || int(id) >= len(l.prog.Nodes) {
			return
		}
		seen[id] = true
		node := &l.prog.Nodes[id]
		if node.Kind == NodeExpr {
			l.validateStrictServerExpression(node.Span, node.Text)
		}
		for _, attr := range node.Attrs {
			if attr.Kind == AttrExpr || attr.Kind == AttrSpread {
				l.validateStrictServerExpression(node.Span, attr.Expr)
			}
		}
		for _, child := range node.Children {
			visit(child)
		}
	}
	visit(root)
}

func (l *lowerer) validateStrictServerExpression(span Span, source string) {
	if err := strictcomponent.ValidateServerExpression(source); err != nil {
		l.errs = append(l.errs, Diagnostic{
			Span:    span,
			Message: fmt.Sprintf("strict server expression %q is not renderable: %v", strings.TrimSpace(source), err),
			Hint:    "use literals or props field selection; compute, index, and call methods before rendering",
		})
	}
}

// surfaceAllowedHandlers is the exhaustive set of on* event names permitted on
// a surface component's root <canvas> element.
var surfaceAllowedHandlers = map[string]bool{
	"onMount":         true,
	"onClick":         true,
	"onDblClick":      true,
	"onPointerDown":   true,
	"onPointerMove":   true,
	"onPointerUp":     true,
	"onPointerCancel": true,
	"onWheel":         true,
	"onKeyDown":       true,
	"onKeyUp":         true,
	"onResize":        true,
	"onDispose":       true,
}

// lowerEngineSurface validates and lowers a surface engine component.
// It verifies the root is <canvas>, collects on* handler bindings, and sets
// comp.EngineSurface when all checks pass.
func (l *lowerer) lowerEngineSurface(comp *Component) {
	if int(comp.Root) >= len(l.prog.Nodes) {
		l.errs = append(l.errs, Diagnostic{
			Span:    comp.Span,
			Message: "engine surface component has no root node",
		})
		return
	}

	root := &l.prog.Nodes[comp.Root]

	// The root element must be <canvas>.
	if root.Kind != NodeElement || root.Tag != "canvas" {
		tag := root.Tag
		if root.Kind == NodeFragment {
			tag = "(fragment)"
		} else if root.Kind == NodeComponent {
			tag = root.Tag
		}
		l.errs = append(l.errs, Diagnostic{
			Span:    root.Span,
			Message: fmt.Sprintf("engine surface root must be <canvas>; got <%s>", tag),
		})
		// Do NOT set EngineSurface — the component is invalid.
		return
	}

	// Walk on* attrs; collect valid handlers, emit diagnostics for unknown ones.
	var handlers []SurfaceHandlerRef
	for _, attr := range root.Attrs {
		if !strings.HasPrefix(attr.Name, "on") || len(attr.Name) <= 2 {
			continue
		}
		if attr.Name[2] < 'A' || attr.Name[2] > 'Z' {
			continue // not an event handler (e.g. "one", "only")
		}
		// It is an on* event handler attribute.
		if !surfaceAllowedHandlers[attr.Name] {
			l.errs = append(l.errs, Diagnostic{
				Span:    root.Span,
				Message: fmt.Sprintf("unknown engine surface event handler %q; allowed: onMount, onClick, onDblClick, onPointerDown, onPointerMove, onPointerUp, onPointerCancel, onWheel, onKeyDown, onKeyUp, onResize, onDispose", attr.Name),
			})
			continue
		}
		// Validate function name shape: non-empty, valid Go identifier.
		fnName := strings.TrimSpace(attr.Expr)
		if fnName == "" {
			l.errs = append(l.errs, Diagnostic{
				Span:    root.Span,
				Message: fmt.Sprintf("engine surface handler %q has empty function name", attr.Name),
			})
			continue
		}
		if !isValidGoIdent(fnName) {
			l.errs = append(l.errs, Diagnostic{
				Span:    root.Span,
				Message: fmt.Sprintf("engine surface handler %q references %q which is not a valid Go identifier", attr.Name, fnName),
			})
			continue
		}
		handlers = append(handlers, SurfaceHandlerRef{
			EventName:    attr.Name,
			FunctionName: fnName,
		})
	}

	comp.EngineSurface = true
	comp.SurfaceHandlers = handlers
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
	return typ == "jsx_element" || typ == "jsx_raw_text_element" ||
		typ == "jsx_self_closing_element" || typ == "jsx_fragment"
}

func (l *lowerer) extractProps(funcDecl *gotreesitter.Node) (string, string) {
	params := l.childByField(funcDecl, "parameters")
	if params == nil {
		return "", ""
	}
	for i := 0; i < int(params.NamedChildCount()); i++ {
		param := params.NamedChild(i)
		if l.nodeType(param) == "parameter_declaration" {
			nameNode := l.childByField(param, "name")
			typeNode := l.childByField(param, "type")
			if typeNode != nil {
				name := ""
				if nameNode != nil {
					name = l.text(nameNode)
				}
				return name, l.text(typeNode)
			}
		}
	}
	return "", ""
}

func (l *lowerer) extractStrictProps(componentDecl *gotreesitter.Node) (string, string) {
	params := l.childByField(componentDecl, "parameters")
	if params == nil {
		return "", ""
	}
	for i := 0; i < int(params.NamedChildCount()); i++ {
		param := params.NamedChild(i)
		if l.nodeType(param) != "gosx_component_parameter" {
			continue
		}
		nameNode := l.childByField(param, "name")
		typeNode := l.childByField(param, "type")
		if nameNode == nil || typeNode == nil {
			return "", ""
		}
		return l.text(nameNode), strings.TrimSpace(l.text(typeNode))
	}
	return "", ""
}

// lowerGSXNode converts a GSX CST node into IR nodes.
func (l *lowerer) lowerGSXNode(n *gotreesitter.Node) NodeID {
	switch l.nodeType(n) {
	case "jsx_element":
		return l.lowerGSXElement(n)
	case "jsx_raw_text_element":
		return l.lowerRawTextElement(n)
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
	l.validateStrictComponentCall(n, tag, attrs, children)
	if l.strict && !IsComponent(tag) {
		normalizeStrictHTMLAttrs(attrs)
	} else if IsComponent(tag) {
		l.normalizeStrictComponentAttrs(tag, attrs)
	}

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

// lowerRawTextElement lowers <script>/<style>. The scanner hands back the body
// and its closing tag as one jsx_raw_text token, so the body is emitted as a
// single NodeRawHTML child: script and stylesheet source must not be escaped,
// or `&&` and CSS `>` selectors break.
func (l *lowerer) lowerRawTextElement(n *gotreesitter.Node) NodeID {
	open := l.childByField(n, "open")
	if open == nil {
		return l.prog.AddNode(Node{Kind: NodeFragment, Span: l.span(n)})
	}

	tag := strings.TrimPrefix(l.text(l.childByField(open, "name")), "<")
	attrs := l.extractAttrs(open)
	if l.strict {
		normalizeStrictHTMLAttrs(attrs)
	}

	var children []NodeID
	if bodyNode := l.childByField(n, "children"); bodyNode != nil {
		if l.nodeType(bodyNode) == "jsx_expression_container" {
			// <script>{ClientScript()}</script> — a Go value supplies the
			// content, so lower it as an ordinary expression hole.
			children = append(children, l.lowerGSXNode(bodyNode))
		} else if body := trimRawTextCloseTag(l.text(bodyNode)); body != "" {
			children = append(children, l.prog.AddNode(Node{
				Kind: NodeRawHTML,
				Text: body,
				Span: l.span(bodyNode),
			}))
		}
	}

	return l.prog.AddNode(Node{
		Kind:     NodeElement,
		Tag:      tag,
		Attrs:    attrs,
		Children: children,
		IsStatic: l.isStaticAttrs(attrs),
		Span:     l.span(n),
	})
}

// trimRawTextCloseTag drops the closing tag the external scanner folds into the
// jsx_raw_text token.
func trimRawTextCloseTag(raw string) string {
	if idx := strings.LastIndex(raw, "</"); idx >= 0 {
		return raw[:idx]
	}
	return raw
}

func (l *lowerer) lowerSelfClosing(n *gotreesitter.Node) NodeID {
	tag := l.extractTagName(n)
	attrs := l.extractAttrs(n)
	l.validateStrictComponentCall(n, tag, attrs, nil)
	if l.strict && !IsComponent(tag) {
		normalizeStrictHTMLAttrs(attrs)
	} else if IsComponent(tag) {
		l.normalizeStrictComponentAttrs(tag, attrs)
	}

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

func normalizeStrictHTMLAttrs(attrs []Attr) {
	for i := range attrs {
		switch attrs[i].Name {
		case "className":
			attrs[i].Name = "class"
		case "htmlFor":
			attrs[i].Name = "for"
		}
	}
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

	// Conditional rendering with JSX operands embedded in the expression:
	//   {cond && <jsx>}     -> render <jsx> when cond is truthy
	//   {cond ? <a> : <b>}  -> render <a> when cond, else <b>
	// The JSX branches must become real child subtrees (reusing the <If>
	// conditional path) rather than raw text handed to the island expression
	// DSL, which has no JSX. A plain ternary with no JSX branch falls through to
	// the expression-hole path below, where the DSL evaluates it directly.
	if id, ok := l.lowerConditionalExprContainer(exprNode); ok {
		return id
	}

	return l.prog.AddNode(Node{
		Kind: NodeExpr,
		Text: l.text(exprNode),
		Span: l.span(n),
	})
}

// lowerConditionalExprContainer lowers `{cond && <jsx>}` and
// `{cond ? <a> : <b>}` into conditional subtrees when at least one branch is
// JSX. It returns (id, true) when it handled the expression; (0, false) leaves
// the caller to treat the container as a plain expression hole.
func (l *lowerer) lowerConditionalExprContainer(n *gotreesitter.Node) (NodeID, bool) {
	switch l.nodeType(n) {
	case "binary_expression":
		op := l.childByField(n, "operator")
		left := l.childByField(n, "left")
		right := l.childByField(n, "right")
		if op == nil || left == nil || right == nil || l.text(op) != "&&" || !l.isGSXNode(right) {
			return 0, false
		}
		thenID := l.lowerGSXNode(right)
		return l.buildIfComponent(l.text(left), []NodeID{thenID}, "", l.span(n)), true

	case "gsx_ternary_expression":
		cond := l.childByField(n, "condition")
		cons := l.childByField(n, "consequence")
		alt := l.childByField(n, "alternative")
		if cond == nil || cons == nil || alt == nil {
			return 0, false
		}
		consGSX := l.isGSXNode(cons)
		altGSX := l.isGSXNode(alt)
		if !consGSX && !altGSX {
			return 0, false // plain value ternary — the DSL handles it as a hole
		}
		condText := l.text(cond)
		span := l.span(n)
		switch {
		case consGSX && altGSX:
			// Fragment of two conditionals so each subtree renders structurally.
			ifThen := l.buildIfComponent(condText, []NodeID{l.lowerGSXNode(cons)}, "", span)
			ifElse := l.buildIfComponent("!("+condText+")", []NodeID{l.lowerGSXNode(alt)}, "", span)
			return l.prog.AddNode(Node{Kind: NodeFragment, Children: []NodeID{ifThen, ifElse}, Span: span}), true
		case consGSX:
			// JSX consequence, expression alternative -> conditional fallback expr.
			return l.buildIfComponent(condText, []NodeID{l.lowerGSXNode(cons)}, l.text(alt), span), true
		default:
			// Expression consequence, JSX alternative -> negate and swap.
			return l.buildIfComponent("!("+condText+")", []NodeID{l.lowerGSXNode(alt)}, l.text(cons), span), true
		}
	}
	return 0, false
}

// buildIfComponent synthesizes an `<If when={whenExpr}>children</If>` component
// node (optionally with a fallback expression), which LowerIsland turns into a
// program.NodeConditional. Reused by the conditional-rendering desugaring above.
func (l *lowerer) buildIfComponent(whenExpr string, children []NodeID, fallbackExpr string, span Span) NodeID {
	attrs := []Attr{{Kind: AttrExpr, Name: "when", Expr: whenExpr}}
	if fallbackExpr != "" {
		attrs = append(attrs, Attr{Kind: AttrExpr, Name: "fallback", Expr: fallbackExpr})
	}
	return l.prog.AddNode(Node{
		Kind:     NodeComponent,
		Tag:      "If",
		Attrs:    attrs,
		Children: children,
		Span:     span,
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
		if typ == "jsx_element" || typ == "jsx_raw_text_element" ||
			typ == "jsx_self_closing_element" ||
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
