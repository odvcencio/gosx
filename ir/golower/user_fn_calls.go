// Slice Y.D — user-defined function call lowering.
//
// Engine-surface handlers frequently dispatch into sibling helpers
// declared in the same file. Pre-Y.D the lowerer rejected every
// bare-identifier call with "calls to user-defined function ..."
// because the supported subset offered no cross-handler dispatch.
//
// Y.D's approach:
//
//  1. A pre-pass (scanUserFuncs, run at LowerASTFile time alongside
//     scanStructTypes from Slice Y.A) walks the file's top-level
//     *ast.FuncDecl nodes and records every non-method function in a
//     per-file registry — name → (ordered param names, return count).
//
//  2. lowerFuncDecl (decl.go) is unchanged for receivers/parameters,
//     but after lowering the function body it ALSO emits a
//     program.FuncDef carrying the body's OpSeq + the registry's
//     metadata. The same lowered Body lives in both the Handler list
//     (so the existing event-dispatch path still finds it) AND in the
//     Funcs list (so OpIndirectCall can dispatch into it).
//
//  3. lowerCallExpr (expr.go) consults the registry first. Bare
//     identifiers that match a registered function emit OpIndirectCall
//     with the callee name in Value and the argument expressions in
//     Operands. Calls whose callee isn't registered fall through to
//     the existing builtin / "unsupported user function" diagnostics.
//
//  4. The VM (client/vm/user_fn_calls.go) pushes a fresh frame on
//     dispatch, binds the evaluated arguments to the FuncDef's Params,
//     evaluates Body, pops the frame, and propagates the return value
//     (or an ObjectVal carrier for multi-return — see lowerMultiAssign
//     in multi_assign.go, which adopts the same `__y_d_call_<pos>`
//     prefix Y.C's retrospective handed off to Y.D).
//
// Composite parameter semantics. Y.C's retrospective documented that
// OpFieldSet on a parameter-typed receiver already propagates because
// Value.Fields is a reference-typed map. Y.D preserves this: argument
// Values are passed via the same struct-field-set semantics, so
// `func upd(v vec2)` that calls `v.X = 1` does mutate the caller's vec2.
// This matches Slice Y.C's in-place mutation contract and explicitly
// keeps the Y.C → Y.D handoff intact.
//
// Recursion safety. The VM caps recursion depth at Program.MaxCallDepth
// (default 256 per program.DefaultMaxCallDepth) and records a
// structured `call_depth_exceeded` diagnostic so a runaway recursion
// is panic-free.

package golower

import (
	"go/ast"

	"m31labs.dev/gosx/island/program"
)

// funcRegistry maps a user-function name to its parameter list and
// return count so the lowerer can emit OpIndirectCall + bind args
// without revisiting the AST.
type funcRegistry struct {
	defs map[string]userFuncInfo
}

// userFuncInfo holds the bits the lowerer needs at call sites: the
// ordered parameter names (for the VM's frame binding) and the return
// count (for the multi-return ObjectVal carrier decision).
type userFuncInfo struct {
	params  []string
	results int
}

// scanUserFuncs walks file.Decls and registers every top-level
// (non-method) *ast.FuncDecl with a body. Method receivers are skipped
// — the existing lowerFuncDecl already rejects them with a diagnostic;
// emitting a registry entry for a method would be misleading because
// OpIndirectCall has no receiver binding contract.
//
// Forward declarations (FuncDecl with nil Body) are skipped: nothing
// for the VM to execute. External linkage is out of scope for the
// engine-surface subset.
func (c *lowerCtx) scanUserFuncs(file *ast.File) {
	c.funcs = funcRegistry{defs: map[string]userFuncInfo{}}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Recv != nil || fn.Body == nil || fn.Name == nil {
			continue
		}
		info := userFuncInfo{
			params:  paramNames(fn.Type.Params),
			results: countResults(fn.Type.Results),
		}
		c.funcs.defs[fn.Name.Name] = info
	}
}

// paramNames flattens a *ast.FieldList into the ordered list of
// parameter names. A field with multiple names (`func f(a, b int)`)
// emits one entry per name; a field with no name (unusual in Go but
// valid for the `func(int)` interface-method form) emits a synthetic
// "_param_<i>" so the call site can still bind by position.
func paramNames(list *ast.FieldList) []string {
	if list == nil {
		return nil
	}
	var out []string
	for _, field := range list.List {
		if len(field.Names) == 0 {
			out = append(out, "_param_unnamed")
			continue
		}
		for _, name := range field.Names {
			if name == nil {
				continue
			}
			out = append(out, name.Name)
		}
	}
	return out
}

// countResults counts the total number of return values a function
// declaration produces. `func f() int` → 1; `func f() (int, error)` →
// 2; `func f()` → 0. The VM uses this to decide whether to wrap the
// return as a multi-value ObjectVal carrier.
func countResults(list *ast.FieldList) int {
	if list == nil {
		return 0
	}
	total := 0
	for _, field := range list.List {
		if len(field.Names) == 0 {
			total++
			continue
		}
		total += len(field.Names)
	}
	return total
}

// lookupUserFunc reports whether name names a registered user function
// and returns the registry entry if so. Called by lowerCallExpr to
// decide whether to emit OpIndirectCall or fall through to the
// existing diagnostic path.
func (c *lowerCtx) lookupUserFunc(name string) (userFuncInfo, bool) {
	info, ok := c.funcs.defs[name]
	return info, ok
}

// emitIndirectCall builds the OpIndirectCall expr for `name(args...)`.
// Arguments lower left-to-right; the VM binds them to the registry's
// param names in dispatch order. The callee name lives in Value so the
// VM's FuncDef lookup is one map probe.
func (c *lowerCtx) emitIndirectCall(name string, args []ast.Expr) program.ExprID {
	argIDs := make([]program.ExprID, 0, len(args))
	for _, a := range args {
		argIDs = append(argIDs, c.lowerExpr(a))
	}
	return c.addExpr(program.Expr{
		Op:       program.OpIndirectCall,
		Value:    name,
		Operands: argIDs,
	})
}
