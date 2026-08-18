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

// Scope names the <Each> loop bindings visible to a strict expression at its
// position in the IR tree — the lowerer's active binding set (see
// ir/lower.go's validateStrictServerExpressions), pushed on an unshadowed
// <Each> node before its children validate (design spec section 2.2).
//
// Items and Indices are kept apart because they admit different shapes: an
// Item name is a valid selector root (row.Label), the same way props is,
// but never a valid bare reference — a loop element is always a struct, and
// the renderer has no single-value spelling for one. An Index name is the
// opposite: always a plain int, so it is a valid bare reference (bare {i}
// renders like an int props field) but never a valid selector root — this
// function knows that unconditionally, with no schema lookup, because an
// index binding's type never varies. Both are still accepted as syntactic
// selector roots by rootedSelectorPath below, so a misuse such as i.Field
// reaches the lowerer's schema-aware diagnostic (design spec section 4.2)
// instead of this schema-blind validator's generic message.
type Scope struct {
	Items   []string
	Indices []string
}

func (s Scope) isItem(name string) bool {
	for _, item := range s.Items {
		if item == name {
			return true
		}
	}
	return false
}

func (s Scope) isIndex(name string) bool {
	for _, index := range s.Indices {
		if index == name {
			return true
		}
	}
	return false
}

func (s Scope) isBinding(name string) bool {
	return s.isItem(name) || s.isIndex(name)
}

// ChildrenBinding is the one identifier a strict body may use to place the
// markup its caller supplied. Four seams read this constant, so the spelling
// cannot drift between them:
//
//   - the file renderer binds this exact name beside props
//     (writeLocalComponent, route/fileprogram.go);
//   - the lowerer reserves it against an <Each> binding, so no loop can
//     shadow it (ir/lower.go);
//   - the lowerer's AcceptsChildren pass looks for it, through
//     IsChildrenExpression below;
//   - the Go projection spells the variadic parameter of every strict
//     component with it (emitStrictComponent, transpile/transpile.go).
const ChildrenBinding = "children"

// exprPosition names the syntactic slot an expression occupies. The strict
// contract admits a different identifier set per slot, so the slot is an
// explicit input to validation rather than a property of the source text.
//
// The two slots differ in exactly one identifier, children:
//
//   - positionChild is a whole child expression hole, {children}. Its value
//     is written into element CONTENT, where markup is legal.
//   - positionAttribute is an attribute value, class={children}. Its value is
//     written into an HTML ATTRIBUTE, where markup is not legal — emitting a
//     rendered node there would splice tags inside quotes and produce broken,
//     unescapable output.
//
// positionAttribute is the zero value, so any caller that does not state a
// position keeps the pre-children behavior and fails closed.
type exprPosition uint8

const (
	positionAttribute exprPosition = iota
	positionChild
)

// ValidateServerExpression accepts exactly the expression shapes implemented
// by the file renderer for strict server components. The v0.39 contract is
// intentionally small: literals and one direct props field (with parentheses).
// v0.42 added a `+` chain that concatenates string literals with props field
// selectors (validateConcatChain documents the accepted shape). v0.42's
// nested-selector change widens every props selector position (a bare read,
// a concat operand, an <If cond>) to accept a field chain rooted at props up
// to three fields deep (see ServerPropPath) instead of exactly one field.
// This function places no upper bound on chain length itself: it is
// schema-blind, so it cannot know where three hops actually lands against a
// given props struct. The lowerer (ir/lower.go) resolves each accepted path
// against the same-file struct schema and reports the three-hop cap there,
// with full component context. Operators outside the one `+` exception,
// indexing, and calls need static Go type/method information that the
// map-backed file renderer does not retain, so they fail closed.
//
// ValidateServerExpression is ValidateServerExpressionScope's empty-scope
// wrapper: it behaves byte-identically to the pre-#182/#184 signature for
// every existing caller, admitting only props as a selector root.
func ValidateServerExpression(source string) error {
	return ValidateServerExpressionScope(source, Scope{})
}

// ValidateServerExpressionScope is ValidateServerExpression, generalized to
// admit a selector chain rooted at any name in scope alongside props — the
// #182 (`<Each>`) extension. See Scope's doc comment for the Items/Indices
// distinction.
//
// This is the ATTRIBUTE-position entry point. It does not admit the children
// identifier; use ValidateServerChildExpressionScope for a child expression
// hole. Keeping the two apart is what stops class={children} from splicing
// rendered markup into an HTML attribute value.
func ValidateServerExpressionScope(source string, scope Scope) error {
	return validateAt(source, scope, positionAttribute)
}

// ValidateServerChildExpressionScope is ValidateServerExpressionScope for a
// whole child expression hole, {expr}, the one position that also admits the
// bare identifier children.
//
// What the admission grants, exactly: emission. children is a single opaque
// gosx.Node the caller already rendered. The callee cannot read a field of
// it, index it, count it, concatenate it, or branch on it — every one of
// those shapes is rejected by a rule this function does not touch
// (rootedSelectorPath, classifyConcatOperand, ValidateServerCondExpression).
// It is not a prop: it never enters PropsFields, PropsPaths, or PropsSlices,
// and no boundary proof reads it.
//
// A body may write {children} more than once. Each occurrence emits the same
// markup again, matching gosx.Expr(children) in the Go projection.
func ValidateServerChildExpressionScope(source string, scope Scope) error {
	return validateAt(source, scope, positionChild)
}

// IsChildrenExpression reports whether source is exactly the children
// identifier, with parentheses transparent. It is the single spelling test
// for "this expression hole places the caller's children", so the lowerer's
// AcceptsChildren pass and this validator can never disagree about what
// children looks like.
func IsChildrenExpression(source string) bool {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return false
	}
	ident, ok := unwrapParens(expr).(*ast.Ident)
	return ok && ident.Name == ChildrenBinding
}

func validateAt(source string, scope Scope, pos exprPosition) error {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return fmt.Errorf("invalid Go expression: %w", err)
	}
	return validate(expr, source, scope, pos)
}

func validate(expr ast.Expr, source string, scope Scope, pos exprPosition) error {
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
		case ChildrenBinding:
			if pos == positionChild {
				return nil
			}
			return fmt.Errorf("children renders as element content, not as an attribute value")
		default:
			if scope.isIndex(node.Name) {
				// An Each index binding is always a plain int — the same
				// parity class already proven for an int props field, so a
				// bare reference renders exactly the way {props.SomeInt}
				// does. See design spec section 2.5.
				return nil
			}
			if scope.isItem(node.Name) {
				return fmt.Errorf("bare %s is not supported; select a field on %s", node.Name, node.Name)
			}
			return fmt.Errorf("identifier %q is not available to the strict server renderer", node.Name)
		}
	case *ast.ParenExpr:
		return validate(node.X, source, scope, pos)
	case *ast.SelectorExpr:
		if _, _, ok := rootedSelectorPath(node, scope); !ok {
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
		return validateConcatChain(node, source, scope)
	case *ast.CallExpr:
		return fmt.Errorf("calls are not supported by the strict server renderer because typed Go methods are not retained in its props map")
	default:
		return fmt.Errorf("expression shape %T is not supported by the strict server renderer", expr)
	}
}

// validateConcatChain accepts a `+` chain whose flattened operands are each a
// string literal or a direct props/binding field selector, with at least one
// of each. The renderer's applyFileBinaryOp takes the string branch whenever
// either side of `+` is a Go string (route/exprlower.go), so this is the one
// binary shape the file renderer and generated Go execute identically.
//
// It takes no exprPosition on purpose: children is never a concat operand,
// in any position. A rendered node has no string value to add, so
// {"prefix " + children} keeps falling into classifyConcatOperand's default
// branch and its existing "not renderable" message.
func validateConcatChain(node *ast.BinaryExpr, source string, scope Scope) error {
	hasStringLiteral := false
	hasPropsField := false
	for _, operand := range flattenAddChain(node) {
		text := operandText(operand, source)
		switch classifyConcatOperand(operand, scope) {
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

func classifyConcatOperand(operand ast.Expr, scope Scope) concatOperandKind {
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
		if _, _, ok := rootedSelectorPath(node, scope); ok {
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
// for why the depth cap lives in the lowerer instead. ServerPropPath is
// ServerSelectorPath's empty-scope wrapper, filtered to a props root, so
// every pre-#182/#184 caller keeps its exact signature and behavior.
func ServerPropPath(source string) ([]string, bool) {
	root, path, ok := ServerSelectorPath(source, Scope{})
	if !ok || root != "props" {
		return nil, false
	}
	return path, true
}

// ServerSelectorPath generalizes ServerPropPath to report both the root
// identifier and the ordered field path of a selector chain rooted at props
// or at any name in scope — the #182 (`<Each>`) binding-selector extension
// (design spec section 2.4: path resolution is (rootType, path), with props
// and an Each item binding both valid roots at this syntactic layer).
func ServerSelectorPath(source string, scope Scope) (root string, path []string, ok bool) {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return "", nil, false
	}
	return rootedSelectorPath(expr, scope)
}

// rootedSelectorPath is ServerSelectorPath's expression-tree implementation,
// reused by the validator (selector shape, concat operand classification,
// cond selector extraction) so every selector position resolves rooted
// paths through one definition. It admits props and, per scope, an Item or
// Index binding name as syntactically valid roots — Scope's doc comment
// explains why an Index root is admitted here even though it is never a
// valid root once the lowerer resolves its type.
func rootedSelectorPath(expr ast.Expr, scope Scope) (root string, path []string, ok bool) {
	selector, isSelector := unwrapParens(expr).(*ast.SelectorExpr)
	if !isSelector || selector.Sel == nil || selector.Sel.Name == "" {
		return "", nil, false
	}
	receiver := unwrapParens(selector.X)
	if ident, isIdent := receiver.(*ast.Ident); isIdent {
		if ident.Name != "props" && !scope.isBinding(ident.Name) {
			return "", nil, false
		}
		return ident.Name, []string{selector.Sel.Name}, true
	}
	root, path, ok = rootedSelectorPath(receiver, scope)
	if !ok {
		return "", nil, false
	}
	return root, append(path, selector.Sel.Name), true
}

// ServerPropField reports the single props field read by a validated direct
// selector (props.X) — ServerPropPath's length-1 case. Parentheses around
// either the selector or props itself do not change the result.
// collectStrictPropReads's CST walk (ir/lower.go) called this once per
// selector_expression node before the nested-reads change generalized that
// walk to ServerPropPath directly; ServerPropField has no production caller
// today and is kept as a tested, documented length-1 convenience over
// ServerPropPath for any future caller that only ever wants a bare field.
func ServerPropField(source string) (string, bool) {
	path, ok := ServerPropPath(source)
	if !ok || len(path) != 1 {
		return "", false
	}
	return path[0], true
}

// RootedPath is one maximal rooted selector path ServerExpressionRootedPaths
// or ServerConcatRootedPaths finds — Root is "props" or a Scope binding
// name, Path is ServerSelectorPath's ordered field list under that root.
type RootedPath struct {
	Root string
	Path []string
}

// ServerExpressionPropPaths reports every maximal props-rooted selector path
// found anywhere in source, regardless of whether source's overall shape
// passes ValidateServerExpression. ServerExpressionRootedPaths' empty-scope
// wrapper, filtered to props roots, so every pre-#182/#184 caller keeps its
// exact signature and behavior.
func ServerExpressionPropPaths(source string) [][]string {
	var paths [][]string
	for _, rp := range ServerExpressionRootedPaths(source, Scope{}) {
		if rp.Root == "props" {
			paths = append(paths, rp.Path)
		}
	}
	return paths
}

// ServerExpressionRootedPaths reports every maximal rooted selector path
// (props- or scope-binding-rooted) found anywhere in source, regardless of
// whether source's overall shape passes ValidateServerExpressionScope.
// Read-tracking passes use this to see every path an expression touches —
// not just the operands of one accepted top-level shape — because the
// grammar's external attribute scanner hands attribute-position expressions
// back as one opaque token with no nested CST, unlike element/text children
// (jsx_expression_container), whose nested Go sub-tree the CST-based
// read-tracking walk can see directly. Attribute-position expressions are
// exactly where every concatenation and <If cond> shape lives, so this
// closes that visibility gap generally instead of only for those two
// shapes; the #182 generalization does the same for a loop binding's reads
// inside an <Each> body.
//
// "Maximal" matters for nested paths: once a *ast.SelectorExpr node yields a
// full rooted path, this stops descending into that node's own operand
// sub-tree. Otherwise a chain like props.Player.Name would also register its
// inner props.Player sub-expression as an independent (and spurious) bare
// read of a non-scalar field — the same class of fail-open gap
// collectStrictPropReads was fixed for once already, in reverse: a false
// rejection of a valid nested read instead of a missed one.
func ServerExpressionRootedPaths(source string, scope Scope) []RootedPath {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return nil
	}
	var paths []RootedPath
	seen := make(map[string]struct{})
	ast.Inspect(expr, func(n ast.Node) bool {
		selector, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		root, path, ok := rootedSelectorPath(selector, scope)
		if !ok {
			return true
		}
		key := root + "\x00" + strings.Join(path, ".")
		if _, dup := seen[key]; !dup {
			seen[key] = struct{}{}
			paths = append(paths, RootedPath{Root: root, Path: path})
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
// expression shape. ServerConcatRootedPaths' empty-scope wrapper, filtered
// to props roots, so every pre-#182/#184 caller keeps its exact signature
// and behavior.
func ServerConcatPropPaths(source string) ([][]string, bool) {
	rooted, ok := ServerConcatRootedPaths(source, Scope{})
	if !ok {
		return nil, false
	}
	var paths [][]string
	for _, rp := range rooted {
		if rp.Root == "props" {
			paths = append(paths, rp.Path)
		}
	}
	return paths, true
}

// ServerConcatRootedPaths generalizes ServerConcatPropPaths to report each
// concat operand's root alongside its field path, admitting an operand
// rooted at a Scope binding the same way ValidateServerExpressionScope's
// concat pass does.
func ServerConcatRootedPaths(source string, scope Scope) ([]RootedPath, bool) {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return nil, false
	}
	bin, ok := unwrapParens(expr).(*ast.BinaryExpr)
	if !ok || bin.Op != token.ADD {
		return nil, false
	}
	var paths []RootedPath
	for _, operand := range flattenAddChain(bin) {
		if root, path, ok := rootedSelectorPath(operand, scope); ok {
			paths = append(paths, RootedPath{Root: root, Path: path})
		}
	}
	return paths, true
}

// ValidateServerCondExpression accepts the two expression shapes a strict
// <If cond={...}> attribute may use: a props field selector (a direct field,
// or a nested field path per ServerPropPath), or that selector compared
// against the literal false with `==`. It reports the props path read and
// whether the comparison negates it. Parentheses are transparent at any
// level. ValidateServerCondExpressionScope's empty-scope wrapper, so every
// pre-#182/#184 caller keeps its exact signature and behavior.
func ValidateServerCondExpression(source string) (path []string, negated bool, err error) {
	_, path, negated, err = ValidateServerCondExpressionScope(source, Scope{})
	return path, negated, err
}

// ValidateServerCondExpressionScope generalizes ValidateServerCondExpression
// to admit a cond selector rooted at a Scope binding (design spec section
// 2.4), reporting the root alongside the path so the lowerer knows which
// schema to resolve the leaf type against.
func ValidateServerCondExpressionScope(source string, scope Scope) (root string, path []string, negated bool, err error) {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return "", nil, false, fmt.Errorf("invalid Go expression: %w", err)
	}
	top := unwrapParens(expr)

	if bin, ok := top.(*ast.BinaryExpr); ok {
		if bin.Op == token.EQL {
			left := unwrapParens(bin.X)
			right := unwrapParens(bin.Y)
			if ident, ok := right.(*ast.Ident); ok && ident.Name == "true" {
				return "", nil, false, fmt.Errorf("comparison \"== true\" is not supported in strict cond; write the field bare")
			}
			if condRoot, condPath, ok := rootedSelectorPath(left, scope); ok {
				if ident, ok := right.(*ast.Ident); ok && ident.Name == "false" {
					return condRoot, condPath, true, nil
				}
			}
		}
		return "", nil, false, fmt.Errorf("strict cond must be a bool props field or a bool props field compared with \"== false\"; got %q", source)
	}

	if condRoot, condPath, ok := rootedSelectorPath(top, scope); ok {
		return condRoot, condPath, false, nil
	}
	return "", nil, false, fmt.Errorf("strict cond must be a bool props field or a bool props field compared with \"== false\"; got %q", source)
}
