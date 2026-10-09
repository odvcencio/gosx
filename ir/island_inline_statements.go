//go:build !tinygo

package ir

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// Go statement boundaries are needed only by the host compiler. Legacy VM
// expressions that are not Go syntax retain their original parsing path.
func islandInlineStatements(source string) []string {
	prefix := "package inline\nfunc handler(){"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "inline.go", prefix+source+"\n}", 0)
	if err != nil {
		return []string{source}
	}
	fn := file.Decls[0].(*ast.FuncDecl)
	var result []string
	for _, stmt := range fn.Body.List {
		if _, ok := stmt.(*ast.ExprStmt); !ok {
			return []string{source}
		}
		start, end := fset.Position(stmt.Pos()).Offset-len(prefix), fset.Position(stmt.End()).Offset-len(prefix)
		result = append(result, source[start:end])
	}
	if len(result) == 0 {
		return []string{source}
	}
	return result
}
