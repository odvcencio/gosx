package strictcheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/transpile"
)

// validateDataLoaderKeysContract is strictcheck's check-time data-loader
// key contract (gosx#249, check 4). A legacy file-routed page reads
// data.someKey; the file router's reflective renderer binds "data" to
// whatever this directory's FileModule.Load hook returns
// (route/fileeval.go: env.values["data"] = ctx.Data). When Load's complete
// key set is visible as a literal map[string]any{...}, a key the template
// reads that never appears in it is guaranteed to evaluate to nil, every
// request, forever -- no error, no crash, just an empty spot on the page.
//
// Warning severity, not error: even a fully literal Load can still be
// legitimately incomplete from this scan's point of view (a key added by
// a same-page FileModule.Bindings hook the map literal never mentions --
// see bindingsMightOverrideData below), and gosx#249 asks that any
// residual uncertainty in a heuristic ship as a warning with that
// uncertainty stated, not as a build-failing error.
func validateDataLoaderKeysContract(files []transpile.PackageFile, opts Options) {
	if opts.Warnings == nil {
		return
	}
	for _, file := range files {
		if file.Program == nil {
			continue
		}
		keys, ok := resolveDataKeysForFile(file.Path)
		if !ok {
			continue
		}
		addWarnings(opts, dataKeyDiagnosticsForFile(file, keys))
	}
}

// resolveDataKeysForFile resolves gsxPath's complete data.X key set.
// gsxPath's own directory and its immediate parent are both searched (see
// candidateServerGoDirs) since a registration may name gsxPath explicitly
// through route.FileModuleFor rather than only through the same-directory
// RegisterFileModuleHere convention (gosx#249 confirmed this against
// examples/goetrope-watch/app/watch/page.server.go, which registers for
// app/watch/[code]/page.gsx this exact way). Three outcomes:
//
//   - Nothing registers for gsxPath at all: "data" is never assigned
//     (route/fileeval.go's baseFileRenderValues starts it at nil and
//     nothing overwrites it without a FileModule), so every data.X read
//     is confidently, always nil -- an empty, resolved key set.
//   - Every registration targeting gsxPath resolves (see resolveLoadKeys):
//     their key sets are unioned.
//   - Any registration's Bindings field might override "data", or its
//     Load field cannot be resolved with confidence: the whole file
//     abstains (ok=false), matching gosx#249's "only report with
//     confidence" rule.
//
// A "*.server.gotmpl" in either searched directory abstains first (see
// hasUnrenderedServerGoTemplate): its real Load is template syntax this
// scan cannot read, so "no *.server.go found" is not the same fact here
// as "confidently, always nil".
func resolveDataKeysForFile(gsxPath string) (map[string]bool, bool) {
	dirs := candidateServerGoDirs(filepath.Dir(gsxPath))
	if hasUnrenderedServerGoTemplate(dirs) {
		return nil, false
	}
	registrations, ok := collectFileModuleRegistrations(dirs)
	if !ok {
		return nil, false
	}
	funcs := funcDeclsAcrossDirs(dirs)
	target := filepath.Clean(gsxPath)
	keys := make(map[string]bool)
	for _, reg := range registrations {
		if reg.target != target {
			continue
		}
		if bindingsMightOverrideData(reg.lit) {
			// gosx#249's honesty rule: FileModule.Bindings can set
			// env.values["data"] directly (route/fileeval.go's
			// withBindings does a whole-value overwrite, not a merge),
			// which this scan cannot trace, so it must not vouch for
			// Load's key set alone -- abstain for the whole file.
			return nil, false
		}
		litKeys, ok := resolveLoadKeys(reg.lit, funcs)
		if !ok {
			return nil, false
		}
		for key := range litKeys {
			keys[key] = true
		}
	}
	return keys, true
}

// bindingsMightOverrideData reports whether lit's Bindings field is
// present and not explicitly nil.
func bindingsMightOverrideData(lit *ast.CompositeLit) bool {
	expr, present := fileModuleField(lit, "Bindings")
	return present && !isNilExpr(expr)
}

// resolveLoadKeys resolves lit's Load field to the complete union of
// literal string keys every "return map[string]any{...}, nil"-shaped exit
// from that function can produce. ok is false whenever any exit point's
// data is not confidently knowable: a missing Load field means data is
// confidently always nil (an empty, resolved key set, not an abstention --
// see below), but a Load field naming an unresolvable function, or a
// function with any exit this scan cannot fully read, means the whole
// directory abstains instead of risking a false positive on one branch it
// could not see.
func resolveLoadKeys(lit *ast.CompositeLit, funcs map[string]*ast.FuncDecl) (map[string]bool, bool) {
	expr, present := fileModuleField(lit, "Load")
	if !present || isNilExpr(expr) {
		// No loader at all: ctx.Data is never assigned (route/fileeval.go's
		// baseFileRenderValues starts "data" at nil and nothing overwrites
		// it), so every data.X read in this page is confidently, always nil.
		return map[string]bool{}, true
	}
	body := resolveLoadFuncBody(expr, funcs)
	if body == nil {
		return nil, false
	}
	return literalReturnMapKeys(body)
}

// resolveLoadFuncBody resolves expr -- an inline func literal or an
// identifier naming a package-level function -- to its body block.
// Anything else (a method value, a call, a variable holding a pre-built
// closure) is unresolvable.
func resolveLoadFuncBody(expr ast.Expr, funcs map[string]*ast.FuncDecl) *ast.BlockStmt {
	switch e := expr.(type) {
	case *ast.FuncLit:
		return e.Body
	case *ast.Ident:
		if fn, ok := funcs[e.Name]; ok {
			return fn.Body
		}
		return nil
	default:
		return nil
	}
}

// literalReturnMapKeys walks every return statement reachable in body and
// unions the literal string keys of every map[string]any{...} composite
// literal returned as the first result. ok is false the moment any return
// statement's first result is neither "nil" nor such a literal (a call, a
// variable, a merge of two maps, a naked return) -- the complete key set
// is then not something this scan can vouch for.
func literalReturnMapKeys(body *ast.BlockStmt) (map[string]bool, bool) {
	keys := make(map[string]bool)
	ok := true
	ast.Inspect(body, func(n ast.Node) bool {
		if !ok {
			return false
		}
		// A return inside a nested closure belongs to that closure, not to
		// Load itself -- do not descend into one, or an unrelated inner
		// func's return shape would wrongly abstain (or wrongly contribute
		// keys to) the outer Load's own result.
		if _, isFuncLit := n.(*ast.FuncLit); isFuncLit {
			return false
		}
		ret, isReturn := n.(*ast.ReturnStmt)
		if !isReturn {
			return true
		}
		if len(ret.Results) == 0 {
			// A naked return in a named-result function: this scan does not
			// trace named-result assignment, so it cannot vouch for what
			// ships. Abstain for the whole function.
			ok = false
			return false
		}
		first := ret.Results[0]
		if isNilExpr(first) {
			return true
		}
		lit, isLit := first.(*ast.CompositeLit)
		if !isLit || !isMapStringAnyType(lit.Type) {
			ok = false
			return false
		}
		litKeys, litOK := literalStringMapKeys(lit)
		if !litOK {
			ok = false
			return false
		}
		for key := range litKeys {
			keys[key] = true
		}
		return true
	})
	if !ok {
		return nil, false
	}
	return keys, true
}

// isMapStringAnyType reports whether t is written as "map[string]any" or
// "map[string]interface{}" -- the only shape ctx.Data's reflective
// binding and route.FileLoadFunc's own "(any, error)" result actually use
// in every example this scan targets. A Load that returns some other
// concrete type (a struct pointer, a named map type) is real, valid Go;
// this scan simply has no data.X key list to offer for it, so it abstains
// rather than guess a shape it was not given.
func isMapStringAnyType(t ast.Expr) bool {
	m, ok := t.(*ast.MapType)
	if !ok {
		return false
	}
	keyIdent, ok := m.Key.(*ast.Ident)
	if !ok || keyIdent.Name != "string" {
		return false
	}
	switch v := m.Value.(type) {
	case *ast.Ident:
		return v.Name == "any"
	case *ast.InterfaceType:
		return v.Methods == nil || len(v.Methods.List) == 0
	default:
		return false
	}
}

// dataKeyDiagnosticsForFile scans file's whole IR tree for a top-level
// "data.X" read whose X is missing from keys, reusing the same
// legacy-template scope ir/validate.go's own ".length" rule targets: a
// strict or island component has typed props, not this reflective "data"
// binding, so neither is in scope here.
func dataKeyDiagnosticsForFile(file transpile.PackageFile, keys map[string]bool) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, comp := range file.Program.Components {
		if comp.Syntax == ir.ComponentSyntaxStrict || comp.IsIsland {
			continue
		}
		for _, id := range collectImageContractNodeIDs(file.Program, comp.Root) {
			node := &file.Program.Nodes[id]
			diags = append(diags, dataKeyNodeDiagnostics(file.Path, node, keys)...)
		}
	}
	return diags
}

func dataKeyNodeDiagnostics(path string, node *ir.Node, keys map[string]bool) []ir.Diagnostic {
	var diags []ir.Diagnostic
	if node.Kind == ir.NodeExpr {
		diags = append(diags, missingDataKeyDiagnostics(path, node.Span, node.Text, keys)...)
	}
	for _, attr := range node.Attrs {
		switch attr.Kind {
		case ir.AttrExpr, ir.AttrSpread:
			diags = append(diags, missingDataKeyDiagnostics(path, node.Span, attr.Expr, keys)...)
		}
	}
	return diags
}

// missingDataKeyDiagnostics parses one expression hole and reports every
// top-level "data.X" selector (exactly one hop off the "data" identifier;
// "data.picks.length" is out of scope here the same way it is for the
// existing ".length" rule -- only the "picks" hop is a loader key) whose X
// is absent from keys.
func missingDataKeyDiagnostics(path string, span ir.Span, source string, keys map[string]bool) []ir.Diagnostic {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil
	}
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return nil
	}
	var diags []ir.Diagnostic
	ast.Inspect(expr, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil {
			return true
		}
		root, ok := sel.X.(*ast.Ident)
		if !ok || root.Name != "data" {
			return true
		}
		key := sel.Sel.Name
		if keys[key] {
			return true
		}
		fileSpan := span
		fileSpan.File = path
		diags = append(diags, ir.Diagnostic{
			Span:     fileSpan,
			Severity: ir.SeverityWarning,
			Message:  fmt.Sprintf("gosx: data.%s is not a key this page's Load hook produces", key),
			Hint:     `this page's data comes from no Load hook, or one whose complete literal map[string]any key set is known, and "` + key + `" is not in it, so it renders as nil; fix the key name, or add it to the loader if this is intentional`,
		})
		return true
	})
	return diags
}
