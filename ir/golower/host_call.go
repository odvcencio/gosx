// Slice Y.E.2 — host-receiver method-call lowering.
//
// Engine-surface handlers receive `c *surface.Canvas` and
// `ctx *surface.Context` parameters and dispatch through them like:
//
//   c.MoveTo(x, y)
//   ctx.PropsInto(&props)
//
// Pre-Y.E the lowerer treated every selector-call as a stdlib
// intrinsic candidate and rejected unknown ones with "call to <X> is
// not in the supported intrinsic set." Y.E introduces OpHostCall and
// routes selector-calls whose receiver is NOT an imported package
// into the host-dispatch path.
//
// Discrimination algorithm — the lowerer needs to tell apart:
//
//   math.Sin(x)           — receiver "math" is an imported package
//                           → OpCall("math.Sin", x) into intrinsic registry
//
//   c.MoveTo(x, y)        — receiver "c" is NOT an imported package
//                           → OpHostCall("c.MoveTo", x, y) into host receiver
//
// To make this decision, a pre-pass walks file.Imports and records the
// set of import names (the alias if present, otherwise the package name
// derived from the import path). lowerCallExpr's selector branch then
// checks the receiver identifier against this set.
//
// The fallback case (receiver is an Ident but not an imported package,
// not in the intrinsic table) routes to OpHostCall. Unbound receivers
// at runtime record a `host_unbound` diagnostic — the same shape as a
// user-fn dispatch into an unregistered name. This means a typo on a
// stdlib package name (`maht.Sin`) now produces a `host_unbound`
// diagnostic at runtime instead of a build-time intrinsic-set error.
// Worth it: the alternative (rejecting every non-imported selector at
// build time) would block every legitimate canvas call.

package golower

import (
	"go/ast"
	"path"
	"strconv"
	"strings"

	"m31labs.dev/gosx/island/program"
)

// scanImports populates ctx.imports with the source-level identifier
// each import declaration introduces. The alias takes precedence over
// the path-derived name when present.
func (c *lowerCtx) scanImports(file *ast.File) {
	c.imports = make(map[string]bool, len(file.Imports))
	for _, spec := range file.Imports {
		if spec == nil || spec.Path == nil {
			continue
		}
		name := importLocalName(spec)
		if name == "" {
			continue
		}
		c.imports[name] = true
	}
}

// importLocalName returns the source-level identifier under which an
// import is referenced. Honors the alias if present (`m "math"` →
// "m") and falls back to the last path segment otherwise
// (`"strings"` → "strings", `"m31labs.dev/gosx/engine/surface"` →
// "surface").
func importLocalName(spec *ast.ImportSpec) string {
	if spec.Name != nil && spec.Name.Name != "" && spec.Name.Name != "_" {
		if spec.Name.Name == "." {
			// Dot-imports merge into the local namespace; the supported
			// subset doesn't use them, but treat as not contributing a
			// name so receivers don't accidentally classify.
			return ""
		}
		return spec.Name.Name
	}
	raw, err := strconv.Unquote(spec.Path.Value)
	if err != nil {
		return ""
	}
	base := path.Base(raw)
	if base == "" || base == "." || base == "/" {
		return ""
	}
	// Strip a major-version suffix (e.g., ".../v2").
	if strings.HasPrefix(base, "v") && len(base) > 1 {
		allDigits := true
		for _, r := range base[1:] {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			// Use the second-to-last segment instead.
			parts := strings.Split(raw, "/")
			if len(parts) >= 2 {
				base = parts[len(parts)-2]
			}
		}
	}
	return base
}

// isImportedPackage reports whether name is the source-level
// identifier of an imported package in the current file.
func (c *lowerCtx) isImportedPackage(name string) bool {
	if c.imports == nil {
		return false
	}
	return c.imports[name]
}

// lowerHostCall emits OpHostCall for a selector-style call where the
// receiver is NOT an imported package. The Value carries
// "<receiverName>.<MethodName>" so the VM can split on the first dot
// and look up the bound HostReceiver.
func (c *lowerCtx) lowerHostCall(recvName string, sel *ast.SelectorExpr, args []ast.Expr) program.ExprID {
	qualified := recvName + "." + sel.Sel.Name
	argIDs := make([]program.ExprID, 0, len(args))
	for _, a := range args {
		argIDs = append(argIDs, c.lowerExpr(a))
	}
	return c.addExpr(program.Expr{
		Op:       program.OpHostCall,
		Value:    qualified,
		Operands: argIDs,
	})
}
