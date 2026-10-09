//go:build !tinygo

package ir

import (
	"fmt"
	"go/constant"

	"m31labs.dev/gosx/island/aot"
	"m31labs.dev/gosx/island/program"
)

func aotScalar(kind aot.ScalarKind) bool {
	return kind == aot.Int || kind == aot.Int32 || kind == aot.Bool || kind == aot.String
}

// Every operand consumes a value, except the receiver of a static selector.
// Sequence operands may also be completed effects, whose result is AnyZero.
func aotScalarOperands(e program.Expr, args []aot.ScalarKind) error {
	for i, kind := range args {
		if aotScalar(kind) || e.Op == program.OpSeq && kind == aot.AnyZero {
			continue
		}
		if kind == aot.SelectorPath && e.Op == program.OpIndex && i == 0 && len(args) == 2 {
			continue // Go selections separately prove the static selector key.
		}
		return fmt.Errorf("operand %d consumes a non-scalar value", i)
	}
	return nil
}

func aotValueFits(kind aot.ScalarKind, value constant.Value, target aot.ScalarKind) bool {
	return aotScalar(kind) && aotScalar(target) && kind == target && (value == nil || kind != aot.Int && kind != aot.Int32 || aotConstantFits(value, kind))
}

// Roots have no expression parent, so operand checks cannot protect them.
// Check every serialized consumer before producing the contract or digest.
func aotScalarRoots(p *program.Program, kinds []aot.ScalarKind, constants []constant.Value, states map[string]aot.ScalarKind) error {
	check := func(id program.ExprID, effect bool, target aot.ScalarKind) error {
		if int(id) >= len(kinds) || !aotScalar(kinds[id]) && !(effect && kinds[id] == aot.AnyZero) {
			return fmt.Errorf("root expression %d consumes a non-scalar value", id)
		}
		if target != "" && !aotValueFits(kinds[id], constants[id], target) {
			return fmt.Errorf("root expression %d does not match its declared scalar kind", id)
		}
		return nil
	}
	for _, node := range p.Nodes {
		switch node.Kind {
		case program.NodeElement, program.NodeText, program.NodeFragment:
		case program.NodeExpr, program.NodeForEach, program.NodeConditional:
			if err := check(node.Expr, false, ""); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown node kind: %d", node.Kind)
		}
		for _, attr := range node.Attrs {
			switch attr.Kind {
			case program.AttrStatic, program.AttrBool, program.AttrEvent:
			case program.AttrExpr:
				if err := check(attr.Expr, false, ""); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unknown attr kind: %d", attr.Kind)
			}
		}
	}
	for _, sig := range p.Signals {
		if err := check(sig.Init, false, states[sig.Name]); err != nil {
			return err
		}
	}
	for _, computed := range p.Computeds {
		if err := check(computed.Expr, false, states[computed.Name]); err != nil {
			return err
		}
	}
	for _, handler := range p.Handlers {
		for _, id := range handler.Body {
			if err := check(id, true, ""); err != nil {
				return err
			}
			if kinds[id] != aot.AnyZero {
				return fmt.Errorf("handler expression %d is not a supported Go effect statement", id)
			}
		}
	}
	for _, fn := range p.Funcs {
		for _, id := range fn.Body {
			if err := check(id, true, ""); err != nil {
				return err
			}
		}
	}
	return nil
}
