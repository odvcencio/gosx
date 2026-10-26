package golower

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestLowerASTFilesRejectsInvalidPackages(t *testing.T) {
	for _, tt := range []struct {
		name    string
		sources []string
		want    string
	}{
		{"empty", nil, "package files are required"},
		{"different packages", []string{"package first", "package second"}, "does not match first"},
		{"duplicate functions", []string{"package p; func F() {}", "package p; func F() {}"}, `duplicate function "F"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			var files []*ast.File
			for _, source := range tt.sources {
				file, err := parser.ParseFile(fset, "source.go", source, 0)
				if err != nil {
					t.Fatal(err)
				}
				files = append(files, file)
			}
			if _, err := LowerASTFiles(fset, files); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLowerASTFilesReportsEachSourceLocation(t *testing.T) {
	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range []string{"first.go", "second.go"} {
		function := "First"
		if name == "second.go" {
			function = "Second"
		}
		file, err := parser.ParseFile(fset, name, "package p\nfunc "+function+"(){\n go work()\n}\n", 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	_, err := LowerASTFiles(fset, files)
	var diagnostics *LowerError
	if !errors.As(err, &diagnostics) || len(diagnostics.Issues) != 2 {
		t.Fatalf("expected both unsupported goroutines, got %v", err)
	}
	for i, name := range []string{"first.go", "second.go"} {
		issue := diagnostics.Issues[i]
		if issue.File != name || issue.Line != 3 || !strings.Contains(err.Error(), name+":3") {
			t.Fatalf("source location lost: %+v; %v", issue, err)
		}
	}
}

func TestLowerASTFilesExpressionLimit(t *testing.T) {
	for _, count := range []int{maxProgramExpressions, maxProgramExpressions + 1} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			files := []*ast.File{{Name: ast.NewIdent("p")}, {Name: ast.NewIdent("p")}}
			for i := 0; i < count; i++ {
				file := files[i%2]
				file.Decls = append(file.Decls, &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{
					&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent("v" + strconv.Itoa(i))}, Values: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "1"}}},
				}})
			}
			p, err := LowerASTFiles(token.NewFileSet(), files)
			if count == maxProgramExpressions {
				if err != nil || len(p.Exprs) != count {
					t.Fatalf("valid boundary rejected: expressions=%d, error=%v", len(p.Exprs), err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "65,535 expression limit") || len(p.Exprs) != maxProgramExpressions {
				t.Fatalf("overflow was not bounded: expressions=%d, error=%v", len(p.Exprs), err)
			}
		})
	}
}
