// Slice Y.C — LHS selector / indexed-set lowering.
//
// Engine-surface handlers routinely mutate package-level state through
// non-trivial LHS expressions:
//
//   - struct field set:     gTx.X = mx - factor * ...
//   - slice index set:      fx[a.ID] += ux * force
//   - map key set:          gPos[gDrag.NodeID] = vec2{wx, wy}
//   - chained selectors:    h.Inner.X = 99    (struct-in-struct)
//   - chained sel-on-index: nodes[i].Y = 3    (struct-in-slice)
//
// Pre-Y.C the lowerer rejected every non-Ident LHS with
// "left-hand side must be a simple identifier". The dispatcher below
// detects the LHS shape and emits one of:
//
//   OpFieldSet(target, value)     for `target.<name> = value`
//   OpIndexSet(coll, key, value)  for `coll[key] = value`
//
// Compound forms (`+=`, `-=`, `*=`, `/=`, `%=`) lower to a fresh
// arithmetic expression whose LHS reads back the current value through
// the same selector/index path; this keeps the LHS evaluation
// once-and-only-once when reading is cheap (the supported subset has
// no side-effectful selectors). For a richer expression model that
// guarantees single-evaluation of the target sub-expressions, Y.D
// can revisit when user-function calls appear in LHS targets.
//
// Chained selectors (`h.Inner.X`) work without any special-case in
// this file: the LHS-set helpers re-use lowerExpr to materialize the
// inner target, which already turns nested selectors into a chain of
// OpIndex reads producing a Value whose Fields map is the inner
// struct. Writing through that Value mutates the shared map (per the
// Y.C in-place mutation decision documented in client/vm/lhs_set.go).
//
// Tok semantics:
//   - token.DEFINE (`:=`) on a selector/index LHS is invalid Go
//     (Go's parser would have refused it), so the dispatcher only
//     ever sees token.ASSIGN or the compound forms here.

package golower

import (
	"fmt"
	"go/ast"
	"go/token"

	"m31labs.dev/gosx/island/program"
)

// lowerSelectorOrIndexAssign handles a single-LHS, single-RHS
// assignment where the LHS is *not* a bare identifier. Returns the
// emitted ExprID. Callers (lowerAssignStmt) decide when to route here
// based on the LHS shape; this function trusts that decision and
// rejects bare identifiers defensively.
func (c *lowerCtx) lowerSelectorOrIndexAssign(s *ast.AssignStmt) program.ExprID {
	if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
		c.addIssue(s, "internal: lowerSelectorOrIndexAssign called with non-1:1 shape", escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpSeq})
	}

	// For compound forms (+=, -=, ...), the read-then-write pattern is:
	//   target.X op= rhs
	//     ↓
	//   OpFieldSet target.X = (OpFieldGet target.X) op rhs
	// where the read uses the existing OpIndex lowering path and the
	// write uses OpFieldSet / OpIndexSet.
	var rhsID program.ExprID
	if isCompoundAssign(s.Tok) {
		op := compoundOpcode(s.Tok)
		readID := c.lowerExpr(s.Lhs[0]) // re-uses OpIndex-style read.
		valueID := c.lowerExpr(s.Rhs[0])
		rhsID = c.addExpr(program.Expr{Op: op, Operands: []program.ExprID{readID, valueID}})
	} else if s.Tok == token.ASSIGN {
		rhsID = c.lowerExpr(s.Rhs[0])
	} else {
		c.addIssue(s, fmt.Sprintf("LHS selector/index assignment with operator %s is not supported", s.Tok), escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpSeq})
	}

	switch lhs := s.Lhs[0].(type) {
	case *ast.SelectorExpr:
		return c.emitFieldSet(lhs, rhsID)
	case *ast.IndexExpr:
		return c.emitIndexSet(lhs, rhsID)
	default:
		c.addIssue(lhs, fmt.Sprintf("unsupported LHS expression %T", lhs), escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpSeq})
	}
}

// emitFieldSet emits `target.<Sel> = valueID` as OpFieldSet. The
// target sub-expression is lowered through lowerExpr, which handles
// arbitrary chains (`a.b.c`, `arr[i].field`, ...) by re-using the
// existing OpIndex read path. The chain's terminal Value has the
// Fields map we want to mutate.
func (c *lowerCtx) emitFieldSet(sel *ast.SelectorExpr, valueID program.ExprID) program.ExprID {
	targetID := c.lowerExpr(sel.X)
	return c.addExpr(program.Expr{
		Op:       program.OpFieldSet,
		Value:    sel.Sel.Name,
		Operands: []program.ExprID{targetID, valueID},
	})
}

// emitIndexSet emits `target[key] = valueID` as OpIndexSet. Like
// emitFieldSet, the collection sub-expression is lowered through
// lowerExpr so chained access (`s[i].Field = v` becomes
// `(s[i]).Field = v` → OpFieldSet over an OpIndex read — handled by
// the selector branch, not this one) and bare collection access
// (`m["k"] = v`) both work.
func (c *lowerCtx) emitIndexSet(idx *ast.IndexExpr, valueID program.ExprID) program.ExprID {
	collID := c.lowerExpr(idx.X)
	keyID := c.lowerExpr(idx.Index)
	return c.addExpr(program.Expr{
		Op:       program.OpIndexSet,
		Operands: []program.ExprID{collID, keyID, valueID},
	})
}

// isLHSSelectorOrIndex reports whether the expression is one of the
// non-Ident LHS shapes the Y.C dispatcher handles. Used by
// lowerAssignStmt to route to the selector/index path instead of
// emitting the legacy "left-hand side must be a simple identifier"
// diagnostic.
func isLHSSelectorOrIndex(e ast.Expr) bool {
	switch e.(type) {
	case *ast.SelectorExpr, *ast.IndexExpr:
		return true
	default:
		return false
	}
}

// isCompoundAssign reports whether tok is one of `+=`, `-=`, `*=`,
// `/=`, `%=`. These are the compound-assign tokens the Y.C dispatcher
// reads through the LHS to emit a read+write pair.
func isCompoundAssign(tok token.Token) bool {
	switch tok {
	case token.ADD_ASSIGN, token.SUB_ASSIGN, token.MUL_ASSIGN, token.QUO_ASSIGN, token.REM_ASSIGN:
		return true
	default:
		return false
	}
}
