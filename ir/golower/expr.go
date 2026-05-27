// Expression lowering: turns each ast.Expr into one (or several) opcode
// expressions, returning the root ExprID. The translation favors direct
// op-for-op mappings to keep the opcode footprint small.
//
// Identifier resolution is intentionally simple: the lowerer doesn't
// know which names are locals vs signals vs props. It emits OpLocalGet
// for every bare identifier and lets the VM's OpLocalGet → OpAssign
// missing-frame fallback (X.A) or signal-aware OpAssign route the
// access correctly at runtime. This trades a small amount of runtime
// cost (one map lookup) for a much simpler lowerer.
//
// Selector expressions split three ways:
//   1. `math.Pi` → zero-arg OpCall (constant intrinsic, see intrinsics_table.go)
//   2. `pkg.Func(args)` → OpCall("pkg.Func", lowered args) when pkg.Func
//      is a registered intrinsic.
//   3. `obj.Field` → OpIndex with a string literal — fields and map keys
//      are treated uniformly so a Value with .Fields works for both.
//
// Call expressions split similarly: selector callees go through the
// intrinsic path; bare-identifier callees are rejected (the supported
// subset has no user-defined function calls — Go calls cross-handler
// only through signal mutation).

package golower

import (
	"fmt"
	"go/ast"
	"go/token"

	"m31labs.dev/gosx/island/program"
)

// lowerExpr is the expression-side mirror of lowerStmt.
func (c *lowerCtx) lowerExpr(e ast.Expr) program.ExprID {
	switch ex := e.(type) {
	case *ast.BasicLit:
		return c.lowerBasicLit(ex)
	case *ast.Ident:
		return c.lowerIdent(ex)
	case *ast.BinaryExpr:
		return c.lowerBinaryExpr(ex)
	case *ast.UnaryExpr:
		return c.lowerUnaryExpr(ex)
	case *ast.ParenExpr:
		return c.lowerExpr(ex.X)
	case *ast.SelectorExpr:
		return c.lowerSelectorExpr(ex)
	case *ast.CallExpr:
		return c.lowerCallExpr(ex)
	case *ast.IndexExpr:
		return c.lowerIndexExpr(ex)
	case *ast.CompositeLit:
		return c.lowerCompositeLit(ex)
	default:
		c.addIssue(e, fmt.Sprintf("unsupported expression %T", e), escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
	}
}

// lowerBasicLit handles int / float / string literals. The Go parser
// already normalizes these so we can hand the Value through unchanged.
func (c *lowerCtx) lowerBasicLit(lit *ast.BasicLit) program.ExprID {
	switch lit.Kind {
	case token.INT:
		return c.addExpr(program.Expr{Op: program.OpLitInt, Value: lit.Value, Type: program.TypeInt})
	case token.FLOAT:
		return c.addExpr(program.Expr{Op: program.OpLitFloat, Value: lit.Value, Type: program.TypeFloat})
	case token.STRING:
		// lit.Value includes the surrounding quotes; strip them.
		v := lit.Value
		if len(v) >= 2 && (v[0] == '"' || v[0] == '`') {
			v = v[1 : len(v)-1]
		}
		return c.addExpr(program.Expr{Op: program.OpLitString, Value: v, Type: program.TypeString})
	default:
		c.addIssue(lit, fmt.Sprintf("unsupported literal kind %s", lit.Kind), escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpLitString, Value: lit.Value, Type: program.TypeAny})
	}
}

// lowerIdent emits OpLocalGet for any bare identifier. Boolean and nil
// identifiers are special-cased into literal opcodes; everything else
// trusts the VM's missing_local diagnostic to surface unbound names.
func (c *lowerCtx) lowerIdent(id *ast.Ident) program.ExprID {
	switch id.Name {
	case "true":
		return c.addExpr(program.Expr{Op: program.OpLitBool, Value: "true", Type: program.TypeBool})
	case "false":
		return c.addExpr(program.Expr{Op: program.OpLitBool, Value: "false", Type: program.TypeBool})
	case "nil":
		// Closest equivalent in the Value type is an empty string of
		// TypeAny. The VM rarely cares — comparisons against nil aren't
		// well-defined in the bytecode set today.
		return c.addExpr(program.Expr{Op: program.OpLitString, Value: "", Type: program.TypeAny})
	default:
		return c.addExpr(program.Expr{Op: program.OpLocalGet, Value: id.Name})
	}
}

// lowerBinaryExpr maps Go's binary operators to VM opcodes. The
// supported set covers all arithmetic, comparison, and boolean
// operators in the plan's Supported Subset; bitwise operators are out
// of scope and produce an unsupported-operator issue.
func (c *lowerCtx) lowerBinaryExpr(b *ast.BinaryExpr) program.ExprID {
	op, ok := binaryOpcode(b.Op)
	if !ok {
		c.addIssue(b, fmt.Sprintf("unsupported binary operator %s", b.Op), escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
	}
	lhsID := c.lowerExpr(b.X)
	rhsID := c.lowerExpr(b.Y)
	return c.addExpr(program.Expr{
		Op:       op,
		Operands: []program.ExprID{lhsID, rhsID},
	})
}

// lowerUnaryExpr handles `-x` and `!x`. The `+x` form is a no-op pass.
// Address-of (`&x`) and dereference (`*x`) are rejected — pointers
// aren't part of the supported subset.
func (c *lowerCtx) lowerUnaryExpr(u *ast.UnaryExpr) program.ExprID {
	switch u.Op {
	case token.ADD:
		return c.lowerExpr(u.X)
	case token.SUB:
		argID := c.lowerExpr(u.X)
		return c.addExpr(program.Expr{Op: program.OpNeg, Operands: []program.ExprID{argID}})
	case token.NOT:
		argID := c.lowerExpr(u.X)
		return c.addExpr(program.Expr{Op: program.OpNot, Operands: []program.ExprID{argID}})
	default:
		c.addIssue(u, fmt.Sprintf("unsupported unary operator %s", u.Op), escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
	}
}

// lowerSelectorExpr handles three cases:
//   1. math.Pi (and any other constant intrinsic) → zero-arg OpCall.
//   2. pkg.Ident where pkg is the importing alias for a stdlib package
//      → emit a marker that the parent CallExpr lowers as OpCall. To
//      keep things simple we emit OpCall("pkg.Ident") with no operands
//      when used outside a call site — runtime treats unrecognized
//      callees as zero, which is the right behavior for the rare bare
//      selector.
//   3. obj.Field (a non-intrinsic selector) → OpIndex with a literal
//      string key, treating the LHS as a Value with .Fields populated.
func (c *lowerCtx) lowerSelectorExpr(s *ast.SelectorExpr) program.ExprID {
	if pkg, ok := identName(s.X); ok {
		qualified := pkg + "." + s.Sel.Name
		if isConstantIntrinsic(qualified) {
			return c.addExpr(program.Expr{Op: program.OpCall, Value: qualified})
		}
		if isIntrinsic(qualified) {
			// A bare selector that names an intrinsic but isn't being
			// called would be a Go function value — out of scope. Emit
			// the call placeholder; the parent CallExpr (if any)
			// overwrites this. Standalone bare selectors aren't
			// reachable from valid Go for these names.
			return c.addExpr(program.Expr{Op: program.OpCall, Value: qualified})
		}
	}
	// Field-access fallthrough.
	objID := c.lowerExpr(s.X)
	keyID := c.addExpr(program.Expr{Op: program.OpLitString, Value: s.Sel.Name, Type: program.TypeString})
	return c.addExpr(program.Expr{Op: program.OpIndex, Operands: []program.ExprID{objID, keyID}})
}

// lowerCallExpr handles `pkg.Func(args)` calls into the intrinsic
// registry and the special-case `make(...)`/`len(...)`/`append(...)`
// builtins. Calls to bare identifiers (user-defined functions) are
// rejected — the supported subset has no inter-function dispatch.
func (c *lowerCtx) lowerCallExpr(call *ast.CallExpr) program.ExprID {
	// Builtin: len(x) → OpLen.
	if id, ok := call.Fun.(*ast.Ident); ok {
		switch id.Name {
		case "len":
			if len(call.Args) != 1 {
				c.addIssue(call, "len requires exactly 1 argument", escapeHatchSuggestion)
				return c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
			}
			argID := c.lowerExpr(call.Args[0])
			return c.addExpr(program.Expr{Op: program.OpLen, Operands: []program.ExprID{argID}})
		case "append":
			if len(call.Args) != 2 {
				c.addIssue(call, "append: only 2-arg form supported", escapeHatchSuggestion)
				return c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
			}
			argA := c.lowerExpr(call.Args[0])
			argB := c.lowerExpr(call.Args[1])
			return c.addExpr(program.Expr{Op: program.OpAppend, Operands: []program.ExprID{argA, argB}})
		case "int":
			if len(call.Args) != 1 {
				c.addIssue(call, "int conversion requires 1 argument", escapeHatchSuggestion)
				return c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
			}
			argID := c.lowerExpr(call.Args[0])
			return c.addExpr(program.Expr{Op: program.OpToInt, Operands: []program.ExprID{argID}})
		case "float64":
			if len(call.Args) != 1 {
				c.addIssue(call, "float64 conversion requires 1 argument", escapeHatchSuggestion)
				return c.addExpr(program.Expr{Op: program.OpLitFloat, Value: "0", Type: program.TypeFloat})
			}
			argID := c.lowerExpr(call.Args[0])
			return c.addExpr(program.Expr{Op: program.OpToFloat, Operands: []program.ExprID{argID}})
		case "string":
			if len(call.Args) != 1 {
				c.addIssue(call, "string conversion requires 1 argument", escapeHatchSuggestion)
				return c.addExpr(program.Expr{Op: program.OpLitString, Value: "", Type: program.TypeString})
			}
			argID := c.lowerExpr(call.Args[0])
			return c.addExpr(program.Expr{Op: program.OpToString, Operands: []program.ExprID{argID}})
		case "make":
			// Slice Y.E: `make(...)` allocates an empty collection. Routed
			// BEFORE the user-fn registry probe per Y.D's retrospective
			// handoff so a user-declared `make` can't accidentally shadow
			// the builtin. The first arg is the collection type literal
			// (a *ast.MapType or *ast.ArrayType); the rest are the
			// optional length / capacity hints.
			return c.lowerMakeCall(call)
		default:
			// Slice Y.D: route in-package calls into OpIndirectCall when
			// the name resolves through the user-function registry built
			// by scanUserFuncs. Unregistered bare identifiers still emit
			// the legacy diagnostic so authors get a clear pointer when
			// they typo a sibling function or call into a deleted helper.
			if _, ok := c.lookupUserFunc(id.Name); ok {
				return c.emitIndirectCall(id.Name, call.Args)
			}
			c.addIssue(call, fmt.Sprintf("calls to user-defined function %q are not supported", id.Name), escapeHatchSuggestion)
			return c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
		}
	}

	// Selector: pkg.Func or obj.Method.
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		c.addIssue(call, fmt.Sprintf("unsupported call target %T", call.Fun), escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
	}
	pkg, pkgOK := identName(sel.X)
	if !pkgOK {
		c.addIssue(call, "method calls on non-package receivers are not supported", escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
	}
	// Slice Y.E: discriminate intrinsic vs host call by checking the
	// receiver against the file's import set. Receivers that aren't
	// imported packages (`c`, `ctx`, ...) route into OpHostCall;
	// imported packages stay on the OpCall intrinsic path. This is
	// also the catch-all for stdlib package names — `math.Sin` is
	// imported, so it goes through intrinsics; a typo `maht.Sin`
	// falls through to host dispatch and records a `host_unbound`
	// diagnostic at evaluation time.
	if c.isImportedPackage(pkg) {
		qualified := pkg + "." + sel.Sel.Name
		if !isIntrinsic(qualified) {
			c.addIssue(call, fmt.Sprintf("call to %s is not in the supported intrinsic set", qualified), escapeHatchSuggestion)
			return c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
		}
		argIDs := make([]program.ExprID, 0, len(call.Args))
		for _, a := range call.Args {
			argIDs = append(argIDs, c.lowerExpr(a))
		}
		return c.addExpr(program.Expr{Op: program.OpCall, Value: qualified, Operands: argIDs})
	}
	// Receiver is not an imported package — treat as a host method
	// call. The VM looks up the bound HostReceiver by the receiver
	// identifier ("c", "ctx", ...) at evaluation time.
	return c.lowerHostCall(pkg, sel, call.Args)
}

// lowerIndexExpr emits OpIndex for both slice and map indexing — the
// VM's IndexVal already handles both cases uniformly.
func (c *lowerCtx) lowerIndexExpr(ix *ast.IndexExpr) program.ExprID {
	objID := c.lowerExpr(ix.X)
	keyID := c.lowerExpr(ix.Index)
	return c.addExpr(program.Expr{Op: program.OpIndex, Operands: []program.ExprID{objID, keyID}})
}

// binaryOpcode maps token.Token binary operators to their VM opcode
// equivalents. Returns (opcode, true) for supported operators or
// (0, false) for unsupported ones so the caller can record a clear
// issue with the operator's source text.
func binaryOpcode(tok token.Token) (program.OpCode, bool) {
	switch tok {
	case token.ADD:
		return program.OpAdd, true
	case token.SUB:
		return program.OpSub, true
	case token.MUL:
		return program.OpMul, true
	case token.QUO:
		return program.OpDiv, true
	case token.REM:
		return program.OpMod, true
	case token.EQL:
		return program.OpEq, true
	case token.NEQ:
		return program.OpNeq, true
	case token.LSS:
		return program.OpLt, true
	case token.GTR:
		return program.OpGt, true
	case token.LEQ:
		return program.OpLte, true
	case token.GEQ:
		return program.OpGte, true
	case token.LAND:
		return program.OpAnd, true
	case token.LOR:
		return program.OpOr, true
	}
	return 0, false
}
