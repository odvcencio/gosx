package transpile

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/program"
)

// Reuse the compiler's resolved signal declarations. Only an explicit slice
// type or a Go slice literal proves the Map callback's element type; the Go
// checker remains responsible for the resulting typed callback.
func islandSliceTypes(p *ir.Program) map[string]map[string]string {
	result := make(map[string]map[string]string)
	for _, component := range p.Components {
		if !component.IsIsland || component.Scope == nil {
			continue
		}
		slices := make(map[string]string)
		for _, signal := range component.Scope.Signals {
			for _, source := range []string{signal.SourceType, signal.InitExpr} {
				expression, err := parser.ParseExpr(source)
				if err != nil {
					continue
				}
				if literal, ok := expression.(*ast.CompositeLit); ok {
					expression = literal.Type
				}
				if slice, ok := expression.(*ast.ArrayType); ok && slice.Len == nil {
					slices[signal.Local] = types.ExprString(slice.Elt)
					break
				}
			}
		}
		for _, computed := range component.Scope.Computeds {
			expression, err := parser.ParseExpr(computed.ReturnType)
			if err != nil {
				continue
			}
			if slice, ok := expression.(*ast.ArrayType); ok && slice.Len == nil {
				slices[computed.Name] = types.ExprString(slice.Elt)
			}
		}
		result[component.Name] = slices
	}
	return result
}

// Implicit event values exist only while a browser handler runs. Give the
// server's Go projection matching zero-valued locals inside that handler,
// using the VM's resolved bindings and types rather than another name list.
// No declarations or instructions are added to the island program.
func projectIslandEvents(source string, p *ir.Program, imports map[string]string) (string, error) {
	hasHandlers := false
	for _, component := range p.Components {
		if component.IsIsland && component.Syntax == ir.ComponentSyntaxStrict && component.Scope != nil && len(component.Scope.Handlers) != 0 {
			hasHandlers = true
			break
		}
	}
	if !hasHandlers {
		return source, nil
	}
	type insertion struct {
		offset int
		text   string
	}
	var edits []insertion
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "islands.go", source, 0)
	if err != nil {
		return "", fmt.Errorf("island Go projection: %w", err)
	}
	for index, component := range p.Components {
		if !component.IsIsland || component.Syntax != ir.ComponentSyntaxStrict || component.Scope == nil || len(component.Scope.Handlers) == 0 {
			continue
		}
		lowered, err := ir.LowerIsland(p, index)
		if err != nil {
			return "", fmt.Errorf("island %s: %w", component.Name, err)
		}
		handlers := make(map[string]map[string]string)
		for _, handler := range lowered.Handlers {
			fields := make(map[string]string)
			pending := append([]program.ExprID(nil), handler.Body...)
			visited := make(map[program.ExprID]bool)
			for len(pending) != 0 {
				id := pending[len(pending)-1]
				pending = pending[:len(pending)-1]
				if visited[id] || int(id) >= len(lowered.Exprs) {
					continue
				}
				visited[id] = true
				expression := lowered.Exprs[id]
				pending = append(pending, expression.Operands...)
				if expression.Op != program.OpEventGet || token.Lookup(expression.Value).IsKeyword() {
					continue
				}
				typ := "any"
				switch expression.Type {
				case program.TypeString:
					typ = "string"
				case program.TypeBool:
					typ = "bool"
				case program.TypeInt:
					typ = "int"
				case program.TypeFloat:
					typ = "float64"
				}
				fields[expression.Value] = typ
			}
			handlers[handler.Name] = fields
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Name.Name != component.Name || function.Body == nil {
				continue
			}
			for _, statement := range function.Body.List {
				assignment, ok := statement.(*ast.AssignStmt)
				if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
					continue
				}
				name, ok := assignment.Lhs[0].(*ast.Ident)
				if !ok || len(handlers[name.Name]) == 0 {
					continue
				}
				body, ok := assignment.Rhs[0].(*ast.FuncLit)
				if !ok {
					continue
				}
				var names []string
				for field := range handlers[name.Name] {
					if file.Scope.Lookup(field) != nil || imports[field] != "" {
						return "", fmt.Errorf("island %s handler %s: implicit event field %s is shadowed by a package declaration or import", component.Name, name.Name, field)
					}
					names = append(names, field)
				}
				sort.Strings(names)
				var text strings.Builder
				for _, field := range names {
					fmt.Fprintf(&text, "var %s %s;", field, handlers[name.Name][field])
				}
				edits = append(edits, insertion{offset: fset.Position(body.Body.Lbrace).Offset + 1, text: text.String()})
			}
		}
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].offset < edits[j].offset })
	var result strings.Builder
	start := 0
	for _, edit := range edits {
		result.WriteString(source[start:edit.offset])
		result.WriteString(edit.text)
		start = edit.offset
	}
	result.WriteString(source[start:])
	return result.String(), nil
}
