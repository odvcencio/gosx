package route

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/action"
	"github.com/odvcencio/gosx/ir"
)

// FilePage describes a discovered file-based page route.
type FilePage struct {
	FilePath  string
	Source    string
	Dir       string
	RoutePath string
	Pattern   string
	Params    []string
	Layouts   []string
	ErrorPage *FilePage
}

// FileRoutes is the discovered result of scanning a file-based route tree.
type FileRoutes struct {
	Pages          []FilePage
	NotFound       *FilePage
	Error          *FilePage
	NotFoundScopes []FileRouteScope
}

// FileRouteScope describes a directory-scoped special page such as not-found.
type FileRouteScope struct {
	Dir       string
	RoutePath string
	Pattern   string
	Params    []string
	Page      FilePage
}

// FileRoutesOptions configures AddDir.
type FileRoutesOptions struct {
	Render       FileRenderFunc
	Modules      *FileModuleRegistry
	Middleware   []Middleware
	Layout       LayoutFunc
	ErrorHandler ErrorHandler
}

// FileRenderFunc renders a discovered file page for a request.
type FileRenderFunc func(ctx *RouteContext, page FilePage) (gosx.Node, error)

var defaultFileExtensions = []string{".gsx", ".html"}

type fileRouteDir struct {
	Rel      string
	Layout   string
	NotFound *FilePage
	Error    *FilePage
}

// ScanDir discovers file-based routes from a directory tree.
func ScanDir(root string) (FileRoutes, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return FileRoutes{}, fmt.Errorf("resolve %s: %w", root, err)
	}

	dirs := map[string]*fileRouteDir{
		"": {Rel: ""},
	}
	pages := []FilePage{}
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path != root && strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return fmt.Errorf("relative path %s: %w", path, err)
			}
			ensureFileRouteDir(dirs, normalizeFileRouteDir(rel))
			return nil
		}
		kind, ok := routeFileKind(info.Name())
		if !ok {
			return nil
		}

		page, _, err := filePageFromPath(root, path)
		if err != nil {
			return err
		}
		dirEntry := ensureFileRouteDir(dirs, page.Dir)

		switch kind {
		case "page":
			pages = append(pages, page)
		case "layout":
			if dirEntry.Layout != "" {
				return fmt.Errorf("multiple layout files in %s: %s and %s", dirEntry.Rel, filepath.Base(dirEntry.Layout), filepath.Base(path))
			}
			dirEntry.Layout = path
		case "not_found":
			if dirEntry.NotFound != nil {
				return fmt.Errorf("multiple not-found files in %s: %s and %s", dirEntry.Rel, filepath.Base(dirEntry.NotFound.FilePath), filepath.Base(path))
			}
			dirEntry.NotFound = cloneFilePage(page)
		case "error":
			if dirEntry.Error != nil {
				return fmt.Errorf("multiple error files in %s: %s and %s", dirEntry.Rel, filepath.Base(dirEntry.Error.FilePath), filepath.Base(path))
			}
			dirEntry.Error = cloneFilePage(page)
		}
		return nil
	})
	if err != nil {
		return FileRoutes{}, err
	}

	result := FileRoutes{
		Pages: make([]FilePage, 0, len(pages)),
	}
	for _, page := range pages {
		page.Layouts = collectFileLayouts(page.Dir, dirs)
		page.ErrorPage = nearestFileErrorPage(page.Dir, dirs)
		result.Pages = append(result.Pages, page)
	}

	if rootDir := dirs[""]; rootDir != nil {
		if rootDir.NotFound != nil {
			page := cloneFilePageValue(*rootDir.NotFound)
			page.Layouts = collectFileLayouts(page.Dir, dirs)
			result.NotFound = &page
		}
		if rootDir.Error != nil {
			page := cloneFilePageValue(*rootDir.Error)
			page.Layouts = collectFileLayouts(page.Dir, dirs)
			result.Error = &page
		}
	}

	scopePatterns := make(map[string]string)
	for _, dir := range sortFileRouteDirs(dirs) {
		if dir.Rel == "" || dir.NotFound == nil {
			continue
		}
		page := cloneFilePageValue(*dir.NotFound)
		page.Layouts = collectFileLayouts(page.Dir, dirs)
		scope := FileRouteScope{
			Dir:       dir.Rel,
			RoutePath: page.RoutePath,
			Pattern:   page.Pattern,
			Params:    append([]string(nil), page.Params...),
			Page:      page,
		}
		if other, ok := scopePatterns[scope.Pattern]; ok {
			return FileRoutes{}, fmt.Errorf("ambiguous scoped not-found pages for %s: %s and %s", scope.Pattern, other, scope.Page.Source)
		}
		scopePatterns[scope.Pattern] = scope.Page.Source
		result.NotFoundScopes = append(result.NotFoundScopes, scope)
	}

	sort.Slice(result.Pages, func(i, j int) bool {
		if result.Pages[i].Pattern == result.Pages[j].Pattern {
			return result.Pages[i].Source < result.Pages[j].Source
		}
		return result.Pages[i].Pattern < result.Pages[j].Pattern
	})
	sort.Slice(result.NotFoundScopes, func(i, j int) bool {
		left := patternDepth(result.NotFoundScopes[i].Pattern)
		right := patternDepth(result.NotFoundScopes[j].Pattern)
		if left == right {
			return result.NotFoundScopes[i].Pattern > result.NotFoundScopes[j].Pattern
		}
		return left > right
	})
	return result, nil
}

// AddDir scans a directory tree and registers its file-based routes.
func (r *Router) AddDir(root string, opts FileRoutesOptions) error {
	bundle, err := ScanDir(root)
	if err != nil {
		return err
	}

	renderFn := opts.Render
	if renderFn == nil {
		renderFn = DefaultFileRenderer
	}
	moduleRegistry := opts.Modules
	if moduleRegistry == nil {
		moduleRegistry = DefaultFileModuleRegistry()
	}
	layoutCache := make(map[string]LayoutFunc)

	if bundle.NotFound != nil {
		page := *bundle.NotFound
		module, _ := resolveFileModule(moduleRegistry, root, page)
		layout, err := loadFileLayoutChain(layoutCache, page.Layouts, opts.Layout)
		if err != nil {
			return err
		}
		r.SetNotFound(func(ctx *RouteContext) gosx.Node {
			node, err := renderFilePage(ctx, page, module, renderFn)
			if err != nil {
				ctx.SetStatus(500)
				return defaultFileRouteError(err)
			}
			return node
		})
		r.notFoundLayout = layout
	}

	for _, scope := range bundle.NotFoundScopes {
		scope := scope
		module, _ := resolveFileModule(moduleRegistry, root, scope.Page)
		layout, err := loadFileLayoutChain(layoutCache, scope.Page.Layouts, opts.Layout)
		if err != nil {
			return err
		}
		r.notFoundScopes = append(r.notFoundScopes, scopedNotFound{
			pattern: scope.Pattern,
			layout:  layout,
			handler: func(ctx *RouteContext) gosx.Node {
				node, err := renderFilePage(ctx, scope.Page, module, renderFn)
				if err != nil {
					ctx.SetStatus(500)
					return defaultFileRouteError(err)
				}
				return node
			},
		})
	}
	sort.Slice(r.notFoundScopes, func(i, j int) bool {
		left := patternDepth(r.notFoundScopes[i].pattern)
		right := patternDepth(r.notFoundScopes[j].pattern)
		if left == right {
			return r.notFoundScopes[i].pattern > r.notFoundScopes[j].pattern
		}
		return left > right
	})

	if bundle.Error != nil {
		page := *bundle.Error
		module, _ := resolveFileModule(moduleRegistry, root, page)
		layout, err := loadFileLayoutChain(layoutCache, page.Layouts, opts.Layout)
		if err != nil {
			return err
		}
		r.SetError(func(ctx *RouteContext, routeErr error) gosx.Node {
			node, err := renderFilePage(ctx, page, module, renderFn)
			if err != nil {
				return defaultFileRouteError(err)
			}
			return node
		})
		r.errorLayout = layout
	}

	routes := make([]Route, 0, len(bundle.Pages))
	for _, page := range bundle.Pages {
		page := page
		module, _ := resolveFileModule(moduleRegistry, root, page)
		layout, err := loadFileLayoutChain(layoutCache, page.Layouts, opts.Layout)
		if err != nil {
			return err
		}
		routeLayout := wrapWithDefaultLayout(r, layout)
		errorHandler := opts.ErrorHandler
		if page.ErrorPage != nil {
			errorPage := cloneFilePageValue(*page.ErrorPage)
			errorModule, _ := resolveFileModule(moduleRegistry, root, errorPage)
			errorHandler = func(ctx *RouteContext, routeErr error) gosx.Node {
				node, err := renderFilePage(ctx, errorPage, errorModule, renderFn)
				if err != nil {
					return defaultFileRouteError(err)
				}
				return node
			}
		}
		if len(module.Actions) > 0 {
			r.Handle(filePageActionPattern(page.Pattern), buildFileActionHandler(page, module.Actions), opts.Middleware...)
		}
		routes = append(routes, Route{
			Pattern:      page.Pattern,
			Layout:       routeLayout,
			Middleware:   append([]Middleware(nil), opts.Middleware...),
			ErrorHandler: errorHandler,
			DataLoader:   filePageDataLoader(page, module),
			Handler: func(ctx *RouteContext) gosx.Node {
				node, err := renderFilePage(ctx, page, module, renderFn)
				if err != nil {
					panic(err)
				}
				return node
			},
		})
	}

	r.Add(routes...)
	return nil
}

// DefaultFileRenderer renders `.gsx` and `.html` page files directly.
func DefaultFileRenderer(ctx *RouteContext, page FilePage) (gosx.Node, error) {
	return renderFileNode(page.FilePath, fileRenderOptions{
		EvalEnv: newFileRenderEnv(ctx, page),
	})
}

func renderFilePage(ctx *RouteContext, page FilePage, module FileModule, renderFn FileRenderFunc) (gosx.Node, error) {
	if module.Metadata != nil {
		meta, err := module.Metadata(ctx, page, ctx.Data)
		if err != nil {
			return gosx.Node{}, err
		}
		ctx.SetMetadata(meta)
	}
	if module.Render != nil {
		return module.Render(ctx, page, ctx.Data)
	}
	return renderFn(ctx, page)
}

func filePageDataLoader(page FilePage, module FileModule) DataLoader {
	if module.Load == nil {
		return nil
	}
	return func(ctx *RouteContext) (any, error) {
		return module.Load(ctx, page)
	}
}

func hasComponent(prog *ir.Program, name string) bool {
	for _, component := range prog.Components {
		if component.Name == name {
			return true
		}
	}
	return false
}

func defaultFileRouteError(err error) gosx.Node {
	return gosx.El("main",
		gosx.El("h1", gosx.Text("Route Error")),
		gosx.El("p", gosx.Text(err.Error())),
	)
}

func filePageFromPath(root, path string) (FilePage, string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return FilePage{}, "", fmt.Errorf("relative path %s: %w", path, err)
	}

	slashRel := filepath.ToSlash(rel)
	dir := normalizeFileRouteDir(filepath.Dir(slashRel))
	base := filepath.Base(slashRel)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	scopeRoutePath, scopePattern, scopeParams := buildFileRoute(fileRouteParts(dir))

	switch name {
	case "not-found":
		return FilePage{FilePath: path, Source: slashRel, Dir: dir, RoutePath: scopeRoutePath, Pattern: scopePattern, Params: scopeParams}, "not_found", nil
	case "error":
		return FilePage{FilePath: path, Source: slashRel, Dir: dir, RoutePath: scopeRoutePath, Pattern: scopePattern, Params: scopeParams}, "error", nil
	case "layout":
		return FilePage{FilePath: path, Source: slashRel, Dir: dir, RoutePath: scopeRoutePath, Pattern: scopePattern, Params: scopeParams}, "layout", nil
	}

	parts := strings.Split(strings.TrimSuffix(slashRel, filepath.Ext(slashRel)), "/")
	switch name {
	case "page":
		parts = parts[:len(parts)-1]
	case "index":
		parts = parts[:len(parts)-1]
	}

	routePath, pattern, params := buildFileRoute(parts)
	return FilePage{
		FilePath:  path,
		Source:    slashRel,
		Dir:       dir,
		RoutePath: routePath,
		Pattern:   pattern,
		Params:    params,
	}, "page", nil
}

func filePageActionPattern(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "/" {
		return "POST /__actions/{__gosx_action}"
	}
	return "POST " + strings.TrimSuffix(pattern, "/") + "/__actions/{__gosx_action}"
}

func buildFileActionHandler(page FilePage, handlers FileActions) http.Handler {
	handlers = cloneFileActions(handlers)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("__gosx_action")
		if name == "" {
			http.Error(w, "action name required", http.StatusBadRequest)
			return
		}
		handler, ok := handlers[name]
		if !ok {
			http.Error(w, fmt.Sprintf("action %q not found for %s", name, page.Source), http.StatusNotFound)
			return
		}
		action.ServeHandler(w, r, handler)
	})
}

func buildFileRoute(parts []string) (string, string, []string) {
	if len(parts) == 0 {
		return "/", "/", nil
	}

	routeSegments := make([]string, 0, len(parts))
	params := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		if isRouteGroupSegment(part) {
			continue
		}
		switch {
		case strings.HasPrefix(part, "[...") && strings.HasSuffix(part, "]"):
			name := strings.TrimSuffix(strings.TrimPrefix(part, "[..."), "]")
			params = append(params, name)
			routeSegments = append(routeSegments, "{"+name+"...}")
		case strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]"):
			name := strings.TrimSuffix(strings.TrimPrefix(part, "["), "]")
			params = append(params, name)
			routeSegments = append(routeSegments, "{"+name+"}")
		default:
			routeSegments = append(routeSegments, part)
		}
	}

	if len(routeSegments) == 0 {
		return "/", "/", params
	}
	pattern := "/" + strings.Join(routeSegments, "/")
	return pattern, pattern, params
}

func isRouteFile(name string) bool {
	ext := filepath.Ext(name)
	if !slices.Contains(defaultFileExtensions, ext) {
		return false
	}
	_, ok := routeFileKind(name)
	return ok
}

func routeFileKind(name string) (string, bool) {
	ext := filepath.Ext(name)
	if !slices.Contains(defaultFileExtensions, ext) {
		return "", false
	}
	switch strings.TrimSuffix(name, ext) {
	case "page", "index":
		return "page", true
	case "layout":
		return "layout", true
	case "not-found":
		return "not_found", true
	case "error":
		return "error", true
	default:
		return "", false
	}
}

func ensureFileRouteDir(dirs map[string]*fileRouteDir, rel string) *fileRouteDir {
	rel = normalizeFileRouteDir(rel)
	if dir, ok := dirs[rel]; ok {
		return dir
	}
	dir := &fileRouteDir{Rel: rel}
	dirs[rel] = dir
	return dir
}

func normalizeFileRouteDir(rel string) string {
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." {
		return ""
	}
	return strings.TrimPrefix(rel, "./")
}

func fileRouteParts(dir string) []string {
	dir = normalizeFileRouteDir(dir)
	if dir == "" {
		return nil
	}
	return strings.Split(dir, "/")
}

func parentFileRouteDirs(dir string) []string {
	dir = normalizeFileRouteDir(dir)
	if dir == "" {
		return []string{""}
	}
	parts := strings.Split(dir, "/")
	dirs := make([]string, 0, len(parts)+1)
	dirs = append(dirs, "")
	for i := range parts {
		dirs = append(dirs, strings.Join(parts[:i+1], "/"))
	}
	return dirs
}

func collectFileLayouts(dir string, dirs map[string]*fileRouteDir) []string {
	layouts := []string{}
	for _, current := range parentFileRouteDirs(dir) {
		entry := dirs[current]
		if entry == nil || entry.Layout == "" {
			continue
		}
		layouts = append(layouts, entry.Layout)
	}
	return layouts
}

func nearestFileErrorPage(dir string, dirs map[string]*fileRouteDir) *FilePage {
	for _, current := range slices.Backward(parentFileRouteDirs(dir)) {
		entry := dirs[current]
		if entry == nil || entry.Error == nil {
			continue
		}
		page := cloneFilePageValue(*entry.Error)
		page.Layouts = collectFileLayouts(page.Dir, dirs)
		return &page
	}
	return nil
}

func sortFileRouteDirs(dirs map[string]*fileRouteDir) []*fileRouteDir {
	items := make([]*fileRouteDir, 0, len(dirs))
	for _, dir := range dirs {
		items = append(items, dir)
	}
	sort.Slice(items, func(i, j int) bool {
		left := patternDepth(mustBuildFileRoutePattern(items[i].Rel))
		right := patternDepth(mustBuildFileRoutePattern(items[j].Rel))
		if left == right {
			return items[i].Rel < items[j].Rel
		}
		return left < right
	})
	return items
}

func mustBuildFileRoutePattern(dir string) string {
	_, pattern, _ := buildFileRoute(fileRouteParts(dir))
	return pattern
}

func patternDepth(pattern string) int {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "/" {
		return 0
	}
	segments := strings.Split(strings.Trim(pattern, "/"), "/")
	depth := 0
	for _, segment := range segments {
		if segment != "" {
			depth++
		}
	}
	return depth
}

func isRouteGroupSegment(part string) bool {
	return strings.HasPrefix(part, "(") && strings.HasSuffix(part, ")") && len(part) > 2
}

func cloneFilePage(page FilePage) *FilePage {
	cp := cloneFilePageValue(page)
	return &cp
}

func cloneFilePageValue(page FilePage) FilePage {
	page.Params = append([]string(nil), page.Params...)
	page.Layouts = append([]string(nil), page.Layouts...)
	if page.ErrorPage != nil {
		clone := cloneFilePageValue(*page.ErrorPage)
		page.ErrorPage = &clone
	}
	return page
}

func loadFileLayoutChain(cache map[string]LayoutFunc, files []string, extra LayoutFunc) (LayoutFunc, error) {
	layouts := []LayoutFunc{}
	if extra != nil {
		layouts = append(layouts, extra)
	}
	for _, file := range files {
		layout, ok := cache[file]
		if !ok {
			loaded, err := FileLayout(file)
			if err != nil {
				return nil, err
			}
			cache[file] = loaded
			layout = loaded
		}
		layouts = append(layouts, layout)
	}
	return composeLayoutFuncs(layouts), nil
}

func composeLayoutFuncs(layouts []LayoutFunc) LayoutFunc {
	filtered := make([]LayoutFunc, 0, len(layouts))
	for _, layout := range layouts {
		if layout != nil {
			filtered = append(filtered, layout)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return func(ctx *RouteContext, content gosx.Node) gosx.Node {
		return applyLayoutFuncs(filtered, ctx, content)
	}
}

func applyLayoutFuncs(layouts []LayoutFunc, ctx *RouteContext, content gosx.Node) gosx.Node {
	node := content
	for i := len(layouts) - 1; i >= 0; i-- {
		node = layouts[i](ctx, node)
	}
	return node
}

func wrapWithDefaultLayout(r *Router, layout LayoutFunc) LayoutFunc {
	if layout == nil {
		return nil
	}
	return func(ctx *RouteContext, content gosx.Node) gosx.Node {
		node := layout(ctx, content)
		if r.defaultLayout != nil {
			node = r.defaultLayout(ctx, node)
		}
		return node
	}
}
