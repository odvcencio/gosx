// Package route provides declarative server routing with layouts for GoSX apps.
//
// Routes map URL patterns to component handlers. Layouts wrap pages with
// shared UI. Nested layouts compose from outermost to innermost.
package route

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/action"
	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/server"
)

// Route defines a URL pattern → component mapping.
type Route struct {
	// Pattern is the URL path pattern (e.g., "/", "/dashboard", "/users/{id}").
	Pattern string

	// Handler renders the page component.
	Handler PageHandler

	// Layout wraps the page output (optional, overrides app-level layout).
	Layout LayoutFunc

	// Middleware runs before the handler.
	Middleware []Middleware

	// Children are nested routes under this pattern.
	Children []Route

	// DataLoader fetches data before rendering (optional).
	DataLoader DataLoader

	// ErrorHandler renders a route-scoped 500 page when the handler panics or the
	// data loader returns an error.
	ErrorHandler ErrorHandler
}

// PageHandler renders a page, receiving route context.
type PageHandler func(ctx *RouteContext) gosx.Node

// LayoutFunc wraps page content with shared layout.
type LayoutFunc func(ctx *RouteContext, content gosx.Node) gosx.Node

// Middleware runs before page handling.
type Middleware func(next http.Handler) http.Handler

// DataLoader fetches data for a route before rendering.
type DataLoader func(ctx *RouteContext) (any, error)

// ErrorHandler renders a route error page.
type ErrorHandler func(ctx *RouteContext, err error) gosx.Node

// ErrNotFound marks a loader failure that should render the router's 404 flow
// instead of the route error handler.
var ErrNotFound = errors.New("route not found")

type notFoundError struct {
	message string
}

func (err notFoundError) Error() string {
	if strings.TrimSpace(err.message) == "" {
		return ErrNotFound.Error()
	}
	return err.message
}

func (err notFoundError) Unwrap() error {
	return ErrNotFound
}

// NotFound returns an error that instructs the router to render the not-found
// page for the current request path.
func NotFound(message string) error {
	return notFoundError{message: message}
}

// IsNotFound reports whether err should be treated as a route-level 404.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// RouteContext provides request context to handlers.
type RouteContext struct {
	Request    *http.Request
	Params     map[string]string
	Data       any
	parentData map[string]any
	status     int
	headers    http.Header
	metadata   server.Metadata
	head       []gosx.Node
	deferred   *server.DeferredRegistry
	cache      *server.CacheState
	runtime    *server.PageRuntime
}

// Param returns a URL path parameter.
func (ctx *RouteContext) Param(name string) string {
	return ctx.Params[name]
}

// Query returns a URL query parameter.
func (ctx *RouteContext) Query(name string) string {
	return ctx.Request.URL.Query().Get(name)
}

// ActionPath returns the current page-relative action endpoint for the given
// action name.
func (ctx *RouteContext) ActionPath(name string) string {
	if ctx == nil || strings.TrimSpace(name) == "" {
		return ""
	}
	base := "/"
	if ctx.Request != nil && ctx.Request.URL != nil && ctx.Request.URL.Path != "" {
		base = ctx.Request.URL.Path
	}
	if base == "/" {
		return "/__actions/" + name
	}
	return strings.TrimSuffix(base, "/") + "/__actions/" + name
}

// ActionState returns the flashed state for a named browser action.
func (ctx *RouteContext) ActionState(name string) (action.View, bool) {
	if ctx == nil {
		return action.View{}, false
	}
	return action.State(ctx.Request, name)
}

// ActionStates returns all flashed action states for the current request.
func (ctx *RouteContext) ActionStates() map[string]action.View {
	if ctx == nil {
		return map[string]action.View{}
	}
	return action.States(ctx.Request)
}

// ParentData returns data loaded by a parent route's DataLoader.
func (ctx *RouteContext) ParentData(key string) any {
	if ctx.parentData == nil {
		return nil
	}
	return ctx.parentData[key]
}

// Header returns response headers to apply after rendering.
func (ctx *RouteContext) Header() http.Header {
	if ctx.headers == nil {
		ctx.headers = make(http.Header)
	}
	return ctx.headers
}

// SetStatus sets the HTTP status code for the response.
func (ctx *RouteContext) SetStatus(status int) {
	ctx.status = status
}

// Cache stores HTTP caching directives for the response.
func (ctx *RouteContext) Cache(policy server.CachePolicy) {
	if ctx == nil {
		return
	}
	if ctx.cache == nil {
		ctx.cache = server.NewCacheState()
	}
	ctx.cache.SetPolicy(policy)
}

// ApplyCacheProfile applies a higher-level cache profile to the response.
func (ctx *RouteContext) ApplyCacheProfile(profile server.CacheProfile) {
	server.ApplyCacheProfile(ctx, profile)
}

// CachePublic marks the response as publicly cacheable for the provided duration.
func (ctx *RouteContext) CachePublic(maxAge time.Duration) {
	ctx.Cache(server.PublicCache(maxAge))
}

// CachePrivate marks the response as privately cacheable for the provided duration.
func (ctx *RouteContext) CachePrivate(maxAge time.Duration) {
	ctx.Cache(server.PrivateCache(maxAge))
}

// NoStore disables response storage by caches.
func (ctx *RouteContext) NoStore() {
	ctx.Cache(server.NoStoreCache())
}

// CacheDynamic disables storage for fully dynamic responses.
func (ctx *RouteContext) CacheDynamic() {
	ctx.ApplyCacheProfile(server.DynamicPage())
}

// CacheStatic marks the response as immutable and publicly cacheable.
func (ctx *RouteContext) CacheStatic(tags ...string) {
	ctx.ApplyCacheProfile(server.StaticPage(tags...))
}

// CacheRevalidate marks a page as publicly cacheable with revalidation.
func (ctx *RouteContext) CacheRevalidate(maxAge, staleWhileRevalidate time.Duration, tags ...string) {
	ctx.ApplyCacheProfile(server.RevalidatePage(maxAge, staleWhileRevalidate, tags...))
}

// CacheData marks shared data as publicly cacheable.
func (ctx *RouteContext) CacheData(maxAge time.Duration, tags ...string) {
	ctx.ApplyCacheProfile(server.PublicData(maxAge, tags...))
}

// CachePrivateData marks user-scoped data as privately cacheable.
func (ctx *RouteContext) CachePrivateData(maxAge time.Duration, tags ...string) {
	ctx.ApplyCacheProfile(server.PrivateData(maxAge, tags...))
}

// CacheTag associates one or more revalidation tags with the response.
func (ctx *RouteContext) CacheTag(tags ...string) {
	if ctx == nil {
		return
	}
	if ctx.cache == nil {
		ctx.cache = server.NewCacheState()
	}
	ctx.cache.AddTags(tags...)
}

// CacheKey appends cache key dimensions used when deriving automatic ETags.
func (ctx *RouteContext) CacheKey(parts ...string) {
	if ctx == nil {
		return
	}
	if ctx.cache == nil {
		ctx.cache = server.NewCacheState()
	}
	ctx.cache.AddKeys(parts...)
}

// SetETag overrides the automatically derived ETag for the response.
func (ctx *RouteContext) SetETag(etag string) {
	if ctx == nil {
		return
	}
	if ctx.cache == nil {
		ctx.cache = server.NewCacheState()
	}
	ctx.cache.SetETag(etag)
}

// SetLastModified sets the resource modification timestamp for conditional requests.
func (ctx *RouteContext) SetLastModified(at time.Time) {
	if ctx == nil {
		return
	}
	if ctx.cache == nil {
		ctx.cache = server.NewCacheState()
	}
	ctx.cache.SetLastModified(at)
}

// SetMetadata merges page metadata into the route context.
func (ctx *RouteContext) SetMetadata(meta server.Metadata) {
	ctx.metadata = mergeMetadata(ctx.metadata, meta)
}

// AddHead appends arbitrary head nodes for layouts to render.
func (ctx *RouteContext) AddHead(nodes ...gosx.Node) {
	for _, node := range nodes {
		if node.IsZero() {
			continue
		}
		ctx.head = append(ctx.head, node)
	}
}

// Runtime returns the page-scoped runtime registry for client engines.
func (ctx *RouteContext) Runtime() *server.PageRuntime {
	if ctx == nil {
		return nil
	}
	if ctx.runtime == nil {
		ctx.runtime = server.NewPageRuntime()
	}
	return ctx.runtime
}

// Engine registers a client engine for this page and returns its mount shell.
func (ctx *RouteContext) Engine(cfg engine.Config, fallback gosx.Node) gosx.Node {
	if ctx == nil {
		return fallback
	}
	return ctx.Runtime().Engine(cfg, fallback)
}

// Defer renders fallback content immediately, then streams the resolved node
// into place once the resolver finishes.
func (ctx *RouteContext) Defer(fallback gosx.Node, resolve server.DeferredResolver) gosx.Node {
	return ctx.DeferWithOptions(server.DeferredOptions{}, fallback, resolve)
}

// DeferWithOptions renders fallback content immediately, then streams the
// resolved node into place once the resolver finishes.
func (ctx *RouteContext) DeferWithOptions(opts server.DeferredOptions, fallback gosx.Node, resolve server.DeferredResolver) gosx.Node {
	if ctx.deferred == nil {
		ctx.deferred = server.NewDeferredRegistry()
	}
	return ctx.deferred.DeferWithOptions(opts, fallback, resolve)
}

// Head returns the merged metadata/head node tree for the current request.
func (ctx *RouteContext) Head() gosx.Node {
	nodes := []gosx.Node{}
	if metaHead := ctx.metadata.Head(); !metaHead.IsZero() {
		nodes = append(nodes, metaHead)
	}
	nodes = append(nodes, ctx.head...)
	if len(nodes) == 0 {
		return gosx.Text("")
	}
	return gosx.Fragment(nodes...)
}

// Title returns the current metadata title or a default fallback.
func (ctx *RouteContext) Title(fallback string) string {
	if ctx.metadata.Title != "" {
		return ctx.metadata.Title
	}
	return fallback
}

// Router builds an http.Handler from a route tree.
type Router struct {
	routes         []Route
	handlers       []handlerRoute
	defaultLayout  LayoutFunc
	notFound       PageHandler
	notFoundLayout LayoutFunc
	notFoundScopes []scopedNotFound
	errorHandler   ErrorHandler
	errorLayout    LayoutFunc
	revalidator    *server.Revalidator
	observers      []server.RequestObserver
}

type handlerRoute struct {
	pattern    string
	handler    http.Handler
	middleware []Middleware
}

type scopedNotFound struct {
	pattern string
	layout  LayoutFunc
	handler PageHandler
}

// NewRouter creates a new router.
func NewRouter() *Router {
	return &Router{revalidator: server.NewRevalidator()}
}

// SetRevalidator replaces the router-wide in-memory revalidator used for
// automatic ETags and explicit path/tag invalidation.
func (r *Router) SetRevalidator(revalidator *server.Revalidator) {
	if revalidator == nil {
		revalidator = server.NewRevalidator()
	}
	r.revalidator = revalidator
}

// Revalidator returns the router-wide in-memory revalidator.
func (r *Router) Revalidator() *server.Revalidator {
	if r.revalidator == nil {
		r.revalidator = server.NewRevalidator()
	}
	return r.revalidator
}

// RevalidatePath invalidates cache validators for the provided path prefix.
func (r *Router) RevalidatePath(target string) uint64 {
	return r.Revalidator().RevalidatePath(target)
}

// RevalidateTag invalidates cache validators for the provided tag.
func (r *Router) RevalidateTag(tag string) uint64 {
	return r.Revalidator().RevalidateTag(tag)
}

// UseObserver appends a request observer to the supported router extension surface.
func (r *Router) UseObserver(observer server.RequestObserver) {
	if observer == nil {
		return
	}
	r.observers = append(r.observers, observer)
}

// SetLayout sets the default layout for all routes.
func (r *Router) SetLayout(layout LayoutFunc) {
	r.defaultLayout = layout
}

// SetNotFound sets the 404 handler.
func (r *Router) SetNotFound(handler PageHandler) {
	r.notFound = handler
}

// SetError sets the default 500 handler.
func (r *Router) SetError(handler ErrorHandler) {
	r.errorHandler = handler
}

// Add registers routes.
func (r *Router) Add(routes ...Route) {
	r.routes = append(r.routes, routes...)
}

// Handle registers a raw HTTP handler alongside page routes.
func (r *Router) Handle(pattern string, handler http.Handler, middleware ...Middleware) {
	if strings.TrimSpace(pattern) == "" || handler == nil {
		return
	}
	r.handlers = append(r.handlers, handlerRoute{
		pattern:    pattern,
		handler:    handler,
		middleware: append([]Middleware(nil), middleware...),
	})
}

// Build compiles the router into an http.Handler.
func (r *Router) Build() http.Handler {
	mux := http.NewServeMux()
	for _, extra := range r.handlers {
		var h http.Handler = extra.handler
		for i := len(extra.middleware) - 1; i >= 0; i-- {
			if extra.middleware[i] == nil {
				continue
			}
			h = extra.middleware[i](h)
		}
		pattern := extra.pattern
		handler := h
		mux.Handle(normalizePattern(extra.pattern), http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			server.MarkObservedRequest(req, "mount", pattern)
			handler.ServeHTTP(w, req)
		}))
	}
	for _, route := range r.routes {
		r.registerRoute(mux, "", route, nil, nil, r.errorHandler, r.errorLayout)
	}

	root := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if _, pattern := mux.Handler(req); pattern != "" {
			mux.ServeHTTP(w, req)
			return
		}

		rec := &interceptResponseWriter{header: make(http.Header)}
		mux.ServeHTTP(rec, req)
		if rec.statusCode != 0 && rec.statusCode != http.StatusNotFound {
			rec.commit(w)
			return
		}

		r.renderNotFound(w, req)
	})
	if len(r.observers) > 0 {
		return server.ObserveHandler(root, append([]server.RequestObserver(nil), r.observers...))
	}
	return root
}

func (r *Router) registerRoute(mux *http.ServeMux, prefix string, route Route, parentLayouts []LayoutFunc, parentMiddleware []Middleware, parentError ErrorHandler, parentErrorLayout LayoutFunc) {
	pattern := joinPattern(prefix, route.Pattern)
	matchPattern := normalizePattern(pattern)

	layouts := make([]LayoutFunc, len(parentLayouts))
	copy(layouts, parentLayouts)
	if route.Layout != nil {
		layouts = append(layouts, route.Layout)
	} else if len(parentLayouts) == 0 && r.defaultLayout != nil {
		layouts = append(layouts, r.defaultLayout)
	}

	middleware := make([]Middleware, len(parentMiddleware))
	copy(middleware, parentMiddleware)
	middleware = append(middleware, route.Middleware...)

	errorHandler := parentError
	errorLayout := parentErrorLayout
	if route.ErrorHandler != nil {
		errorHandler = route.ErrorHandler
		errorLayout = nil
	}

	if route.Handler != nil {
		handler := r.buildHandler(pattern, route, layouts, errorHandler, errorLayout)

		var h http.Handler = handler
		for i := len(middleware) - 1; i >= 0; i-- {
			h = middleware[i](h)
		}

		mux.Handle(matchPattern, h)
	}

	for _, child := range route.Children {
		r.registerRoute(mux, pattern, child, layouts, middleware, errorHandler, errorLayout)
	}
}

func (r *Router) buildHandler(pattern string, route Route, layouts []LayoutFunc, errorHandler ErrorHandler, errorLayout LayoutFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		server.MarkObservedRequest(req, "page", pattern)
		ctx := newRouteContext(req)
		ctx.Params = extractParams(req, pattern)

		defer func() {
			if recovered := recover(); recovered != nil {
				r.renderError(w, ctx, layouts, errorHandler, errorLayout, panicError(recovered), pattern)
			}
		}()

		if route.DataLoader != nil {
			data, err := route.DataLoader(ctx)
			if err != nil {
				if IsNotFound(err) {
					r.renderNotFound(w, req)
					return
				}
				r.renderError(w, ctx, layouts, errorHandler, errorLayout, err, pattern)
				return
			}
			ctx.Data = data
		}

		node := route.Handler(ctx)
		r.renderPage(w, ctx, layouts, node, http.StatusOK)
	}
}

func (r *Router) renderPage(w http.ResponseWriter, ctx *RouteContext, layouts []LayoutFunc, node gosx.Node, defaultStatus int) {
	if ctx.status == 0 {
		ctx.status = defaultStatus
	}
	if ctx.runtime != nil {
		ctx.AddHead(ctx.runtime.Head())
	}

	for i := len(layouts) - 1; i >= 0; i-- {
		node = layouts[i](ctx, node)
	}

	if server.ApplyCacheHeaders(ctx.Request, ctx.headers, ctx.status, ctx.cache, r.Revalidator()) {
		server.WriteNotModified(w, ctx.headers)
		return
	}

	server.WriteHTML(w, server.HTMLResponse{
		Status:   ctx.status,
		Headers:  ctx.headers,
		Node:     node,
		Deferred: ctx.deferred,
	})
}

func (r *Router) renderNotFound(w http.ResponseWriter, req *http.Request) {
	server.MarkObservedRequest(req, "not_found", "")
	ctx := newRouteContext(req)
	ctx.SetStatus(http.StatusNotFound)

	layouts := []LayoutFunc{}
	if r.defaultLayout != nil {
		layouts = append(layouts, r.defaultLayout)
	}

	var node gosx.Node
	rootNotFoundUsed := false
	if scope, ok := r.matchNotFoundScope(req); ok {
		ctx.Params = extractPatternParams(scope.pattern, req.URL.Path)
		if scope.handler != nil {
			node = scope.handler(ctx)
		}
		if scope.layout != nil {
			layouts = append(layouts, scope.layout)
		}
	}
	if r.notFound != nil {
		if node.IsZero() {
			node = r.notFound(ctx)
			rootNotFoundUsed = true
		}
		if rootNotFoundUsed && r.notFoundLayout != nil {
			layouts = append(layouts, r.notFoundLayout)
		}
	}
	if node.IsZero() {
		ctx.SetMetadata(server.Metadata{Title: "Not Found"})
		node = defaultStatusBody("Page not found", "The requested page could not be found.")
	}

	r.renderPage(w, ctx, layouts, node, http.StatusNotFound)
}

func (r *Router) renderError(w http.ResponseWriter, ctx *RouteContext, layouts []LayoutFunc, errorHandler ErrorHandler, errorLayout LayoutFunc, err error, pattern string) {
	if ctx == nil {
		ctx = newRouteContext(nil)
	}
	server.MarkObservedRequest(ctx.Request, "error", pattern)
	if ctx.status == 0 {
		ctx.status = http.StatusInternalServerError
	}

	var node gosx.Node
	if errorHandler != nil {
		node = errorHandler(ctx, err)
	} else {
		title := http.StatusText(ctx.status)
		if title == "" {
			title = "Server Error"
		}
		ctx.SetMetadata(server.Metadata{Title: title})
		node = defaultStatusBody(title, defaultErrorMessage(err, pattern))
	}

	if errorLayout != nil {
		layouts = append(append([]LayoutFunc(nil), layouts...), errorLayout)
	}
	r.renderPage(w, ctx, layouts, node, ctx.status)
}

func extractParams(req *http.Request, pattern string) map[string]string {
	params := make(map[string]string)
	for _, name := range patternParamNames(pattern) {
		if value := req.PathValue(name); value != "" {
			params[name] = value
		}
	}
	return params
}

func extractPatternParams(pattern string, requestPath string) map[string]string {
	patternSegs := patternSegments(pattern)
	pathSegs := cleanPathSegments(requestPath)
	params := make(map[string]string)
	pathIndex := 0

	for _, segment := range patternSegs {
		if isCatchAllPatternSegment(segment) {
			name := strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "...}")
			params[name] = strings.Join(pathSegs[pathIndex:], "/")
			return params
		}
		if pathIndex >= len(pathSegs) {
			return params
		}
		if name, ok := patternSegmentName(segment); ok {
			params[name] = pathSegs[pathIndex]
		}
		pathIndex++
	}
	return params
}

func newRouteContext(req *http.Request) *RouteContext {
	return &RouteContext{
		Request:  req,
		Params:   make(map[string]string),
		headers:  make(http.Header),
		deferred: server.NewDeferredRegistry(),
		cache:    server.NewCacheState(),
	}
}

func mergeMetadata(base, extra server.Metadata) server.Metadata {
	if extra.Title != "" {
		base.Title = extra.Title
	}
	if extra.Description != "" {
		base.Description = extra.Description
	}
	if extra.Canonical != "" {
		base.Canonical = extra.Canonical
	}
	if len(extra.Meta) > 0 {
		base.Meta = append(base.Meta, extra.Meta...)
	}
	if len(extra.Links) > 0 {
		base.Links = append(base.Links, extra.Links...)
	}
	return base
}

func joinPattern(prefix, pattern string) string {
	if prefix == "" {
		return pattern
	}
	return prefix + pattern
}

func normalizePattern(pattern string) string {
	fields := strings.Fields(pattern)
	if len(fields) == 1 && fields[0] == "/" {
		return "/{$}"
	}
	if len(fields) == 2 && fields[1] == "/" {
		return fields[0] + " /{$}"
	}
	return strings.TrimSpace(pattern)
}

func patternParamNames(pattern string) []string {
	pattern = patternPath(pattern)

	names := []string{}
	seen := make(map[string]struct{})
	for {
		start := strings.IndexByte(pattern, '{')
		if start < 0 {
			return names
		}
		pattern = pattern[start+1:]
		end := strings.IndexByte(pattern, '}')
		if end < 0 {
			return names
		}
		name := pattern[:end]
		pattern = pattern[end+1:]
		name = strings.TrimSuffix(name, "...")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
}

func patternPath(pattern string) string {
	fields := strings.Fields(strings.TrimSpace(pattern))
	if len(fields) == 2 {
		return fields[1]
	}
	return strings.TrimSpace(pattern)
}

func patternSegments(pattern string) []string {
	pattern = strings.Trim(patternPath(pattern), "/")
	if pattern == "" {
		return nil
	}
	return strings.Split(pattern, "/")
}

func cleanPathSegments(requestPath string) []string {
	cleaned := cleanRequestPath(requestPath)
	if cleaned == "/" {
		return nil
	}
	return strings.Split(strings.Trim(cleaned, "/"), "/")
}

func cleanRequestPath(requestPath string) string {
	requestPath = strings.TrimSpace(requestPath)
	if requestPath == "" {
		return "/"
	}
	cleaned := path.Clean(requestPath)
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	return cleaned
}

func patternSegmentName(segment string) (string, bool) {
	if !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}")
	name = strings.TrimSuffix(name, "...")
	if name == "" {
		return "", false
	}
	return name, true
}

func isCatchAllPatternSegment(segment string) bool {
	return strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "...}")
}

func matchPatternPrefix(pattern string, requestPath string) bool {
	patternSegs := patternSegments(pattern)
	pathSegs := cleanPathSegments(requestPath)
	if len(patternSegs) == 0 {
		return true
	}
	pathIndex := 0
	for _, segment := range patternSegs {
		if isCatchAllPatternSegment(segment) {
			return true
		}
		if pathIndex >= len(pathSegs) {
			return false
		}
		if name, ok := patternSegmentName(segment); ok && name != "" {
			pathIndex++
			continue
		}
		if segment != pathSegs[pathIndex] {
			return false
		}
		pathIndex++
	}
	return true
}

func (r *Router) matchNotFoundScope(req *http.Request) (scopedNotFound, bool) {
	if req == nil || req.URL == nil {
		return scopedNotFound{}, false
	}
	requestPath := req.URL.Path
	for _, scope := range r.notFoundScopes {
		if !matchPatternPrefix(scope.pattern, requestPath) {
			continue
		}
		return scope, true
	}
	return scopedNotFound{}, false
}

func panicError(recovered any) error {
	if err, ok := recovered.(error); ok {
		return err
	}
	return fmt.Errorf("%v", recovered)
}

func defaultStatusBody(title string, message string) gosx.Node {
	return gosx.El("main", gosx.Attrs(gosx.Attr("style", "font-family: sans-serif; max-width: 40rem; margin: 4rem auto; padding: 0 1.5rem; line-height: 1.5")),
		gosx.El("h1", gosx.Text(title)),
		gosx.El("p", gosx.Text(message)),
	)
}

func defaultErrorMessage(err error, pattern string) string {
	if err == nil {
		return "The server encountered an unexpected error."
	}
	if pattern == "" {
		return err.Error()
	}
	return fmt.Sprintf("%s (%s)", err.Error(), pattern)
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

type interceptResponseWriter struct {
	header     http.Header
	body       strings.Builder
	statusCode int
}

func (w *interceptResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *interceptResponseWriter) Write(data []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.body.Write(data)
}

func (w *interceptResponseWriter) WriteHeader(status int) {
	w.statusCode = status
}

func (w *interceptResponseWriter) commit(dst http.ResponseWriter) {
	copyHeaders(dst.Header(), w.header)
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	dst.WriteHeader(w.statusCode)
	_, _ = dst.Write([]byte(w.body.String()))
}
