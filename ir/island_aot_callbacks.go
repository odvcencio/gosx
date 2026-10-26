//go:build !tinygo && !js

package ir

import (
	"fmt"
	"go/ast"
	"go/types"

	"m31labs.dev/gosx/island/program"
)

func (e *aotEvidence) validateCallbackSource(owner Component, attr Attr, call bool) error {
	checked, err := e.sourceNode(islandExprOrigin{span: attr.Span})
	if err != nil {
		return err
	}
	checked = callbackSourceExpression(checked)
	signature, ok := e.checked.info.TypeOf(checked).(*types.Signature)
	if !ok || signature.Params().Len() != 0 || signature.Results().Len() != 0 || signature.Variadic() {
		return fmt.Errorf("evidence_binding_mismatch: callback signature")
	}
	exprs, root, err := ParseExpr(attr.Expr, nil)
	if err != nil {
		return err
	}
	if field, forwarded := directPropsField(exprs, root); forwarded {
		selector, ok := checked.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != field || !islandCallbackType(owner.PropsFields[field]) {
			return fmt.Errorf("evidence_binding_mismatch: callback field")
		}
		receiver, ok := callbackSourceExpression(selector.X).(*ast.Ident)
		if !ok {
			return fmt.Errorf("evidence_binding_mismatch: callback receiver")
		}
		var props types.Object
		if function := e.checked.functions[owner.Name]; function != nil && function.Type.Params != nil {
			for _, parameter := range function.Type.Params.List {
				for _, name := range parameter.Names {
					if name.Name == owner.PropsName {
						props = e.checked.info.Defs[name]
					}
				}
			}
		}
		if props == nil || e.checked.info.Uses[receiver] != props {
			return fmt.Errorf("evidence_binding_mismatch: callback props object")
		}
	} else {
		name := exprs[root].Value
		identifier, ok := checked.(*ast.Ident)
		if exprs[root].Op != program.OpPropGet || !ok || owner.Name != e.component.Name || e.handlers[name] == nil || e.checked.info.Uses[identifier] != e.handlerObjects[name] {
			return fmt.Errorf("evidence_binding_mismatch: callback handler object")
		}
	}
	if call && !e.callbackSlotMatches(checked, attr.Name) {
		return fmt.Errorf("evidence_binding_mismatch: callback prop assignment")
	}
	return nil
}

func (e *aotEvidence) callbackSlotMatches(value ast.Expr, name string) bool {
	if e.callbackSlots == nil {
		e.callbackSlots = make(map[ast.Expr]string)
		ast.Inspect(e.checked.file, func(node ast.Node) bool {
			if assignment, ok := node.(*ast.KeyValueExpr); ok {
				if field, ok := assignment.Key.(*ast.Ident); ok {
					e.callbackSlots[callbackSourceExpression(assignment.Value)] = field.Name
				}
			}
			return true
		})
	}
	return e.callbackSlots[value] == name
}

func callbackSourceExpression(expr ast.Expr) ast.Expr {
	for {
		parenthesized, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = parenthesized.X
	}
}
