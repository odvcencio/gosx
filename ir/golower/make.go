// Slice Y.E.1 — lowering for the `make(...)` builtin.
//
// `make` is a Go builtin (not a user function), and the lowerer must
// route it BEFORE the user-function registry probe in lowerCallExpr.
// Per Y.D's retrospective handoff, the dispatch lives alongside
// len/append/int/float64/string in the `switch id.Name` block so a
// user-declared `make` (even though such a declaration would shadow the
// builtin and confuse Go itself) cannot misroute the call.
//
// Supported forms — matches the Y.E plan's coverage exactly:
//
//   make(map[K]V)               → OpMake("map")
//   make(map[K]V, hint)         → OpMake("map")          // hint is ignored
//   make([]T, len)              → OpMake("slice", len)
//   make([]T, len, cap)         → OpMake("slice", len)   // cap is ignored
//   make([]T, 0)                → OpMake("slice", 0)
//
// The VM has no concept of map capacity or slice capacity; both Go
// values are advisory at runtime in the standard library too (the
// runtime grows storage on demand). Dropping them at lower time keeps
// the wire format clean. Channels (`make(chan T)`) are not supported —
// goroutines / channels are explicitly out of the engine-surface
// authoring subset per the package doc in golower.go.

package golower

import (
	"go/ast"

	"m31labs.dev/gosx/island/program"
)

// lowerMakeCall lowers a call expression whose callee is the bare
// identifier `make`. The first argument carries the type literal that
// selects map vs slice; the remaining arguments are sizing hints which
// the VM doesn't use (see file-level doc).
func (c *lowerCtx) lowerMakeCall(call *ast.CallExpr) program.ExprID {
	if len(call.Args) == 0 {
		c.addIssue(call, "make requires at least 1 argument (the type literal)", escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpMake, Value: "map"})
	}
	switch typ := call.Args[0].(type) {
	case *ast.MapType:
		// make(map[K]V) or make(map[K]V, hint). The optional hint is
		// dropped — the VM has no notion of map capacity, and Go's
		// runtime treats the hint as advisory only.
		return c.addExpr(program.Expr{Op: program.OpMake, Value: "map"})
	case *ast.ArrayType:
		// make([]T, n) or make([]T, n, cap). The cap is dropped for the
		// same reason as the map hint. ArrayType with a non-nil Len
		// would be a fixed-size array literal, which Go's `make` doesn't
		// accept syntactically, so we treat it as the slice form.
		if len(call.Args) < 2 {
			// `make([]T)` is invalid Go but the VM tolerates it as an
			// empty slice — record a diagnostic and emit a zero-length
			// OpMake so the rest of the function continues to lower.
			c.addIssue(call, "make([]T) requires a length argument", escapeHatchSuggestion)
			lenZero := c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
			return c.addExpr(program.Expr{Op: program.OpMake, Value: "slice", Operands: []program.ExprID{lenZero}})
		}
		lenID := c.lowerExpr(call.Args[1])
		// Args[2] (cap) is intentionally dropped — see file-level doc.
		_ = typ
		return c.addExpr(program.Expr{Op: program.OpMake, Value: "slice", Operands: []program.ExprID{lenID}})
	default:
		c.addIssue(call, "make: only []T and map[K]V types are supported (chan T and named types are not)", escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpMake, Value: "map"})
	}
}
