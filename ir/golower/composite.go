// Slice Y.A — composite literal lowering. Adds `*ast.CompositeLit`
// support to expression lowering: struct, slice, and map literals all
// become OpComposite expressions whose operand pairs are
// (keyExpr, valueExpr) per the encoding documented on
// program.OpComposite.
//
// The lowerer builds a per-file struct type registry at the start of
// LowerASTFile so it can map positional struct literals like
// `vec2{x, y}` to their named-field equivalent (`{X:x, Y:y}`) without
// requiring the author to spell the names. Named literals
// (`vec2{X:x, Y:y}`) skip the registry lookup entirely.
//
// Slice and map literals don't need the registry; their keys are
// either positional integer indices (slice) or per-pair key
// expressions (map).
//
// Nested composites (`[]Node{Node{ID:"a"}, Node{ID:"b"}}`) recurse
// through the same lowerExpr path. Elided element types
// (`[]Node{{ID:"a"}}`) are deferred to Y.B; they're rare in the Y.A
// fixture set (graph_surface.go uses the explicit form everywhere).

package golower

import (
	"fmt"
	"go/ast"

	"m31labs.dev/gosx/island/program"
)

// structTypeInfo records the ordered field names of a struct declared
// somewhere in the source file. Positional composite literals look up
// the entry by type name to recover field names.
type structTypeInfo struct {
	// fields is the ordered list of (field name, count) entries — a
	// single declaration like `struct{ X, Y float64 }` contributes
	// two entries ("X", "Y") in declaration order so positional
	// literals map left-to-right.
	fields []string
}

// scanStructTypes walks the file's top-level type declarations and
// populates ctx.structs with one entry per struct TypeSpec. Non-struct
// type declarations are ignored. Called once at the head of
// LowerASTFile so positional struct-literal lowering has the field
// names ready when expression lowering needs them.
func (c *lowerCtx) scanStructTypes(file *ast.File) {
	if c.structs == nil {
		c.structs = map[string]structTypeInfo{}
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			c.structs[ts.Name.Name] = structInfoFromAST(st)
		}
	}
}

// structInfoFromAST flattens a Go struct type's field list into the
// positional name list OpComposite needs. Embedded fields are ignored
// (they would require an EmbeddedFieldName lookup, which the Y.A
// supported subset doesn't claim to handle).
func structInfoFromAST(st *ast.StructType) structTypeInfo {
	var fields []string
	if st == nil || st.Fields == nil {
		return structTypeInfo{}
	}
	for _, field := range st.Fields.List {
		for _, name := range field.Names {
			fields = append(fields, name.Name)
		}
	}
	return structTypeInfo{fields: fields}
}

// lowerCompositeLit dispatches the three shapes of *ast.CompositeLit
// (struct, slice, map) by inspecting the literal's Type expression.
// Element-type elision is rejected with a clear issue pointing at the
// escape hatch — the Y.A subset requires explicit element types.
func (c *lowerCtx) lowerCompositeLit(lit *ast.CompositeLit) program.ExprID {
	switch t := lit.Type.(type) {
	case *ast.Ident:
		// `Name{ ... }` — named struct literal.
		return c.lowerStructLit(t.Name, lit)
	case *ast.ArrayType:
		// `[]T{ ... }` — slice or array literal.
		return c.lowerSliceLit(lit)
	case *ast.MapType:
		// `map[K]V{ ... }` — map literal.
		return c.lowerMapLit(lit)
	case nil:
		c.addIssue(lit, "elided composite-literal element type is not supported in the Y.A subset", escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
	default:
		c.addIssue(lit, fmt.Sprintf("unsupported composite literal type %T", lit.Type), escapeHatchSuggestion)
		return c.addExpr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
	}
}

// lowerStructLit handles both named-form (`Node{ID: id, Pos: pos}`)
// and positional-form (`vec2{x, y}`) struct literals. The named form
// uses the explicit keys directly; the positional form looks the type
// up in the registry built by scanStructTypes and zips the element
// list against the field list.
func (c *lowerCtx) lowerStructLit(typeName string, lit *ast.CompositeLit) program.ExprID {
	operands := make([]program.ExprID, 0, len(lit.Elts)*2)

	for i, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			keyName, ok := identName(kv.Key)
			if !ok {
				c.addIssue(kv, "struct literal key must be a simple identifier (field name)", escapeHatchSuggestion)
				continue
			}
			keyID := c.addExpr(program.Expr{Op: program.OpLitString, Value: keyName, Type: program.TypeString})
			valueID := c.lowerExpr(kv.Value)
			operands = append(operands, keyID, valueID)
			continue
		}

		// Positional element — look up the i-th field name in the type
		// registry. Missing entries surface a clear issue rather than
		// silently emitting an empty key (which would alias every
		// positional struct under "").
		info, ok := c.structs[typeName]
		if !ok || i >= len(info.fields) {
			c.addIssue(el, fmt.Sprintf("positional struct literal needs known field layout for %q (field %d)", typeName, i), escapeHatchSuggestion)
			continue
		}
		keyID := c.addExpr(program.Expr{Op: program.OpLitString, Value: info.fields[i], Type: program.TypeString})
		valueID := c.lowerExpr(el)
		operands = append(operands, keyID, valueID)
	}

	return c.addExpr(program.Expr{
		Op:       program.OpComposite,
		Value:    "struct:" + typeName,
		Operands: operands,
	})
}

// lowerSliceLit emits an OpComposite slice — operands are pairs of
// (index literal, lowered element). Index literals are emitted so the
// runtime encoding is uniform across all three composite kinds; the
// VM evaluator ignores them on the read side.
func (c *lowerCtx) lowerSliceLit(lit *ast.CompositeLit) program.ExprID {
	operands := make([]program.ExprID, 0, len(lit.Elts)*2)
	for i, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			// `[]T{2: x, 5: y}` — explicit indices. Lower the key as a
			// regular expression so any constant int literal works.
			keyID := c.lowerExpr(kv.Key)
			valueID := c.lowerExpr(kv.Value)
			operands = append(operands, keyID, valueID)
			continue
		}
		keyID := c.addExpr(program.Expr{
			Op:    program.OpLitInt,
			Value: fmt.Sprintf("%d", i),
			Type:  program.TypeInt,
		})
		valueID := c.lowerExpr(el)
		operands = append(operands, keyID, valueID)
	}
	return c.addExpr(program.Expr{
		Op:       program.OpComposite,
		Value:    "slice",
		Operands: operands,
	})
}

// lowerMapLit emits an OpComposite map — operands are arbitrary
// (key expr, value expr) pairs as written in source. Maps without
// KeyValueExpr elements are a parse error in Go, so the missing-key
// branch records an issue rather than silently dropping the element.
func (c *lowerCtx) lowerMapLit(lit *ast.CompositeLit) program.ExprID {
	operands := make([]program.ExprID, 0, len(lit.Elts)*2)
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			c.addIssue(el, "map literal element must be a key:value pair", escapeHatchSuggestion)
			continue
		}
		keyID := c.lowerExpr(kv.Key)
		valueID := c.lowerExpr(kv.Value)
		operands = append(operands, keyID, valueID)
	}
	return c.addExpr(program.Expr{
		Op:       program.OpComposite,
		Value:    "map",
		Operands: operands,
	})
}
