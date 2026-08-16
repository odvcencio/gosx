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
	"strings"
)

var (
	strictDecimalInteger = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	strictDecimalFloat   = regexp.MustCompile(`^(([0-9]+\.[0-9]*|\.[0-9]+)([eE][+-]?[0-9]+)?|[0-9]+[eE][+-]?[0-9]+)$`)
)

// ValidateServerExpression accepts exactly the expression shapes implemented
// by the file renderer for strict server components. The v0.39 contract is
// intentionally small: literals and one direct props field (with parentheses).
// v0.42 added a `+` chain that concatenates string literals with props field
// selectors (validateConcatChain documents the accepted shape). This change
// widens every props selector position (a bare read, a concat operand, an
// <If cond>) to accept a field chain rooted at props up to three fields deep
// (see ServerPropPath) instead of exactly one field. This function places no
// upper bound on chain length itself: it is schema-blind, so it cannot know
// where three hops actually lands against a given props struct. The lowerer
// (ir/lower.go) resolves each accepted path against the same-file struct
// schema and reports the three-hop cap there, with full component context.
// Operators outside the one `+` exception, indexing, and calls need static
// Go type/method information that the map-backed file renderer does not
// retain, so they fail closed.
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
		if _, ok := propsSelectorPath(node); !ok {
			return fmt.Errorf("selector must be a field chain rooted at props, with every step a plain field access; anything else cannot preserve Go nil-pointer behavior")
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
		if _, ok := propsSelectorPath(node); ok {
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

// ServerPropPath reports the ordered field path of a selector chain rooted
// at the identifier props, for example props.Player.Name -> []string{
// "Player", "Name"}. Parentheses are transparent at any level, including
// around props itself and around any intermediate selector. It places no
// upper bound on chain length — see ValidateServerExpression's doc comment
// for why the depth cap lives in the lowerer instead.
func ServerPropPath(source string) ([]string, bool) {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return nil, false
	}
	return propsSelectorPath(expr)
}

// propsSelectorPath is ServerPropPath's expression-tree implementation,
// reused by the validator (selector shape, concat operand classification,
// cond selector extraction) so every props selector position resolves
// nested paths through one definition.
func propsSelectorPath(expr ast.Expr) ([]string, bool) {
	selector, ok := unwrapParens(expr).(*ast.SelectorExpr)
	if !ok || selector.Sel == nil || selector.Sel.Name == "" {
		return nil, false
	}
	receiver := unwrapParens(selector.X)
	if ident, ok := receiver.(*ast.Ident); ok {
		if ident.Name != "props" {
			return nil, false
		}
		return []string{selector.Sel.Name}, true
	}
	path, ok := propsSelectorPath(receiver)
	if !ok {
		return nil, false
	}
	return append(path, selector.Sel.Name), true
}

// ServerPropField reports the single props field read by a validated direct
// selector (props.X) — ServerPropPath's length-1 case. Parentheses around
// either the selector or props itself do not change the result. Kept as its
// own function because collectStrictPropReads's CST walk (ir/lower.go) calls
// it once per selector_expression node it visits, independent of any
// top-level validated shape.
func ServerPropField(source string) (string, bool) {
	path, ok := ServerPropPath(source)
	if !ok || len(path) != 1 {
		return "", false
	}
	return path[0], true
}

// ServerExpressionPropPaths reports every maximal props-rooted selector path
// found anywhere in source, regardless of whether source's overall shape
// passes ValidateServerExpression. Read-tracking passes use this to see
// every props path an expression touches — not just the operands of one
// accepted top-level shape — because the grammar's external attribute
// scanner hands attribute-position expressions back as one opaque token with
// no nested CST, unlike element/text children (jsx_expression_container),
// whose nested Go sub-tree the CST-based read-tracking walk can see
// directly. Attribute-position expressions are exactly where every
// concatenation and <If cond> shape lives, so this closes that visibility
// gap generally instead of only for those two shapes.
//
// "Maximal" matters for nested paths: once a *ast.SelectorExpr node yields a
// full props path, this stops descending into that node's own operand
// sub-tree. Otherwise a chain like props.Player.Name would also register its
// inner props.Player sub-expression as an independent (and spurious) bare
// read of a non-scalar field — the same class of fail-open gap
// collectStrictPropReads was fixed for once already, in reverse: a false
// rejection of a valid nested read instead of a missed one.
func ServerExpressionPropPaths(source string) [][]string {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return nil
	}
	var paths [][]string
	seen := make(map[string]struct{})
	ast.Inspect(expr, func(n ast.Node) bool {
		selector, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		path, ok := propsSelectorPath(selector)
		if !ok {
			return true
		}
		key := strings.Join(path, ".")
		if _, dup := seen[key]; !dup {
			seen[key] = struct{}{}
			paths = append(paths, path)
		}
		return false
	})
	return paths
}

// ServerConcatPropPaths reports, in operand order, the props field paths
// read by a validated string-concatenation chain (see
// ValidateServerExpression); each entry is ServerPropPath's ordered field
// list for that operand. It returns ok=false when source's top-level shape
// (parentheses transparent) is not a `+` expression — callers use that to
// skip the concat-specific type pass for every other already-validated
// expression shape.
func ServerConcatPropPaths(source string) ([][]string, bool) {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return nil, false
	}
	bin, ok := unwrapParens(expr).(*ast.BinaryExpr)
	if !ok || bin.Op != token.ADD {
		return nil, false
	}
	var paths [][]string
	for _, operand := range flattenAddChain(bin) {
		if path, ok := propsSelectorPath(operand); ok {
			paths = append(paths, path)
		}
	}
	return paths, true
}

// ValidateServerCondExpression accepts the two expression shapes a strict
// <If cond={...}> attribute may use: a props field selector (a direct field,
// or a nested field path per ServerPropPath), or that selector compared
// against the literal false with `==`. It reports the props path read and
// whether the comparison negates it. Parentheses are transparent at any
// level.
func ValidateServerCondExpression(source string) (path []string, negated bool, err error) {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return nil, false, fmt.Errorf("invalid Go expression: %w", err)
	}
	root := unwrapParens(expr)

	if bin, ok := root.(*ast.BinaryExpr); ok {
		if bin.Op == token.EQL {
			left := unwrapParens(bin.X)
			right := unwrapParens(bin.Y)
			if ident, ok := right.(*ast.Ident); ok && ident.Name == "true" {
				return nil, false, fmt.Errorf("comparison \"== true\" is not supported in strict cond; write the field bare")
			}
			if condPath, ok := propsSelectorPath(left); ok {
				if ident, ok := right.(*ast.Ident); ok && ident.Name == "false" {
					return condPath, true, nil
				}
			}
		}
		return nil, false, fmt.Errorf("strict cond must be a bool props field or a bool props field compared with \"== false\"; got %q", source)
	}

	if condPath, ok := propsSelectorPath(root); ok {
		return condPath, false, nil
	}
	return nil, false, fmt.Errorf("strict cond must be a bool props field or a bool props field compared with \"== false\"; got %q", source)
}
