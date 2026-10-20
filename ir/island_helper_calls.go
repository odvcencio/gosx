package ir

import (
	"fmt"

	"m31labs.dev/gosx/island/program"
)

// linkHandlerCalls reuses authored handler bodies as callable functions. Only
// called helpers need descriptors; their expression bodies are not duplicated.
// Void helpers can be statement effects, never render, initializer, condition,
// or argument values. The VM already isolates returns and bounds call depth.
func (l *islandLowerer) linkHandlerCalls(scope *ComponentScope) error {
	p := l.dst
	var hasCalls bool
	for _, expr := range p.Exprs {
		if expr.Op == program.OpIndirectCall {
			hasCalls = true
			break
		}
	}
	if !hasCalls {
		return nil
	}

	statements := make(map[program.ExprID]bool)
	var markStatement func(program.ExprID)
	markStatement = func(id program.ExprID) {
		if statements[id] || int(id) >= len(p.Exprs) {
			return
		}
		statements[id] = true
		expr := p.Exprs[id]
		switch expr.Op {
		case program.OpSeq:
			for _, operand := range expr.Operands {
				markStatement(operand)
			}
		case program.OpCond:
			// The predicate consumes a value; only its branches are statements.
			if len(expr.Operands) == 3 {
				markStatement(expr.Operands[1])
				markStatement(expr.Operands[2])
			}
		}
	}
	declared := make(map[string]bool, len(p.Handlers))
	for _, handler := range p.Handlers {
		declared[handler.Name] = true
		for _, root := range handler.Body {
			markStatement(root)
		}
	}
	called := make(map[string]bool)
	for index, expr := range p.Exprs {
		if expr.Op != program.OpIndirectCall {
			continue
		}
		if !declared[expr.Value] {
			return fmt.Errorf("island helper %s has no declared handler body", expr.Value)
		}
		if !statements[program.ExprID(index)] {
			return fmt.Errorf("island helper %s can only be called as a handler statement", expr.Value)
		}
		if len(expr.Operands) != 0 {
			return fmt.Errorf("island helper %s requires a func() call without arguments", expr.Value)
		}
		called[expr.Value] = true
	}
	if scope != nil {
		for _, handler := range scope.Handlers {
			if called[handler.Name] && (handler.HasParameters || handler.HasResults) {
				return fmt.Errorf("island helper %s must declare func() without parameters or results", handler.Name)
			}
		}
	}
	for _, handler := range p.Handlers {
		if called[handler.Name] {
			p.Funcs = append(p.Funcs, program.FuncDef{Name: handler.Name, Body: handler.Body})
		}
	}
	return nil
}
