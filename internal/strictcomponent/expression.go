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
// The v0.42 contract adds one narrow extension: a `+` chain that concatenates
// string literals with props field selectors (ValidateConcatExpression
// documents the accepted shape). Operators outside that one exception,
// indexing, and calls need static Go type/method information that the
// map-backed file renderer does not retain, so they fail closed.
func ValidateServerExpression(source string) error {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return fmt.Errorf("invalid Go expression: %w", err)
	}
	if err := validate(expr, source); err != nil {
		return err
	}
	return nil
}

func validate(expr ast.Expr, source string) error {
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
		return validate(node.X, source)
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
		if node.Op != token.ADD {
			return fmt.Errorf("binary operator %q is not supported by the strict server renderer; only string concatenation with %q is renderable", node.Op, "+")
		}
		return validateConcatChain(node, source)
	case *ast.CallExpr:
		return fmt.Errorf("calls are not supported by the strict server renderer because typed Go methods are not retained in its props map")
	default:
		return fmt.Errorf("expression shape %T is not supported by the strict server renderer", expr)
	}
}

// validateConcatChain accepts a `+` chain whose flattened operands are each a
// string literal or a direct props field selector, with at least one of each.
// The renderer's applyFileBinaryOp takes the string branch whenever either
// side of `+` is a Go string (route/exprlower.go), so this is the one binary
// shape the file renderer and generated Go execute identically.
func validateConcatChain(node *ast.BinaryExpr, source string) error {
	hasStringLiteral := false
	hasPropsField := false
	for _, operand := range flattenAddChain(node) {
		text := operandText(operand, source)
		switch classifyConcatOperand(operand) {
		case concatOperandString:
			hasStringLiteral = true
		case concatOperandNonStringLiteral:
			return fmt.Errorf("\"+\" operand `%s` is not a string literal; the strict server renderer does not perform numeric addition", text)
		case concatOperandSelector:
			hasPropsField = true
		default:
			return fmt.Errorf("\"+\" operand `%s` is not renderable; strict concatenation accepts string literals and props field selectors only", text)
		}
	}
	if !hasStringLiteral {
		return fmt.Errorf("strict concatenation requires at least one string literal operand")
	}
	if !hasPropsField {
		return fmt.Errorf("strict concatenation requires at least one props field operand; fold literal-only chains by hand")
	}
	return nil
}

// concatOperandKind classifies one flattened `+` operand.
type concatOperandKind int

const (
	concatOperandInvalid concatOperandKind = iota
	concatOperandString
	concatOperandNonStringLiteral
	concatOperandSelector
)

func classifyConcatOperand(operand ast.Expr) concatOperandKind {
	switch node := unwrapParens(operand).(type) {
	case *ast.BasicLit:
		if node.Kind == token.STRING {
			if _, err := strconv.Unquote(node.Value); err != nil {
				return concatOperandInvalid
			}
			return concatOperandString
		}
		return concatOperandNonStringLiteral
	case *ast.SelectorExpr:
		if _, ok := directPropsField(node); ok {
			return concatOperandSelector
		}
		return concatOperandInvalid
	default:
		return concatOperandInvalid
	}
}

// flattenAddChain splits a left-associative `+` chain into its operands,
// unwrapping parentheses only at each chain link it walks through. A
// parenthesized right-hand operand that is itself a `+` expression is
// deliberately left intact as one operand — Go's left-associative parse
// already puts every top-level `+` on the chain's left spine, so a
// right-hand `+` only appears when the source wrote it in explicit
// parentheses, and it stays a single (invalid) operand for diagnostics that
// echo the exact source text back to the author.
func flattenAddChain(expr ast.Expr) []ast.Expr {
	if bin, ok := unwrapParens(expr).(*ast.BinaryExpr); ok && bin.Op == token.ADD {
		return append(flattenAddChain(bin.X), bin.Y)
	}
	return []ast.Expr{expr}
}

// unwrapParens strips every layer of parentheses around expr.
func unwrapParens(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

// operandText slices the exact source text of operand (parentheses
// stripped) out of the original source string. parser.ParseExpr builds its
// single-file token.FileSet at base 1, so a node's Pos/End are 1-based byte
// offsets into source with no other adjustment needed.
func operandText(operand ast.Expr, source string) string {
	node := unwrapParens(operand)
	start := int(node.Pos()) - 1
	end := int(node.End()) - 1
	if start < 0 || end > len(source) || start > end {
		return source
	}
	return source[start:end]
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

// ServerExpressionPropFields reports every direct props field selector
// (props.X) found anywhere in source, regardless of whether source's overall
// shape passes ValidateServerExpression. Read-tracking passes use this to see
// every props field an expression touches — not just the operands of one
// accepted top-level shape — because the grammar's external attribute
// scanner hands attribute-position expressions back as one opaque token with
// no nested CST, unlike element/text children (jsx_expression_container),
// whose nested Go sub-tree the CST-based read-tracking walk can see
// directly. Attribute-position expressions are exactly where every v0.42
// concatenation and <If cond> shape lives, so this closes that visibility
// gap generally instead of only for the two new shapes.
func ServerExpressionPropFields(source string) []string {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return nil
	}
	var fields []string
	seen := make(map[string]struct{})
	ast.Inspect(expr, func(n ast.Node) bool {
		selector, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if field, ok := directPropsField(selector); ok {
			if _, dup := seen[field]; !dup {
				seen[field] = struct{}{}
				fields = append(fields, field)
			}
		}
		return true
	})
	return fields
}

// ServerConcatPropFields reports, in operand order, the props fields read by
// a validated string-concatenation chain (see ValidateServerExpression). It
// returns ok=false when source's top-level shape (parentheses transparent)
// is not a `+` expression — callers use that to skip the concat-specific
// type pass for every other already-validated expression shape.
func ServerConcatPropFields(source string) ([]string, bool) {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return nil, false
	}
	bin, ok := unwrapParens(expr).(*ast.BinaryExpr)
	if !ok || bin.Op != token.ADD {
		return nil, false
	}
	var fields []string
	for _, operand := range flattenAddChain(bin) {
		selector, ok := unwrapParens(operand).(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if field, ok := directPropsField(selector); ok {
			fields = append(fields, field)
		}
	}
	return fields, true
}

// ValidateServerCondExpression accepts the two expression shapes a strict
// <If cond={...}> attribute may use: a bare props field selector, or that
// selector compared against the literal false with `==`. It reports the
// props field read and whether the comparison negates it. Parentheses are
// transparent at any level.
func ValidateServerCondExpression(source string) (field string, negated bool, err error) {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return "", false, fmt.Errorf("invalid Go expression: %w", err)
	}
	root := unwrapParens(expr)

	if bin, ok := root.(*ast.BinaryExpr); ok {
		if bin.Op == token.EQL {
			left := unwrapParens(bin.X)
			right := unwrapParens(bin.Y)
			if ident, ok := right.(*ast.Ident); ok && ident.Name == "true" {
				return "", false, fmt.Errorf("comparison \"== true\" is not supported in strict cond; write the field bare")
			}
			if selector, ok := left.(*ast.SelectorExpr); ok {
				if ident, ok := right.(*ast.Ident); ok && ident.Name == "false" {
					if field, ok := directPropsField(selector); ok {
						return field, true, nil
					}
				}
			}
		}
		return "", false, fmt.Errorf("strict cond must be a bool props field or a bool props field compared with \"== false\"; got %q", source)
	}

	if selector, ok := root.(*ast.SelectorExpr); ok {
		if field, ok := directPropsField(selector); ok {
			return field, false, nil
		}
	}
	return "", false, fmt.Errorf("strict cond must be a bool props field or a bool props field compared with \"== false\"; got %q", source)
}
