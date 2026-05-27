// Slice Y.E.3 — *ast.SliceExpr lowering.
//
// Go's slice-expression syntax (`s[i:j]`, `s[i:j:k]`) translates to
// the VM's OpSlice opcode for arrays/slices and OpSubstring for
// strings. The lowerer can't tell the operand type at lower time
// without a go/types pass, so we emit OpSlice and rely on the VM's
// sliceValue handler to dispatch on the runtime Value shape (Items
// vs Str). The full-slice form `s[i:j:k]` is rejected — the
// three-index form's `cap` is meaningless in the VM (it has no
// capacity concept, see make.go's note).
//
// Missing bounds default to:
//   - low (i):  0
//   - high (j): len(s)
// matching Go's semantics.

package golower

import (
	"go/ast"

	"m31labs.dev/gosx/island/program"
)

// lowerSliceExpr emits OpSlice with the collection and resolved
// low/high bounds. The VM's sliceValue handler dispatches on the
// collection's runtime kind.
func (c *lowerCtx) lowerSliceExpr(s *ast.SliceExpr) program.ExprID {
	if s.Slice3 {
		c.addIssue(s, "three-index slice s[i:j:k] is not supported (cap is meaningless in the VM)", escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpSlice})
	}
	collID := c.lowerExpr(s.X)
	var lowID program.ExprID
	if s.Low != nil {
		lowID = c.lowerExpr(s.Low)
	} else {
		lowID = c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
	}
	var highID program.ExprID
	if s.High != nil {
		highID = c.lowerExpr(s.High)
	} else {
		// Default high bound is len(collection).
		highID = c.addExpr(program.Expr{Op: program.OpLen, Operands: []program.ExprID{collID}})
	}
	return c.addExpr(program.Expr{
		Op:       program.OpSlice,
		Operands: []program.ExprID{collID, lowID, highID},
	})
}
