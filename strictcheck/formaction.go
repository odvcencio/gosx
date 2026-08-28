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
		// CSRF coverage is independent of the registration scan below. A
		// page.server.go may be dynamic (and therefore outside the action-name
		// check's proof), while the page's own form tree still gives us a
		// statically provable missing token.
		csrfErrors, csrfWarnings := formCSRFDiagnostics(file)
		diags = append(diags, csrfErrors...)
		addWarnings(opts, csrfWarnings)
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

// formCSRFDiagnostics reports a missing CSRF form control only when the form's
// mutating action and complete descendant tree are statically visible. File
// actions are protected by session.Manager.Protect for POST, PUT, PATCH, and
// DELETE requests; a native GET form and an external/native action are outside
// this check. A dynamic/unknown descendant gets a warning rather than an
// error, because it may provide the token at render time.
func formCSRFDiagnostics(file transpile.PackageFile) ([]ir.Diagnostic, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	var warnings []ir.Diagnostic
	for _, comp := range file.Program.Components {
		for _, id := range collectImageContractNodeIDs(file.Program, comp.Root) {
			if int(id) >= len(file.Program.Nodes) {
				continue
			}
			node := &file.Program.Nodes[id]
			if node.Kind != ir.NodeElement || !strings.EqualFold(node.Tag, "form") {
				continue
			}
			actionName, ok := mutatingFileActionName(node)
			if !ok {
				continue
			}
			state := formCSRFDescendantState(file.Program, node.Children)
			if state == formCSRFUnknown {
				span := node.Span
				span.File = file.Path
				warnings = append(warnings, ir.Diagnostic{
					Severity: ir.SeverityWarning,
					Span:     span,
					Message:  fmt.Sprintf("gosx: could not prove that mutating file-action form actionPath(%q) includes a descendant control named %q", actionName, defaultCSRFField),
					Hint:     "verify the rendered form includes a hidden csrf_token control; dynamic components, expression content, spreads, and raw HTML are outside this static check",
				})
				continue
			}
			if state != formCSRFMissing {
				continue
			}
			span := node.Span
			span.File = file.Path
			diags = append(diags, ir.Diagnostic{
				Span:    span,
				Message: fmt.Sprintf("gosx: mutating file-action form actionPath(%q) is missing a descendant control named %q", actionName, defaultCSRFField),
				Hint:    "add <input type=\"hidden\" name=\"csrf_token\" value={csrf.token}></input> inside the form, or keep the token in a statically visible descendant",
			})
		}
	}
	return diags, warnings
}

const defaultCSRFField = "csrf_token"

type formCSRFState uint8

const (
	formCSRFMissing formCSRFState = iota
	formCSRFPresent
	formCSRFUnknown
)

// mutatingFileActionName returns the static actionPath name for a form whose
// method is one of the unsafe methods protected by session.Manager.Protect.
// An absent method is HTML's GET default; an expression or an unfamiliar
// method is deliberately unknown rather than assumed to mutate.
func mutatingFileActionName(node *ir.Node) (string, bool) {
	var action ir.Attr
	var method ir.Attr
	var hasAction bool
	var hasMethod bool
	for _, attr := range node.Attrs {
		switch {
		case strings.EqualFold(attr.Name, "action") && !hasAction:
			action = attr
			hasAction = true
		case strings.EqualFold(attr.Name, "method") && !hasMethod:
			method = attr
			hasMethod = true
		}
	}
	if !hasAction || action.Kind != ir.AttrExpr {
		return "", false
	}
	name, ok := actionPathCallArg(action.Expr)
	if !ok || !hasMethod || method.Kind != ir.AttrStatic {
		return "", false
	}
	switch strings.ToLower(strings.TrimSpace(method.Value)) {
	case "post", "put", "patch", "delete":
		return name, true
	default:
		return "", false
	}
}

// formCSRFDescendantState walks only the form's own element tree. A
// component, expression hole, raw HTML node, spread, or dynamic control name
// may provide a token this IR cannot prove, so it returns formCSRFUnknown and
// the caller emits a warning rather than an error. Plain fragments and
// ordinary elements remain transparent, which catches the Gridiron shape even
// when a hidden input is nested under a static wrapper.
func formCSRFDescendantState(prog *ir.Program, roots []ir.NodeID) formCSRFState {
	seen := make(map[ir.NodeID]bool)
	var walk func(ir.NodeID) formCSRFState
	walk = func(id ir.NodeID) formCSRFState {
		if seen[id] {
			return formCSRFUnknown
		}
		seen[id] = true
		if int(id) >= len(prog.Nodes) {
			return formCSRFUnknown
		}
		node := &prog.Nodes[id]
		switch node.Kind {
		case ir.NodeComponent, ir.NodeExpr, ir.NodeRawHTML:
			return formCSRFUnknown
		case ir.NodeText:
			return formCSRFMissing
		case ir.NodeFragment:
			return walkFormCSRFChildren(walk, node.Children)
		case ir.NodeElement:
			state := formCSRFControlState(node)
			if state == formCSRFPresent || state == formCSRFUnknown {
				return state
			}
			return walkFormCSRFChildren(walk, node.Children)
		default:
			return formCSRFUnknown
		}
	}

	return walkFormCSRFChildren(walk, roots)
}

func walkFormCSRFChildren(walk func(ir.NodeID) formCSRFState, children []ir.NodeID) formCSRFState {
	unknown := false
	for _, child := range children {
		switch walk(child) {
		case formCSRFPresent:
			return formCSRFPresent
		case formCSRFUnknown:
			unknown = true
		}
	}
	if unknown {
		return formCSRFUnknown
	}
	return formCSRFMissing
}

// formCSRFControlState identifies a statically present token control, or an
// otherwise dynamic form control whose spread/name might supply one.
func formCSRFControlState(node *ir.Node) formCSRFState {
	switch strings.ToLower(node.Tag) {
	case "input", "select", "textarea", "button":
	default:
		return formCSRFMissing
	}
	for _, attr := range node.Attrs {
		if attr.Kind == ir.AttrSpread {
			return formCSRFUnknown
		}
		if !strings.EqualFold(attr.Name, "name") {
			continue
		}
		if attr.Kind != ir.AttrStatic {
			return formCSRFUnknown
		}
		if attr.Value == defaultCSRFField {
			return formCSRFPresent
		}
		return formCSRFMissing
	}
	return formCSRFMissing
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
//
// A "*.server.gotmpl" in either searched directory abstains first (see
// hasUnrenderedServerGoTemplate): its real Actions are template syntax
// this scan cannot read, so "no *.server.go found" is not the same fact
// here as "confidently registers nothing".
func registeredFileActionNamesForFile(gsxPath string) (map[string]bool, bool) {
	dirs := candidateServerGoDirs(filepath.Dir(gsxPath))
	if hasUnrenderedServerGoTemplate(dirs) {
		return nil, false
	}
	registrations, ok := collectFileModuleRegistrations(dirs)
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
