package route

import (
	"go/ast"
	"go/token"
)

// Ahead-of-time lowering of `.gsx` `{expr}` holes.
//
// WHY: ir.Node.Text keeps the raw Go source of every expression hole, so the
// render path first re-parsed the source and then re-walked the AST with a type
// switch on every request. Lowering turns one expression source into one closure
// tree, built once. The render path then calls the closure. Three costs go away:
//
//   - go/parser.ParseExpr per hole per render (37.1% of allocated objects).
//   - The ast.Expr type switch per node per render.
//   - Literal decoding per render. evalBasicLit called strconv.Unquote,
//     ParseInt or ParseFloat on every render of every literal. Lowering folds
//     each literal to its decoded value.
//
// Semantics match the old AST interpreter exactly, including the eager
// evaluation of both operands of && and ||. route/exprlower_test.go holds the
// reference interpreter and a differential test that proves the match.

// fileExprFunc evaluates one lowered expression against a render environment.
type fileExprFunc func(env fileRenderEnv) any

// nilFileExpr answers nil for expression shapes the evaluator does not support.
func nilFileExpr(fileRenderEnv) any { return nil }

// lowerFileExpr compiles a parsed expression into a closure tree.
func lowerFileExpr(expr ast.Expr) fileExprFunc {
	switch node := expr.(type) {
	case *ast.BasicLit:
		// Fold the literal now. The decoded value never changes.
		value := evalBasicLit(node)
		return func(fileRenderEnv) any { return value }

	case *ast.Ident:
		// evalIdent answers these three names before it consults the
		// environment, so fold them.
		switch node.Name {
		case "true":
			return func(fileRenderEnv) any { return true }
		case "false":
			return func(fileRenderEnv) any { return false }
		case "nil":
			return nilFileExpr
		}
		name := node.Name
		return func(env fileRenderEnv) any { return evalIdent(name, env) }

	case *ast.BinaryExpr:
		left := lowerFileExpr(node.X)
		right := lowerFileExpr(node.Y)
		op := node.Op
		return func(env fileRenderEnv) any {
			// Evaluate both sides. && and || do not short-circuit here, and
			// the old interpreter did not either.
			return applyFileBinaryOp(op, left(env), right(env))
		}

	case *ast.UnaryExpr:
		operand := lowerFileExpr(node.X)
		op := node.Op
		return func(env fileRenderEnv) any {
			return applyFileUnaryOp(op, operand(env))
		}

	case *ast.ParenExpr:
		return lowerFileExpr(node.X)

	case *ast.SelectorExpr:
		target := lowerFileExpr(node.X)
		name := node.Sel.Name
		return func(env fileRenderEnv) any {
			return selectValue(target(env), name)
		}

	case *ast.IndexExpr:
		target := lowerFileExpr(node.X)
		index := lowerFileExpr(node.Index)
		return func(env fileRenderEnv) any {
			return indexValue(target(env), index(env))
		}

	case *ast.CallExpr:
		fn := lowerFileExpr(node.Fun)
		args := make([]fileExprFunc, len(node.Args))
		for i, arg := range node.Args {
			args[i] = lowerFileExpr(arg)
		}
		if len(args) == 0 {
			return func(env fileRenderEnv) any {
				return callValue(fn(env), nil)
			}
		}
		return func(env fileRenderEnv) any {
			values := make([]any, len(args))
			for i, arg := range args {
				values[i] = arg(env)
			}
			return callValue(fn(env), values)
		}

	default:
		return nilFileExpr
	}
}

func applyFileBinaryOp(op token.Token, left, right any) any {
	switch op {
	case token.ADD:
		if isStringLike(left) || isStringLike(right) {
			return stringifyValue(left) + stringifyValue(right)
		}
		return numericValue(left) + numericValue(right)
	case token.SUB:
		return numericValue(left) - numericValue(right)
	case token.MUL:
		return numericValue(left) * numericValue(right)
	case token.QUO:
		divisor := numericValue(right)
		if divisor == 0 {
			return 0
		}
		return numericValue(left) / divisor
	case token.REM:
		divisor := int64(numericValue(right))
		if divisor == 0 {
			return 0
		}
		return int64(numericValue(left)) % divisor
	case token.LAND:
		return truthy(left) && truthy(right)
	case token.LOR:
		return truthy(left) || truthy(right)
	case token.EQL:
		return equalValues(left, right)
	case token.NEQ:
		return !equalValues(left, right)
	case token.GTR:
		return compareValues(left, right) > 0
	case token.GEQ:
		return compareValues(left, right) >= 0
	case token.LSS:
		return compareValues(left, right) < 0
	case token.LEQ:
		return compareValues(left, right) <= 0
	default:
		return nil
	}
}

func applyFileUnaryOp(op token.Token, value any) any {
	switch op {
	case token.NOT:
		return !truthy(value)
	case token.SUB:
		return -numericValue(value)
	case token.ADD:
		return numericValue(value)
	default:
		return nil
	}
}
