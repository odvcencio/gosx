// Package golower lowers Go source files into program.Program values for
// the shared VM. It is the AST-compiler initiative's lowerer: it walks
// Go's standard go/ast representation of an engine-surface handler file
// and emits handler-by-handler opcode sequences using the
// statement-sequencing opcodes and the stdlib intrinsic registry.
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
// Lightweight go/types checking is a future improvement (the answer
// for now is "skip").
//
// Errors surface as a single LowerError that carries a list of
// per-handler issues with line numbers and (where possible) the
// suggestion to use the surface=wasm escape hatch.

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
// points the author at the escape-hatch contract.
type Issue struct {
	File       string
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
		if i.File != "" && i.Handler != "" {
			fmt.Fprintf(&b, "  %s:%d (%s): %s", i.File, i.Line, i.Handler, i.Message)
		} else if i.File != "" {
			fmt.Fprintf(&b, "  %s:%d: %s", i.File, i.Line, i.Message)
		} else {
			fmt.Fprintf(&b, "  %s:%d: %s", i.Handler, i.Line, i.Message)
		}
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
	return LowerASTFiles(fset, []*ast.File{file})
}

// LowerASTFiles lowers one package in the supplied file/declaration order.
// Functions and struct layouts are package-scoped; import aliases retain their
// file scope. This accepts the same Go subset as LowerFile, without type checking.
func LowerASTFiles(fset *token.FileSet, files []*ast.File) (*program.Program, error) {
	if fset == nil || len(files) == 0 {
		return nil, fmt.Errorf("golower: a file set and package files are required")
	}
	for i, file := range files {
		if file == nil || file.Name == nil {
			return nil, fmt.Errorf("golower: invalid package file %d", i)
		}
		if file.Name.Name != files[0].Name.Name {
			return nil, fmt.Errorf("golower: %s: package %s does not match %s", fset.Position(file.Pos()).Filename, file.Name.Name, files[0].Name.Name)
		}
	}
	prog := &program.Program{
		Name: files[0].Name.Name,
	}
	lerr := &LowerError{File: fset.Position(files[0].Pos()).Filename}

	ctx := &lowerCtx{
		prog:   prog,
		fset:   fset,
		issues: lerr,
		exprs:  []program.Expr{},
	}

	// Register the entire package before lowering any function body, including
	// positional composite literals and forward calls into another source file.
	for _, file := range files {
		ctx.scanStructTypes(file)
		ctx.scanUserFuncs(file)
	}
	for _, file := range files {
		ctx.scanImports(file)
		for _, decl := range file.Decls {
			ctx.lowerTopLevelDecl(decl)
		}
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
	prog            *program.Program
	fset            *token.FileSet
	issues          *LowerError
	exprs           []program.Expr
	handler         string // name of the handler currently being lowered (for diagnostics)
	expressionLimit bool

	// currentResults is the declared return-value count of the function
	// whose body is currently being lowered. Used by lowerReturnStmt
	// to decide whether `return a, b` lowers to a multi-
	// value ObjectVal carrier or stays a diagnostic. Zero outside a
	// FuncDecl body.
	currentResults int

	// structs holds the ordered field-name list for every struct type
	// declared at the top level of the package. Populated by
	// scanStructTypes so positional struct literals like
	// `vec2{x, y}` can recover field names without a separate
	// go/types pass.
	structs map[string]structTypeInfo

	// funcs is the package-wide user-function registry. Populated by
	// scanUserFuncs so call sites can detect a
	// bare-identifier call into a sibling helper and emit
	// OpIndirectCall instead of the legacy "calls to user-defined
	// function" diagnostic.
	funcs funcRegistry

	// imports maps the source-level identifier of each imported package
	// in the current file to its canonical package name. Populated by
	// scanImports so lowerCallExpr can tell `math.Sin(x)` (intrinsic
	// dispatch through OpCall) apart from `c.MoveTo(x, y)` (host
	// receiver dispatch through OpHostCall). The map's key is the local
	// identifier a call site actually writes — the alias when the
	// import declares one (`m "math"` → key "m") — and its value is
	// always the canonical name (`m "math"` → value "math"), because
	// knownIntrinsics in intrinsics_table.go is keyed by canonical
	// package name, not by whatever alias one file happens to use.
	// canonicalPackageName resolves a key to its value, doubling as the
	// import-set membership check every caller needs.
	imports map[string]string

	// closureLocals holds the set of bare identifiers that the current
	// handler body assigns a *ast.FuncLit value to. Used
	// by lowerCallExpr to decide between OpIndirectCall (closure
	// dispatch via the VM's local-first contract) and the legacy
	// "unsupported user function" diagnostic. Re-populated per
	// handler in lowerFuncDecl.
	closureLocals map[string]bool
}

const maxProgramExpressions = 1<<16 - 1

// addExpr never wraps the uint16 expression table. A rejected package retains
// one diagnostic and a bounded partial program; callers must check the error.
func (c *lowerCtx) addExpr(e program.Expr) program.ExprID {
	if len(c.exprs) >= maxProgramExpressions {
		if !c.expressionLimit {
			c.addIssue(nil, "package exceeds the 65,535 expression limit", "")
			c.expressionLimit = true
		}
		return 0
	}
	id := program.ExprID(len(c.exprs))
	c.exprs = append(c.exprs, e)
	return id
}

// addIssue records a lowering problem against the current handler. The
// caller can keep going so multiple problems surface in one pass.
func (c *lowerCtx) addIssue(node ast.Node, msg, suggestion string) {
	line := 0
	file := ""
	if node != nil {
		position := c.fset.Position(node.Pos())
		line, file = position.Line, position.Filename
	}
	c.issues.Issues = append(c.issues.Issues, Issue{
		File:       file,
		Handler:    c.handler,
		Line:       line,
		Message:    msg,
		Suggestion: suggestion,
	})
}

// escapeHatchSuggestion is the standard wording the lowerer uses when a
// construct is unsupported. Centralized so updates to the reference
// text only happen in one place.
const escapeHatchSuggestion = "use the surface=wasm escape hatch (ADR 0006) — see gosx-vm-capability-gaps.md"
