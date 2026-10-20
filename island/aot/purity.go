package aot

import "m31labs.dev/gosx/island/program"

func effectRules(u Unit) *rejection {
	p, c := u.Program, u.Contract
	computed := map[string]program.ExprID{}
	for _, s := range p.Computeds {
		computed[s.Name] = s.Expr
	}
	pure, done := make([]bool, len(p.Exprs)), make([]bool, len(p.Exprs))
	var infer func(program.ExprID) bool
	infer = func(id program.ExprID) bool {
		if done[id] {
			return pure[id]
		}
		e := p.Exprs[id]
		value := e.Op != program.OpSignalSet
		for _, child := range e.Operands {
			value = infer(child) && value
		}
		if body, ok := computed[e.Value]; e.Op == program.OpSignalGet && ok {
			value = infer(body) && value
		}
		pure[id], done[id] = value, true
		return value
	}
	for i := range p.Exprs {
		if infer(program.ExprID(i)) != c.Expressions[i].Pure {
			return reject("purity", "expressions", i)
		}
	}
	for i, e := range p.Exprs {
		if (e.Op == program.OpAnd || e.Op == program.OpOr) && (!pure[e.Operands[0]] || !pure[e.Operands[1]]) || e.Op == program.OpCond && !pure[e.Operands[0]] {
			return reject("impure_operand", "expressions", i)
		}
	}
	// Aggregate prefixes have no runtime value and may only feed the base of a
	// proved selector. Writes/sequences may only be handler statements.
	used := make([]bool, len(p.Exprs))
	check := func(id program.ExprID, statement, handler bool) *rejection {
		e := p.Exprs[id]
		if c.Expressions[id].Kind == SelectorPath {
			return reject("selector_escape", "expressions", int(id))
		}
		if (e.Op == program.OpSignalSet || e.Op == program.OpSeq) && !statement {
			return reject("effect_scope", "expressions", int(id))
		}
		if !handler && (!pure[id] || e.Op == program.OpEventGet) {
			return reject("effect_scope", "expressions", int(id))
		}
		return nil
	}
	var walk func(program.ExprID, bool) *rejection
	walkDone := [2][]bool{make([]bool, len(p.Exprs)), make([]bool, len(p.Exprs))}
	walk = func(id program.ExprID, handler bool) *rejection {
		used[id] = used[id] || handler
		context := 0
		if handler {
			context = 1
		}
		if walkDone[context][id] {
			return nil
		}
		walkDone[context][id] = true
		e := p.Exprs[id]
		for j, child := range e.Operands {
			if e.Op == program.OpIndex && j == 0 {
				if failure := walk(child, handler); failure != nil {
					return failure
				}
				continue
			}
			if failure := check(child, handler && e.Op == program.OpSeq, handler); failure != nil {
				return failure
			}
			if failure := walk(child, handler); failure != nil {
				return failure
			}
		}
		if body, ok := computed[e.Value]; e.Op == program.OpSignalGet && ok {
			if failure := check(body, false, false); failure != nil {
				return failure
			}
			return walk(body, false)
		}
		return nil
	}
	root := func(id program.ExprID, handler bool) *rejection {
		if failure := check(id, handler, handler); failure != nil {
			return failure
		}
		return walk(id, handler)
	}
	for _, s := range p.Signals {
		if failure := root(s.Init, false); failure != nil {
			return failure
		}
	}
	for _, s := range p.Computeds {
		if failure := root(s.Expr, false); failure != nil {
			return failure
		}
	}
	for _, n := range p.Nodes {
		if n.Kind == program.NodeExpr {
			if failure := root(n.Expr, false); failure != nil {
				return failure
			}
		}
		for _, attr := range n.Attrs {
			if attr.Kind == program.AttrExpr {
				if failure := root(attr.Expr, false); failure != nil {
					return failure
				}
			}
		}
	}
	for _, h := range p.Handlers {
		for _, id := range h.Body {
			if failure := root(id, true); failure != nil {
				return failure
			}
		}
	}
	prefixUsed := make([]bool, len(p.Exprs))
	for i, e := range p.Exprs {
		if !used[i] && (e.Op == program.OpSignalSet || e.Op == program.OpSeq || e.Op == program.OpEventGet) {
			return reject("effect_scope", "expressions", i)
		}
		for j, id := range e.Operands {
			if c.Expressions[id].Kind == SelectorPath && (e.Op != program.OpIndex || j != 0) {
				return reject("selector_escape", "expressions", int(id))
			}
			if c.Expressions[id].Kind == SelectorPath {
				prefixUsed[id] = true
			}
			if (p.Exprs[id].Op == program.OpSignalSet || p.Exprs[id].Op == program.OpSeq) && e.Op != program.OpSeq {
				return reject("effect_scope", "expressions", int(id))
			}
		}
	}
	for i, proof := range c.Expressions {
		if proof.Kind == SelectorPath && !prefixUsed[i] {
			return reject("selector_escape", "expressions", i)
		}
	}
	return nil
}
