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

// validateLayoutDataContract checks the data selectors in a file-routed
// layout against the literal Load keys of each descendant page that actually
// uses that layout. A layout is rendered with the page's ctx.Data; its own
// FileModule.Load is not invoked by the file router.
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

	type pageUse struct {
		page route.FilePage
		keys map[string]bool
	}
	type layoutUse struct {
		selectors map[string]ir.Span
		pages     map[string]pageUse
	}
	layouts := make(map[string]*layoutUse)
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
		for _, page := range routes.Pages {
			pagePath, err := filepath.Abs(page.FilePath)
			if err != nil || !sourceSet[filepath.Clean(pagePath)] {
				continue
			}
			_, ok := loadPackageFile(pagePath)
			if !ok {
				continue
			}
			pageKeys, ok := resolveDataKeysForFile(pagePath)
			if !ok {
				// A nonliteral Load, a possible Bindings overwrite, or an
				// unrendered server template leaves the page's key set
				// unknown. Do not guess at a layout warning.
				continue
			}
			for _, layoutSource := range page.Layouts {
				layoutPath, err := filepath.Abs(layoutSource)
				if err != nil {
					continue
				}
				layoutPath = filepath.Clean(layoutPath)
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
					layout = &layoutUse{
						selectors: selectors,
						pages:     make(map[string]pageUse),
					}
					layouts[layoutPath] = layout
				}
				pageID := pagePath + "\x00" + page.Pattern
				if _, exists := layout.pages[pageID]; !exists {
					layout.pages[pageID] = pageUse{page: page, keys: pageKeys}
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
		pageIDs := make([]string, 0, len(layout.pages))
		for pageID := range layout.pages {
			pageIDs = append(pageIDs, pageID)
		}
		sort.Strings(pageIDs)
		for _, pageID := range pageIDs {
			use := layout.pages[pageID]
			for _, key := range keys {
				if use.keys[key] {
					continue
				}
				span := layout.selectors[key]
				routePath := strings.TrimSpace(use.page.Pattern)
				if routePath == "" {
					routePath = strings.TrimSpace(use.page.RoutePath)
				}
				if routePath == "" {
					routePath = "/"
				}
				pageSource := filepath.ToSlash(use.page.Source)
				addWarnings(opts, []ir.Diagnostic{{
					Span:     span,
					Severity: ir.SeverityWarning,
					Message: fmt.Sprintf(
						"gosx: layout data.%s is not produced by descendant page %s (route %s)",
						key, pageSource, routePath,
					),
					Hint: fmt.Sprintf(
						"this layout receives the descendant page's ctx.Data; add %q to %s Load, or remove the selector if it is not shared by this route",
						key, pageSource,
					),
				}})
			}
		}
	}
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
