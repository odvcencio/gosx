// Slice Y.E.3 — `[]T(x)` ArrayType-cast lowering.
//
// Go's AST represents `[]rune(s)` as a CallExpr whose Fun is an
// *ast.ArrayType with Elt = "rune". This was Y.D's "residual #3" —
// not in Y.D's plan-defined scope but recognized as needing handling
// before graph_surface.go could lower cleanly.
//
// Supported element types — exactly what graph_surface.go uses:
//
//   []rune(s)   → OpToRunes(s)         — string → rune-array
//   []byte(s)   → OpToRunes(s)         — same VM shape; bytes/runes
//                                        are indistinguishable at the
//                                        Value level (no byte type).
//
// Other element types (`[]int(...)`, `[]Node(...)`) are rejected with
// a clear diagnostic — they'd need genuine type-aware conversion that
// the supported subset doesn't promise.

package golower

import (
	"go/ast"

	"m31labs.dev/gosx/island/program"
)

// lowerArrayTypeCast emits OpToRunes for `[]rune(s)` / `[]byte(s)`.
// Other element types fall through to an issue + zero-value Expr so
// the rest of the surrounding expression keeps lowering.
func (c *lowerCtx) lowerArrayTypeCast(at *ast.ArrayType, call *ast.CallExpr) program.ExprID {
	if at.Len != nil {
		c.addIssue(call, "fixed-length array conversions are not supported (use []T or []rune)", escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
	}
	if len(call.Args) != 1 {
		c.addIssue(call, "[]T(x) cast requires exactly 1 argument", escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
	}
	elt, ok := at.Elt.(*ast.Ident)
	if !ok {
		c.addIssue(call, "only `[]rune(s)` and `[]byte(s)` casts are supported (element type must be a builtin identifier)", escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
	}
	switch elt.Name {
	case "rune", "byte", "int32", "uint8":
		argID := c.lowerExpr(call.Args[0])
		return c.addExpr(program.Expr{
			Op:       program.OpToRunes,
			Operands: []program.ExprID{argID},
		})
	default:
		c.addIssue(call, "only `[]rune(s)` and `[]byte(s)` casts are supported", escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
	}
}
