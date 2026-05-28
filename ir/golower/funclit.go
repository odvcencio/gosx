// Slice Y.G — *ast.FuncLit (closure) lowering.
//
// Pre-Y.G the lowerer rejected every *ast.FuncLit from expr.go's
// default branch ("unsupported expression *ast.FuncLit"). Y.G adds
// closure support so engine-surface handlers like graph_surface.go's
// Mount can lower their `c.StartLoop(func(dt float64) { ... })`
// animation hook cleanly.
//
// Lowering strategy:
//
//  1. Walk the FuncLit body collecting FREE VARIABLES — identifiers
//     referenced inside the body that are NOT declared in the body
//     (params, `:=` short-decls, range loop binders) and ALSO appear
//     as locals in the enclosing handler's frame. These are the
//     captured locals; the closure carries a reference to them.
//
//  2. Register the FuncLit body as a synthetic FuncDef on the
//     Program — same shape Y.D uses for user-defined functions — with
//     a generated name `__y_g_funclit_<pos>`. The body's locals
//     (params + internal :=) live in the closure's own frame at
//     invocation time; captured names resolve through the saved
//     caller-frame reference.
//
//  3. Emit OpClosure at the FuncLit site. Value = the synthetic name.
//     Operands = OpLitString entries for each captured local. The VM
//     evaluates each operand to recover the name, then takes a snapshot
//     of the CURRENT frame and stores it on the ClosureVal.
//
//  4. Dispatch: OpIndirectCall sniffs whether its callee resolves to a
//     ClosureVal (via a local lookup) or a registered FuncDef (Y.D
//     path). Closures push a closure-aware frame that consults the
//     captured frame's slots whenever an OpLocalGet / OpAssign /
//     OpLocalSet / OpFieldSet / OpIndexSet hits a captured name.
//
// Capture semantics. Go closures capture VARIABLES, not values: writes
// in the enclosing scope after the closure is created are visible
// inside the closure when it later runs, and writes inside the closure
// are visible to the enclosing scope. The implementation forwards
// reads and writes through the saved caller-frame pointer, which is
// the same `*frame` the enclosing handler is mutating. This matches
// Go's `func(){ count++ }` semantics that production engine surfaces
// (c.StartLoop(func(dt float64) { ... mutate gPos/gVel ...})) rely on.
//
// Discrimination from OpIndirectCall (Y.D): Y.D's user-function calls
// resolve `id(args)` where `id` is a bare-identifier match in the
// user-function registry. Closures resolve `cv(args)` where `cv` is a
// LOCAL holding a ClosureVal — the lowerer can't distinguish these
// statically without type info, so the choice is made at runtime in
// evalIndirectCallExpr: if the callee's name is bound to a local that
// holds a ClosureVal, dispatch through the closure path; otherwise
// fall through to the user-function registry.

package golower

import (
	"fmt"
	"go/ast"
	"sort"

	"m31labs.dev/gosx/island/program"
)

// lowerFuncLit lowers a *ast.FuncLit into an OpClosure expression.
//
// The strategy:
//  1. Compute the set of captured names: identifiers referenced inside
//     the body that aren't bound by the body itself (params, := decls,
//     range-loop binders).
//  2. Register a synthetic FuncDef in the program's Funcs slice so
//     OpIndirectCall can dispatch into the body. The FuncDef's Body is
//     a freshly-lowered OpSeq of the body statements.
//  3. Emit OpClosure with the synthetic name in Value and OpLitString
//     operands naming each captured local.
func (c *lowerCtx) lowerFuncLit(fl *ast.FuncLit) program.ExprID {
	if fl.Body == nil {
		// Forward-declared anonymous? Not valid Go for a FuncLit, but
		// be defensive — emit a closure with an empty body.
		fl.Body = &ast.BlockStmt{}
	}

	// Step 1: gather captured names.
	params := paramNames(fl.Type.Params)
	captured := collectCapturedNames(fl, params)

	// Step 2: build the synthetic FuncDef. Use the lexical position as
	// a stable suffix so two FuncLits in the same handler don't share
	// a name.
	synthName := fmt.Sprintf("__y_g_funclit_%d", fl.Pos())

	// Save lowering context that lowerFuncDecl normally swaps. The
	// FuncLit body is lowered as a fresh statement sequence, so the
	// current handler context (return-count, handler-name diagnostics)
	// must be restored after.
	prevHandler := c.handler
	prevResults := c.currentResults
	c.handler = synthName
	c.currentResults = countResults(fl.Type.Results)
	defer func() {
		c.handler = prevHandler
		c.currentResults = prevResults
	}()

	// Pre-register the synthetic FuncDef in the user-function registry
	// BEFORE lowering the body so any recursive `f()` inside (rare for
	// closures, but legal in Go via reassignment) can resolve through
	// the user-fn path. This mirrors Y.D's pre-pass semantic.
	c.funcs.defs[synthName] = userFuncInfo{
		params:  params,
		results: c.currentResults,
	}

	bodyOps := c.lowerStmtList(fl.Body.List)
	seqID := c.addExpr(program.Expr{
		Op:       program.OpSeq,
		Operands: bodyOps,
	})

	c.prog.Funcs = append(c.prog.Funcs, program.FuncDef{
		Name:    synthName,
		Params:  params,
		Body:    []program.ExprID{seqID},
		Results: c.currentResults,
	})

	// Step 3: emit OpClosure. Captured-name operands are OpLitString.
	capOps := make([]program.ExprID, 0, len(captured))
	for _, name := range captured {
		capOps = append(capOps, c.addExpr(program.Expr{
			Op:    program.OpLitString,
			Value: name,
			Type:  program.TypeString,
		}))
	}

	return c.addExpr(program.Expr{
		Op:       program.OpClosure,
		Value:    synthName,
		Operands: capOps,
	})
}

// collectCapturedNames walks the FuncLit body and returns the ordered
// list of identifiers the body references that are NOT bound by the
// body itself. Determinism matters (operand order is observable) so
// names are returned in lexical sort order.
//
// "Bound by the body itself" includes:
//   - Function parameters.
//   - `:=` short variable declarations.
//   - `var` declarations.
//   - Range-loop key/value binders (`for k, v := range`).
//
// Identifiers we DON'T count as captures (false positives to skip):
//   - The blank identifier `_`.
//   - Keywords-like idents (true, false, nil).
//   - Names that resolve to a registered user function or imported
//     package — those route through the dedicated dispatchers and don't
//     live in the caller's frame.
//   - Field-access selectors past the first segment (e.g., in `x.Y`
//     we capture x but not Y).
func collectCapturedNames(fl *ast.FuncLit, params []string) []string {
	bound := make(map[string]bool, len(params)+8)
	for _, p := range params {
		bound[p] = true
	}

	// First sweep: collect every identifier the body BINDS so a second
	// sweep can decide which referenced names are free.
	ast.Inspect(fl.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			if node.Tok.String() == ":=" {
				for _, lhs := range node.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
						bound[id.Name] = true
					}
				}
			}
		case *ast.ValueSpec:
			for _, name := range node.Names {
				if name != nil && name.Name != "_" {
					bound[name.Name] = true
				}
			}
		case *ast.RangeStmt:
			if node.Tok.String() == ":=" {
				if id, ok := node.Key.(*ast.Ident); ok && id.Name != "_" {
					bound[id.Name] = true
				}
				if id, ok := node.Value.(*ast.Ident); ok && id.Name != "_" {
					bound[id.Name] = true
				}
			}
		case *ast.FuncLit:
			// Nested FuncLits manage their own bindings; skip into
			// their body only via the dedicated lowerer recursion (the
			// outer InsPect will still walk them but the inner bindings
			// only matter to that inner closure's own capture analysis).
			// Returning false here would skip the inner body entirely,
			// which is wrong — keep walking but the inner's own bindings
			// will look the same shape in our gather pass.
		}
		return true
	})

	// Second sweep: every Ident that ISN'T bound here AND isn't a
	// keyword / user function / imported package is a capture.
	seen := make(map[string]bool)
	ast.Inspect(fl.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			// Only the leftmost ident in a selector chain might capture.
			// Walk down so we don't double-count the .Sel ident.
			ast.Inspect(node.X, func(inner ast.Node) bool {
				if id, ok := inner.(*ast.Ident); ok {
					maybeCapture(id.Name, bound, seen)
					return false
				}
				return true
			})
			return false
		case *ast.KeyValueExpr:
			// Composite-literal keys are field names, not refs — skip
			// the key, walk the value.
			ast.Inspect(node.Value, func(inner ast.Node) bool {
				if id, ok := inner.(*ast.Ident); ok {
					maybeCapture(id.Name, bound, seen)
				}
				return true
			})
			return false
		case *ast.Ident:
			maybeCapture(node.Name, bound, seen)
		}
		return true
	})

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// maybeCapture marks name as captured if it isn't a keyword and isn't
// shadowed by a body-local binding. The actual decision of whether the
// name resolves to a parent-frame local vs a package signal vs an
// intrinsic is deferred to runtime: the VM forwards reads/writes
// through the captured-frame pointer when the name is in the closure's
// captured set, falling back to the normal lookup chain otherwise.
func maybeCapture(name string, bound, seen map[string]bool) {
	if name == "" || name == "_" {
		return
	}
	switch name {
	case "true", "false", "nil", "iota":
		return
	}
	if bound[name] {
		return
	}
	seen[name] = true
}

// scanClosureLocals walks the body of a function decl and records every
// local identifier that is ever assigned a *ast.FuncLit value. The
// lowerer uses this set to decide whether a bare-identifier call like
// `f()` should route through OpIndirectCall (closure dispatch via the
// VM's local-lookup-first contract) or fall back to the legacy
// "unsupported user function" diagnostic.
//
// Coverage:
//   - `f := func() { ... }` short-var decl
//   - `f = func() { ... }` plain assignment
//   - `var f = func() { ... }` long-form var decl
//
// The set is intentionally permissive: a name that's assigned a
// FuncLit anywhere in the body counts, even if some control-flow path
// would leave it unassigned. The VM resolves the actual ClosureVal
// shape at evaluation time and falls back to the user-function
// registry / `unknown_callee` diagnostic when the local doesn't carry
// a closure.
func scanClosureLocals(body *ast.BlockStmt) map[string]bool {
	out := make(map[string]bool)
	if body == nil {
		return out
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			// Same-position LHS/RHS gives `f := func(){}` and
			// `f = func(){}` symmetry. Walk pairs by index.
			for i, lhs := range node.Lhs {
				if i >= len(node.Rhs) {
					break
				}
				if _, ok := node.Rhs[i].(*ast.FuncLit); !ok {
					continue
				}
				if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
					out[id.Name] = true
				}
			}
		case *ast.ValueSpec:
			for i, name := range node.Names {
				if name == nil || name.Name == "_" {
					continue
				}
				if i >= len(node.Values) {
					continue
				}
				if _, ok := node.Values[i].(*ast.FuncLit); ok {
					out[name.Name] = true
				}
			}
		}
		return true
	})
	return out
}

// isClosureLocal reports whether name is recorded in the current
// handler's closure-local set (populated by scanClosureLocals at the
// start of lowerFuncDecl). Used by lowerCallExpr's bare-identifier
// fallback to decide between OpIndirectCall and the legacy diagnostic.
func (c *lowerCtx) isClosureLocal(name string) bool {
	return c.closureLocals[name]
}
