// Slice Y.E.3 — *ast.SwitchStmt lowering.
//
// Go's switch statement is essentially syntactic sugar for an
// if/else-if/else chain when the cases are scalar (value-switch, no
// fallthrough). The supported subset in the engine-surface authoring
// contract covers exactly that — graph_surface.go's typeColor is the
// canonical example: a string-tag switch with a default case and no
// fallthrough.
//
// Supported shapes:
//
//   switch tag {                    — tag-switch over a scalar
//   case a, b, c: ...               — multi-value case (any/or match)
//   case x: ...                     — single-value case
//   default: ...                    — default case (optional)
//   }
//
//   switch {                        — bare-switch (no tag); each case
//   case cond1: ...                   carries its own boolean condition
//   case cond2: ...
//   }
//
// NOT supported:
//
//   - `fallthrough` keyword (translates to a new control-flow concept
//     the VM doesn't model; rejected with a clear diagnostic).
//   - `switch x.(type)` type-switches (out of subset; requires
//     run-time type info).
//   - `switch init; tag { ... }` init statements are folded into a
//     preceding OpSeq member, same shape as lowerIfStmt.

package golower

import (
	"go/ast"
	"go/token"

	"m31labs.dev/gosx/island/program"
)

// lowerSwitchStmt translates a Go switch into a chain of OpCond
// branches. The tag (if any) is evaluated once into a synthetic local
// so each case comparison can re-read it without re-evaluating
// side-effectful tag expressions.
func (c *lowerCtx) lowerSwitchStmt(s *ast.SwitchStmt) program.ExprID {
	var seqOps []program.ExprID

	// Init clause (`switch x := f(); tag { ... }`) lowers as a
	// preceding OpSeq member so the init's local is visible to the
	// rest of the switch body.
	if s.Init != nil {
		seqOps = append(seqOps, c.lowerStmt(s.Init))
	}

	// Cache the tag once so each case's comparison reads a local
	// rather than re-evaluating the tag expression. The synthetic
	// local uses a reserved prefix per the Y.B / Y.D convention.
	var tagRefID program.ExprID
	hasTag := s.Tag != nil
	if hasTag {
		tagName := freshSwitchTagLocal(s.Pos())
		tagInitID := c.lowerExpr(s.Tag)
		seqOps = append(seqOps, c.addExpr(program.Expr{Op: program.OpLocalDecl, Value: tagName}))
		seqOps = append(seqOps, c.addExpr(program.Expr{
			Op:       program.OpAssign,
			Value:    tagName,
			Operands: []program.ExprID{tagInitID},
		}))
		tagRefID = c.addExpr(program.Expr{Op: program.OpLocalGet, Value: tagName})
	}

	// Build the case chain from the bottom up so the final else is
	// the default case body (or an OpSeq noop when no default).
	cases := s.Body.List
	var defaultBody []program.ExprID
	cond := []caseClause{}
	for _, cs := range cases {
		cc, ok := cs.(*ast.CaseClause)
		if !ok {
			c.addIssue(cs, "switch body must contain CaseClause entries", escapeHatchSuggestion)
			continue
		}
		// Reject fallthrough — the VM has no concept of falling
		// through to the next case without re-checking its condition.
		for _, st := range cc.Body {
			if br, ok := st.(*ast.BranchStmt); ok && br.Tok == token.FALLTHROUGH {
				c.addIssue(st, "fallthrough is not supported in switch", escapeHatchSuggestion)
			}
		}
		if len(cc.List) == 0 {
			// Default case.
			defaultBody = c.lowerStmtList(cc.Body)
			continue
		}
		cond = append(cond, caseClause{
			matchExprs: cc.List,
			body:       cc.Body,
		})
	}

	// Materialize the default branch's body expression.
	var elseID program.ExprID
	if len(defaultBody) == 0 {
		elseID = c.addExpr(program.Expr{Op: program.OpSeq})
	} else {
		elseID = c.addExpr(program.Expr{Op: program.OpSeq, Operands: defaultBody})
	}

	// Build the case chain right-to-left so the deepest else is the
	// default, then each enclosing OpCond's else is the next case in
	// source order.
	currentElse := elseID
	for i := len(cond) - 1; i >= 0; i-- {
		clause := cond[i]
		condID := c.buildCaseCondition(hasTag, tagRefID, clause.matchExprs)
		thenID := c.addExpr(program.Expr{Op: program.OpSeq, Operands: c.lowerStmtList(clause.body)})
		currentElse = c.addExpr(program.Expr{
			Op:       program.OpCond,
			Operands: []program.ExprID{condID, thenID, currentElse},
		})
	}

	seqOps = append(seqOps, currentElse)
	if len(seqOps) == 1 {
		return seqOps[0]
	}
	return c.addExpr(program.Expr{Op: program.OpSeq, Operands: seqOps})
}

// buildCaseCondition emits the boolean expression for one case clause.
// For a tag-switch, each matchExpr is `tag == matchExpr`; multi-value
// cases (`case a, b, c:`) OR them together. For a bare-switch
// (tag-less), the matchExpr is already a boolean expression.
func (c *lowerCtx) buildCaseCondition(hasTag bool, tagRefID program.ExprID, matchExprs []ast.Expr) program.ExprID {
	var condID program.ExprID
	for i, me := range matchExprs {
		var thisCondID program.ExprID
		if hasTag {
			// `tag == me`. The tag local is re-read fresh per case to
			// keep OpLocalGet pure (no shared state across cases).
			tagID := c.addExpr(program.Expr{Op: program.OpLocalGet, Value: switchTagLocalName(tagRefID, c)})
			meID := c.lowerExpr(me)
			thisCondID = c.addExpr(program.Expr{
				Op:       program.OpEq,
				Operands: []program.ExprID{tagID, meID},
			})
		} else {
			thisCondID = c.lowerExpr(me)
		}
		if i == 0 {
			condID = thisCondID
		} else {
			condID = c.addExpr(program.Expr{
				Op:       program.OpOr,
				Operands: []program.ExprID{condID, thisCondID},
			})
		}
	}
	return condID
}

// switchTagLocalName recovers the synthetic local name from a
// previously emitted OpLocalGet ExprID. The lowerer's expression
// table is append-only so reading c.exprs[tagRefID] gives back the
// Value (the local name) we baked in.
func switchTagLocalName(tagRefID program.ExprID, c *lowerCtx) string {
	if int(tagRefID) >= len(c.exprs) {
		return ""
	}
	return c.exprs[tagRefID].Value
}

// freshSwitchTagLocal generates a collision-free local name for the
// cached switch tag. Uses the AST token.Pos so multiple switches in
// the same handler don't collide. Reserved prefix `__y_e_switch_`.
func freshSwitchTagLocal(pos token.Pos) string {
	return "__y_e_switch_" + posToString(pos)
}

// caseClause is a small carrier used during the right-to-left
// chain build.
type caseClause struct {
	matchExprs []ast.Expr
	body       []ast.Stmt
}

// posToString turns a token.Pos into a tmp-local-safe suffix.
func posToString(pos token.Pos) string {
	if pos == token.NoPos {
		return "0"
	}
	n := int(pos)
	if n < 0 {
		n = -n
	}
	// Small dependency-free integer-to-string. Reuses the same shape
	// the LHS helpers use elsewhere in this package.
	var (
		buf [20]byte
		i   = len(buf)
	)
	if n == 0 {
		return "0"
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
