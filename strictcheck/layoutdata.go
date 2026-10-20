package strictcheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"path/filepath"
	"sort"
	"strings"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/transpile"
)

const (
	layoutDataContextPage     = "page"
	layoutDataContextNotFound = "not-found"
	layoutDataContextError    = "error"
)

type layoutDataRenderContext struct {
	source  string
	display string
	pattern string
	kind    string
	layouts []string
}

type layoutDataContextUse struct {
	context layoutDataRenderContext
	keys    map[string]bool
}

type layoutDataLayoutUse struct {
	selectors map[string]ir.Span
	contexts  map[string]layoutDataContextUse
}

// validateLayoutDataContract checks the data selectors in a file-routed
// layout against the literal Load keys of each page or special-page render
// context that actually uses that layout. A layout is rendered with that
// context's ctx.Data; its own FileModule.Load is not invoked by the file router.
func validateLayoutDataContract(root string, sources []string, opts Options) {
	if opts.Warnings == nil || len(sources) == 0 {
		return
	}
	mountRoots, ok := resolveProjectAddDirRoots(root)
	if !ok || len(mountRoots) == 0 {
		return
	}

	sourceSet := make(map[string]bool, len(sources))
	for _, source := range sources {
		abs, err := filepath.Abs(source)
		if err == nil {
			sourceSet[filepath.Clean(abs)] = true
		}
	}

	type packageFileResult struct {
		file transpile.PackageFile
		ok   bool
	}
	packageFiles := make(map[string]packageFileResult)
	loadPackageFile := func(path string) (transpile.PackageFile, bool) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return transpile.PackageFile{}, false
		}
		abs = filepath.Clean(abs)
		if !sourceSet[abs] {
			return transpile.PackageFile{}, false
		}
		if result, found := packageFiles[abs]; found {
			return result.file, result.ok
		}
		files, err := transpile.LoadPackage(abs)
		if err != nil {
			packageFiles[abs] = packageFileResult{}
			return transpile.PackageFile{}, false
		}
		for _, file := range files {
			filePath, pathErr := filepath.Abs(file.Path)
			if pathErr != nil || filepath.Clean(filePath) != abs {
				continue
			}
			packageFiles[abs] = packageFileResult{file: file, ok: true}
			return file, true
		}
		packageFiles[abs] = packageFileResult{}
		return transpile.PackageFile{}, false
	}

	layouts := make(map[string]*layoutDataLayoutUse)
	layoutBindingsChecked := make(map[string]bool)
	layoutBindingsAbsent := make(map[string]bool)
	seenMounts := make(map[string]bool)

	for _, mountRoot := range mountRoots {
		absMount, err := filepath.Abs(mountRoot)
		if err != nil {
			continue
		}
		absMount = filepath.Clean(absMount)
		if seenMounts[absMount] {
			continue
		}
		seenMounts[absMount] = true

		routes, err := route.ScanDir(absMount)
		if err != nil {
			continue
		}
		for _, renderContext := range layoutDataRenderContexts(routes) {
			contextKeys, ok := resolveDataKeysForFile(renderContext.source)
			if !ok {
				// A nonliteral Load, a possible Bindings overwrite, or an
				// unrendered server template leaves the render context's key set
				// unknown. Do not guess at a layout warning.
				continue
			}
			for _, layoutSource := range renderContext.layouts {
				layoutPath, err := filepath.Abs(layoutSource)
				if err != nil {
					continue
				}
				layoutPath = filepath.Clean(layoutPath)
				if !layoutBindingsChecked[layoutPath] {
					layoutBindingsChecked[layoutPath] = true
					layoutBindingsAbsent[layoutPath] = layoutDataBindingsKnownAbsent(layoutPath)
				}
				if !layoutBindingsAbsent[layoutPath] {
					// The rendered layout module's Bindings can replace "data".
					// Its true selector key set is not statically knowable.
					continue
				}
				layoutFile, ok := loadPackageFile(layoutPath)
				if !ok {
					continue
				}
				selectors := layoutDataSelectorsForFile(layoutFile)
				if len(selectors) == 0 {
					continue
				}
				layout := layouts[layoutPath]
				if layout == nil {
					layout = &layoutDataLayoutUse{
						selectors: selectors,
						contexts:  make(map[string]layoutDataContextUse),
					}
					layouts[layoutPath] = layout
				}
				contextID := renderContext.source + "\x00" + renderContext.pattern + "\x00" + renderContext.kind
				if _, exists := layout.contexts[contextID]; !exists {
					layout.contexts[contextID] = layoutDataContextUse{context: renderContext, keys: contextKeys}
				}
			}
		}
	}

	layoutPaths := make([]string, 0, len(layouts))
	for path := range layouts {
		layoutPaths = append(layoutPaths, path)
	}
	sort.Strings(layoutPaths)
	for _, layoutPath := range layoutPaths {
		layout := layouts[layoutPath]
		keys := make([]string, 0, len(layout.selectors))
		for key := range layout.selectors {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		contextIDs := make([]string, 0, len(layout.contexts))
		for contextID := range layout.contexts {
			contextIDs = append(contextIDs, contextID)
		}
		sort.Strings(contextIDs)
		for _, contextID := range contextIDs {
			use := layout.contexts[contextID]
			for _, key := range keys {
				if use.keys[key] {
					continue
				}
				span := layout.selectors[key]
				routePath := strings.TrimSpace(use.context.pattern)
				if routePath == "" {
					routePath = "/"
				}
				addWarnings(opts, []ir.Diagnostic{{
					Span:     span,
					Severity: ir.SeverityWarning,
					Message: fmt.Sprintf(
						"gosx: layout data.%s is not produced by %s %s (route %s)",
						key, use.context.kind, filepath.ToSlash(use.context.display), routePath,
					),
					Hint: fmt.Sprintf(
						"this layout receives that render context's ctx.Data; add %q to %s Load, or remove the selector if it is not shared by this route",
						key, filepath.ToSlash(use.context.display),
					),
				}})
			}
		}
	}
}

func layoutDataRenderContexts(routes route.FileRoutes) []layoutDataRenderContext {
	contexts := make(map[string]layoutDataRenderContext)
	add := func(page route.FilePage, pattern, kind string, layouts []string) {
		source, err := filepath.Abs(page.FilePath)
		if err != nil {
			return
		}
		source = filepath.Clean(source)
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			pattern = strings.TrimSpace(page.Pattern)
		}
		if pattern == "" {
			pattern = "/"
		}
		id := source + "\x00" + pattern + "\x00" + kind
		context, found := contexts[id]
		if !found {
			display := strings.TrimSpace(page.Source)
			if display == "" {
				display = source
			}
			context = layoutDataRenderContext{
				source:  source,
				display: display,
				pattern: pattern,
				kind:    kind,
			}
		}
		context.layouts = mergeLayoutDataSources(context.layouts, layouts)
		contexts[id] = context
	}

	for _, page := range routes.Pages {
		add(page, page.Pattern, layoutDataContextPage, page.Layouts)
		if page.ErrorPage != nil {
			// The failing route pattern distinguishes this render context, but
			// the nearest error page renders with its own layout chain and Load.
			add(*page.ErrorPage, page.Pattern, layoutDataContextError, page.ErrorPage.Layouts)
		}
	}
	if routes.NotFound != nil {
		add(*routes.NotFound, routes.NotFound.Pattern, layoutDataContextNotFound, routes.NotFound.Layouts)
	}
	for _, scope := range routes.NotFoundScopes {
		add(scope.Page, scope.Pattern, layoutDataContextNotFound, scope.Page.Layouts)
	}
	if routes.Error != nil {
		add(*routes.Error, routes.Error.Pattern, layoutDataContextError, routes.Error.Layouts)
	}

	ids := make([]string, 0, len(contexts))
	for id := range contexts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]layoutDataRenderContext, 0, len(ids))
	for _, id := range ids {
		out = append(out, contexts[id])
	}
	return out
}

func mergeLayoutDataSources(current, added []string) []string {
	seen := make(map[string]bool, len(current)+len(added))
	merged := make([]string, 0, len(current)+len(added))
	for _, sources := range [][]string{current, added} {
		for _, source := range sources {
			clean := filepath.Clean(source)
			if clean == "" || seen[clean] {
				continue
			}
			seen[clean] = true
			merged = append(merged, clean)
		}
	}
	sort.Strings(merged)
	return merged
}

func layoutDataBindingsKnownAbsent(layoutPath string) bool {
	dirs := candidateServerGoDirs(filepath.Dir(layoutPath))
	if hasUnrenderedServerGoTemplate(dirs) {
		return false
	}
	registrations, ok := collectFileModuleRegistrations(dirs)
	if !ok {
		return false
	}
	target := filepath.Clean(layoutPath)
	for _, registration := range registrations {
		if registration.target == target && bindingsMightOverrideData(registration.lit) {
			return false
		}
	}
	return true
}

func isFileRouteLayout(path string) bool {
	return strings.EqualFold(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), "layout")
}

// layoutDataSelectorsForFile returns one source span for each distinct
// reflective data.X selector in a legacy layout. Strict components and
// islands use typed/runtime-specific inputs rather than the file renderer's
// reflective data binding and are therefore outside this contract.
func layoutDataSelectorsForFile(file transpile.PackageFile) map[string]ir.Span {
	selectors := make(map[string]ir.Span)
	if file.Program == nil {
		return selectors
	}
	for _, component := range file.Program.Components {
		if component.Syntax == ir.ComponentSyntaxStrict || component.IsIsland {
			continue
		}
		for _, id := range collectImageContractNodeIDs(file.Program, component.Root) {
			if int(id) >= len(file.Program.Nodes) {
				continue
			}
			node := &file.Program.Nodes[id]
			collectLayoutDataSelectors(selectors, file.Path, node.Span, node)
		}
	}
	return selectors
}

func collectLayoutDataSelectors(selectors map[string]ir.Span, path string, span ir.Span, node *ir.Node) {
	if node == nil {
		return
	}
	if node.Kind == ir.NodeExpr {
		collectLayoutDataSelectorsFromExpr(selectors, path, span, node.Text)
	}
	for _, attr := range node.Attrs {
		switch attr.Kind {
		case ir.AttrExpr, ir.AttrSpread:
			collectLayoutDataSelectorsFromExpr(selectors, path, span, attr.Expr)
		}
	}
}

func collectLayoutDataSelectorsFromExpr(selectors map[string]ir.Span, path string, span ir.Span, source string) {
	source = strings.TrimSpace(source)
	if source == "" {
		return
	}
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return
	}
	span.File = path
	ast.Inspect(expr, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok || selector.Sel == nil {
			return true
		}
		root, ok := selector.X.(*ast.Ident)
		if !ok || root.Name != "data" {
			return true
		}
		if _, exists := selectors[selector.Sel.Name]; !exists {
			selectors[selector.Sel.Name] = span
		}
		return true
	})
}
