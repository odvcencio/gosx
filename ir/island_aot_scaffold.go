//go:build !tinygo

package ir

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"

	"m31labs.dev/gosx/island/program"
)

const aotConditionalHelper = "__gosx_aot_choose"

// Insert scaffold statements without changing any authored byte. Update all
// source maps, splitting copy ranges when a declaration is inserted inside one.
func aotScaffoldLocals(p *Program, projection aotCheckingFile) (aotCheckingFile, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "scaffold.go", projection.bytes, 0)
	if err != nil {
		return projection, fmt.Errorf("evidence_shape_mismatch: %w", err)
	}
	type insertion struct {
		offset int
		text   string
		events map[int]string
	}
	var edits []insertion
	uses := map[*ast.BlockStmt]map[string]bool{}
	handlers := map[*ast.FuncLit]bool{}
	handlerNames := map[string]bool{}
	for _, comp := range p.Components {
		if comp.Scope != nil {
			for _, h := range comp.Scope.Handlers {
				handlerNames[h.Name] = true
			}
		}
	}
	addUse := func(block *ast.BlockStmt, name string) {
		if name == "_" {
			return
		}
		if uses[block] == nil {
			uses[block] = map[string]bool{}
		}
		uses[block][name] = true
	}
	params := func(block *ast.BlockStmt, fields *ast.FieldList) {
		if fields != nil {
			for _, field := range fields.List {
				for _, name := range field.Names {
					addUse(block, name.Name)
				}
			}
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.FuncDecl:
			if n.Body != nil {
				params(n.Body, n.Type.Params)
				params(n.Body, n.Type.Results)
			}
		case *ast.FuncLit:
			params(n.Body, n.Type.Params)
			params(n.Body, n.Type.Results)
			for _, region := range projection.regions {
				if fset.Position(n.Pos()).Offset == region.start {
					handlers[n] = true
				}
			}
		case *ast.AssignStmt:
			if len(n.Lhs) == 1 && len(n.Rhs) == 1 {
				if name, ok := n.Lhs[0].(*ast.Ident); ok && handlerNames[name.Name] {
					if fn, ok := n.Rhs[0].(*ast.FuncLit); ok {
						handlers[fn] = true
					}
				}
			}
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE {
				for _, expr := range []ast.Expr{n.Key, n.Value} {
					if name, ok := expr.(*ast.Ident); ok {
						addUse(n.Body, name.Name)
					}
				}
			}
		case *ast.BlockStmt:
			for _, stmt := range n.List {
				switch d := stmt.(type) {
				case *ast.AssignStmt:
					if d.Tok == token.DEFINE {
						for _, expr := range d.Lhs {
							if name, ok := expr.(*ast.Ident); ok {
								addUse(n, name.Name)
							}
						}
					}
				case *ast.DeclStmt:
					if decl, ok := d.Decl.(*ast.GenDecl); ok {
						for _, spec := range decl.Specs {
							if v, ok := spec.(*ast.ValueSpec); ok {
								for _, name := range v.Names {
									addUse(n, name.Name)
								}
							}
						}
					}
				}
			}
		}
		return true
	})
	for fn := range handlers {
		needed := map[string]bool{}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			id, ok := node.(*ast.Ident)
			if ok && id.Obj == nil && file.Scope.Lookup(id.Name) == nil && slices.Contains(islandEventFields, id.Name) && !token.Lookup(id.Name).IsKeyword() {
				needed[id.Name] = true
			}
			return true
		})
		var names []string
		for name := range needed {
			names = append(names, name)
		}
		slices.Sort(names)
		edit := insertion{offset: fset.Position(fn.Body.Lbrace).Offset + 1, events: map[int]string{}}
		for _, name := range names {
			typ := "interface{}"
			switch eventFieldType(name) {
			case program.TypeString:
				typ = "string"
			case program.TypeBool:
				typ = "bool"
			case program.TypeInt:
				typ = "int"
			case program.TypeFloat:
				typ = "float64"
			}
			edit.events[len(edit.text)+len("\nvar ")] = name
			edit.text += "\nvar " + name + " " + typ + ";\n"
			addUse(fn.Body, name)
		}
		if edit.text != "" {
			edits = append(edits, edit)
		}
	}
	for block, names := range uses {
		var sorted []string
		for name := range names {
			sorted = append(sorted, name)
		}
		slices.Sort(sorted)
		var text strings.Builder
		for _, name := range sorted {
			text.WriteString("\n_ = " + name + ";\n")
		}
		offset := fset.Position(block.Rbrace).Offset
		if len(block.List) > 0 {
			last := block.List[len(block.List)-1]
			switch last.(type) {
			case *ast.ReturnStmt, *ast.IfStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.ForStmt:
				offset = fset.Position(last.Pos()).Offset
			}
		}
		edits = append(edits, insertion{offset: offset, text: text.String()})
	}
	slices.SortFunc(edits, func(a, b insertion) int { return a.offset - b.offset })
	shift := func(offset int) int {
		total := 0
		for _, edit := range edits {
			if edit.offset <= offset {
				total += len(edit.text)
			}
		}
		return offset + total
	}
	var out bytes.Buffer
	start := 0
	for _, edit := range edits {
		out.Write(projection.bytes[start:edit.offset])
		begin := out.Len()
		out.WriteString(edit.text)
		for pos, name := range edit.events {
			projection.implicit[begin+pos] = name
		}
		start = edit.offset
	}
	out.Write(projection.bytes[start:])
	var copies []aotCheckCopy
	for _, copied := range projection.copies {
		lo, hi := copied.checkStart, copied.checkStart+copied.sourceEnd-copied.sourceStart
		cursor := lo
		for _, edit := range edits {
			if edit.offset > cursor && edit.offset < hi {
				copies = append(copies, aotCheckCopy{copied.sourceStart + cursor - lo, copied.sourceStart + edit.offset - lo, shift(cursor)})
				cursor = edit.offset
			}
		}
		copies = append(copies, aotCheckCopy{copied.sourceStart + cursor - lo, copied.sourceEnd, shift(cursor)})
	}
	projection.copies = copies
	for span, region := range projection.regions {
		projection.regions[span] = aotCheckRegion{shift(region.start), shift(region.end)}
	}
	for name, region := range projection.components {
		projection.components[name] = aotCheckRegion{shift(region.start), shift(region.end)}
	}
	for name, region := range projection.synthetic {
		projection.synthetic[name] = aotCheckRegion{shift(region.start), shift(region.end)}
	}
	projection.bytes = out.Bytes()
	return projection, nil
}
