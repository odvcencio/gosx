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
	"regexp"
	"sort"
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
	strictReads   map[string]map[string]strictReadClass
	structFields  map[string]map[string]string
	structTypes   map[string]map[string]string
	strictServer  bool

	// legacyProps records every legacy (func-spelled) renderer's declared
	// props type text, and typedLegacyProps the subset whose base type is a
	// struct declared in this same .gsx file (gosx#240). A name in
	// typedLegacyProps is a TYPED legacy component: it carries the same
	// schema a strict component does, so it participates in strict spread
	// boundaries in both directions. A name in legacyProps but not in
	// typedLegacyProps is an UNTYPED legacy component (`props any`, an
	// AttrList, or a type declared elsewhere), which keeps every v0.48
	// rule, including gosx#229's rejection.
	legacyProps      map[string]string
	typedLegacyProps map[string]string

	// childrenHoles records, per same-file component name, whether that
	// component's body places the caller's children with a {children}
	// expression hole — the single owner of the "renders children" predicate
	// inside ir (see Component.AcceptsChildren). It is filled for the WHOLE
	// file by collectStrictSchemas before any body is lowered, because
	// validateStrictComponentCall must read a callee's flag from inside the
	// CALLER's body walk, and a callee may be declared later in the file.
	// Deriving it from the built IR instead would make a legal call fail or
	// pass purely on declaration order — the same order-dependence bug
	// collectStrictSchemas' two-pass split already fixed once for
	// l.strictNames (gosx#182/#184 M-3).
	//
	// It covers all three component categories, not only strict ones. The
	// file renderer binds children the same way for every same-file callee
	// (writeLocalComponent), and gosx#240 made a TYPED legacy component a
	// legal callee inside a strict body, so a rule that read only strict
	// declarations would tell an author to write {children} in a body that
	// already has it.
	//
	// What it does NOT record is the legacy props.Children channel. A legacy
	// body may also read its children out of the render props map, because
	// componentProps injects that key whether or not the props struct
	// declares it. That contract is older, separate, and deliberately kept
	// (see TestTypedLegacyComponentKeepsItsMapBinding); it is not a
	// {children} hole and this map does not claim it is.
	childrenHoles map[string]bool

	// slotHoles records, per same-file component name, the sorted set of
	// named slots that component's body declares — every {slotName}
	// expression hole it contains (gosx#249), childrenHoles' counterpart for
	// a named, additional injection point. Filled by collectStrictSchemas
	// the same pass, and for the same reason: a caller's slot-arity rule
	// must read a callee's declared set from inside the CALLER's own body
	// walk, and a callee may be declared later in the file.
	//
	// Unlike childrenHoles, this is populated for strict components only:
	// the design brief scopes named slots to strict components ("A strict
	// component accepts exactly one children slot" is the problem it
	// solves), and a legacy component already reads an arbitrary value out
	// of its flattened props map with no reserved-identifier scheme to
	// collide with.
	slotHoles map[string][]string

	// currentStrictComponent and currentStrictPropsType name the strict
	// component whose body lowerGSXNode is currently walking (empty
	// outside strict lowering). E2 tier 1 (validateStrictToStrictSpreadCall)
	// resolves a spread source's declared type against the CALLER's own
	// schema, so it needs this context even though it fires from the
	// generic per-element validateStrictComponentCall, which has no other
	// way to see which component's body it is validating.
	currentStrictComponent string
	currentStrictPropsType string

	// currentLegacyComponent and currentLegacyPropsName name the legacy
	// (func-spelled) component whose body lowerGSXNode is currently
	// walking, and the identifier that component declares for its props
	// parameter. Both are empty outside legacy lowering, and
	// currentLegacyPropsName is empty for a legacy component that declares
	// no parameter at all (func Page() Node). E2 tier 2
	// (validateLegacyToStrictCall) needs the pair to prove gosx#229's
	// rejection: a spread whose source is the enclosing legacy component's
	// own props can never satisfy a strict callee, and naming the caller in
	// the diagnostic needs the caller's own name here.
	currentLegacyComponent string
	currentLegacyPropsName string

	// strictEachElems records, per strict component and per depth-1
	// props-rooted <Each of> path, the same-file element struct name that
	// path's loopable-type check (section 2.3) resolved — populated by
	// validateStrictRenderedProps (the early, component-span diagnostic
	// pass) and consumed by the second validation pass
	// (validateStrictServerExpressions) so a structurally invalid of
	// source is reported exactly once instead of twice.
	strictEachElems map[string]map[string]string
}

// strictReadClass records the position(s) a props read appears in, so
// validateStrictRenderedProps can apply the right admission rule per
// position: a field read as a scalar (bare, concat operand, cond selector)
// keeps the exact-scalar rule; a field read as an <Each of> loop source
// takes section 2.3's loopable-[]T-of-struct rule; a field read as an E2
// spread source (design spec section 3.2, "the predecessor's open question
// 1, answered narrowly: struct values reach strict props by forwarding")
// admits any same-file declared struct type with no requirement of nested
// reads underneath — the callee, not the read tracker, owns which fields
// it actually needs. More than one bit can be set for one path — a field
// used more than one way keeps its own diagnostic for each position it
// appears in (section 2.6).
type strictReadClass uint8

const (
	strictReadScalar strictReadClass = 1 << iota
	strictReadLoopSource
	strictReadSpreadForward
)

func (c strictReadClass) has(bit strictReadClass) bool { return c&bit != 0 }

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

// collectStrictSchemas runs in two passes over root's top-level
// declarations rather than one, precisely because collectStrictPropReads
// (via collectStrictElementReads' isSpreadForwardTag check) needs
// l.strictNames complete for the WHOLE file before it can classify any
// component's spread reads correctly (gosx#182/#184 M-3). A single pass
// populated l.strictNames incrementally in file order, so a spread source
// forwarded to a same-file strict callee declared LATER in the file — e.g.
// Outer declared before Mark, with Outer's body containing
// <Mark {...props.Away}> — was not yet in l.strictNames when Outer's own
// reads were collected. isSpreadForwardTag then read false, the spread
// fell through to the generic scalar-read walk, and
// validateStrictRenderedProps rejected the struct-typed props.Away as "not
// ... string, bool, integer, or floating-point" — a real compile failure
// that depended only on declaration order, not on any type-safety
// difference between the two orders.
func (l *lowerer) collectStrictSchemas(root *gotreesitter.Node) {
	l.strictNames = make(map[string]struct{})
	l.legacyNames = make(map[string]struct{})
	l.strictProps = make(map[string]string)
	l.strictReads = make(map[string]map[string]strictReadClass)
	l.structFields = make(map[string]map[string]string)
	l.structTypes = make(map[string]map[string]string)
	l.legacyProps = make(map[string]string)
	l.typedLegacyProps = make(map[string]string)
	l.childrenHoles = make(map[string]bool)
	l.slotHoles = make(map[string][]string)
	// Pass 1: every same-file strict component's name and props type,
	// every legacy (non-strict) top-level renderer function's name and
	// declared props type, every component's {children} hole, and every
	// declared struct's field schema — nothing here depends on declaration
	// order among components.
	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		switch l.nodeType(child) {
		case "function_declaration":
			name := l.childByField(child, "name")
			body := l.childByField(child, "body")
			if name != nil && body != nil && l.findGSXReturn(body) != nil {
				componentName := l.text(name)
				l.legacyNames[componentName] = struct{}{}
				propsName, propsType := l.extractProps(child)
				// The file renderer binds the identifier "props"
				// literally, so a differently spelled parameter is not in
				// scope at render time — such a component has no props
				// schema this pass can honor.
				if strings.TrimSpace(propsName) == "props" && strings.TrimSpace(propsType) != "" {
					l.legacyProps[componentName] = propsType
				}
				// A legacy body places children through the same {children}
				// hole a strict one does — writeLocalComponent binds the
				// name for every same-file callee, whatever its category.
				// gosx#240 made a typed legacy component a legal callee
				// inside a strict body, so this pass must see legacy bodies
				// or the children arity rule would refuse a call the
				// renderer executes correctly.
				if l.componentRendersChildren(child) {
					l.childrenHoles[componentName] = true
				}
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
				// Pass 1, not pass 1b or pass 2: the flag depends on nothing
				// but this declaration's own body — not on the struct table
				// pass 1b needs, and not on the whole-file strict name set
				// pass 2 needs — and every caller's shape check needs it
				// complete for the file regardless of declaration order.
				if l.componentRendersChildren(child) {
					l.childrenHoles[componentName] = true
				}
				if slots := l.componentDeclaredSlots(child); len(slots) > 0 {
					l.slotHoles[componentName] = slots
				}
			}
		case "type_declaration":
			l.collectStructSchemas(child)
		}
	}
	// Pass 1b (gosx#240): promote each legacy renderer whose declared props
	// type is a struct declared in THIS file to a typed legacy component.
	// It runs after the whole of pass 1 for the same reason pass 2 does:
	// l.structTypes must hold every same-file struct before any component
	// is classified, so a props struct declared below its own component
	// classifies exactly as one declared above it. A props type declared in
	// a sibling .go file stays untyped here, matching the same-file schema
	// rule strict components already answer to
	// (validateStrictRenderedProps).
	for componentName, propsType := range l.legacyProps {
		if _, declared := l.structTypes[propsBaseType(propsType)]; declared {
			l.typedLegacyProps[componentName] = propsType
		}
	}
	// Pass 2: each strict and each typed legacy component's prop reads, now
	// that l.strictNames holds every same-file strict component regardless
	// of where in the file it is declared.
	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		var componentName string
		switch l.nodeType(child) {
		case "gosx_component_declaration":
			name := l.childByField(child, "name")
			if name == nil {
				continue
			}
			componentName = l.text(name)
			if _, hasProps := l.strictProps[componentName]; !hasProps {
				continue
			}
		case "function_declaration":
			name := l.childByField(child, "name")
			if name == nil {
				continue
			}
			componentName = l.text(name)
			if _, typed := l.typedLegacyProps[componentName]; !typed {
				continue
			}
		default:
			continue
		}
		l.strictReads[componentName] = l.collectStrictPropReads(child)
	}
}

// componentRendersChildren reports whether decl's body contains at least one
// {children} child expression hole — the whole of Component.AcceptsChildren's
// definition, computed from the CST so it is available before any body is
// lowered.
//
// It deliberately does NOT descend into an attribute. An attribute value can
// itself be a jsx_expression_container (lowerAttr's third case), so a walk
// that counted every container would read class={children} as a children
// placement — and that expression is rejected, by
// ValidateServerChildExpressionScope's attribute-position twin, precisely
// because rendered markup cannot go inside an HTML attribute value. Counting
// it would declare a component to accept children that can never place them.
//
// Multiple holes stay one flag. The flag answers "does this body place the
// caller's children", not "how many times".
func (l *lowerer) componentRendersChildren(decl *gotreesitter.Node) bool {
	body := l.childByField(decl, "body")
	if body == nil {
		return false
	}
	found := false
	var walk func(*gotreesitter.Node)
	walk = func(node *gotreesitter.Node) {
		if node == nil || found {
			return
		}
		switch l.nodeType(node) {
		case "jsx_attribute", "jsx_spread_attribute":
			return
		case "jsx_expression_container":
			exprNode := l.childByField(node, "expression")
			if exprNode != nil && strictcomponent.IsChildrenExpression(l.text(exprNode)) {
				found = true
				return
			}
		}
		for i := 0; i < int(node.NamedChildCount()); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(body)
	return found
}

// componentDeclaredSlots reports the sorted, de-duplicated set of named
// slots decl's body declares — every {slotName} child expression hole it
// contains (strictcomponent.IsSlotExpression), the slot counterpart to
// componentRendersChildren. It shares that function's CST walk shape,
// including the same deliberate refusal to descend into an attribute value
// (see componentRendersChildren's doc comment for why: class={slotFoo}
// would otherwise register a slot placement that can never render markup).
//
// Unlike componentRendersChildren it does not stop at the first match: a
// layout-shaped component may declare more than one named slot, and every
// one of them must reach l.slotHoles so validateStrictCalleeSlots-shaped
// callers and the runtime EntrySlots check both see the complete set.
func (l *lowerer) componentDeclaredSlots(decl *gotreesitter.Node) []string {
	body := l.childByField(decl, "body")
	if body == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var names []string
	var walk func(*gotreesitter.Node)
	walk = func(node *gotreesitter.Node) {
		if node == nil {
			return
		}
		switch l.nodeType(node) {
		case "jsx_attribute", "jsx_spread_attribute":
			return
		case "jsx_expression_container":
			exprNode := l.childByField(node, "expression")
			if exprNode != nil {
				if name, ok := strictcomponent.IsSlotExpression(l.text(exprNode)); ok {
					if _, dup := seen[name]; !dup {
						seen[name] = struct{}{}
						names = append(names, name)
					}
				}
			}
		}
		for i := 0; i < int(node.NamedChildCount()); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(body)
	sort.Strings(names)
	return names
}

// collectStrictPropReads records every props field path (dot-joined, e.g.
// "Tone" or "Player.Name") whose value the file renderer must observe. Local
// strict calls must provide the ROOT field of every registered path
// explicitly: generated Go composite literals otherwise synthesize typed
// zero values while the map-backed renderer would observe a missing key as
// nil (see validateStrictComponentCall, which derives required root names
// from these paths).
//
// jsx_attribute_expression (an attribute's `{...}` value) gets its own branch
// because the grammar's external attribute scanner hands its content back as
// one opaque token with no nested CST — unlike jsx_expression_container
// (element/text children), whose Go sub-expression parses into real
// selector_expression descendants this walk already finds. Concatenation and
// <If cond> shapes live in attribute position (class, aria-label, cond), so
// without this branch their props operands would never reach
// validateStrictRenderedProps, the renderer-boundary type check, or the
// explicit-required-prop rule below — exactly the class of divergence the
// strict contract exists to prevent.
//
// Both branches register a nested chain (props.Player.Name) as one maximal
// path and stop descending into its own inner selector once matched — see
// strictcomponent.ServerExpressionPropPaths' doc comment. Falling through to
// also register the chain's own props.Player sub-expression as an
// independent bare read would fail closed on a struct-typed root that has no
// bare use anywhere in the source, which is the same class of bug a prior
// review caught in this collector once already (see CHANGELOG.md's v0.42.0
// entry), just inverted from a missed read to a false rejection.
//
// A third branch (element open tags) special-cases a strict <Each>'s of
// attribute: its props source is a loop-source read (section 2.3's
// loopable-[]T-of-struct rule), not a scalar read, so it must not fall
// through to the jsx_attribute_expression branch above, which would apply
// the scalar rule and reject a legitimate []T slice field. The tag is
// matched by name only, not shadow-checked against l.strictNames — that map
// is not reliably complete this early for a same-file forward reference
// (collectStrictSchemas is still populating it), so a file that declares an
// actual component named Each and also happens to use <Each of=...> before
// declaring it is a known, narrow limitation, not engineered around here;
// validateStrictComponentCall and the second validation pass, both of which
// run once every component's schema is known, are the real authority on
// whether a given <Each> is the builtin or a shadowing component call.
func (l *lowerer) collectStrictPropReads(n *gotreesitter.Node) map[string]strictReadClass {
	reads := make(map[string]strictReadClass)
	var walk func(*gotreesitter.Node)
	walk = func(node *gotreesitter.Node) {
		if node == nil {
			return
		}
		switch l.nodeType(node) {
		case "selector_expression":
			if path, ok := strictcomponent.ServerPropPath(l.text(node)); ok {
				registerStrictPropRead(reads, path, strictReadScalar)
				return
			}
		case "jsx_attribute_expression":
			expr := stripGSXAttributeExpressionText(l.text(node))
			for _, path := range strictcomponent.ServerExpressionPropPaths(expr) {
				registerStrictPropRead(reads, path, strictReadScalar)
			}
			return
		case "jsx_element", "jsx_self_closing_element":
			l.collectStrictElementReads(node, reads, walk)
			return
		}
		for i := 0; i < int(node.NamedChildCount()); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(n)
	return reads
}

// registerStrictPropRead OR's class into path's entry, so a field read in
// both a scalar position and a loop-source position (section 2.6) keeps
// both bits instead of one overwriting the other.
func registerStrictPropRead(reads map[string]strictReadClass, path []string, class strictReadClass) {
	reads[strings.Join(path, ".")] |= class
}

// collectStrictElementReads walks one element's open-tag attributes and, for
// jsx_element, its children — the same subtree collectStrictPropReads' walk
// would otherwise cover directly — special-casing a strict <Each>'s of
// attribute and, on an actual same-file strict component tag, a spread
// attribute's own top-level expression (see the two per-caller doc
// comments below). Every other attribute and every child still runs
// through walk unchanged, including a spread on If, Each, an HTML element,
// or an unresolved component tag — those keep the pre-#184 behavior of
// falling through to the generic selector_expression case, since their own
// shape rules (validateStrictConditionalCall, validateStrictHTMLElement,
// the "not renderable" rejection) do not turn on a read's class the way a
// strict callee's spread-forward proof does.
func (l *lowerer) collectStrictElementReads(node *gotreesitter.Node, reads map[string]strictReadClass, walk func(*gotreesitter.Node)) {
	open := node
	isElement := l.nodeType(node) == "jsx_element"
	if isElement {
		if o := l.childByField(node, "open"); o != nil {
			open = o
		}
	}
	tag := l.extractTagName(open)
	isEach := tag == "Each"
	_, isStrictCallee := l.strictNames[tag]
	// l.strictNames is complete for the whole file by the time this runs —
	// collectStrictSchemas' pass 2 (gosx#182/#184 M-3) only starts
	// collecting any component's reads after its pass 1 has recorded every
	// same-file strict component's name, regardless of declaration order.
	// A forward-referenced strict callee (declared later in the file than
	// the caller) is therefore seen here exactly as an earlier-declared one
	// is.
	// isStrictCallee alone decides this (gosx#182/#184 minor m-6): it is
	// already false for the builtin If/Each (neither is ever a key in
	// l.strictNames), so an explicit "tag != If/Each" exclusion here only
	// ever changed anything for the one case it should NOT have — a
	// same-file strict component literally named If or Each (a legitimate
	// shadow, section 2.1's carve-out). A spread into a shadowed If/Each
	// used to fall through to the generic scalar-read walk instead of
	// getting the spread-forward class, so validateStrictRenderedProps'
	// later pass saw a struct-typed source registered as a scalar read
	// and rejected it with the scalar diagnostic — even when
	// validateStrictToStrictSpreadCall (a separate pass) would have
	// accepted the very same call as a valid E2 tier-1 spread.
	isSpreadForwardTag := isStrictCallee
	for i := 0; i < int(open.NamedChildCount()); i++ {
		attrChild := open.NamedChild(i)
		if isEach && l.nodeType(attrChild) == "jsx_attribute" {
			nameNode := l.childByField(attrChild, "name")
			if nameNode != nil && l.text(nameNode) == "of" {
				if valueNode := l.childByField(attrChild, "value"); valueNode != nil && l.nodeType(valueNode) == "jsx_attribute_expression" {
					expr := stripGSXAttributeExpressionText(l.text(valueNode))
					for _, path := range strictcomponent.ServerExpressionPropPaths(expr) {
						registerStrictPropRead(reads, path, strictReadLoopSource)
					}
				}
				continue
			}
		}
		if isSpreadForwardTag && l.nodeType(attrChild) == "jsx_spread_attribute" {
			// A spread's own top-level expression is, by the E2 shape rule
			// (design spec section 3.1), exactly props or a props field
			// selector — never some larger expression a struct-typed
			// selector could be buried inside. Registering it directly
			// here, instead of falling through to walk (which would find
			// the same inner selector_expression and register it with the
			// scalar class instead), is what gives a struct-typed spread
			// source the spread-forward admission rule.
			if exprNode := l.childByField(attrChild, "expression"); exprNode != nil {
				if path, ok := strictcomponent.ServerPropPath(l.text(exprNode)); ok {
					registerStrictPropRead(reads, path, strictReadSpreadForward)
				}
			}
			continue
		}
		walk(attrChild)
	}
	if !isElement {
		return
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		typ := l.nodeType(child)
		if typ == "jsx_opening_element" || typ == "jsx_closing_element" {
			continue
		}
		walk(child)
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
			fieldTypes := make(map[string]string)
			var collectFields func(*gotreesitter.Node)
			collectFields = func(current *gotreesitter.Node) {
				if current == nil {
					return
				}
				if l.nodeType(current) == "field_declaration" {
					fieldType := ""
					if fieldTypeNode := l.childByField(current, "type"); fieldTypeNode != nil {
						fieldType = strings.TrimSpace(l.text(fieldTypeNode))
					}
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
						fieldTypes[field] = fieldType
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
				name := l.text(nameNode)
				l.structFields[name] = fields
				l.structTypes[name] = fieldTypes
			}
			return
		}
		for i := 0; i < int(node.NamedChildCount()); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(n)
}

func strictRendererScalarType(typeName string) bool {
	switch strings.TrimSpace(typeName) {
	case "string", "bool",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"byte", "rune", "float32", "float64":
		return true
	default:
		return false
	}
}

func (l *lowerer) validateStrictRenderedProps(n *gotreesitter.Node, componentName, propsType string) {
	reads := l.strictReads[componentName]
	if len(reads) == 0 {
		return
	}
	baseType := propsBaseType(propsType)
	if _, declared := l.structTypes[baseType]; !declared {
		l.errorf(n, "strict component %s renders props fields from %s, whose struct schema is not declared in this .gsx file; declare the renderer-visible props struct beside the component", componentName, propsType)
		l.hintLast(sameFileSchemaHint)
		return
	}
	paths := make([]string, 0, len(reads))
	for path := range reads {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		class := reads[path]
		segments := strings.Split(path, ".")
		if class.has(strictReadScalar) {
			l.resolveStrictSelectorPath(n, componentName, propsType, segments)
		}
		if class.has(strictReadLoopSource) {
			if elem := l.resolveStrictEachSourceType(n, componentName, propsType, segments); elem != "" {
				if l.strictEachElems == nil {
					l.strictEachElems = make(map[string]map[string]string)
				}
				if l.strictEachElems[componentName] == nil {
					l.strictEachElems[componentName] = make(map[string]string)
				}
				l.strictEachElems[componentName][path] = elem
			}
		}
		if class.has(strictReadSpreadForward) {
			l.resolveStrictSpreadForwardType(n, componentName, propsType, segments)
		}
	}
}

// strictSelectorPathDepthLimit caps how many struct-field hops a nested
// selector path may cross: props.A.B.C (or, for an Each binding, row.A.B.C)
// is the deepest chain the strict renderer resolves. strictcomponent's path
// extractors place no upper bound on shape (they are schema-blind), so this
// lowerer function is where the cap is enforced, with full component
// context in the diagnostic.
const strictSelectorPathDepthLimit = 3

// strictHopFailKind classifies why walkStrictHops stopped short of the
// final hop. strictHopOK is not a failure — the walk reached the final hop
// cleanly and res.leafType holds its raw declared type text, unfiltered by
// any admission rule; every caller applies its own (scalar, loopable-[]T,
// or E2 tier-1 struct-identity).
type strictHopFailKind int

const (
	strictHopOK strictHopFailKind = iota
	strictHopTooDeep
	strictHopPointer
	strictHopThroughScalar
	strictHopUndeclaredStruct
	// strictHopUnknownField is only ever returned for hop 0 (the root's own
	// field). Every caller that reaches walkStrictHops with a fully known
	// root schema reports it, reusing strictHopUnknownFieldDeep's wording
	// with its own root substituted — there is no caller left that defers
	// it to the package checker:
	//   - An <Each> binding root (validateEachBindingRead): the binding's
	//     element struct is fully known here, and the projection compiles
	//     in the SAME package as its declaration, so Go resolves a
	//     promoted or unexported field there exactly as it resolves a
	//     declared one — there is no compiler backstop. This caller
	//     reports it, reusing strictHopUnknownFieldDeep's wording with the
	//     binding root substituted (gosx#182/#184).
	//   - The props root, a direct read (resolveStrictSelectorPath):
	//     reports it the same way (gosx#195). validateStrictRenderedProps
	//     already refuses to call resolveStrictSelectorPath at all unless
	//     the props struct's schema is declared same-file (l.structTypes
	//     has it) — see its own early "struct schema is not declared"
	//     gate — so the props struct is always as fully known here as an
	//     <Each> element struct is. Deferring an unknown field to the
	//     package checker was sound only for a genuinely absent field,
	//     which IS a compile error there; for a promoted or unexported
	//     field it was not — Go resolves those too, same package, no
	//     backstop. That silent deferral was gosx#195's bug: a promoted or
	//     unexported hop-0 props read compiled, checked clean, and
	//     rendered differently on the map-backed file renderer and the
	//     generated Go.
	//   - The props root, an <Each of> loop source (resolveStrictEachSourceType)
	//     and an E2 spread-forward source (resolveStrictSpreadForwardType):
	//     both resolve against the same same-file props schema
	//     resolveStrictSelectorPath does, under the identical
	//     validateStrictRenderedProps gate, so gosx#195's fix applied to
	//     one props-root caller and not the other two left the identical
	//     bug live for a promoted or unexported field used as a loop
	//     source or a spread source. Both now report it the same way
	//     (gosx#206).
	strictHopUnknownField
	// strictHopUnknownFieldDeep is gosx#183's B1 fix, generalized: an
	// unknown field at hop i>0 gets no compiler backstop. Field promotion
	// through an embedded type, and same-package unexported access, are
	// both legal Go, so the check program resolves a promoted, unexported,
	// or genuinely absent field the same way it resolves a declared one,
	// while the map-backed file renderer's structTypes schema never records
	// a promoted or unexported field at any hop. Deferring here would let
	// the component compile while the two renderers disagree on what the
	// selector resolves to, or panic trying. Every caller of walkStrictHops
	// reports this kind through strictHopMessage instead of skipping it —
	// for a props root and, gosx#182/#184, for an <Each> binding root.
	strictHopUnknownFieldDeep
)

// strictHopResult is walkStrictHops' outcome: either a clean resolution
// (strictHopOK, with leafType set) or the first structural failure, with
// enough context for strictHopMessage to format a diagnostic.
type strictHopResult struct {
	leafType  string
	pathText  string
	failKind  strictHopFailKind
	failField string
	failType  string
}

// walkStrictHops is the one implementation of the strict selector path
// resolution rule every strict expression position composes on: given
// (rootLabel, rootType) and a field path, it walks each hop through the
// same-file struct schema and either returns the final hop's raw declared
// type or the first structural problem. rootLabel only affects the
// returned pathText, which every diagnostic quotes back to the author. This
// generalizes the pre-#182/#184 resolveStrictSelectorPath to the (rootType,
// path) composition contract in the design spec's section 2.4: a props
// read resolves with rootLabel "props" and rootType the props struct; an
// Each item binding read resolves with rootLabel the binding's name and
// rootType its element struct — both are just a (rootLabel, rootType) pair
// to this function, and it applies the identical pointer-ban,
// scalar-cannot-be-selected-through, undeclared-struct, and depth-cap rules
// to either root. Unknown-field handling splits by hop, not by root: an
// unknown field at hop 0 is the root's own field (strictHopUnknownField —
// see its doc comment for the full list of callers that report it); an
// unknown field at any later hop — promoted, unexported, or genuinely
// absent — always fails closed here (gosx#183's B1 fix), since no compiler
// backstop catches it for either root shape past hop 0 (see
// strictHopUnknownFieldDeep).
func (l *lowerer) walkStrictHops(rootLabel, rootType string, path []string) strictHopResult {
	if len(path) > strictSelectorPathDepthLimit {
		return strictHopResult{pathText: rootLabel + "." + strings.Join(path, "."), failKind: strictHopTooDeep}
	}
	currentType := rootType
	pathText := rootLabel
	for i, field := range path {
		fieldType, known := l.structTypes[currentType][field]
		if !known {
			if i == 0 {
				// The root's own field. failField/failType are populated
				// the same way as the i>0 branch below so every caller —
				// see strictHopUnknownField's doc comment for the current
				// list — can format the identical message.
				return strictHopResult{pathText: pathText, failKind: strictHopUnknownField, failField: field, failType: currentType}
			}
			// gosx#183's B1 fix, generalized to any root: a promoted,
			// unexported, or genuinely absent field past the root fails
			// closed here — see strictHopUnknownFieldDeep.
			return strictHopResult{pathText: pathText, failKind: strictHopUnknownFieldDeep, failField: field, failType: currentType}
		}
		trimmed := strings.TrimSpace(fieldType)
		pathText += "." + field
		if i == len(path)-1 {
			return strictHopResult{leafType: trimmed, pathText: pathText, failKind: strictHopOK}
		}
		switch {
		case strings.HasPrefix(trimmed, "*"):
			return strictHopResult{pathText: pathText, failKind: strictHopPointer, failType: trimmed}
		case strictRendererScalarType(trimmed):
			return strictHopResult{pathText: pathText, failKind: strictHopThroughScalar, failField: path[i+1], failType: trimmed}
		default:
			if _, isStruct := l.structTypes[trimmed]; isStruct {
				currentType = trimmed
				continue
			}
			return strictHopResult{pathText: pathText, failKind: strictHopUndeclaredStruct, failField: path[i+1], failType: trimmed}
		}
	}
	return strictHopResult{pathText: pathText, failKind: strictHopOK}
}

// strictHopMessage formats every walkStrictHops structural failure except
// strictHopOK with full component context. One definition keeps the
// message text identical regardless of which selector position (a props
// read, a loop-source read, an Each binding read) triggered it — the
// failure is about the schema shape, not what the path is used for.
// strictHopUnknownField (hop 0) reaches this function through every caller
// that reports it instead of deferring — an <Each> binding root
// (validateEachBindingRead, gosx#182/#184), a direct props read
// (resolveStrictSelectorPath, gosx#195), an <Each of> loop source
// (resolveStrictEachSourceType, gosx#206), and an E2 spread-forward source
// (resolveStrictSpreadForwardType, gosx#206) — see strictHopUnknownField's
// own doc comment for the full list.
//
// strictHopUnknownFieldDeep and (when a caller reports it)
// strictHopUnknownField both reuse gosx#183's B1 wording verbatim,
// substituting only the root: the original fix names props explicitly
// ("struct %s declares no visible field %s"); this generalized copy
// composes the identical sentence from res.pathText, which already carries
// whichever root (props or an <Each> binding name) walkStrictHops was
// given.
func strictHopMessage(componentName string, res strictHopResult) string {
	switch res.failKind {
	case strictHopTooDeep:
		return fmt.Sprintf("strict component %s selector %s is too deep; the strict renderer resolves at most three fields", componentName, res.pathText)
	case strictHopPointer:
		return fmt.Sprintf("strict component %s cannot select through %s of pointer type %s; pointer fields cannot preserve Go nil-pointer behavior in the file renderer", componentName, res.pathText, res.failType)
	case strictHopThroughScalar:
		return fmt.Sprintf("strict component %s cannot select %q through %s of type %s; selector paths cross same-file struct fields only", componentName, res.failField, res.pathText, res.failType)
	case strictHopUndeclaredStruct:
		return fmt.Sprintf("strict component %s cannot resolve %s.%s: struct %s is not declared in this .gsx file; declare the renderer-visible struct beside the component", componentName, res.pathText, res.failField, res.failType)
	case strictHopUnknownFieldDeep, strictHopUnknownField:
		return fmt.Sprintf("strict component %s cannot resolve %s.%s: struct %s declares no visible field %s; promoted, unexported, and unknown fields cannot cross the file renderer boundary", componentName, res.pathText, res.failField, res.failType, res.failField)
	default:
		return ""
	}
}

// sameFileSchemaHint answers the question the same-file struct rule always
// raises, and gosx#230's ask 1 asked out loud: the .gsx file compiles into
// the sibling .go file's own package, so why can the props struct not live
// there?
//
// Because the Go compiler is not the only reader of that struct. The
// map-backed file renderer (route/fileprogram.go) executes the IR itself
// and never compiles Go, so it resolves every rendered field from schema
// data the IR carries — Component.PropsFields, PropsPaths, and PropsSlices.
// The lowerer builds that data from the type declarations of the one .gsx
// file it is given: ir.Lower takes a parse tree and a byte slice, holds no
// path, opens no file, and is called per file by the LSP and by the dev
// renderer as well as by the build. A type declared in a sibling .go file
// is therefore invisible at the exact moment the schema is built, and a
// component whose schema is missing cannot be proved at any boundary.
//
// gosx#230's real cost — a nested type forced to be both .gsx-local and
// identical to the sibling .go converter's type — is removed at the other
// end instead, by proving a spread's nested struct fields structurally
// (route's requireStrictSpreadStructField). The two types no longer have to
// match, so they no longer have to be the same declaration.
const sameFileSchemaHint = "ir.Lower reads one .gsx file, so the file renderer never sees a sibling .go type; declare the renderer-visible struct here and let the .go converter keep its own type"

// strictHopHint returns the remedy for a hop failure whose message cannot
// carry it. Only an undeclared intermediate struct has one: every other
// kind (too deep, a pointer, a scalar, an unknown field) is a defect in the
// path itself, not a question about where a type may be declared.
func strictHopHint(res strictHopResult) string {
	if res.failKind == strictHopUndeclaredStruct {
		return sameFileSchemaHint
	}
	return ""
}

// resolveStrictSelectorPath walks a props-rooted field path (see
// strictcomponent.ServerPropPath) against the same-file struct schema,
// reporting exactly one diagnostic when any hop cannot preserve Go's
// selector semantics in the map-backed file renderer: too deep, a pointer
// intermediate, an intermediate whose type is a renderer scalar (nothing to
// select further through), an intermediate struct type this .gsx file does
// not declare, an unknown field at the root or past it, or a non-scalar
// leaf. The props base type itself is assumed already declared in this
// file — validateStrictRenderedProps checks that gate before calling this
// for any path, and refuses the component entirely (its own "struct schema
// is not declared in this .gsx file" diagnostic) when it is not, so this
// function is never reached with an unknown props schema.
//
// An unknown field at hop 0 (a direct field of the props struct itself)
// used to be left to the package checker (gosx#195). That deferral was a
// real backstop only for a genuinely absent field — the check program's
// real props parameter is exactly this same-file struct type, so a field
// this .gsx file never declares anywhere is a compile error there
// regardless of what this lowerer does. It was NOT a backstop for a
// promoted or unexported field: the check program compiles those exactly
// as it compiles a directly declared field, same package, so the
// lowerer's silence let a promoted or unexported hop-0 props read compile,
// check clean, and render differently between the map-backed file
// renderer (whose schema never recorded the field) and the generated Go
// (which resolved it fine). Since the props struct's schema is always
// known here (see above), this now reports hop 0 the same way it always
// reported every later hop, and the same way an <Each> binding root
// reports its own hop 0 (validateEachBindingRead, gosx#182/#184): a
// promoted, unexported, or genuinely absent field at any hop, including
// the root's own, fails closed with strictHopMessage's B1-style wording.
// This mirrors the lowerer rule strictSelectorPathType applies for its
// own, diagnostic-free callers.
func (l *lowerer) resolveStrictSelectorPath(n *gotreesitter.Node, componentName, propsType string, path []string) {
	res := l.walkStrictHops("props", propsBaseType(propsType), path)
	switch res.failKind {
	case strictHopOK:
		if !strictRendererScalarType(res.leafType) {
			if l.isStrictEachLoopableSliceType(res.leafType) {
				// A slice whose element is a same-file value struct has its
				// own admitted rendering shape (design spec section 4.2's
				// dedicated message) — the generic non-scalar-leaf message
				// below would otherwise be technically true but misleading,
				// naming builtins this slice could never satisfy instead of
				// naming the one shape that does admit it. A slice this
				// .gsx file's <Each of> would ALSO reject ([]string, a
				// scalar-element slice) keeps the generic message — v0.42.2
				// already rejected it that way, before <Each> existed.
				l.errorf(n, "strict component %s cannot render %s of type %s here; slice props render only through <Each of>", componentName, res.pathText, res.leafType)
				return
			}
			l.errorf(n, "strict component %s cannot render %s of type %s; renderer-visible props fields must use exact string, bool, integer, or floating-point builtins", componentName, res.pathText, res.leafType)
		}
	default:
		l.errorf(n, "%s", strictHopMessage(componentName, res))
		l.hintLast(strictHopHint(res))
	}
}

// isStrictEachLoopableSliceType reports whether typeName is a "[]T" whose T
// is a same-file declared struct — the same admission admitStrictEachElemType
// would give it inside a real <Each of>, checked here only to choose which
// diagnostic a bare (non-loop) read of the same field gets.
func (l *lowerer) isStrictEachLoopableSliceType(typeName string) bool {
	trimmed := strings.TrimSpace(typeName)
	if !strings.HasPrefix(trimmed, "[]") {
		return false
	}
	elem := strings.TrimSpace(strings.TrimPrefix(trimmed, "[]"))
	if elem == "" || strings.HasPrefix(elem, "*") {
		return false
	}
	_, ok := l.structTypes[elem]
	return ok
}

// resolveStrictEachSourceType validates a strict <Each of> source's
// loopable-type table (design spec section 2.3): the resolved field's
// declared type must read exactly "[]T" where T is a same-file value
// (non-pointer) struct. It returns T's bare name on success, or "" after
// reporting exactly one diagnostic.
//
// This release resolves an of source one field deep only (a direct
// props.Field, not a nested props.A.B): section 2.4 admits a nested of
// source once every intermediate is a same-file value struct, but the file
// renderer boundary (route/fileprogram.go's strictComponentAttrValue) only
// dispatches a slice-typed check off a top-level props field name today.
// Accepting a nested of source here without that boundary wiring would
// type-check and transpile cleanly while leaving the loop's element type
// unverified at the one place a caller-supplied value can diverge from what
// the body assumes — exactly the class of gap this whole design exists to
// close. No acceptance component needs a nested of source, so this widens
// only alongside the boundary support, not ahead of it.
//
// A hop-0 unknown field on the props root — promoted, unexported, or
// genuinely absent — used to be left to the package checker, the same
// deferral gosx#195 removed from resolveStrictSelectorPath. That deferral
// really did let gosx.Compile accept the component with no diagnostic at
// all here: this function returned "" and the caller (see below) simply
// skipped populating l.strictEachElems for the path, so the transpiled
// loop callback fell back to an "any"-typed loop binding — legal Go on
// its own, but every same-file read of a loop field on it (row.Label)
// then failed the SEPARATE, later `go build` of the generated program
// with a confusing "row.Label undefined (type any has no field or method
// Label)", not this function's own clear B1-style message, and only when
// something downstream of gosx.Compile actually built the generated Go —
// exactly the class of file-renderer/generated-Go divergence gosx#195
// fixed for a direct props read. gosx#195 fixed only that caller and left
// this <Each of> source caller deferring the identical field shapes; it
// now reports them here, at gosx.Compile time, with the same B1-style
// message (gosx#206).
func (l *lowerer) resolveStrictEachSourceType(n *gotreesitter.Node, componentName, propsType string, path []string) string {
	if len(path) != 1 {
		l.errorf(n, "strict component %s cannot loop over props.%s; <Each> resolves loop sources one field deep in this release, not a nested selector", componentName, strings.Join(path, "."))
		return ""
	}
	res := l.walkStrictHops("props", propsBaseType(propsType), path)
	switch res.failKind {
	case strictHopOK:
		elem, msg := admitStrictEachElemType(componentName, res.pathText, res.leafType, l.structTypes)
		if msg != "" {
			l.errorf(n, "%s", msg)
			return ""
		}
		return elem
	default:
		l.errorf(n, "%s", strictHopMessage(componentName, res))
		l.hintLast(strictHopHint(res))
		return ""
	}
}

// admitStrictEachElemType implements section 2.3's loopable-type table: the
// final hop's declared type must read exactly "[]T", T a same-file value
// struct. It returns T on success, or "" and a formatted message on any
// other shape — a scalar-element slice, a pointer-element slice, a named
// slice type, an array, a map, or a cross-file/undeclared element type
// (Go's core-type generic inference would accept a named slice type such as
// `type Rows []T`, so this exact-declared-text check is deliberately
// stricter than the Go compiler alone would be here).
func admitStrictEachElemType(componentName, pathText, leafType string, structTypes map[string]map[string]string) (elem string, message string) {
	trimmed := strings.TrimSpace(leafType)
	if strings.HasPrefix(trimmed, "[]") {
		elemType := strings.TrimSpace(strings.TrimPrefix(trimmed, "[]"))
		if strings.HasPrefix(elemType, "*") {
			return "", fmt.Sprintf("strict component %s cannot loop over %s of type %s; pointer elements cannot preserve Go nil-pointer behavior in the file renderer", componentName, pathText, trimmed)
		}
		if elemType != "" {
			if _, ok := structTypes[elemType]; ok {
				return elemType, ""
			}
		}
		if strictRendererScalarType(elemType) {
			return "", fmt.Sprintf("strict component %s cannot loop over %s of type %s; loop elements must be structs declared in this .gsx file", componentName, pathText, trimmed)
		}
	}
	return "", fmt.Sprintf("strict component %s cannot loop over %s of type %s; <Each> sources must be []T slices of structs declared in this .gsx file", componentName, pathText, trimmed)
}

// resolveStrictSpreadForwardType validates an E2 spread source read (design
// spec section 3.2, the "spread-forward" position class): the resolved
// field's declared type must be a same-file declared struct — any struct
// this .gsx file's schema knows, not necessarily one with further rendered
// reads under it, since the callee (not this read-tracking pass) owns which
// fields it actually needs. A pointer or scalar leaf fails closed with the
// same "cannot forward" shape either way.
//
// A hop-0 unknown field on the props root — promoted, unexported, or
// genuinely absent — used to return silently here (the same
// case strictHopUnknownField: return deferral gosx#195 removed from
// resolveStrictSelectorPath), instead of reporting the B1-style message
// every other walkStrictHops caller reports. For THIS caller specifically,
// every reachable spread-forward read is also a tier-1 spread call
// (isSpreadForwardTag requires a same-file strict callee), so
// validateStrictToStrictSpreadCall's own tierOneSpreadSourceType check —
// which shares walkStrictHops and fails closed unconditionally on any
// non-OK hop, without a per-caller deferral — already rejected the same
// promoted or unexported source with its own "is not renderable"
// diagnostic; the deferral here never let an affected component compile
// clean. It DID leave this function silently reporting nothing of its
// own for a shape strictHopMessage's B1-style wording exists to name, the
// same latent gap gosx#195's fix left unaddressed in this caller
// (verified during #195: no test relies on the deferred-accept
// behavior). It now reports the field here too (gosx#206), so this
// caller's own diagnostic no longer depends on a sibling check to name
// the actual cause.
func (l *lowerer) resolveStrictSpreadForwardType(n *gotreesitter.Node, componentName, propsType string, path []string) {
	res := l.walkStrictHops("props", propsBaseType(propsType), path)
	switch res.failKind {
	case strictHopOK:
		trimmed := strings.TrimSpace(res.leafType)
		if _, isStruct := l.structTypes[trimmed]; !isStruct {
			l.errorf(n, "strict component %s cannot forward %s of type %s; a spread source field must be a same-file declared struct", componentName, res.pathText, res.leafType)
		}
	default:
		l.errorf(n, "%s", strictHopMessage(componentName, res))
		l.hintLast(strictHopHint(res))
	}
}

// strictSelectorPathType is resolveStrictSelectorPath's silent counterpart:
// it resolves path the same way but reports no diagnostic, returning
// ok=false for any structural problem (an unknown field at hop 0 or later,
// too deep, a pointer or otherwise non-struct intermediate, an undeclared
// intermediate struct) and also for a leaf that is not a renderer scalar at
// all. It treats an unknown field at hop 0 the same as at any later hop —
// as of gosx#195, so does resolveStrictSelectorPath itself, so there is no
// remaining hop-0-vs-later distinction for this caller to mirror — just
// without ever emitting a diagnostic itself: validateStrictRenderedProps
// already reports each registered read's own root cause once, whichever
// hop it fails at; a second caller (the concat exact-string pass, the <If
// cond> exact-bool pass) restating it for the same path would just
// duplicate that diagnostic, so those callers use this and skip emitting
// anything when ok is false.
func (l *lowerer) strictSelectorPathType(propsType string, path []string) (string, bool) {
	return l.resolveRootedFieldType(propsBaseType(propsType), path, true)
}

// resolveRootedFieldType generalizes strictSelectorPathType to an arbitrary
// (rootType, path) pair — the same composition contract walkStrictHops
// documents — for a caller that needs the leaf type with no component-span
// diagnostic. scalarOnly=true keeps strictSelectorPathType's existing
// exact-renderer-scalar gate (the concat and <If cond> exact-type passes,
// generalized to a binding root in section 2.4); scalarOnly=false returns
// the raw leaf type unfiltered — struct, slice, or scalar — for a caller
// that applies its own admission rule, such as E2 tier 1's struct-identity
// spread check.
func (l *lowerer) resolveRootedFieldType(rootType string, path []string, scalarOnly bool) (string, bool) {
	if len(path) == 0 {
		return "", false
	}
	res := l.walkStrictHops("", rootType, path)
	if res.failKind != strictHopOK {
		return "", false
	}
	if scalarOnly && !strictRendererScalarType(res.leafType) {
		return "", false
	}
	return res.leafType, true
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
	_, typedLegacyCallee := l.typedLegacyProps[tag]
	if l.strict && legacyCallee && !typedLegacyCallee {
		l.errorf(n, "strict component cannot call untyped legacy component %s; component styles may coexist but calls must stay within one style", tag)
		l.hintLast("declare " + tag + "'s props parameter with a struct type declared in this file, or declare " + tag + " as a strict component")
		return
	}
	if !l.strict && strictCallee {
		l.validateLegacyToStrictCall(n, tag, attrs, children)
		return
	}
	// gosx#240: inside a strict body a TYPED legacy callee answers to the
	// callee-side rules a strict callee answers to — the explicit-supply
	// rule, the single-spread shape, and the tier-1 identity proof. The
	// widening is one-directional on purpose. Every shape below was an
	// outright error before this release, so admitting some of them cannot
	// change any program that compiles today, whereas imposing the same
	// rules on a LEGACY caller would newly reject calls that work now.
	if l.strict && typedLegacyCallee {
		strictCallee = true
	}
	if l.strictServer && tag == "If" && !strictCallee {
		l.validateStrictConditionalCall(n, attrs)
		return
	}
	if l.strictServer && tag == "Each" && !strictCallee {
		// Shape, binding, and type rules for a strict <Each> run in the
		// second validation pass (validateStrictServerExpressions), which
		// tracks the active binding scope while it walks the built IR tree
		// — see section 2.2 of the design spec. Nothing to check here.
		return
	}
	if l.strictServer && IsComponent(tag) && !strictCallee {
		if l.isSharedComponentTag(tag) {
			l.validateSharedComponentCallShape(n, tag, attrs)
			return
		}
		l.errorf(n, "strict server component %s is not renderable; v0.39 strict server components may call only same-file strict components", tag)
		return
	}
	if !strictCallee {
		return
	}
	if attrHasSpread(attrs) {
		l.validateStrictToStrictSpreadCall(n, tag, attrs, children)
		return
	}
	calleePropsType, acceptsProps := l.calleePropsType(tag)
	if !acceptsProps && len(attrs) > 0 {
		l.errorf(n, "strict component %s does not accept props", tag)
	}
	if required := l.strictReads[tag]; len(required) > 0 {
		supplied := make(map[string]struct{}, len(attrs))
		aliases := l.structFields[propsBaseType(calleePropsType)]
		hasUnknownSameFileAttr := false
		for _, attr := range attrs {
			name := attr.Name
			if len(aliases) > 0 {
				field, known := aliases[name]
				if !known || field == "" {
					hasUnknownSameFileAttr = true
					continue
				}
				name = field
			}
			supplied[name] = struct{}{}
		}
		// Let the package checker produce the authoritative unknown-field
		// diagnostic before checking omissions in a same-file schema.
		if !hasUnknownSameFileAttr {
			// required is keyed by dot-joined read path ("Player.Name"); the
			// explicit-supply rule only needs each path's root field — the
			// caller supplies the whole root value once, regardless of how
			// many nested paths this callee reads under it.
			roots := make(map[string]struct{}, len(required))
			for path := range required {
				root, _, _ := strings.Cut(path, ".")
				roots[root] = struct{}{}
			}
			fields := make([]string, 0, len(roots))
			for field := range roots {
				fields = append(fields, field)
			}
			sort.Strings(fields)
			for _, field := range fields {
				// Companion Go structs are intentionally exact at the package-check
				// boundary, but accepting the schema alias here avoids masking that
				// more useful Go diagnostic as a zero-value omission.
				if len(aliases) == 0 {
					if _, ok := supplied[lowerCamelInitialism(field)]; ok {
						continue
					}
				}
				if _, ok := supplied[field]; !ok {
					l.errorf(n, "strict component %s requires prop %s because its renderer reads props.%s; provide it explicitly to preserve Go zero-value semantics", tag, field, field)
				}
			}
		}
	}
	l.validateStrictCalleeChildren(n, tag, children)
}

// isSharedComponentTag reports whether tag is a dotted component tag whose
// alias names a shared (./ or ../ prefixed) import recorded in this file's
// own Program.Imports (lowerImportSpec runs before any component, so
// l.prog.Imports is already complete — see isImportAlias's doc comment for
// the same guarantee).
//
// This is a purely syntactic, file-local signal. Lower performs no file
// I/O — the LSP runs it synchronously on every keystroke with no debounce,
// so adding I/O here would be paid per keystroke (shared components design,
// section 6) — so this function can say only that the call SHAPE reaches
// through a shared import, never that the target directory actually
// declares a matching strict component. gosx check (strictcheck) resolves
// the target directory and proves that, through the real Go compiler.
func (l *lowerer) isSharedComponentTag(tag string) bool {
	alias, _, ok := SplitMemberTag(tag)
	if !ok {
		return false
	}
	for _, imp := range l.prog.Imports {
		if imp.Alias == alias {
			return IsSharedImportPath(imp.Path)
		}
	}
	return false
}

// validateSharedComponentCallShape proves call SHAPE only for a shared
// (./ or ../ prefixed) import call: single spread versus named attributes,
// the one rule every strict callee answers to regardless of where its body
// lives (singleSpreadShape). It never inspects the target directory's
// declared props — Lower has no way to read them (see isSharedComponentTag)
// — so a named-attribute call always passes here; gosx check proves field
// names and types against the target's real Go declaration, and the file
// renderer re-proves spread coverage at the render boundary
// (strictSpreadProps), exactly as it already does for a same-file strict
// callee.
func (l *lowerer) validateSharedComponentCallShape(n *gotreesitter.Node, tag string, attrs []Attr) {
	if !attrHasSpread(attrs) {
		return
	}
	if _, ok := singleSpreadShape(attrs); !ok {
		l.errorf(n, "shared component call %s accepts at most one spread attribute and no other attributes", tag)
	}
}

// validateStrictCalleeChildren is the ONE arity rule for children at a callee
// the strict rules apply to, shared by all three call shapes (named
// attributes, a strict caller's single spread, a legacy caller's single
// spread). A callee that places children accepts them; a callee that does not
// rejects them, with a message that names both remedies instead of truncating
// the content silently.
//
// Since gosx#240 a TYPED legacy component reaches this rule too, because a
// strict body may now call one. It answers the same question there: the flag
// comes from l.childrenHoles, which reads every same-file component
// declaration, so a legacy body that writes {children} accepts children and
// one that does not is told to add it — advice that is true, because
// writeLocalComponent binds the name for every same-file callee.
//
// The rule is arity only. It proves nothing about the children themselves and
// has nothing to prove: they are markup the CALLER owns, rendered by the
// caller's renderer, in the caller's env, against the caller's program,
// before the callee is entered. Every read inside them already passed the
// caller's own lower-time, check-time, and render-time proofs, so no
// obligation crosses the call.
func (l *lowerer) validateStrictCalleeChildren(n *gotreesitter.Node, tag string, children []NodeID) {
	if len(children) == 0 {
		return
	}
	if l.childrenHoles[tag] {
		return
	}
	l.errorf(n, "strict component %s renders no children; remove the child content or render {children} in %s's body", tag, tag)
}

// validateStrictCalleeSlots is validateStrictCalleeChildren's named-slot
// counterpart (gosx#249): every slot name a caller supplies through a
// static slot="Name" attribute on a direct child must be one the callee
// actually declares (l.slotHoles), the same arity precedent — a caller
// error, not a silent no-op, when a callee cannot place what it is
// handed. It answers this independently of props shape, so it is called
// once, from lowerGSXElement, rather than duplicated across
// validateStrictComponentCall's three call shapes the way
// validateStrictCalleeChildren is: a named slot is supplied only through
// a direct child's own attribute, never through the call's own attribute
// list, so which of the three prop-shapes the call uses (named
// attributes, a strict caller's single spread, a legacy caller's single
// spread) has nothing to do with which slots it filled.
//
// l.slotHoles is populated for a strict component only (gosx#249 scopes
// named slots to strict components, the same scope AcceptsChildren's own
// legacy-inclusive comment explicitly does NOT extend to slots), so a
// slot supplied to a legacy or unresolved callee always fails this check
// — l.slotHoles[tag] is empty for one, so nothing is ever "declared".
func (l *lowerer) validateStrictCalleeSlots(n *gotreesitter.Node, tag string, slots map[string]NodeID) {
	if len(slots) == 0 {
		return
	}
	declared := make(map[string]struct{}, len(l.slotHoles[tag]))
	for _, name := range l.slotHoles[tag] {
		declared[name] = struct{}{}
	}
	names := make([]string, 0, len(slots))
	for name := range slots {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, ok := declared[name]; !ok {
			l.errorf(n, "strict component %s declares no slot named %q; declare {%s} in %s's body or remove this slot", tag, name, strictcomponent.SlotBindingName(name), tag)
		}
	}
}

// attrHasSpread reports whether attrs contains at least one spread
// attribute — the trigger for validateStrictComponentCall's E2 branches.
func attrHasSpread(attrs []Attr) bool {
	for _, attr := range attrs {
		if attr.Kind == AttrSpread {
			return true
		}
	}
	return false
}

// singleSpreadShape reports whether attrs is exactly one attribute, and
// that attribute is a spread — the only call shape design spec section 3.1
// admits at a strict callee: no named attributes beside the spread, no
// second spread.
func singleSpreadShape(attrs []Attr) (Attr, bool) {
	if len(attrs) != 1 || attrs[0].Kind != AttrSpread {
		return Attr{}, false
	}
	return attrs[0], true
}

// validateLegacyToStrictCall narrows the legacy-caller/strict-callee
// cross-style ban (design spec section 3.3, E2 tier 2): a legacy body may
// call a same-file strict component when the call is the single-spread
// shape. The lowerer proves shape only here — a legacy expression has no
// declared type — so the renderer boundary (strictSpreadProps,
// route/fileprogram.go) re-proves the value structurally at run time. Every
// other shape (no attributes, named attributes, multiple spreads, spread
// plus named attributes) keeps the v0.39 cross-style ban, with an updated
// message for the named-attributes case that names the supported spelling.
//
// Shape is not the only thing provable here. One single-spread SOURCE —
// the enclosing legacy component's own props identifier — is statically
// known to fail at that renderer boundary, whatever the data is
// (validateLegacyPropsSpread, gosx#229).
func (l *lowerer) validateLegacyToStrictCall(n *gotreesitter.Node, tag string, attrs []Attr, children []NodeID) {
	if len(attrs) == 0 {
		l.errorf(n, "legacy component cannot call strict component %s; component styles may coexist but calls must stay within one style", tag)
		return
	}
	if spread, ok := singleSpreadShape(attrs); ok {
		l.validateStrictCalleeChildren(n, tag, children)
		l.validateLegacyPropsSpread(n, tag, spread)
		return
	}
	l.errorf(n, "legacy component cannot call strict component %s with named attributes; pass one {...source} spread and the renderer will prove it at the boundary", tag)
}

// calleePropsType reports the declared props type of a same-file callee the
// strict rules apply to — a strict component, or (gosx#240) a typed legacy
// one. The two categories declare their schema the same way, so every
// callee-side rule reads the type through this one lookup instead of
// naming l.strictProps directly.
func (l *lowerer) calleePropsType(tag string) (string, bool) {
	if propsType, ok := l.strictProps[tag]; ok {
		return propsType, true
	}
	propsType, ok := l.typedLegacyProps[tag]
	return propsType, ok
}

// validateLegacyPropsSpread rules on gosx#229's shape: a legacy body
// spreading its OWN props identifier into a strict callee. Shape alone
// accepts that call (validateLegacyToStrictCall above), so this is where
// the two legacy categories part.
//
// An UNTYPED legacy component (`props any`, an AttrList, a props type from
// another file, or a props parameter spelled something other than "props")
// is rejected, unchanged from v0.48. The source expression carries no
// declared type anything could be checked against, so before that rule the
// composition compiled, checked, and transpiled clean, then failed at every
// render — total, data-independent, and only on a code path that runs once
// real data exists. An untyped legacy render frame binds props to the
// reduced map[string]any the file renderer builds out of the call site's
// attributes (localComponentProps, route/fileprogram.go) — never a Go
// struct, even when a struct value produced the attributes — and
// strictSpreadProps proves field coverage on struct values only, because a
// map can omit a key where the generated-Go twin would synthesize a typed
// zero.
//
// A TYPED legacy component is retrofitted instead of rejected (gosx#240).
// Its props type is a struct declared in this same file, so the lowerer can
// prove the forward at the DECLARATION: validateTypedLegacyPropsForward
// checks the caller's own struct declares every field the callee renders,
// with the same declared type. That proof does not depend on how any caller
// invokes the typed legacy component, so it holds identically at every one
// of its call sites — the property that made this retrofit acceptable where
// preserving a raw value beside the flattened map was not.
//
// The rule is deliberately narrow in both directions. It fires only on the
// bare props identifier, so the shapes that already render keep compiling: a
// struct-typed FIELD of props ({...props.Away}) survives the shallow flatten
// with its own type intact, and any other expression (a local, a loader
// value, a page's data binding) is unconstrained by this rule.
func (l *lowerer) validateLegacyPropsSpread(n *gotreesitter.Node, tag string, spread Attr) {
	propsName := strings.TrimSpace(l.currentLegacyPropsName)
	if propsName != "props" || strings.TrimSpace(spread.Expr) != propsName {
		return
	}
	caller := strings.TrimSpace(l.currentLegacyComponent)
	if caller == "" {
		return
	}
	if callerProps, typed := l.typedLegacyProps[caller]; typed {
		l.validateTypedLegacyPropsForward(n, caller, callerProps, tag)
		return
	}
	l.errorf(n, "untyped legacy component %s cannot spread props into strict component %s; an untyped legacy render frame binds props to map[string]any, and the strict spread boundary proves field coverage on struct values only", caller, tag)
	l.hintLast("declare " + caller + "'s props parameter with a struct type declared in this file, or declare " + caller + " as a strict component so props keeps its declared type")
}

// validateTypedLegacyPropsForward is gosx#240's declaration-level proof for
// a typed legacy body forwarding its whole props value into a strict
// callee. Both schemas are structs declared in this one file, so the check
// is exact: for every field the callee renders, the caller's props struct
// must declare a field of the same name and the same declared type.
//
// Type identity, rather than the render boundary's structural rule for a
// nested struct (gosx#230), is the right demand here for two reasons. The
// caller forwards its WHOLE props value, so the callee reads the caller's
// own fields by name and there is no converter type in between. And both
// names resolve in the same file, so an identical spelling is an identical
// type — the check costs an author nothing that is not already true.
//
// A callee whose props type is not declared in this file has no schema to
// compare against. validateStrictRenderedProps already reports that on the
// callee's own declaration, so this stays silent rather than reporting the
// same defect a second time at every call site.
func (l *lowerer) validateTypedLegacyPropsForward(n *gotreesitter.Node, caller, callerPropsType, tag string) {
	reads := l.strictReads[tag]
	if len(reads) == 0 {
		return
	}
	calleePropsType := l.strictProps[tag]
	calleeFields := l.structTypes[propsBaseType(calleePropsType)]
	if len(calleeFields) == 0 {
		return
	}
	callerFields := l.structTypes[propsBaseType(callerPropsType)]
	roots := make(map[string]struct{}, len(reads))
	for path := range reads {
		root, _, _ := strings.Cut(path, ".")
		roots[root] = struct{}{}
	}
	ordered := make([]string, 0, len(roots))
	for root := range roots {
		ordered = append(ordered, root)
	}
	sort.Strings(ordered)
	for _, root := range ordered {
		want, declared := calleeFields[root]
		if !declared {
			continue
		}
		got, present := callerFields[root]
		if !present {
			l.errorf(n, "typed legacy component %s cannot spread props into strict component %s: %s does not declare field %s (%s), which %s renders as props.%s", caller, tag, callerPropsType, root, want, tag, root)
			l.hintLast("add " + root + " " + want + " to " + callerPropsType + ", or spread a struct-typed field of props instead (for example {...props.Team})")
			continue
		}
		if got != want {
			l.errorf(n, "typed legacy component %s cannot spread props into strict component %s: field %s is %s on %s and %s on %s; a whole-props forward needs the declared types to match", caller, tag, root, got, callerPropsType, want, calleePropsType)
		}
	}
}

// validateStrictToStrictSpreadCall validates E2 tier 1 (design spec section
// 3.2): a strict caller may spread exactly one value into a same-file
// strict callee when the source's declared type, resolved against the
// caller's own schema, is exactly the callee's props type. Coverage is then
// trivial: every rendered read is a field of the very struct the callee
// declares, so no per-field proof is needed here — the emitted Go call is
// verbatim (transpile.go), so the Go compiler proves the rest.
func (l *lowerer) validateStrictToStrictSpreadCall(n *gotreesitter.Node, tag string, attrs []Attr, children []NodeID) {
	spread, ok := singleSpreadShape(attrs)
	if !ok {
		l.errorf(n, "strict component call %s accepts at most one spread attribute and no other attributes", tag)
		return
	}
	l.validateStrictCalleeChildren(n, tag, children)
	calleeProps, _ := l.calleePropsType(tag)
	if strings.TrimSpace(calleeProps) == "" {
		l.errorf(n, "strict component %s does not accept props", tag)
		return
	}
	sourceType, ok := l.tierOneSpreadSourceType(spread.Expr)
	if !ok {
		l.errorf(n, "strict spread source %q is not renderable; spread sources are props or a props field selector", strings.TrimSpace(spread.Expr))
		return
	}
	if propsBaseType(sourceType) != propsBaseType(calleeProps) {
		l.errorf(n, "strict spread source %s has type %s, want exact %s; a strict caller spreads a value whose declared type is the callee props type", strings.TrimSpace(spread.Expr), sourceType, calleeProps)
	}
}

// tierOneSpreadSourceType resolves a strict spread source's declared type
// against l.currentStrictPropsType, the schema of the component whose body
// is being lowered — validateStrictToStrictSpreadCall's caller-side half of
// the tier-1 identity proof. It admits bare props (the whole props value)
// and any props field selector the widened selector rule (section 2.4)
// admits; every other expression shape returns ok=false, reported by the
// caller with the "is not renderable" message (design spec section 4.4).
func (l *lowerer) tierOneSpreadSourceType(expr string) (string, bool) {
	if strings.TrimSpace(expr) == "props" {
		return l.currentStrictPropsType, true
	}
	path, ok := strictcomponent.ServerPropPath(expr)
	if !ok {
		return "", false
	}
	return l.resolveRootedFieldType(propsBaseType(l.currentStrictPropsType), path, false)
}

// validateStrictConditionalCall enforces the shape of a strict <If cond={...}>
// call: exactly one attribute, named cond, of expression kind. It runs only
// when tag "If" is not shadowed by a same-file strict component (the carve-out
// in validateStrictComponentCall). Children are unrestricted — that is the
// point of the tag — so this checks attributes only.
func (l *lowerer) validateStrictConditionalCall(n *gotreesitter.Node, attrs []Attr) {
	condCount := 0
	sawSpreadError := false
	for i := range attrs {
		attr := &attrs[i]
		if attr.Name == "cond" && attr.Kind == AttrExpr {
			condCount++
			continue
		}
		if attr.Kind == AttrSpread {
			l.errorf(n, "strict <If> does not accept spread attributes; cond is the only supported attribute")
			sawSpreadError = true
			continue
		}
		if attr.Name != "cond" {
			l.errorf(n, "strict <If> does not accept attribute %q; cond is the only supported attribute", attr.Name)
		}
	}
	// A spread attribute already reported its own diagnostic above; the
	// cond-count check below would otherwise double-report the same call
	// (a spread never counts toward condCount, so it always fails it too).
	if sawSpreadError {
		return
	}
	if condCount != 1 {
		l.errorf(n, "strict <If> requires exactly one cond attribute")
	}
}

func (l *lowerer) validateStrictHTMLElement(n *gotreesitter.Node, tag string, attrs []Attr) {
	if !l.strictServer || IsComponent(tag) {
		return
	}
	for _, attr := range attrs {
		if attr.Kind == AttrSpread {
			l.errorf(n, "spread attributes are not supported on strict server HTML elements; map and slice expansion cannot preserve generated Go rendering semantics")
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

// isImportAlias reports whether name is this file's explicit alias for an
// import (import name "path"). Imports lower before any component
// (lowerSourceFile's single pass visits import_declaration before any
// gosx_component_declaration — Go itself requires imports at the top of
// the file), so l.prog.Imports is complete by the time strictEachShape
// calls this. Only an EXPLICIT alias is checked: a bare `import "path"`'s
// effective identifier is the imported package's own declared name, which
// this lowerer does not resolve, so that narrower collision is left
// alone (gosx#182/#184 nit n-1).
func (l *lowerer) isImportAlias(name string) bool {
	if name == "" {
		return false
	}
	for _, imp := range l.prog.Imports {
		if imp.Alias == name {
			return true
		}
	}
	return false
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

	// Lower the GSX tree. The caller context (name plus props identifier)
	// stays set for the whole walk so a strict call site inside this body
	// can name the enclosing legacy component and recognize its props
	// binding — see validateLegacyPropsSpread (gosx#229).
	prevLegacyComponent := l.currentLegacyComponent
	prevLegacyPropsName := l.currentLegacyPropsName
	l.currentLegacyComponent = name
	l.currentLegacyPropsName = propsName
	rootID := l.lowerGSXNode(gsxRoot)
	l.currentLegacyComponent = prevLegacyComponent
	l.currentLegacyPropsName = prevLegacyPropsName

	// Analyze the function body for signal/computed/handler declarations.
	// This extracts the component scope needed for island lowering.
	scope := l.analyzeBody(bodyNode)

	// Run before reading the directives, so a misspelled one is reported as
	// itself rather than as a component that mysteriously is not an island.
	l.checkDirectiveTypos(n)

	// gosx#240: a legacy component whose props parameter names a struct
	// declared in this same file carries the same schema a strict one does.
	// PropsFields and PropsPaths are a declaration-level property here, the
	// same as for a strict component: they describe what this body reads,
	// never how a caller invokes it, so every call site of this component
	// sees one answer.
	_, propsTyped := l.typedLegacyProps[name]
	var propsFields, propsPaths map[string]string
	if propsTyped {
		propsFields, propsPaths = l.copyStrictPropTypes(propsType, l.strictReads[name])
	}

	comp := Component{
		Name:        name,
		PropsType:   propsType,
		PropsName:   propsName,
		PropsFields: propsFields,
		PropsPaths:  propsPaths,
		PropsTyped:  propsTyped,
		// Read, never recomputed: collectStrictSchemas already decided this
		// for every component in the file, whatever its category, and every
		// call-site rule read the same map. One owner.
		AcceptsChildren: l.childrenHoles[name],
		Syntax:          ComponentSyntaxLegacy,
		Root:            rootID,
		IsIsland:        l.hasIslandDirective(n),
		Scope:           scope,
		Span:            l.span(n),
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
	// A strict island's props cross the client boundary through the same
	// proof a strict server component uses (localComponentProps /
	// strictSpreadProps in route/fileprogram.go), then travel to the
	// browser as the same flat JSON map every island already ships — the
	// client VM already exposes that map under both its flat keys and a
	// reserved "props" object binding (client/vm/island.go's parseProps),
	// so a strict island body's props.Field selectors resolve with no VM
	// change. See CHANGELOG.md for the full writeup.
	if isEngine {
		l.errorf(n, "strict engine declarations are not yet supported: the file renderer has no typed dispatch for an engine surface's per-frame host calls, so a strict engine body cannot be executed faithfully")
		l.hintLast("declare this engine with the legacy func Name(...) Node style")
		return
	}
	propsName, propsType := l.extractStrictProps(n)
	componentName := l.text(nameNode)
	if propsType != "" && propsName != "props" {
		l.errorf(n, "strict component props parameter must be named props; got %q", propsName)
		l.hintLast("use component " + componentName + "(props: " + propsType + ")")
	}
	l.validateStrictRenderedProps(n, componentName, propsType)

	gsxRoot := l.strictComponentGSXRoot(bodyNode, isIsland || isEngine)
	if gsxRoot == nil {
		return
	}

	wasStrict := l.strict
	wasStrictServer := l.strictServer
	prevComponent := l.currentStrictComponent
	prevPropsType := l.currentStrictPropsType
	l.strict = true
	l.strictServer = !isIsland && !isEngine
	l.currentStrictComponent = componentName
	l.currentStrictPropsType = propsType
	rootID := l.lowerGSXNode(gsxRoot)
	l.strict = wasStrict
	l.strictServer = wasStrictServer
	l.currentStrictComponent = prevComponent
	l.currentStrictPropsType = prevPropsType
	scope := l.analyzeBody(bodyNode)
	propsFields, propsPaths := l.copyStrictPropTypes(propsType, l.strictReads[componentName])
	comp := Component{
		Name:        componentName,
		PropsType:   propsType,
		PropsName:   propsName,
		PropsFields: propsFields,
		PropsPaths:  propsPaths,
		// Read, never recomputed: collectStrictSchemas already decided this
		// for every strict component in the file, and every call-site rule
		// read the same map. One owner.
		AcceptsChildren: l.childrenHoles[componentName],
		AcceptsSlots:    l.slotHoles[componentName],
		Syntax:          ComponentSyntaxStrict,
		Root:            rootID,
		IsIsland:        isIsland,
		Scope:           scope,
		Span:            l.span(n),
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
		comp.PropsSlices = l.validateStrictServerExpressions(comp.Root, componentName, propsType)
	}
	l.prog.Components = append(l.prog.Components, comp)
}

// copyStrictPropTypes builds the two IR maps the renderer boundary uses.
// PropsFields records, for the root field of every registered read path,
// that root's own declared type (a scalar builtin for a direct read, a
// same-file struct name for a nested read's root, or a "[]T" slice text for
// an <Each of> loop-source read — section 2.6's back-compat rule: the
// explicit-supply rule and the boundary dispatch see the field's raw
// declared text unchanged, regardless of read class). PropsPaths records,
// for every read path with at least one hop past its root, the leaf
// field's declared type keyed by the full dot-joined path — populated only
// when the path resolves cleanly; a broken path already has its own
// diagnostic from validateStrictRenderedProps, so this silently omits it
// rather than producing partial data for a component that is failing to
// compile anyway. A loop-source read is always one field deep in this
// release (resolveStrictEachSourceType), so it never reaches the
// len(segments) > 1 branch below; its element schema lives in PropsSlices
// instead, built by validateStrictServerExpressions.
func (l *lowerer) copyStrictPropTypes(propsType string, reads map[string]strictReadClass) (map[string]string, map[string]string) {
	fields := l.structTypes[propsBaseType(propsType)]
	if len(fields) == 0 || len(reads) == 0 {
		return nil, nil
	}
	propsFields := make(map[string]string, len(reads))
	var propsPaths map[string]string
	for path := range reads {
		segments := strings.Split(path, ".")
		root := segments[0]
		if fieldType, ok := fields[root]; ok {
			propsFields[root] = fieldType
		}
		if len(segments) > 1 {
			if leafType, ok := l.strictSelectorPathType(propsType, segments); ok {
				if propsPaths == nil {
					propsPaths = make(map[string]string)
				}
				propsPaths[path] = leafType
			}
		}
	}
	if len(propsFields) == 0 {
		propsFields = nil
	}
	return propsFields, propsPaths
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

// eachScope is one active <Each> binding the second validation pass has
// pushed while it walks a strict component's IR tree — design spec section
// 2.2's scoping rule and section 2.4's composition contract: a selector
// chain may root at props or at any name in the active scope, resolved
// through itemType (the element struct) instead of the props struct. It
// forms a parent-linked chain the same shape as route/fileeval.go's
// fileRenderScope, so nested loops shadow newest-first the same way at both
// compile time and render time — with shadowing banned (section 2.2), the
// two cannot disagree.
type eachScope struct {
	parent    *eachScope
	itemName  string
	itemType  string // same-file element struct name
	indexName string
	reads     map[string]string // binding-relative read paths -> leaf types; becomes this level's SlicePropSchema.Reads
}

func (s *eachScope) hasBinding(name string) bool {
	for cur := s; cur != nil; cur = cur.parent {
		if cur.itemName == name || (cur.indexName != "" && cur.indexName == name) {
			return true
		}
	}
	return false
}

// resolve looks up name in the active scope chain, reporting whether it is
// an item binding (with its element type) or an index binding.
func (s *eachScope) resolve(name string) (itemType string, isIndex, ok bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if cur.itemName == name {
			return cur.itemType, false, true
		}
		if cur.indexName != "" && cur.indexName == name {
			return "", true, true
		}
	}
	return "", false, false
}

func (s *eachScope) items() []string {
	var out []string
	for cur := s; cur != nil; cur = cur.parent {
		out = append(out, cur.itemName)
	}
	return out
}

func (s *eachScope) indices() []string {
	var out []string
	for cur := s; cur != nil; cur = cur.parent {
		if cur.indexName != "" {
			out = append(out, cur.indexName)
		}
	}
	return out
}

func (s *eachScope) strictScope() strictcomponent.Scope {
	return strictcomponent.Scope{Items: s.items(), Indices: s.indices()}
}

// recordRead registers a binding-rooted read's resolved leaf type into the
// owning scope level's Reads accumulator (SlicePropSchema.Reads), walking
// outward to find the level whose item name is root — a nested Each may
// read an outer binding's field, and that read belongs to the outer
// level's schema, not the innermost one.
func (s *eachScope) recordRead(root string, path []string, leafType string) {
	for cur := s; cur != nil; cur = cur.parent {
		if cur.itemName == root {
			if cur.reads != nil {
				cur.reads[strings.Join(path, ".")] = leafType
			}
			return
		}
	}
}

// validateStrictServerExpressions walks a strict component's built IR tree,
// validating every expression hole and expression/spread attribute, and
// (design spec section 2.2) pushing an unshadowed <Each>'s declared
// bindings onto the active scope before visiting its children. It returns
// the component's PropsSlices — every depth-1 <Each of> loop-source read
// this walk found, together with the binding-relative fields the loop body
// actually reads (nil when the component has no strict <Each>).
func (l *lowerer) validateStrictServerExpressions(root NodeID, componentName, propsType string) map[string]SlicePropSchema {
	seen := make(map[NodeID]bool)
	_, ifShadowed := l.strictNames["If"]
	_, eachShadowed := l.strictNames["Each"]
	slices := make(map[string]SlicePropSchema)
	var visit func(NodeID, *eachScope)
	visit = func(id NodeID, scope *eachScope) {
		if seen[id] || int(id) >= len(l.prog.Nodes) {
			return
		}
		seen[id] = true
		node := &l.prog.Nodes[id]
		if node.Kind == NodeExpr {
			// CHILD position. A NodeExpr is only ever built from a
			// jsx_expression_container in child position (lowerExprContainer);
			// an attribute expression is stored on Attr.Expr instead and takes
			// the attribute branch below. That split is what lets the two
			// positions admit different identifier sets.
			l.validateStrictServerChildExpression(node.Span, node.Text, componentName, propsType, scope)
		}
		isBuiltinIf := node.Kind == NodeComponent && node.Tag == "If" && !ifShadowed
		isBuiltinEach := node.Kind == NodeComponent && node.Tag == "Each" && !eachShadowed
		if isBuiltinEach {
			childScope, ok := l.enterStrictEach(node, componentName, propsType, scope, slices)
			if !ok {
				return
			}
			for _, child := range node.Children {
				visit(child, childScope)
			}
			return
		}
		for _, attr := range node.Attrs {
			if isBuiltinIf && attr.Name == "cond" && attr.Kind == AttrExpr {
				l.validateStrictConditionalExpression(node.Span, attr.Expr, componentName, propsType, scope)
				continue
			}
			if attr.Kind == AttrExpr {
				// ATTRIBUTE position. This entry point does not admit
				// children: an attribute value is written inside quotes, and
				// splicing rendered markup there produces broken HTML that no
				// escaping rule can repair. See
				// ValidateServerChildExpressionScope's doc comment.
				l.validateStrictServerExpression(node.Span, attr.Expr, componentName, propsType, scope)
			}
			// AttrSpread is not revalidated as an ordinary expression here:
			// every spread position already has its own, more specific
			// pass-1 validation (validateStrictToStrictSpreadCall for a
			// strict callee's proven E2 tier-1 shape, whose admission rule
			// legitimately differs from an ordinary expression position —
			// bare props is a valid spread source but not a valid bare
			// expression; validateStrictHTMLElement's unconditional ban for
			// a strict HTML element; the "not renderable" rejection for any
			// other unresolved component tag). Revalidating here would
			// either duplicate a diagnostic or wrongly reject a proven
			// spread with the bare-props message.
		}
		for _, child := range node.Children {
			visit(child, scope)
		}
	}
	visit(root, nil)
	if len(slices) == 0 {
		return nil
	}
	return slices
}

func (l *lowerer) validateStrictServerExpression(span Span, source, componentName, propsType string, scope *eachScope) {
	if err := strictcomponent.ValidateServerExpressionScope(source, scope.strictScope()); err != nil {
		l.reportStrictServerExpression(span, source, err)
		return
	}
	l.validateStrictBindingReadTypes(span, source, componentName, scope)
	l.validateStrictExpressionTypes(span, source, componentName, propsType, scope)
}

// validateStrictServerChildExpression is validateStrictServerExpression for a
// whole child expression hole. It differs in the validator entry point only,
// so the two type passes below stay shared and no rule is duplicated.
//
// Both type passes are no-ops for a bare children: it holds no selector, so
// ServerExpressionRootedPaths reports no path, and it is no `+` chain, so
// ServerConcatRootedPaths reports none either. There is nothing to type-check
// in an already-rendered node.
func (l *lowerer) validateStrictServerChildExpression(span Span, source, componentName, propsType string, scope *eachScope) {
	if err := strictcomponent.ValidateServerChildExpressionScope(source, scope.strictScope()); err != nil {
		l.reportStrictServerExpression(span, source, err)
		return
	}
	l.validateStrictBindingReadTypes(span, source, componentName, scope)
	l.validateStrictExpressionTypes(span, source, componentName, propsType, scope)
}

func (l *lowerer) reportStrictServerExpression(span Span, source string, err error) {
	l.errs = append(l.errs, Diagnostic{
		Span:    span,
		Message: fmt.Sprintf("strict server expression %q is not renderable: %v", strings.TrimSpace(source), err),
		Hint:    "use literals or props field selection; compute, index, and call methods before rendering",
	})
}

// validateStrictBindingReadTypes is a props read's counterpart for a loop
// binding: props reads get their scalar-ness checked once, early, by
// validateStrictRenderedProps, before any <Each> scope even exists to
// track; a binding read has no such early pass (the binding set is only
// known once this second pass walks an <Each> node), so this scans source
// for every binding-rooted path — bare, or nested inside a concat or cond
// — and resolves+records each one here. A props-rooted path is skipped: it
// already went through validateStrictRenderedProps, and restating it here
// would just duplicate the diagnostic.
func (l *lowerer) validateStrictBindingReadTypes(span Span, source, componentName string, scope *eachScope) {
	for _, rp := range strictcomponent.ServerExpressionRootedPaths(source, scope.strictScope()) {
		if rp.Root == "props" {
			continue
		}
		l.validateEachBindingRead(span, componentName, scope, rp.Root, rp.Path)
	}
}

// validateEachBindingRead resolves one binding-rooted read against its
// owning <Each> element schema (walkStrictHops, generalized per design spec
// section 2.4 to a binding root), reports exactly one diagnostic on
// failure, and records a clean scalar resolution into the owning scope
// level's SlicePropSchema.Reads.
//
// Unlike the props-rooted callers, a hop-0 unknown field here (root's own
// field on the element struct) is NOT deferred: the element struct is
// fully known — this is the whole point of a typed <Each> — and the
// package checker gives no backstop for a promoted or unexported field (it
// resolves those the same as a declared one, same package). Deferring
// would let this exact field read compile while the transpiled Go and the
// map-backed file renderer diverge on what it resolves to (gosx#182/#184
// M-2). So this reports the same message an hop-i>0 unknown field gets.
func (l *lowerer) validateEachBindingRead(span Span, componentName string, scope *eachScope, root string, path []string) {
	itemType, isIndex, found := scope.resolve(root)
	if !found {
		return
	}
	if isIndex {
		l.errs = append(l.errs, Diagnostic{Span: span, Message: fmt.Sprintf("strict component %s cannot use index binding %s in a selector; the index is an int value", componentName, root)})
		return
	}
	res := l.walkStrictHops(root, itemType, path)
	switch res.failKind {
	case strictHopOK:
		if !strictRendererScalarType(res.leafType) {
			l.errs = append(l.errs, Diagnostic{Span: span, Message: fmt.Sprintf("strict component %s cannot render %s of type %s; loop selectors must reach an exact scalar field", componentName, res.pathText, res.leafType)})
			return
		}
		scope.recordRead(root, path, res.leafType)
	default:
		l.errs = append(l.errs, Diagnostic{Span: span, Message: strictHopMessage(componentName, res), Hint: strictHopHint(res)})
	}
}

// resolveScopedFieldType resolves path's leaf type against root's schema —
// the props struct when root is "props", or an active binding's element
// struct otherwise (design spec section 2.4) — for the concat and cond
// exact-type passes, which need the type regardless of which root supplied
// it.
func (l *lowerer) resolveScopedFieldType(propsType string, scope *eachScope, root string, path []string) (string, bool) {
	if root == "props" {
		return l.strictSelectorPathType(propsType, path)
	}
	itemType, isIndex, found := scope.resolve(root)
	if !found || isIndex {
		return "", false
	}
	return l.resolveRootedFieldType(itemType, path, true)
}

// validateStrictExpressionTypes runs the type-side gate for expression shapes
// the syntactic validator accepts but cannot itself type-check: the
// string-concatenation chain. Every operand — a direct or nested props
// field, or (section 2.4) a direct or nested binding field — must resolve,
// through the same-file struct schema, to declared type exactly string —
// not a named string type, not []byte, not a struct. A path that fails to
// resolve at all is skipped here; a props path already got its own root
// cause from validateStrictRenderedProps, and a binding path from
// validateStrictBindingReadTypes above — restating either would just
// duplicate the diagnostic.
func (l *lowerer) validateStrictExpressionTypes(span Span, source, componentName, propsType string, scope *eachScope) {
	rootedPaths, ok := strictcomponent.ServerConcatRootedPaths(source, scope.strictScope())
	if !ok {
		return
	}
	for _, rp := range rootedPaths {
		leafType, known := l.resolveScopedFieldType(propsType, scope, rp.Root, rp.Path)
		if !known {
			continue
		}
		if leafType != "string" {
			suffix := "fields"
			if rp.Root == "props" {
				suffix = "props fields"
			}
			pathText := rp.Root + "." + strings.Join(rp.Path, ".")
			l.errs = append(l.errs, Diagnostic{
				Span:    span,
				Message: fmt.Sprintf("strict component %s cannot concatenate %s of type %s; \"+\" operands must be exact string %s", componentName, pathText, leafType, suffix),
			})
		}
	}
}

// validateStrictConditionalExpression validates a strict <If cond={...}>
// attribute's expression content: the syntactic shape (bare bool selector,
// possibly a nested path rooted at props or, section 2.4, at a binding, or
// that selector == false) through the dedicated cond validator, then the
// exact bool type through the same-file struct schema. General expression
// positions never reach this function — only the cond attribute of an
// unshadowed <If> does, dispatched from validateStrictServerExpressions.
func (l *lowerer) validateStrictConditionalExpression(span Span, source, componentName, propsType string, scope *eachScope) {
	root, path, _, err := strictcomponent.ValidateServerCondExpressionScope(source, scope.strictScope())
	if err != nil {
		l.errs = append(l.errs, Diagnostic{
			Span:    span,
			Message: fmt.Sprintf("strict server expression %q is not renderable: %v", strings.TrimSpace(source), err),
			Hint:    "use literals or props field selection; compute, index, and call methods before rendering",
		})
		return
	}
	if root != "" && root != "props" {
		l.validateEachBindingRead(span, componentName, scope, root, path)
	}
	fieldType, known := l.resolveScopedFieldType(propsType, scope, root, path)
	if !known {
		return
	}
	if fieldType != "bool" {
		suffix := "field"
		if root == "props" {
			suffix = "props field"
		}
		pathText := root + "." + strings.Join(path, ".")
		l.errs = append(l.errs, Diagnostic{
			Span:    span,
			Message: fmt.Sprintf("strict component %s cannot use %s of type %s in <If cond>; cond requires an exact bool %s", componentName, pathText, fieldType, suffix),
		})
	}
}

// strictBindingNamePattern is design spec section 2.2's binding-name rule:
// lowercase-first, valid Go identifier characters. Lowercase-first keeps a
// binding visually and lexically disjoint from component tags and type
// names.
var strictBindingNamePattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)

// goKeywords is the exhaustive Go keyword set; a binding may not use one,
// since it is emitted verbatim as a Go func parameter name
// (transpile.go's emitStrictEach).
var goKeywords = map[string]bool{
	"break": true, "default": true, "func": true, "interface": true, "select": true,
	"case": true, "defer": true, "go": true, "map": true, "struct": true,
	"chan": true, "else": true, "goto": true, "package": true, "switch": true,
	"const": true, "fallthrough": true, "if": true, "range": true, "type": true,
	"continue": true, "for": true, "import": true, "return": true, "var": true,
}

func invalidStrictBindingNameReason(name string) string {
	if !strictBindingNamePattern.MatchString(name) || goKeywords[name] {
		return "must start with a lowercase letter and be a valid Go identifier"
	}
	return ""
}

// enterStrictEach validates one strict <Each> node's own shape, binding
// names, and of-source type, and returns the scope its children should
// validate under. ok=false means the subtree should not be walked further:
// a shape or type problem already reported its own diagnostic (or, for an
// of source whose type already failed validateStrictRenderedProps' early
// pass, was already reported there), and there is no well-defined binding
// to check descendant expressions against.
func (l *lowerer) enterStrictEach(node *Node, componentName, propsType string, scope *eachScope, slices map[string]SlicePropSchema) (*eachScope, bool) {
	itemName, indexName, ofAttr, ok := l.strictEachShape(node, scope)
	if !ok {
		return nil, false
	}
	if err := strictcomponent.ValidateServerExpression(ofAttr.Expr); err != nil {
		l.errs = append(l.errs, Diagnostic{
			Span:    node.Span,
			Message: fmt.Sprintf("strict server expression %q is not renderable: %v", strings.TrimSpace(ofAttr.Expr), err),
			Hint:    "of must select a []T slice field on props",
		})
		return nil, false
	}
	path, sok := strictcomponent.ServerPropPath(ofAttr.Expr)
	if !sok {
		l.errs = append(l.errs, Diagnostic{
			Span:    node.Span,
			Message: fmt.Sprintf("strict <Each> of attribute %q must select a props field; of sources are props or a props field selector", strings.TrimSpace(ofAttr.Expr)),
		})
		return nil, false
	}
	dotted := strings.Join(path, ".")
	elem, known := l.strictEachElems[componentName][dotted]
	if !known {
		// validateStrictRenderedProps (the early, component-span pass) has
		// already reported this exact path's own diagnostic — see
		// collectStrictPropReads' loop-source classification and
		// resolveStrictEachSourceType. Stop walking this subtree without a
		// duplicate.
		return nil, false
	}
	reads := make(map[string]string)
	if existing, exists := slices[dotted]; exists && existing.Reads != nil {
		// Two <Each> loops over the same props path share one boundary
		// schema — the union of fields either loop reads.
		reads = existing.Reads
	}
	slices[dotted] = SlicePropSchema{Elem: elem, Reads: reads}
	itemScope := &eachScope{parent: scope, itemName: itemName, itemType: elem, indexName: indexName, reads: reads}
	return itemScope, true
}

// strictEachShape validates a strict <Each> node's own attributes (design
// spec section 4.1): exactly one of attribute with an expression value, a
// static as attribute, at most one static index attribute, and no others;
// then the binding-name rules (section 2.2): valid Go identifier syntax,
// not reserved (props/children), not shared between as and index, and not
// already bound by an enclosing <Each>. Each of of/as/index is counted
// across all attributes on the node — a duplicate (e.g. two as attributes)
// is rejected the same way validateStrictConditionalCall rejects a
// duplicate cond: silently accepting the last-wins attribute here would
// diverge from route/fileprogram.go's attrValue, which binds the FIRST
// match, producing two components that compile clean but bind different
// values at transpile time vs. file-render time.
func (l *lowerer) strictEachShape(node *Node, scope *eachScope) (itemName, indexName string, ofAttr *Attr, ok bool) {
	ok = true
	var asAttr, indexAttrPtr *Attr
	ofCount, asCount, indexCount := 0, 0, 0
	for i := range node.Attrs {
		attr := &node.Attrs[i]
		switch {
		case attr.Kind == AttrSpread:
			l.errs = append(l.errs, Diagnostic{Span: node.Span, Message: "strict <Each> does not accept spread attributes; of, as, and index are the only supported attributes"})
			ok = false
		case attr.Name == "of":
			ofCount++
			if attr.Kind != AttrExpr {
				l.errs = append(l.errs, Diagnostic{Span: node.Span, Message: "strict <Each> requires exactly one of attribute with an expression value"})
				ok = false
				continue
			}
			ofAttr = attr
		case attr.Name == "as":
			asCount++
			asAttr = attr
		case attr.Name == "index":
			indexCount++
			indexAttrPtr = attr
		default:
			l.errs = append(l.errs, Diagnostic{Span: node.Span, Message: fmt.Sprintf("strict <Each> does not accept attribute %q; of, as, and index are the only supported attributes", attr.Name)})
			ok = false
		}
	}
	if ofCount > 1 {
		l.errs = append(l.errs, Diagnostic{Span: node.Span, Message: "strict <Each> requires exactly one of attribute with an expression value"})
		ok = false
	} else if ofAttr == nil {
		l.errs = append(l.errs, Diagnostic{Span: node.Span, Message: "strict <Each> requires exactly one of attribute with an expression value"})
		ok = false
	}
	if asCount > 1 {
		l.errs = append(l.errs, Diagnostic{Span: node.Span, Message: "strict <Each> requires exactly one as attribute naming the loop binding"})
		ok = false
	} else if asAttr == nil || asAttr.Kind != AttrStatic || strings.TrimSpace(asAttr.Value) == "" {
		l.errs = append(l.errs, Diagnostic{Span: node.Span, Message: "strict <Each> requires a static as attribute naming the loop binding"})
		ok = false
	}
	if indexCount > 1 {
		l.errs = append(l.errs, Diagnostic{Span: node.Span, Message: "strict <Each> requires exactly one index attribute naming the index binding"})
		ok = false
	} else if indexAttrPtr != nil && (indexAttrPtr.Kind != AttrStatic || strings.TrimSpace(indexAttrPtr.Value) == "") {
		l.errs = append(l.errs, Diagnostic{Span: node.Span, Message: "strict <Each> requires a static index attribute naming the index binding"})
		ok = false
	}
	if !ok {
		return "", "", nil, false
	}
	itemName = asAttr.Value
	if indexAttrPtr != nil {
		indexName = indexAttrPtr.Value
	}
	for _, binding := range []string{itemName, indexName} {
		if binding == "" {
			continue
		}
		if msg := invalidStrictBindingNameReason(binding); msg != "" {
			l.errs = append(l.errs, Diagnostic{Span: node.Span, Message: fmt.Sprintf("strict <Each> binding %q %s", binding, msg)})
			ok = false
			continue
		}
		// The reservation predates the children feature and is what makes it
		// safe: a loop binding may not shadow the identifier a body uses to
		// place its caller's markup. strictcomponent.IsSlotBindingName
		// extends the same protection to a named slot (gosx#249): a loop
		// binding spelled slotFoo would otherwise shadow a real slot
		// placement inside the loop the same way a binding named "children"
		// would shadow children.
		if binding == "props" || binding == strictcomponent.ChildrenBinding || strictcomponent.IsSlotBindingName(binding) {
			l.errs = append(l.errs, Diagnostic{Span: node.Span, Message: fmt.Sprintf("strict <Each> binding %q is reserved; choose another name", binding)})
			ok = false
			continue
		}
		if l.isImportAlias(binding) {
			// gosx#182/#184 nit n-1: a binding named after an explicit
			// import alias already fails closed in the strictcheck
			// projection (the generated Go shadows the package alias
			// inside the loop body, so a package-qualified reference to it
			// there resolves to the loop variable instead and the
			// projection fails to compile) — but with a confusing Go
			// compiler error far from the .gsx source, not a clear one
			// here. Reject it at the source instead.
			l.errs = append(l.errs, Diagnostic{Span: node.Span, Message: fmt.Sprintf("strict <Each> binding %q shadows this file's %q import; choose another name", binding, binding)})
			ok = false
		}
	}
	if indexName != "" && indexName == itemName {
		l.errs = append(l.errs, Diagnostic{Span: node.Span, Message: fmt.Sprintf("strict <Each> binding %q is already bound by this <Each>; as and index must name different bindings", itemName)})
		ok = false
	}
	for _, binding := range []string{itemName, indexName} {
		if binding != "" && scope.hasBinding(binding) {
			l.errs = append(l.errs, Diagnostic{Span: node.Span, Message: fmt.Sprintf("strict <Each> binding %q is already bound by an enclosing <Each>; loop bindings cannot shadow", binding)})
			ok = false
		}
	}
	if !ok {
		return "", "", nil, false
	}
	return itemName, indexName, ofAttr, true
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
	rawChildren := l.extractChildren(n)
	children, slots := l.partitionCallSlots(IsComponent(tag), rawChildren)
	l.validateStrictComponentCall(n, tag, attrs, children)
	l.validateStrictCalleeSlots(n, tag, slots)
	l.validateStrictHTMLElement(n, tag, attrs)
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
		Slots:    slots,
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
	l.validateStrictHTMLElement(n, tag, attrs)
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
	rawChildren := l.extractChildren(n)
	// A Fragment is never a component call (isComponentCall false
	// unconditionally): partitionCallSlots still runs, so a slot="Name" on
	// a Fragment's own direct child is reported instead of silently
	// joining the Fragment's children — see partitionCallSlots' doc
	// comment on why an intervening Fragment disqualifies a slot the same
	// way an intervening plain HTML element does.
	children, _ := l.partitionCallSlots(false, rawChildren)
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

// partitionCallSlots splits rawChildren — n's own already-lowered direct
// children — into the default children group and named-slot children
// (gosx#249's caller-side supply), following a static slot="Name"
// attribute on each direct child.
//
// isComponentCall must be IsComponent(tag) for the element rawChildren
// belongs to: a Fragment and an ordinary HTML element both call this
// (lowerFragment, lowerGSXElement) with isComponentCall false, since
// neither is a call a named slot can bind against — nothing anywhere
// reads a slots map keyed to a Fragment or a <div>. A slot found there is
// reported, never silently dropped: an author who mistypes the nesting —
// wraps a slot-tagged element in a plain HTML element or a Fragment
// before handing it to the component call, so the tagged element is no
// longer a DIRECT child of the call — deserves to hear about it, not have
// the element silently join the anonymous children group instead.
//
// A slot's own "slot" attribute is stripped from its rendered Attrs
// either way, valid or not: it is gosx's routing marker, never a real
// HTML attribute a browser should see. An invalid one still fails the
// whole compile, so what its element renders as does not matter, but a
// valid one must never leak "slot" into the output.
func (l *lowerer) partitionCallSlots(isComponentCall bool, rawChildren []NodeID) (children []NodeID, slots map[string]NodeID) {
	children = rawChildren
	for _, childID := range rawChildren {
		if int(childID) >= len(l.prog.Nodes) {
			continue
		}
		child := &l.prog.Nodes[childID]
		idx, attr, ok := findSlotAttr(child.Attrs)
		if !ok {
			continue
		}
		child.Attrs = append(child.Attrs[:idx:idx], child.Attrs[idx+1:]...)
		// child.Span, not the call site's own span: a diagnostic about a
		// mistagged element should point at that element, not at whatever
		// happens to enclose it.
		errAt := func(format string, args ...any) {
			l.errs = append(l.errs, Diagnostic{Span: child.Span, Message: fmt.Sprintf(format, args...)})
		}

		if !isComponentCall {
			errAt("slot attribute is only meaningful on a direct child of a component call; this element is not one")
			continue
		}
		if attr.Kind != AttrStatic {
			errAt("slot must be a static string literal; a computed slot name is not supported because the caller-supplied value would have to be evaluated before the callee's children are known, which reintroduces the ordering problem named slots close")
			continue
		}
		name := strings.TrimSpace(attr.Value)
		if !strictcomponent.IsSlotBindingName(strictcomponent.SlotBindingName(name)) {
			errAt("slot name %q is not valid; use a non-empty, upper-case-initial name (e.g. slot=\"Title\")", attr.Value)
			continue
		}
		if _, dup := slots[name]; dup {
			errAt("slot %q is supplied more than once at this call", name)
			continue
		}
		if slots == nil {
			slots = make(map[string]NodeID)
		}
		slots[name] = childID
		children = removeNodeID(children, childID)
	}
	return children, slots
}

// findSlotAttr reports the index and value of the first "slot" attribute
// in attrs, if any.
func findSlotAttr(attrs []Attr) (idx int, attr Attr, ok bool) {
	for i, a := range attrs {
		if a.Name == "slot" {
			return i, a, true
		}
	}
	return 0, Attr{}, false
}

// removeNodeID returns ids with id's first occurrence removed, preserving
// the relative order of every other element.
func removeNodeID(ids []NodeID, id NodeID) []NodeID {
	out := make([]NodeID, 0, len(ids))
	removed := false
	for _, existing := range ids {
		if !removed && existing == id {
			removed = true
			continue
		}
		out = append(out, existing)
	}
	return out
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
