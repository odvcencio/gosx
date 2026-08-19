package strictcheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"path/filepath"
	"strconv"
	"strings"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/transpile"
)

// validateFormActionContract is strictcheck's check-time form-action
// registry contract (gosx#249, check 1). A .gsx element writes
// action={actionPath("name")} or formaction={actionPath("name")};
// route.RouteContext.ActionPath (route/route.go) builds the runtime URL
// from that bare name with NO lookup against what is actually
// registered -- so a name with no matching entry in this directory's
// route.FileActions (registered through RegisterFileModuleHere and its
// siblings in a "*.server.go" file, see route/filemodule.go) is a
// guaranteed 404 the first time a real request reaches it. Both sides are
// compile-time facts: the .gsx side from the IR, the Go side from a
// composite literal in a "*.server.go" file. This exact shape shipped in
// this repository's own examples/dashboard (gosx#249's premise table).
//
// Error severity: an unresolvable name is not a matter of interpretation
// the way check 2's CSS heuristic or check 4's loader-key heuristic are --
// either the name is registered or the request 404s.
func validateFormActionContract(files []transpile.PackageFile, opts Options) error {
	var diags []ir.Diagnostic
	for _, file := range files {
		if file.Program == nil {
			continue
		}
		registered, resolved := registeredFileActionNamesForFile(file.Path)
		if !resolved {
			// A dynamic Actions construction, or an unresolvable
			// registration call (gosx#249's "only report with confidence"
			// rule): stay silent for this file rather than risk flagging a
			// name this scan simply could not see.
			continue
		}
		diags = append(diags, formActionDiagnostics(file, registered)...)
	}
	return ir.NewDiagnosticsError("form-action", diags)
}

// registeredFileActionNamesForFile returns the complete set of action
// names any "*.server.go" registration targeting gsxPath registers, and
// whether that set could be resolved with confidence. gsxPath's own
// directory and its immediate parent are both searched (see
// candidateServerGoDirs) since a registration may name gsxPath explicitly
// through route.FileModuleFor rather than only through the same-directory
// RegisterFileModuleHere convention. An empty-but-resolved result
// (ok=true, empty map) is a real, meaningful finding: nothing registers
// any action for gsxPath at all, so ANY actionPath(...) reference found in
// it is unregistered.
func registeredFileActionNamesForFile(gsxPath string) (map[string]bool, bool) {
	registrations, ok := collectFileModuleRegistrations(candidateServerGoDirs(filepath.Dir(gsxPath)))
	if !ok {
		return nil, false
	}
	target := filepath.Clean(gsxPath)
	registered := make(map[string]bool)
	for _, reg := range registrations {
		if reg.target != target {
			continue
		}
		expr, present := fileModuleField(reg.lit, "Actions")
		if !present || isNilExpr(expr) {
			continue
		}
		keys, ok := resolveCompositeExpr(expr, reg.vars)
		if !ok {
			return nil, false
		}
		for name := range keys {
			registered[name] = true
		}
	}
	return registered, true
}

// resolveCompositeExpr resolves expr -- either an inline composite literal
// or an identifier naming a package-level composite-literal variable in
// vars -- to its complete literal string key set.
func resolveCompositeExpr(expr ast.Expr, vars map[string]*ast.CompositeLit) (map[string]bool, bool) {
	switch e := expr.(type) {
	case *ast.CompositeLit:
		return literalStringMapKeys(e)
	case *ast.Ident:
		if lit, ok := vars[e.Name]; ok {
			return literalStringMapKeys(lit)
		}
		return nil, false
	default:
		return nil, false
	}
}

// formActionDiagnostics scans file's whole IR tree for a static "action" or
// "formaction" attribute holding a bare actionPath("name") call, and
// reports one it cannot find in registered.
func formActionDiagnostics(file transpile.PackageFile, registered map[string]bool) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, comp := range file.Program.Components {
		for _, id := range collectImageContractNodeIDs(file.Program, comp.Root) {
			node := &file.Program.Nodes[id]
			for _, attr := range node.Attrs {
				if attr.Name != "action" && attr.Name != "formaction" {
					continue
				}
				if attr.Kind != ir.AttrExpr {
					continue
				}
				name, ok := actionPathCallArg(attr.Expr)
				if !ok || registered[name] {
					continue
				}
				span := node.Span
				span.File = file.Path
				diags = append(diags, ir.Diagnostic{
					Span:    span,
					Message: fmt.Sprintf("gosx: %s references actionPath(%q), which is not registered in any FileActions reachable from this page", attr.Name, name),
					Hint:    fmt.Sprintf("register it in a *.server.go beside this page, for example Actions: route.FileActions{%q: func(ctx *action.Context) error { ... }}, or fix the name if this is a typo", name),
				})
			}
		}
	}
	return diags
}

// actionPathCallArg reports whether source is exactly a call to the
// actionPath template function with one string-literal argument, and if
// so, that argument's value. Any other shape (a variable, a concatenation,
// a different function) is out of this check's reach and reported as
// ok=false -- it cannot 404 against a name this scan can compare, but it
// also is not something this check can vouch for; the node is simply
// skipped, not flagged either way.
func actionPathCallArg(source string) (string, bool) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", false
	}
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return "", false
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return "", false
	}
	fn, ok := call.Fun.(*ast.Ident)
	if !ok || fn.Name != "actionPath" {
		return "", false
	}
	basic, ok := call.Args[0].(*ast.BasicLit)
	if !ok {
		return "", false
	}
	name, err := strconv.Unquote(basic.Value)
	if err != nil {
		return "", false
	}
	return name, true
}
