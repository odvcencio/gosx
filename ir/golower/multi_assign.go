// Slice Y.B — multi-value assignment lowering.
//
// This file holds the lowerMultiAssign dispatcher and its two
// per-pattern helpers:
//
//   - lowerCommaOkMapIndex handles `v, ok := m[k]` (and `_, ok := m[k]`,
//     `v, _ := m[k]`). It emits one OpMapLookup against the map +
//     key expressions, stashes the result ObjectVal into a fresh
//     synthetic local, then emits per-LHS OpAssign reads of the
//     "value" and "ok" fields via OpIndex.
//
//   - lowerParallelAssign handles `a, b := x, y` (and `a, b = x, y`).
//     It honors Go's evaluate-all-Rhs-before-assigning-Lhs rule by
//     materializing each Rhs into a synthetic local first, then
//     assigning those locals to the LHS bindings in a second pass.
//     This is what makes the canonical swap `a, b = b, a` correct.
//
// User-function multi-return (`a, b := f()`) is *not* handled here —
// the supported subset has no user-defined function calls until
// Slice Y.D adds OpIndirectCall + a per-surface function registry.
// Multi-return intrinsics aren't in the X.B registry either, so this
// path is currently unreachable for non-map Rhs. Y.B emits a clear
// "cannot lower multi-value Rhs" issue for any non-map single-Rhs
// case so the diagnostic points at the right follow-up slice.

package golower

import (
	"fmt"
	"go/ast"
	"go/token"

	"m31labs.dev/gosx/island/program"
)

// lowerMultiAssign dispatches an AssignStmt with len(Lhs) > 1 to the
// right per-pattern helper based on the shape of Rhs.
func (c *lowerCtx) lowerMultiAssign(s *ast.AssignStmt) program.ExprID {
	if s.Tok != token.ASSIGN && s.Tok != token.DEFINE {
		c.addIssue(s, fmt.Sprintf("multi-value assignment operator %s is not supported", s.Tok), escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpSeq})
	}

	// Pattern A: comma-ok map index. `v, ok := m[k]` — exactly two LHS,
	// single Rhs that is an *ast.IndexExpr.
	if len(s.Lhs) == 2 && len(s.Rhs) == 1 {
		if idx, ok := s.Rhs[0].(*ast.IndexExpr); ok {
			return c.lowerCommaOkMapIndex(s, idx)
		}
		// Single-Rhs multi-Lhs that isn't a map index would be a
		// multi-return function call — Y.D territory.
		c.addIssue(s, "multi-value assignment from a function call is not supported (use comma-ok map index, parallel assign, or wait for Slice Y.D)", escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpSeq})
	}

	// Pattern B: parallel assignment. `a, b := x, y` — len(Lhs) == len(Rhs).
	if len(s.Lhs) == len(s.Rhs) {
		return c.lowerParallelAssign(s)
	}

	c.addIssue(s, fmt.Sprintf("multi-value assignment shape (%d LHS, %d RHS) is not supported", len(s.Lhs), len(s.Rhs)), escapeHatchSuggestion)
	return c.addExpr(program.Expr{Op: program.OpSeq})
}

// lowerCommaOkMapIndex emits the bytecode for `v, ok := m[k]`. The
// shape is:
//
//     __tmp := OpMapLookup(m, k)        // single ObjectVal{value, ok}
//     v     := __tmp["value"]            // OpIndex read
//     ok    := __tmp["ok"]               // OpIndex read
//
// Blank-identifier bindings (`_`) are skipped — the lowerer omits the
// LocalDecl + Assign pair so a `_, ok := m[k]` lowering doesn't leak
// an unused local into the frame.
//
// The synthetic local name is suffixed with the statement's source
// position so multiple comma-ok forms in the same handler don't
// stomp on each other's temporary.
func (c *lowerCtx) lowerCommaOkMapIndex(s *ast.AssignStmt, idx *ast.IndexExpr) program.ExprID {
	mapID := c.lowerExpr(idx.X)
	keyID := c.lowerExpr(idx.Index)
	lookupID := c.addExpr(program.Expr{
		Op:       program.OpMapLookup,
		Operands: []program.ExprID{mapID, keyID},
	})

	tmpName := c.freshLocal("commaOk", s.Pos())

	var ops []program.ExprID
	// Always declare the tmp local up front so OpAssign has a frame slot.
	ops = append(ops, c.addExpr(program.Expr{Op: program.OpLocalDecl, Value: tmpName}))
	ops = append(ops, c.addExpr(program.Expr{
		Op:       program.OpAssign,
		Value:    tmpName,
		Operands: []program.ExprID{lookupID},
	}))

	// Bind each LHS to the corresponding field of the tmp result.
	if name, isBlank := lhsTarget(s.Lhs[0]); !isBlank {
		ops = append(ops, c.bindFromTmp(name, tmpName, "value", s.Tok))
	}
	if name, isBlank := lhsTarget(s.Lhs[1]); !isBlank {
		ops = append(ops, c.bindFromTmp(name, tmpName, "ok", s.Tok))
	}

	return c.addExpr(program.Expr{Op: program.OpSeq, Operands: ops})
}

// lowerParallelAssign emits the bytecode for `a, b := x, y`. Per Go
// semantics, all RHS expressions evaluate before any LHS write — so
// the lowerer materializes each Rhs into a synthetic local first,
// then runs the assignments in a second pass. This is what makes the
// canonical swap idiom `a, b = b, a` correct without any LHS
// dependency analysis.
//
// Blank-identifier bindings still evaluate their RHS for side effects
// (consistent with Go) but don't generate the per-LHS OpAssign.
func (c *lowerCtx) lowerParallelAssign(s *ast.AssignStmt) program.ExprID {
	var ops []program.ExprID

	// Phase 1: lower each RHS into a fresh local. We always create the
	// tmp slot even for blank-identifier LHS so the RHS still evaluates
	// once (matching Go's order-of-evaluation guarantees). Each RHS
	// gets its own indexed slot so the statement's parallel RHS exprs
	// don't collide on a shared tmp name.
	base := c.freshLocal("par", s.Pos())
	tmpNames := make([]string, len(s.Rhs))
	for i, rhs := range s.Rhs {
		valueID := c.lowerExpr(rhs)
		tmpNames[i] = fmt.Sprintf("%s_%d", base, i)
		ops = append(ops, c.addExpr(program.Expr{Op: program.OpLocalDecl, Value: tmpNames[i]}))
		ops = append(ops, c.addExpr(program.Expr{
			Op:       program.OpAssign,
			Value:    tmpNames[i],
			Operands: []program.ExprID{valueID},
		}))
	}

	// Phase 2: assign each tmp to its LHS binding (skipping blanks).
	for i, lhs := range s.Lhs {
		name, isBlank := lhsTarget(lhs)
		if isBlank {
			continue
		}
		ops = append(ops, c.bindFromLocal(name, tmpNames[i], s.Tok))
	}

	return c.addExpr(program.Expr{Op: program.OpSeq, Operands: ops})
}

// bindFromTmp emits the OpLocalDecl (for `:=`) + OpAssign sequence
// that binds an LHS name to a field of a synthetic tmp ObjectVal.
// The field read happens via OpIndex against the tmp's local — same
// machinery as a struct field access.
func (c *lowerCtx) bindFromTmp(lhsName, tmpName, field string, tok token.Token) program.ExprID {
	tmpReadID := c.addExpr(program.Expr{Op: program.OpLocalGet, Value: tmpName})
	keyID := c.addExpr(program.Expr{Op: program.OpLitString, Value: field, Type: program.TypeString})
	fieldReadID := c.addExpr(program.Expr{Op: program.OpIndex, Operands: []program.ExprID{tmpReadID, keyID}})
	return c.bindLHS(lhsName, fieldReadID, tok)
}

// bindFromLocal emits the OpLocalDecl (for `:=`) + OpAssign sequence
// that binds an LHS name to the current value of another local. Used
// by parallel assignment's phase 2.
func (c *lowerCtx) bindFromLocal(lhsName, sourceLocal string, tok token.Token) program.ExprID {
	readID := c.addExpr(program.Expr{Op: program.OpLocalGet, Value: sourceLocal})
	return c.bindLHS(lhsName, readID, tok)
}

// bindLHS is the shared tail for the `:=` vs `=` distinction across
// the multi-assign helpers. For `:=` it emits an OpLocalDecl + OpAssign
// pair wrapped in OpSeq; for `=` it emits the OpAssign alone (signal
// or local resolution happens at runtime).
func (c *lowerCtx) bindLHS(name string, valueID program.ExprID, tok token.Token) program.ExprID {
	if tok == token.DEFINE {
		declID := c.addExpr(program.Expr{Op: program.OpLocalDecl, Value: name})
		assignID := c.addExpr(program.Expr{Op: program.OpAssign, Value: name, Operands: []program.ExprID{valueID}})
		return c.addExpr(program.Expr{Op: program.OpSeq, Operands: []program.ExprID{declID, assignID}})
	}
	return c.addExpr(program.Expr{Op: program.OpAssign, Value: name, Operands: []program.ExprID{valueID}})
}

// lhsTarget is the package-level helper that classifies an LHS slot
// as (name, isBlank). Non-identifier LHSs (selectors, index exprs)
// are not Y.B's scope — they show up in Y.C — so we treat them as
// blank here and let the caller's per-statement diagnostic carry the
// "left-hand side must be a simple identifier" issue from the
// existing single-assign path. The caller is responsible for catching
// non-ident LHS before reaching here when possible.
func lhsTarget(e ast.Expr) (string, bool) {
	id, ok := e.(*ast.Ident)
	if !ok {
		return "", true
	}
	if id.Name == "_" {
		return "", true
	}
	return id.Name, false
}

// freshLocal returns a synthetic local name namespaced by purpose and
// source position. Suffixing with token.Pos keeps multiple multi-assign
// statements in the same handler from colliding on their tmp slot.
func (c *lowerCtx) freshLocal(kind string, pos token.Pos) string {
	return fmt.Sprintf("__y_b_%s_%d", kind, int(pos))
}
