//go:build !tinygo && !js

package ir

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"m31labs.dev/gosx/island/aot"
	"m31labs.dev/gosx/island/program"
)

func aotSignalKind(typ types.Type) (kind aot.ScalarKind, mutable, allowed bool) {
	if typ == nil {
		return "", false, false
	}
	ptr, ok := types.Unalias(typ).(*types.Pointer)
	if !ok {
		return "", false, false
	}
	named, ok := types.Unalias(ptr.Elem()).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != signalImportPath || named.TypeArgs().Len() != 1 {
		return "", false, false
	}
	if named.Obj().Name() != "Signal" && named.Obj().Name() != "Computed" {
		return "", false, false
	}
	kind = aotGoKind(named.TypeArgs().At(0))
	return kind, named.Obj().Name() == "Signal", kind != ""
}

func aotAuthoredStatements(block *ast.BlockStmt) []ast.Stmt {
	var result []ast.Stmt
	for _, stmt := range block.List {
		if assign, ok := stmt.(*ast.AssignStmt); ok && assign.Tok == token.ASSIGN && len(assign.Lhs) == 1 {
			if id, ok := assign.Lhs[0].(*ast.Ident); ok && id.Name == "_" {
				continue
			}
		}
		if _, ok := stmt.(*ast.DeclStmt); ok {
			continue
		}
		result = append(result, stmt)
	}
	return result
}

// GoSX ternaries are projected from the grammar's condition/arm fields, just
// as in the checking file. No source lexer or type facts are duplicated here.
func (e *aotEvidence) parseExpression(source string) (ast.Expr, error) {
	if expr, err := parser.ParseExpr(source); err == nil {
		return expr, nil
	}
	p, err := e.lower([]byte("package example\nfunc Expression() Node {return <div>{" + source + "}</div>}\n"))
	if err != nil {
		return nil, fmt.Errorf("evidence_shape_mismatch: %w", err)
	}
	projection, err := p.aotBindings.project(p)
	if err != nil {
		return nil, err
	}
	file, err := parser.ParseFile(token.NewFileSet(), "expression.go", projection.bytes, 0)
	if err != nil {
		return nil, fmt.Errorf("evidence_shape_mismatch: %w", err)
	}
	var result ast.Expr
	ast.Inspect(file, func(node ast.Node) bool {
		if assign, ok := node.(*ast.AssignStmt); ok && len(assign.Rhs) == 1 {
			result = assign.Rhs[0]
			return false
		}
		return result == nil
	})
	if result == nil {
		return nil, fmt.Errorf("evidence_shape_mismatch: expression missing")
	}
	return result, nil
}

func aotEventKind(name string) aot.ScalarKind {
	switch eventFieldType(name) {
	case program.TypeString:
		return aot.String
	case program.TypeBool:
		return aot.Bool
	case program.TypeInt:
		return aot.Int
	}
	return ""
}
