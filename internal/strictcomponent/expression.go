// Package strictcomponent contains the shared fail-closed contract for the
// strict GoSX component spelling.
package strictcomponent

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

// ValidateServerExpression accepts exactly the expression shapes implemented
// by the file renderer for strict server components. Calls must be rooted in
// props (for example props.Formatter(props.Value)); free helpers and imported
// package calls type-check as Go but cannot be executed by the IR renderer.
func ValidateServerExpression(source string) error {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return fmt.Errorf("invalid Go expression: %w", err)
	}
	if err := validate(expr); err != nil {
		return err
	}
	return nil
}

func validate(expr ast.Expr) error {
	switch node := expr.(type) {
	case *ast.BasicLit:
		return nil
	case *ast.Ident:
		switch node.Name {
		case "props", "true", "false", "nil":
			return nil
		default:
			return fmt.Errorf("identifier %q is not available to the strict server renderer", node.Name)
		}
	case *ast.ParenExpr:
		return validate(node.X)
	case *ast.SelectorExpr:
		return validate(node.X)
	case *ast.IndexExpr:
		if err := validate(node.X); err != nil {
			return err
		}
		return validate(node.Index)
	case *ast.UnaryExpr:
		switch node.Op {
		case token.ADD, token.SUB, token.NOT:
			return validate(node.X)
		default:
			return fmt.Errorf("operator %q is not supported by the strict server renderer", node.Op)
		}
	case *ast.BinaryExpr:
		switch node.Op {
		case token.ADD, token.SUB, token.MUL, token.QUO, token.REM,
			token.LAND, token.LOR, token.EQL, token.NEQ,
			token.LSS, token.LEQ, token.GTR, token.GEQ:
		default:
			return fmt.Errorf("operator %q is not supported by the strict server renderer", node.Op)
		}
		if err := validate(node.X); err != nil {
			return err
		}
		return validate(node.Y)
	case *ast.CallExpr:
		if !rootedInProps(node.Fun) {
			return fmt.Errorf("call target must be rooted in props; helper and imported calls are not executed by the strict server renderer")
		}
		if err := validate(node.Fun); err != nil {
			return err
		}
		for _, arg := range node.Args {
			if err := validate(arg); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("expression shape %T is not supported by the strict server renderer", expr)
	}
}

func rootedInProps(expr ast.Expr) bool {
	switch node := expr.(type) {
	case *ast.Ident:
		return node.Name == "props"
	case *ast.SelectorExpr:
		return rootedInProps(node.X)
	case *ast.IndexExpr:
		return rootedInProps(node.X)
	case *ast.ParenExpr:
		return rootedInProps(node.X)
	default:
		return false
	}
}
