// Statement lowering: turns each ast.Stmt into one (or a small bundle
// of) opcode expressions chained through OpSeq.
//
// The translation deliberately mirrors Go semantics where possible
// without claiming full coverage. Constructs outside the supported
// subset emit Issue entries (so the build can fail with all problems
// at once) and produce an OpSeq-noop placeholder so subsequent
// statements keep lowering — this gives authors a complete picture of
// what needs the escape hatch instead of one-error-at-a-time iteration.

package golower

import (
	"fmt"
	"go/ast"
	"go/token"

	"m31labs.dev/gosx/island/program"
)

// lowerStmtList lowers each statement in order and returns the list of
// ExprIDs the caller (lowerFuncDecl, lowerForStmt body) should wrap in
// OpSeq. Empty lists are honored — they produce an empty OpSeq.
func (c *lowerCtx) lowerStmtList(stmts []ast.Stmt) []program.ExprID {
	out := make([]program.ExprID, 0, len(stmts))
	for _, s := range stmts {
		out = append(out, c.lowerStmt(s))
	}
	return out
}

// lowerStmt dispatches by statement kind. Returns a single ExprID
// (often an OpSeq wrapping multiple operations).
func (c *lowerCtx) lowerStmt(s ast.Stmt) program.ExprID {
	switch st := s.(type) {
	case *ast.AssignStmt:
		return c.lowerAssignStmt(st)
	case *ast.IfStmt:
		return c.lowerIfStmt(st)
	case *ast.ForStmt:
		return c.lowerForStmt(st)
	case *ast.RangeStmt:
		return c.lowerRangeStmt(st)
	case *ast.ReturnStmt:
		return c.lowerReturnStmt(st)
	case *ast.ExprStmt:
		return c.lowerExpr(st.X)
	case *ast.BlockStmt:
		return c.lowerBlockStmt(st)
	case *ast.DeclStmt:
		return c.lowerDeclStmt(st)
	case *ast.IncDecStmt:
		return c.lowerIncDecStmt(st)
	case *ast.BranchStmt:
		return c.lowerBranchStmt(st)
	case *ast.SwitchStmt:
		return c.lowerSwitchStmt(st)
	case *ast.EmptyStmt:
		return c.addExpr(program.Expr{Op: program.OpSeq})
	default:
		c.addIssue(s, fmt.Sprintf("unsupported statement %T", s), escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpSeq})
	}
}

// lowerAssignStmt handles `x = expr` (`=`) and `x := expr` (`:=`),
// plus the compound-assign forms (`+=`, `-=`, ...). Multi-value
// assigns dispatch to lowerMultiAssign (Slice Y.B) for the comma-ok
// map idiom and parallel assignment; multi-return function calls
// remain Y.D territory and surface as a clear diagnostic from there.
// LHS selector / indexed-set forms (e.g. `node.X = ...`, `m[k] = ...`)
// dispatch to lowerSelectorOrIndexAssign (Slice Y.C).
func (c *lowerCtx) lowerAssignStmt(s *ast.AssignStmt) program.ExprID {
	if len(s.Lhs) > 1 {
		return c.lowerMultiAssign(s)
	}
	if len(s.Rhs) != 1 {
		c.addIssue(s, "multi-value assignment is not supported", escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpSeq})
	}
	// Slice Y.C: route selector / index LHS forms before the bare-ident
	// check so `node.X = ...` and `m[k] = ...` lower through the new
	// OpFieldSet / OpIndexSet path instead of falling through to the
	// "left-hand side must be a simple identifier" diagnostic.
	if isLHSSelectorOrIndex(s.Lhs[0]) {
		return c.lowerSelectorOrIndexAssign(s)
	}
	lhsName, ok := identName(s.Lhs[0])
	if !ok {
		c.addIssue(s, "left-hand side must be a simple identifier", escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpSeq})
	}
	valueID := c.lowerExpr(s.Rhs[0])

	switch s.Tok {
	case token.DEFINE:
		// `x := expr` — declare the local, then OpAssign.
		declID := c.addExpr(program.Expr{Op: program.OpLocalDecl, Value: lhsName})
		assignID := c.addExpr(program.Expr{Op: program.OpAssign, Value: lhsName, Operands: []program.ExprID{valueID}})
		return c.addExpr(program.Expr{Op: program.OpSeq, Operands: []program.ExprID{declID, assignID}})
	case token.ASSIGN:
		return c.addExpr(program.Expr{Op: program.OpAssign, Value: lhsName, Operands: []program.ExprID{valueID}})
	case token.ADD_ASSIGN, token.SUB_ASSIGN, token.MUL_ASSIGN, token.QUO_ASSIGN, token.REM_ASSIGN:
		// `x += expr` → `x = x op expr` lowered as
		//   OpAssign x  Operands=[OpBinop OpLocalGet(x), valueID]
		op := compoundOpcode(s.Tok)
		readID := c.addExpr(program.Expr{Op: program.OpLocalGet, Value: lhsName})
		binID := c.addExpr(program.Expr{Op: op, Operands: []program.ExprID{readID, valueID}})
		return c.addExpr(program.Expr{Op: program.OpAssign, Value: lhsName, Operands: []program.ExprID{binID}})
	default:
		c.addIssue(s, fmt.Sprintf("assignment operator %s is not supported", s.Tok), escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpSeq})
	}
}

// compoundOpcode maps `+=`, `-=`, etc. to the underlying arithmetic
// opcode. Callers verify the token first.
func compoundOpcode(tok token.Token) program.OpCode {
	switch tok {
	case token.ADD_ASSIGN:
		return program.OpAdd
	case token.SUB_ASSIGN:
		return program.OpSub
	case token.MUL_ASSIGN:
		return program.OpMul
	case token.QUO_ASSIGN:
		return program.OpDiv
	case token.REM_ASSIGN:
		return program.OpMod
	}
	return program.OpAdd // defensive default; caller already filtered
}

// lowerIfStmt turns Go's if/else into an OpCond expression. Else-if
// chains nest naturally because the else branch is itself an *ast.IfStmt.
//
// The "init" clause (`if x := f(); cond { ... }`) is lowered as a
// preceding OpSeq member; the init's local is visible to both branches
// because frames are flat (see frame.go's design note).
func (c *lowerCtx) lowerIfStmt(s *ast.IfStmt) program.ExprID {
	var seqOps []program.ExprID
	if s.Init != nil {
		seqOps = append(seqOps, c.lowerStmt(s.Init))
	}
	condID := c.lowerExpr(s.Cond)
	thenID := c.lowerBlockStmt(s.Body)

	var elseID program.ExprID
	if s.Else != nil {
		elseID = c.lowerStmt(s.Else)
	} else {
		// OpCond requires three operands; use a noop OpSeq as the else.
		elseID = c.addExpr(program.Expr{Op: program.OpSeq})
	}

	condExpr := c.addExpr(program.Expr{
		Op:       program.OpCond,
		Operands: []program.ExprID{condID, thenID, elseID},
	})
	if len(seqOps) == 0 {
		return condExpr
	}
	seqOps = append(seqOps, condExpr)
	return c.addExpr(program.Expr{Op: program.OpSeq, Operands: seqOps})
}

// lowerForStmt handles 3-clause for loops. The VM's OpFor opcode (added
// in Slice X.C alongside the lowerer) carries init / cond / post / body
// as four operand slots with built-in iteration-cap safety.
//
// `for {}` (infinite) and `for cond {}` (cond-only) lower the same way
// — missing init/post are noop OpSeq, missing cond is OpLitBool(true).
func (c *lowerCtx) lowerForStmt(s *ast.ForStmt) program.ExprID {
	initID := c.lowerOptionalStmt(s.Init)
	condID := c.lowerOptionalCond(s.Cond)
	postID := c.lowerOptionalStmt(s.Post)
	bodyID := c.lowerBlockStmt(s.Body)
	return c.addExpr(program.Expr{
		Op:       program.OpFor,
		Operands: []program.ExprID{initID, condID, postID, bodyID},
	})
}

// lowerRangeStmt lowers `for k, v := range coll { body }` to OpForRange.
// The VM injects "_index" / "_item" / "_key" props inside the body's
// frame; the lowerer rewrites the user's key/value identifiers to
// reference those props by emitting a leading OpLocalDecl + OpAssign
// pair that pulls from the synthetic props.
//
// The user's first range variable (Go's `k` in `for k, v := range`)
// binds to "_key" instead of "_index" — for slices the VM sets
// _key = _index (an IntVal of the iteration counter), so slice code
// like `for i, v := range s` continues to behave identically; for
// maps _key holds the StringVal map key, which is what graph
// surfaces like `for id, p := range gPos` actually want (Slice Y.B).
func (c *lowerCtx) lowerRangeStmt(s *ast.RangeStmt) program.ExprID {
	collID := c.lowerExpr(s.X)

	// Pre-emit binding statements: if the user wrote `for k, v := range`,
	// alias their local names to the VM's _key / _item props before
	// each body iteration.
	var bodyOps []program.ExprID
	if name, ok := identName(s.Key); ok && name != "_" {
		bodyOps = append(bodyOps, c.bindRangeAlias(name, "_key"))
	}
	if s.Value != nil {
		if name, ok := identName(s.Value); ok && name != "_" {
			bodyOps = append(bodyOps, c.bindRangeAlias(name, "_item"))
		}
	}
	bodyOps = append(bodyOps, c.lowerStmtList(s.Body.List)...)
	bodyID := c.addExpr(program.Expr{Op: program.OpSeq, Operands: bodyOps})

	return c.addExpr(program.Expr{
		Op:       program.OpForRange,
		Operands: []program.ExprID{collID, bodyID},
	})
}

// bindRangeAlias emits the two-instruction prelude that binds a user
// local (e.g. `i`) to a runtime prop (e.g. `_index`). Lowering uses
// this for both the index and value sides of a range loop.
func (c *lowerCtx) bindRangeAlias(localName, propName string) program.ExprID {
	declID := c.addExpr(program.Expr{Op: program.OpLocalDecl, Value: localName})
	readID := c.addExpr(program.Expr{Op: program.OpPropGet, Value: propName})
	assignID := c.addExpr(program.Expr{Op: program.OpAssign, Value: localName, Operands: []program.ExprID{readID}})
	return c.addExpr(program.Expr{Op: program.OpSeq, Operands: []program.ExprID{declID, assignID}})
}

// lowerOptionalStmt lowers s if non-nil, otherwise returns a noop OpSeq.
// Used for the optional init / post clauses of for loops.
func (c *lowerCtx) lowerOptionalStmt(s ast.Stmt) program.ExprID {
	if s == nil {
		return c.addExpr(program.Expr{Op: program.OpSeq})
	}
	return c.lowerStmt(s)
}

// lowerOptionalCond lowers a for-loop condition. A nil condition
// (Go's `for { }` infinite loop) becomes OpLitBool(true), letting the
// VM's iteration cap be the only termination condition — visible as a
// loop_cap_exceeded diagnostic if the body never breaks out.
func (c *lowerCtx) lowerOptionalCond(e ast.Expr) program.ExprID {
	if e == nil {
		return c.addExpr(program.Expr{Op: program.OpLitBool, Value: "true", Type: program.TypeBool})
	}
	return c.lowerExpr(e)
}

// lowerReturnStmt emits OpReturn so the enclosing OpSeq / OpFor /
// OpForRange unwinds back to the handler's EvalWithFrame boundary.
// `return` with no value lowers to a bare OpReturn with no operand.
//
// Slice Y.D extends this for multi-value returns: `return a, b` in a
// function declared with 2+ return values lowers to an OpReturn whose
// payload is an OpComposite ObjectVal carrier keyed `__ret_<i>`. The
// caller's lowerMultiAssign reads each `__ret_<i>` field via OpIndex
// — same pattern as Slice Y.B's OpMapLookup carrier.
func (c *lowerCtx) lowerReturnStmt(s *ast.ReturnStmt) program.ExprID {
	switch len(s.Results) {
	case 0:
		return c.addExpr(program.Expr{Op: program.OpReturn})
	case 1:
		valueID := c.lowerExpr(s.Results[0])
		return c.addExpr(program.Expr{Op: program.OpReturn, Operands: []program.ExprID{valueID}})
	default:
		// Multi-value return: build an OpComposite carrier of kind
		// "map" with __ret_<i> keys. This reuses Y.A's compositeMap
		// path without expanding the Value model. The caller binds
		// each LHS via OpIndex against the carrier (Y.B's bindFromTmp
		// helper, called via lowerMultiAssign).
		carrierID := c.buildReturnCarrier(s.Results)
		return c.addExpr(program.Expr{Op: program.OpReturn, Operands: []program.ExprID{carrierID}})
	}
}

// buildReturnCarrier emits an OpComposite ObjectVal of kind "map"
// whose Fields are keyed `__ret_0`, `__ret_1`, ... — one per return
// value, in declaration order. The Slice Y.D multi-return contract
// expects the OpIndirectCall caller to read each slot via OpIndex on
// the same key scheme.
func (c *lowerCtx) buildReturnCarrier(results []ast.Expr) program.ExprID {
	operands := make([]program.ExprID, 0, len(results)*2)
	for i, res := range results {
		keyID := c.addExpr(program.Expr{
			Op:    program.OpLitString,
			Value: returnKey(i),
			Type:  program.TypeString,
		})
		valueID := c.lowerExpr(res)
		operands = append(operands, keyID, valueID)
	}
	return c.addExpr(program.Expr{
		Op:       program.OpComposite,
		Value:    "map",
		Operands: operands,
	})
}

// returnKey is the Y.D multi-return carrier's key scheme. Reserved
// prefix (`__ret_`) prevents collision with user identifiers and
// matches Y.B's `__y_b_*` / Y.D's `__y_d_*` namespacing convention.
func returnKey(i int) string {
	return fmt.Sprintf("__ret_%d", i)
}

// lowerBlockStmt wraps a block's statements in OpSeq. Empty blocks
// produce a zero-length OpSeq (which the VM handles as a noop).
func (c *lowerCtx) lowerBlockStmt(s *ast.BlockStmt) program.ExprID {
	if s == nil || len(s.List) == 0 {
		return c.addExpr(program.Expr{Op: program.OpSeq})
	}
	ops := c.lowerStmtList(s.List)
	return c.addExpr(program.Expr{Op: program.OpSeq, Operands: ops})
}

// lowerDeclStmt handles `var x int` and `var x = expr` inside a function
// body. The local-declaration opcode reserves a slot; the optional
// initializer is appended as an OpAssign.
func (c *lowerCtx) lowerDeclStmt(s *ast.DeclStmt) program.ExprID {
	gen, ok := s.Decl.(*ast.GenDecl)
	if !ok || (gen.Tok != token.VAR && gen.Tok != token.CONST) {
		c.addIssue(s, "only var/const declarations are supported inside function bodies", escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpSeq})
	}
	var ops []program.ExprID
	for _, spec := range gen.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, name := range vs.Names {
			if name == nil || name.Name == "_" {
				continue
			}
			ops = append(ops, c.addExpr(program.Expr{Op: program.OpLocalDecl, Value: name.Name}))
			if i < len(vs.Values) {
				valueID := c.lowerExpr(vs.Values[i])
				ops = append(ops, c.addExpr(program.Expr{Op: program.OpAssign, Value: name.Name, Operands: []program.ExprID{valueID}}))
			}
		}
	}
	return c.addExpr(program.Expr{Op: program.OpSeq, Operands: ops})
}

// lowerIncDecStmt lowers `x++` / `x--` as a self-add/sub of 1.
func (c *lowerCtx) lowerIncDecStmt(s *ast.IncDecStmt) program.ExprID {
	name, ok := identName(s.X)
	if !ok {
		c.addIssue(s, "increment/decrement target must be a simple identifier", escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpSeq})
	}
	one := c.addExpr(program.Expr{Op: program.OpLitInt, Value: "1", Type: program.TypeInt})
	read := c.addExpr(program.Expr{Op: program.OpLocalGet, Value: name})
	op := program.OpAdd
	if s.Tok == token.DEC {
		op = program.OpSub
	}
	add := c.addExpr(program.Expr{Op: op, Operands: []program.ExprID{read, one}})
	return c.addExpr(program.Expr{Op: program.OpAssign, Value: name, Operands: []program.ExprID{add}})
}

// lowerBranchStmt lowers break/continue to their VM opcodes. Labeled
// branches, goto, and fallthrough remain unsupported — the supported
// subset is bounded enough that any need for these is a capability gap.
func (c *lowerCtx) lowerBranchStmt(s *ast.BranchStmt) program.ExprID {
	if s.Label != nil {
		c.addIssue(s, "labeled break/continue is not supported", escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpSeq})
	}
	switch s.Tok {
	case token.BREAK:
		return c.addExpr(program.Expr{Op: program.OpBreak})
	case token.CONTINUE:
		return c.addExpr(program.Expr{Op: program.OpContinue})
	default:
		c.addIssue(s, fmt.Sprintf("branch statement %s is not supported", s.Tok), escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpSeq})
	}
}

// identName extracts a simple identifier name from an LHS expression.
// Selector expressions (struct fields, package vars) and index
// expressions are rejected — those would need OpFieldSet / OpIndexSet
// opcodes that aren't in the supported set.
func identName(e ast.Expr) (string, bool) {
	id, ok := e.(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}
