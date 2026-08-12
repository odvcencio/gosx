// Package strictcomponent contains the shared fail-closed contract for the
// strict GoSX component spelling.
package strictcomponent

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
)

var (
	strictDecimalInteger = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	strictDecimalFloat   = regexp.MustCompile(`^(([0-9]+\.[0-9]*|\.[0-9]+)([eE][+-]?[0-9]+)?|[0-9]+[eE][+-]?[0-9]+)$`)
)

// ValidateServerExpression accepts exactly the expression shapes implemented
// by the file renderer for strict server components. The v0.39 contract is
// intentionally small: literals and one direct props field (with parentheses).
// Operators, indexing, and calls need static Go type/method information that
// the map-backed file renderer does not retain, so they fail closed.
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
		return validateLiteral(node)
	case *ast.Ident:
		switch node.Name {
		case "true", "false":
			return nil
		case "nil":
			return fmt.Errorf("nil is not supported because GoSX expression and file renderers serialize it differently")
		case "props":
			return fmt.Errorf("bare props is not supported; select a props field")
		default:
			return fmt.Errorf("identifier %q is not available to the strict server renderer", node.Name)
		}
	case *ast.ParenExpr:
		return validate(node.X)
	case *ast.SelectorExpr:
		if _, ok := directPropsField(node); !ok {
			return fmt.Errorf("selector must be one field directly on props; nested selector chains cannot preserve Go nil-pointer behavior")
		}
		return nil
	case *ast.IndexExpr:
		return fmt.Errorf("index expressions are not supported by the strict server renderer because out-of-range behavior differs from Go")
	case *ast.UnaryExpr:
		return fmt.Errorf("unary operator %q is not supported by the strict server renderer because its dynamic coercion cannot preserve Go types", node.Op)
	case *ast.BinaryExpr:
		return fmt.Errorf("binary operator %q is not supported by the strict server renderer because its dynamic coercion cannot preserve Go types", node.Op)
	case *ast.CallExpr:
		return fmt.Errorf("calls are not supported by the strict server renderer because typed Go methods are not retained in its props map")
	default:
		return fmt.Errorf("expression shape %T is not supported by the strict server renderer", expr)
	}
}

func validateLiteral(literal *ast.BasicLit) error {
	switch literal.Kind {
	case token.STRING:
		if _, err := strconv.Unquote(literal.Value); err != nil {
			return fmt.Errorf("invalid string literal: %w", err)
		}
		return nil
	case token.INT:
		if !strictDecimalInteger.MatchString(literal.Value) {
			return fmt.Errorf("integer literal %q is not supported; use an ungrouped base-10 literal", literal.Value)
		}
		if _, err := strconv.ParseInt(literal.Value, 10, 64); err != nil {
			return fmt.Errorf("integer literal %q is outside the strict renderer range", literal.Value)
		}
		return nil
	case token.FLOAT:
		if !strictDecimalFloat.MatchString(literal.Value) {
			return fmt.Errorf("float literal %q is not supported; use an ungrouped decimal literal", literal.Value)
		}
		if _, err := strconv.ParseFloat(literal.Value, 64); err != nil {
			return fmt.Errorf("float literal %q is outside the strict renderer range", literal.Value)
		}
		return nil
	case token.CHAR:
		return fmt.Errorf("character literals are not supported because the renderer treats them as strings rather than Go runes")
	case token.IMAG:
		return fmt.Errorf("imaginary literals are not supported by the strict server renderer")
	default:
		return fmt.Errorf("literal kind %s is not supported by the strict server renderer", literal.Kind)
	}
}

// ServerPropField reports the single props field read by a validated strict
// server expression. Parentheses around either the selector or props itself do
// not change the result.
func ServerPropField(source string) (string, bool) {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return "", false
	}
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			break
		}
		expr = paren.X
	}
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	return directPropsField(selector)
}

func directPropsField(selector *ast.SelectorExpr) (string, bool) {
	var receiver ast.Expr = selector.X
	for {
		paren, ok := receiver.(*ast.ParenExpr)
		if !ok {
			break
		}
		receiver = paren.X
	}
	ident, ok := receiver.(*ast.Ident)
	if !ok || ident.Name != "props" || selector.Sel == nil || selector.Sel.Name == "" {
		return "", false
	}
	return selector.Sel.Name, true
}
