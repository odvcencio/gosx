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
	Root      string
	FilePath  string
	Source    string
	Dir       string
	RoutePath string
	Pattern   string
	Params    []string
	Layouts   []string
	Config    FileRouteConfig
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
	DirModules   *DirModuleRegistry
	Middleware   []Middleware
	Layout       LayoutFunc
	ErrorHandler ErrorHandler
}

// FileRenderFunc renders a discovered file page for a request.
type FileRenderFunc func(ctx *RouteContext, page FilePage) (gosx.Node, error)

var defaultFileExtensions = []string{".gsx", ".html"}

type fileRouteDir struct {
	Rel       string
	Layout    string
	Config    FileRouteConfig
	HasConfig bool
	NotFound  *FilePage
	Error     *FilePage
}

type resolvedFilePage struct {
	page       FilePage
	module     FileModule
	dirModules []DirModule
	layout     LayoutFunc
}

type fileRouteRegistrar struct {
	router            *Router
	root              string
	opts              FileRoutesOptions
	renderFn          FileRenderFunc
	moduleRegistry    *FileModuleRegistry
	dirModuleRegistry *DirModuleRegistry
	layoutCache       map[string]LayoutFunc
}

type fileRouteScanner struct {
	root  string
	dirs  map[string]*fileRouteDir
	pages []FilePage
}

// ScanDir discovers file-based routes from a directory tree.
func ScanDir(root string) (FileRoutes, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return FileRoutes{}, fmt.Errorf("resolve %s: %w", root, err)
	}
	scanner := newFileRouteScanner(root)
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return scanner.visit(path, info)
	})
	if err != nil {
		return FileRoutes{}, err
	}
	return scanner.build()
}

func newFileRouteScanner(root string) *fileRouteScanner {
	return &fileRouteScanner{
		root: root,
		dirs: map[string]*fileRouteDir{
			"": {Rel: ""},
		},
	}
}

func (s *fileRouteScanner) visit(path string, info os.FileInfo) error {
	if info.IsDir() {
		return s.visitDir(path, info)
	}
	if info.Name() == "route.config.json" {
		return s.visitConfig(path)
	}
	return s.visitRouteFile(path, info.Name())
}

func (s *fileRouteScanner) visitDir(path string, info os.FileInfo) error {
	if path != s.root && strings.HasPrefix(info.Name(), ".") {
		return filepath.SkipDir
	}
	rel, err := s.relativePath(path)
	if err != nil {
		return err
	}
	ensureFileRouteDir(s.dirs, normalizeFileRouteDir(rel))
	return nil
}

func (s *fileRouteScanner) visitConfig(path string) error {
	rel, err := s.relativePath(path)
	if err != nil {
		return err
	}
	dirEntry := ensureFileRouteDir(s.dirs, normalizeFileRouteDir(filepath.Dir(filepath.ToSlash(rel))))
	config, err := readFileRouteConfig(path)
	if err != nil {
		return err
	}
	dirEntry.Config = config
	dirEntry.HasConfig = true
	return nil
}

func (s *fileRouteScanner) visitRouteFile(path, name string) error {
	kind, ok := routeFileKind(name)
	if !ok {
		return nil
	}
	page, _, err := filePageFromPath(s.root, path)
	if err != nil {
		return err
	}
	dirEntry := ensureFileRouteDir(s.dirs, page.Dir)
	switch kind {
	case "page":
		s.pages = append(s.pages, page)
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
}

func (s *fileRouteScanner) relativePath(path string) (string, error) {
	rel, err := filepath.Rel(s.root, path)
	if err != nil {
		return "", fmt.Errorf("relative path %s: %w", path, err)
	}
	return rel, nil
}

func (s *fileRouteScanner) build() (FileRoutes, error) {
	result := FileRoutes{
		Pages: make([]FilePage, 0, len(s.pages)),
	}
	for _, page := range s.pages {
		result.Pages = append(result.Pages, s.decoratePage(page))
	}
	s.applyRootSpecialPages(&result)
	if err := s.appendScopedNotFoundPages(&result); err != nil {
		return FileRoutes{}, err
	}
	sortFileRouteResults(&result)
	return result, nil
}

func (s *fileRouteScanner) decoratePage(page FilePage) FilePage {
	page.Layouts = collectFileLayouts(page.Dir, s.dirs)
	page.Config = collectFileRouteConfig(page.Dir, s.dirs)
	page.ErrorPage = nearestFileErrorPage(page.Dir, s.dirs)
	return page
}

func (s *fileRouteScanner) applyRootSpecialPages(result *FileRoutes) {
	rootDir := s.dirs[""]
	if rootDir == nil {
		return
	}
	if rootDir.NotFound != nil {
		page := s.decoratePage(cloneFilePageValue(*rootDir.NotFound))
		result.NotFound = &page
	}
	if rootDir.Error != nil {
		page := s.decoratePage(cloneFilePageValue(*rootDir.Error))
		result.Error = &page
	}
}

func (s *fileRouteScanner) appendScopedNotFoundPages(result *FileRoutes) error {
	scopePatterns := make(map[string]string)
	for _, dir := range sortFileRouteDirs(s.dirs) {
		if dir.Rel == "" || dir.NotFound == nil {
			continue
		}
		page := s.decoratePage(cloneFilePageValue(*dir.NotFound))
		scope := FileRouteScope{
			Dir:       dir.Rel,
			RoutePath: page.RoutePath,
			Pattern:   page.Pattern,
			Params:    append([]string(nil), page.Params...),
			Page:      page,
		}
		if other, ok := scopePatterns[scope.Pattern]; ok {
			return fmt.Errorf("ambiguous scoped not-found pages for %s: %s and %s", scope.Pattern, other, scope.Page.Source)
		}
		scopePatterns[scope.Pattern] = scope.Page.Source
		result.NotFoundScopes = append(result.NotFoundScopes, scope)
	}
	return nil
}

func sortFileRouteResults(result *FileRoutes) {
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
}

// AddDir scans a directory tree and registers its file-based routes.
func (r *Router) AddDir(root string, opts FileRoutesOptions) error {
	bundle, err := ScanDir(root)
	if err != nil {
		return err
	}
	registrar := newFileRouteRegistrar(r, root, opts)
	if err := registrar.registerSpecialPages(bundle); err != nil {
		return err
	}
	routes, err := registrar.buildRoutes(bundle.Pages)
	if err != nil {
		return err
	}
	r.Add(routes...)
	return nil
}

func newFileRouteRegistrar(router *Router, root string, opts FileRoutesOptions) *fileRouteRegistrar {
	moduleRegistry := opts.Modules
	if moduleRegistry == nil {
		moduleRegistry = DefaultFileModuleRegistry()
	}
	dirModuleRegistry := opts.DirModules
	if dirModuleRegistry == nil {
		dirModuleRegistry = DefaultDirModuleRegistry()
	}
	return &fileRouteRegistrar{
		router:            router,
		root:              root,
		opts:              opts,
		renderFn:          opts.Render,
		moduleRegistry:    moduleRegistry,
		dirModuleRegistry: dirModuleRegistry,
		layoutCache:       make(map[string]LayoutFunc),
	}
}

func (r *fileRouteRegistrar) resolve(page FilePage) (resolvedFilePage, error) {
	module, _ := resolveFileModule(r.moduleRegistry, r.root, page)
	dirModules := resolveDirModules(r.dirModuleRegistry, r.root, page.Dir)
	layout, err := loadFileLayoutChain(r.layoutCache, page.Layouts, r.opts.Layout, r.root, r.moduleRegistry)
	if err != nil {
		return resolvedFilePage{}, err
	}
	return resolvedFilePage{
		page:       page,
		module:     module,
		dirModules: dirModules,
		layout:     layout,
	}, nil
}

func (r *fileRouteRegistrar) setNotFound(page FilePage) error {
	resolved, err := r.resolve(page)
	if err != nil {
		return err
	}
	r.router.SetNotFound(func(ctx *RouteContext) gosx.Node {
		node, err := r.renderPrepared(ctx, resolved)
		if err != nil {
			ctx.SetStatus(500)
			return defaultFileRouteError(err)
		}
		return node
	})
	r.router.notFoundLayout = resolved.layout
	return nil
}

func (r *fileRouteRegistrar) registerSpecialPages(bundle FileRoutes) error {
	if err := r.registerNotFound(bundle); err != nil {
		return err
	}
	return r.registerError(bundle)
}

func (r *fileRouteRegistrar) registerNotFound(bundle FileRoutes) error {
	if bundle.NotFound != nil {
		if err := r.setNotFound(*bundle.NotFound); err != nil {
			return err
		}
	}
	for _, scope := range bundle.NotFoundScopes {
		if err := r.addScopedNotFound(scope); err != nil {
			return err
		}
	}
	sortScopedNotFounds(r.router.notFoundScopes)
	return nil
}

func (r *fileRouteRegistrar) registerError(bundle FileRoutes) error {
	if bundle.Error == nil {
		return nil
	}
	return r.setError(*bundle.Error)
}

func sortScopedNotFounds(scopes []scopedNotFound) {
	sort.Slice(scopes, func(i, j int) bool {
		left := patternDepth(scopes[i].pattern)
		right := patternDepth(scopes[j].pattern)
		if left == right {
			return scopes[i].pattern > scopes[j].pattern
		}
		return left > right
	})
}

func (r *fileRouteRegistrar) addScopedNotFound(scope FileRouteScope) error {
	resolved, err := r.resolve(scope.Page)
	if err != nil {
		return err
	}
	r.router.notFoundScopes = append(r.router.notFoundScopes, scopedNotFound{
		pattern: scope.Pattern,
		layout:  resolved.layout,
		handler: func(ctx *RouteContext) gosx.Node {
			node, err := r.renderPrepared(ctx, resolved)
			if err != nil {
				ctx.SetStatus(500)
				return defaultFileRouteError(err)
			}
			return node
		},
	})
	return nil
}

func (r *fileRouteRegistrar) buildRoutes(pages []FilePage) ([]Route, error) {
	routes := make([]Route, 0, len(pages))
	for _, page := range pages {
		route, err := r.buildRoute(page)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	return routes, nil
}

func (r *fileRouteRegistrar) setError(page FilePage) error {
	resolved, err := r.resolve(page)
	if err != nil {
		return err
	}
	r.router.SetError(func(ctx *RouteContext, routeErr error) gosx.Node {
		if err := prepareFileRouteContext(ctx, resolved.page, resolved.module, resolved.dirModules); err != nil {
			if ctx != nil {
				ctx.SetStatus(500)
			}
			return defaultFileRouteError(err)
		}
		node, err := renderFilePage(ctx, resolved.page, resolved.module, r.renderFn)
		if err != nil {
			return defaultFileRouteError(err)
		}
		return node
	})
	r.router.errorLayout = resolved.layout
	return nil
}

func (r *fileRouteRegistrar) buildRoute(page FilePage) (Route, error) {
	resolved, err := r.resolve(page)
	if err != nil {
		return Route{}, err
	}
	routeMiddleware := append([]Middleware(nil), r.opts.Middleware...)
	routeMiddleware = append(routeMiddleware, collectDirMiddleware(resolved.dirModules)...)
	errorHandler := r.opts.ErrorHandler
	if resolved.page.ErrorPage != nil {
		errorPage, err := r.resolve(cloneFilePageValue(*resolved.page.ErrorPage))
		if err != nil {
			return Route{}, err
		}
		errorHandler = func(ctx *RouteContext, routeErr error) gosx.Node {
			if err := prepareFileRouteContext(ctx, errorPage.page, errorPage.module, errorPage.dirModules); err != nil {
				if ctx != nil {
					ctx.SetStatus(500)
				}
				return defaultFileRouteError(err)
			}
			node, err := renderFilePage(ctx, errorPage.page, errorPage.module, r.renderFn)
			if err != nil {
				return defaultFileRouteError(err)
			}
			return node
		}
	}
	if len(resolved.module.Actions) > 0 {
		r.router.Handle(filePageActionPattern(resolved.page.Pattern), buildFileActionHandler(resolved.page, resolved.module.Actions), routeMiddleware...)
	}
	return Route{
		Pattern:      resolved.page.Pattern,
		Layout:       wrapWithDefaultLayout(r.router, resolved.layout),
		Middleware:   routeMiddleware,
		ErrorHandler: errorHandler,
		DataLoader:   filePageDataLoader(resolved.page, resolved.module, resolved.dirModules),
		Handler: func(ctx *RouteContext) gosx.Node {
			node, err := renderFilePage(ctx, resolved.page, resolved.module, r.renderFn)
			if err != nil {
				panic(err)
			}
			return node
		},
	}, nil
}

func (r *fileRouteRegistrar) renderPrepared(ctx *RouteContext, resolved resolvedFilePage) (gosx.Node, error) {
	if err := prepareFileRouteContext(ctx, resolved.page, resolved.module, resolved.dirModules); err != nil {
		return gosx.Node{}, err
	}
	return renderFilePage(ctx, resolved.page, resolved.module, r.renderFn)
}

// DefaultFileRenderer renders `.gsx` and `.html` page files directly.
func DefaultFileRenderer(ctx *RouteContext, page FilePage) (gosx.Node, error) {
	return renderFileNode(page.FilePath, fileRenderOptions{
		EvalEnv: newFileRenderEnv(ctx, page),
	})
}

func filePageRenderEnv(ctx *RouteContext, page FilePage, module FileModule) fileRenderEnv {
	env := newFileRenderEnv(ctx, page)
	if module.Bindings == nil {
		return env
	}
	return env.withBindings(module.Bindings(ctx, page, ctx.Data))
}

func prepareFileRouteContext(ctx *RouteContext, page FilePage, module FileModule, dirModules []DirModule) error {
	if err := applyFileRouteConfig(ctx, page.Config); err != nil {
		return err
	}
	if err := applyDirModules(ctx, page, dirModules); err != nil {
		return err
	}
	if module.Load != nil {
		data, err := module.Load(ctx, page)
		if err != nil {
			return err
		}
		ctx.Data = data
	}
	return nil
}

func renderFilePage(ctx *RouteContext, page FilePage, module FileModule, renderFn FileRenderFunc) (gosx.Node, error) {
	addRouteFileCSSHead(ctx, page)
	addFileMetadata(ctx, page.Layouts...)
	addFileMetadata(ctx, page.FilePath)
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
	if renderFn == nil {
		return renderFileNode(page.FilePath, fileRenderOptions{
			EvalEnv: filePageRenderEnv(ctx, page, module),
		})
	}
	return renderFn(ctx, page)
}

func filePageDataLoader(page FilePage, module FileModule, dirModules []DirModule) DataLoader {
	return func(ctx *RouteContext) (any, error) {
		if err := prepareFileRouteContext(ctx, page, module, dirModules); err != nil {
			return nil, err
		}
		return ctx.Data, nil
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
		return FilePage{Root: root, FilePath: path, Source: slashRel, Dir: dir, RoutePath: scopeRoutePath, Pattern: scopePattern, Params: scopeParams}, "not_found", nil
	case "error":
		return FilePage{Root: root, FilePath: path, Source: slashRel, Dir: dir, RoutePath: scopeRoutePath, Pattern: scopePattern, Params: scopeParams}, "error", nil
	case "layout":
		return FilePage{Root: root, FilePath: path, Source: slashRel, Dir: dir, RoutePath: scopeRoutePath, Pattern: scopePattern, Params: scopeParams}, "layout", nil
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
		Root:      root,
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
		segment, param, include := fileRouteSegment(part)
		if !include {
			continue
		}
		if param != "" {
			params = append(params, param)
		}
		routeSegments = append(routeSegments, segment)
	}

	if len(routeSegments) == 0 {
		return "/", "/", params
	}
	pattern := "/" + strings.Join(routeSegments, "/")
	return pattern, pattern, params
}

func fileRouteSegment(part string) (segment string, param string, include bool) {
	if part == "" || isRouteGroupSegment(part) {
		return "", "", false
	}
	switch {
	case strings.HasPrefix(part, "[...") && strings.HasSuffix(part, "]"):
		name := strings.TrimSuffix(strings.TrimPrefix(part, "[..."), "]")
		return "{" + name + "...}", name, true
	case strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]"):
		name := strings.TrimSuffix(strings.TrimPrefix(part, "["), "]")
		return "{" + name + "}", name, true
	default:
		return part, "", true
	}
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
		page.Config = collectFileRouteConfig(page.Dir, dirs)
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
	page.Config = cloneFileRouteConfig(page.Config)
	if page.ErrorPage != nil {
		clone := cloneFilePageValue(*page.ErrorPage)
		page.ErrorPage = &clone
	}
	return page
}

func loadFileLayoutChain(cache map[string]LayoutFunc, files []string, extra LayoutFunc, root string, registry *FileModuleRegistry) (LayoutFunc, error) {
	layouts := []LayoutFunc{}
	if extra != nil {
		layouts = append(layouts, extra)
	}
	for _, file := range files {
		layout, ok := cache[file]
		if !ok {
			loaded, err := loadBoundFileLayout(file, root, registry)
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

func loadBoundFileLayout(file, root string, registry *FileModuleRegistry) (LayoutFunc, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", file, err)
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("stat %s: %w", abs, err)
	}
	page := layoutFilePage(root, abs)
	module := resolveLayoutModule(registry, root, abs)
	return buildFileLayout(abs, page, module, FileLayoutOptions{}), nil
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
