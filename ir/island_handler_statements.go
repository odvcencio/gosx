//go:build !tinygo && !js

package ir

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"m31labs.dev/gosx/island/program"
)

// lowerHandlerStatements admits Go branches and bare returns. Expressions keep
// their existing parser, binding rules, and opcode layout. Expression-only and
// legacy non-Go bodies retain the original lowering path entirely.
func (l *islandLowerer) lowerHandlerStatements(statements []string, scope *ExprScope) ([]program.ExprID, bool, error) {
	const prefix = "package inline\nfunc handler(){\n"
	source := prefix + strings.Join(statements, "\n") + "\n}"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "handler.go", source, 0)
	if err != nil {
		return nil, false, nil
	}
	body := file.Decls[0].(*ast.FuncDecl).Body
	structured := false
	for _, stmt := range body.List {
		if _, expression := stmt.(*ast.ExprStmt); !expression {
			structured = true
			break
		}
	}
	if !structured {
		return nil, false, nil
	}
	compiler := islandStatementLowerer{island: l, scope: scope, source: source, fset: fset}
	root, err := compiler.block(body)
	return []program.ExprID{root}, true, err
}

type islandStatementLowerer struct {
	island *islandLowerer
	scope  *ExprScope
	source string
	fset   *token.FileSet
}

func (c *islandStatementLowerer) expression(expr ast.Expr) (program.ExprID, error) {
	source := c.source[c.fset.Position(expr.Pos()).Offset:c.fset.Position(expr.End()).Offset]
	exprs, root, err := ParseExpr(source, c.scope)
	if err != nil {
		return 0, fmt.Errorf("expression %q: %w", source, err)
	}
	return c.island.appendExprs(exprs, root, source)
}

func (c *islandStatementLowerer) block(block *ast.BlockStmt) (program.ExprID, error) {
	var operands []program.ExprID
	for _, stmt := range block.List {
		id, err := c.statement(stmt)
		if err != nil {
			return 0, err
		}
		operands = append(operands, id)
	}
	// One root sequence makes return unwind the entire handler, including
	// statements following an if. No browser dispatcher change is needed.
	return c.island.addExprDirect(program.Expr{Op: program.OpSeq, Operands: operands})
}

func (c *islandStatementLowerer) statement(stmt ast.Stmt) (program.ExprID, error) {
	switch stmt := stmt.(type) {
	case *ast.ExprStmt:
		return c.expression(stmt.X)
	case *ast.BlockStmt:
		return c.block(stmt)
	case *ast.IfStmt:
		if stmt.Init != nil {
			return 0, fmt.Errorf("if initializers are not supported in island handlers")
		}
		condition, err := c.expression(stmt.Cond)
		if err != nil {
			return 0, err
		}
		then, err := c.block(stmt.Body)
		if err != nil {
			return 0, err
		}
		var otherwise program.ExprID
		if stmt.Else != nil {
			otherwise, err = c.statement(stmt.Else)
		} else {
			otherwise, err = c.island.addExprDirect(program.Expr{Op: program.OpSeq})
		}
		if err != nil {
			return 0, err
		}
		return c.island.addExprDirect(program.Expr{Op: program.OpCond, Operands: []program.ExprID{condition, then, otherwise}})
	case *ast.ReturnStmt:
		if len(stmt.Results) != 0 {
			return 0, fmt.Errorf("island handlers support only bare return")
		}
		return c.island.addExprDirect(program.Expr{Op: program.OpReturn})
	case *ast.EmptyStmt:
		return c.island.addExprDirect(program.Expr{Op: program.OpSeq})
	default:
		return 0, fmt.Errorf("unsupported island handler statement %T", stmt)
	}
}
