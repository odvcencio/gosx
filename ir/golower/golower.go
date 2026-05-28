// Package golower lowers Go source files into program.Program values for
// the shared VM. It is the AST-compiler initiative's lowerer (Slice X.C):
// it walks Go's standard go/ast representation of an engine-surface
// handler file and emits handler-by-handler opcode sequences using the
// statement-sequencing opcodes from Slice X.A and the stdlib intrinsic
// registry from Slice X.B.
//
// Supported subset (locked, per plan):
//   - Function declarations with parameter binding and `return` results.
//   - Statements: assign (`=`), short-var-decl (`:=`), if/else, for
//     (3-clause and range), return, expression statements.
//   - Expressions: literals (int/float/string/bool), identifiers,
//     unary/binary arithmetic + comparison + boolean, parens, selector
//     reads, call expressions resolving to registered intrinsics.
//   - Package-level `var x = ...` declarations become program signals.
//   - Single-expression closures via the existing ir/exprparse path
//     (multi-statement closures remain unsupported per plan scope).
//
// Out of scope (intentionally):
//   - Goroutines, channels, select, defer, recover.
//   - Interfaces, embedding, generics, reflection, unsafe.
//   - Stdlib calls outside the registered intrinsic set.
//   - Type assertions on user-defined types.
//
// The lowerer is permissive about types — it accepts whatever go/ast
// gives it and lets runtime type errors surface as VM diagnostics.
// Lightweight go/types checking is a future improvement (X.C.6 noted
// the choice; the answer is "skip for now").
//
// Errors surface as a single LowerError that carries a list of
// per-handler issues with line numbers and (where possible) the
// suggestion to use the surface=wasm escape hatch per ADR 0006.

package golower

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"m31labs.dev/gosx/island/program"
)

// LowerError accumulates per-handler lowering problems so a build can
// report several issues at once instead of failing on the first.
type LowerError struct {
	File   string
	Issues []Issue
}

// Issue is a single lowering problem. The Suggestion field, when set,
// points the author at the escape-hatch contract (ADR 0006).
type Issue struct {
	Handler    string
	Line       int
	Message    string
	Suggestion string
}

func (e *LowerError) Error() string {
	if len(e.Issues) == 0 {
		return fmt.Sprintf("golower: %s: no issues", e.File)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "golower: %s: %d issue(s)\n", e.File, len(e.Issues))
	for _, i := range e.Issues {
		fmt.Fprintf(&b, "  %s:%d: %s", i.Handler, i.Line, i.Message)
		if i.Suggestion != "" {
			fmt.Fprintf(&b, "  (suggestion: %s)", i.Suggestion)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// LowerFile parses src as a Go source file and lowers each top-level
// function declaration into a program.Handler whose Body is a single
// OpSeq expression that evaluates the function body. Package-level
// `var` declarations become program.SignalDef entries.
//
// The returned Program has Name set to the source file's package name;
// callers (engine/surface/lowering.go) override it with the surface
// component name.
func LowerFile(src []byte) (*program.Program, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.AllErrors)
	if err != nil {
		return nil, fmt.Errorf("golower: parse: %w", err)
	}
	return LowerASTFile(fset, file)
}

// LowerASTFile lowers an already-parsed *ast.File. Separate from
// LowerFile so callers that already have an AST (caching, multi-file
// surfaces) don't re-parse.
func LowerASTFile(fset *token.FileSet, file *ast.File) (*program.Program, error) {
	prog := &program.Program{
		Name: file.Name.Name,
	}
	lerr := &LowerError{File: fset.Position(file.Pos()).Filename}

	ctx := &lowerCtx{
		prog:   prog,
		fset:   fset,
		issues: lerr,
		exprs:  []program.Expr{},
	}

	// Pre-pass (Slice Y.A): build the struct-type registry so positional
	// composite literals (`vec2{x, y}`) can recover field names without
	// a full go/types pass. Named-form literals (`Node{ID: id}`) don't
	// need this — they carry field names inline — but the registry is
	// cheap and keeps the lowering paths uniform.
	ctx.scanStructTypes(file)

	// Pre-pass (Slice Y.D): build the user-function registry so
	// in-package calls (`updatePosition(node)` calling a sibling
	// helper) can resolve through OpIndirectCall instead of the
	// legacy "calls to user-defined function" diagnostic. The pass
	// runs BEFORE lowerTopLevelDecl so forward references resolve
	// (Go allows `func A() { B() }; func B() {}`).
	ctx.scanUserFuncs(file)

	// Pre-pass (Slice Y.E): record the source-level identifier of
	// each imported package so lowerCallExpr's selector branch can
	// tell pkg.Func (intrinsic) apart from receiver.Method (host
	// dispatch through OpHostCall).
	ctx.scanImports(file)

	// Two passes:
	//   1. Hoist package-level var declarations into signal defs so
	//      function bodies can reference them by name. Init expressions
	//      defer to expression lowering, which may forward-reference
	//      other signals declared later in the file.
	//   2. Lower each function declaration into a Handler entry.
	for _, decl := range file.Decls {
		ctx.lowerTopLevelDecl(decl)
	}

	prog.Exprs = ctx.exprs

	if len(lerr.Issues) > 0 {
		return prog, lerr
	}
	return prog, nil
}

// lowerCtx threads parsing state through the recursive lowering helpers
// without forcing each one to take ten parameters.
type lowerCtx struct {
	prog    *program.Program
	fset    *token.FileSet
	issues  *LowerError
	exprs   []program.Expr
	handler string // name of the handler currently being lowered (for diagnostics)

	// currentResults is the declared return-value count of the function
	// whose body is currently being lowered. Used by lowerReturnStmt
	// (Slice Y.D) to decide whether `return a, b` lowers to a multi-
	// value ObjectVal carrier or stays a diagnostic. Zero outside a
	// FuncDecl body.
	currentResults int

	// structs holds the ordered field-name list for every struct type
	// declared at the top level of the file. Populated by
	// scanStructTypes (Slice Y.A) so positional struct literals like
	// `vec2{x, y}` can recover field names without a separate
	// go/types pass.
	structs map[string]structTypeInfo

	// funcs is the per-file user-function registry. Populated by
	// scanUserFuncs (Slice Y.D) so call sites can detect a
	// bare-identifier call into a sibling helper and emit
	// OpIndirectCall instead of the legacy "calls to user-defined
	// function" diagnostic.
	funcs funcRegistry

	// imports holds the source-level identifier of each imported
	// package in the current file (Slice Y.E). Populated by
	// scanImports so lowerCallExpr can tell `math.Sin(x)` (intrinsic
	// dispatch through OpCall) apart from `c.MoveTo(x, y)` (host
	// receiver dispatch through OpHostCall).
	imports map[string]bool

	// closureLocals holds the set of bare identifiers that the current
	// handler body assigns a *ast.FuncLit value to (Slice Y.G). Used
	// by lowerCallExpr to decide between OpIndirectCall (closure
	// dispatch via the VM's local-first contract) and the legacy
	// "unsupported user function" diagnostic. Re-populated per
	// handler in lowerFuncDecl.
	closureLocals map[string]bool
}

// addExpr appends e to the program's expression table and returns the
// ExprID. Centralizing this lets future optimization passes (CSE,
// constant folding) hook in without touching every call site.
func (c *lowerCtx) addExpr(e program.Expr) program.ExprID {
	id := program.ExprID(len(c.exprs))
	c.exprs = append(c.exprs, e)
	return id
}

// addIssue records a lowering problem against the current handler. The
// caller can keep going so multiple problems surface in one pass.
func (c *lowerCtx) addIssue(node ast.Node, msg, suggestion string) {
	line := 0
	if node != nil {
		line = c.fset.Position(node.Pos()).Line
	}
	c.issues.Issues = append(c.issues.Issues, Issue{
		Handler:    c.handler,
		Line:       line,
		Message:    msg,
		Suggestion: suggestion,
	})
}

// escapeHatchSuggestion is the standard wording the lowerer uses when a
// construct is unsupported. Centralized so updates to ADR 0006's
// reference text only happen in one place.
const escapeHatchSuggestion = "use the surface=wasm escape hatch (ADR 0006) — see gosx-vm-capability-gaps.md"
