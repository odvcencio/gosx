// Top-level declaration lowering. Functions become handlers; package
// vars become signal definitions; everything else is rejected with a
// pointer at the escape hatch.

package golower

import (
	"go/ast"
	"go/token"

	"m31labs.dev/gosx/island/program"
)

// lowerTopLevelDecl dispatches by declaration kind.
func (c *lowerCtx) lowerTopLevelDecl(decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		c.lowerFuncDecl(d)
	case *ast.GenDecl:
		c.lowerGenDecl(d)
	default:
		c.addIssue(decl, "unsupported top-level declaration", escapeHatchSuggestion)
	}
}

// lowerFuncDecl emits a program.Handler whose Body is a single OpSeq
// expression. Parameters become locals declared in the handler's
// frame before the body runs; the caller (engine/surface/lowering.go
// in Slice X.D) is responsible for populating those locals from event
// data before invoking the handler.
//
// Function receivers are rejected — engine-surface handlers are always
// top-level package functions per the runtime symbol table.
func (c *lowerCtx) lowerFuncDecl(fn *ast.FuncDecl) {
	if fn.Recv != nil {
		c.handler = fn.Name.Name
		c.addIssue(fn, "method receivers are not supported in engine-surface handlers", escapeHatchSuggestion)
		return
	}
	c.handler = fn.Name.Name
	defer func() { c.handler = "" }()

	if fn.Body == nil {
		// Forward declaration / external linkage — skip silently.
		return
	}

	// Parameters are NOT pre-declared as locals. The runtime supplies
	// parameter values through the props table (Slice X.D's
	// engine/surface/lowering.go is responsible for populating those
	// props from event data before invoking the handler). The
	// OpLocalGet fallback chain (frame → signals → props) then
	// resolves a bare parameter reference to the prop value.
	//
	// Declaring parameters as locals here would shadow the props with
	// zero values, which is why the previous attempt failed silently.
	bodyOps := c.lowerStmtList(fn.Body.List)

	seqID := c.addExpr(program.Expr{
		Op:       program.OpSeq,
		Operands: bodyOps,
	})

	c.prog.Handlers = append(c.prog.Handlers, program.Handler{
		Name: fn.Name.Name,
		Body: []program.ExprID{seqID},
	})
}

// lowerGenDecl handles `var` and `const` declarations. `var x = expr`
// becomes a signal whose init expression is the lowered expr. `const`
// is treated the same way for the X.B/X.C supported subset: constants
// participate as initial signal values, which is good enough for the
// engine-surface authoring contract.
//
// `import` and `type` GenDecls are ignored — imports don't carry to
// the VM, type aliases are erased.
func (c *lowerCtx) lowerGenDecl(d *ast.GenDecl) {
	switch d.Tok {
	case token.IMPORT, token.TYPE:
		return
	case token.VAR, token.CONST:
		for _, spec := range d.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			c.lowerValueSpec(vs)
		}
	default:
		c.addIssue(d, "unsupported GenDecl token", escapeHatchSuggestion)
	}
}

// lowerValueSpec turns one `var x[, y] = a[, b]` line into one signal
// definition per name. A name with no initializer becomes a signal
// initialized to the zero value of its declared type (or TypeAny when
// the type is unknown).
func (c *lowerCtx) lowerValueSpec(vs *ast.ValueSpec) {
	exprType := goExprType(vs.Type)
	for i, name := range vs.Names {
		if name == nil || name.Name == "_" {
			continue
		}
		var initID program.ExprID
		if i < len(vs.Values) {
			initID = c.lowerExpr(vs.Values[i])
		} else {
			// Zero-value initializer.
			initID = c.addExpr(zeroValueExpr(exprType))
		}
		c.prog.Signals = append(c.prog.Signals, program.SignalDef{
			Name: name.Name,
			Type: exprType,
			Init: initID,
		})
	}
}

// goExprType maps a Go AST type expression to a program.ExprType for
// signal typing. Best-effort: unknown types become TypeAny so the
// runtime relies on Value's intrinsic typing.
func goExprType(t ast.Expr) program.ExprType {
	ident, ok := t.(*ast.Ident)
	if !ok {
		return program.TypeAny
	}
	switch ident.Name {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "byte", "rune":
		return program.TypeInt
	case "float32", "float64":
		return program.TypeFloat
	case "bool":
		return program.TypeBool
	case "string":
		return program.TypeString
	default:
		return program.TypeAny
	}
}

// zeroValueExpr returns an Expr that evaluates to the zero value of typ.
// Used for `var x int` where no explicit initializer is given.
func zeroValueExpr(typ program.ExprType) program.Expr {
	switch typ {
	case program.TypeInt:
		return program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt}
	case program.TypeFloat:
		return program.Expr{Op: program.OpLitFloat, Value: "0", Type: program.TypeFloat}
	case program.TypeBool:
		return program.Expr{Op: program.OpLitBool, Value: "false", Type: program.TypeBool}
	case program.TypeString:
		return program.Expr{Op: program.OpLitString, Value: "", Type: program.TypeString}
	default:
		// TypeAny zero is an empty string literal — the closest thing
		// to nil the Value type carries without panicking on later
		// arithmetic. The lowerer rarely takes this path for handlers.
		return program.Expr{Op: program.OpLitString, Value: "", Type: program.TypeAny}
	}
}
