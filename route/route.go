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

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/action"
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
	handlerErr error
	server.PageState
}

// SetHandlerError records an error to be dispatched through the error handler
// after the handler returns. This replaces panic-based error propagation.
func (ctx *RouteContext) SetHandlerError(err error) {
	ctx.handlerErr = err
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

// Form renders a form tag opted into the GoSX navigation/runtime submission
// layer while preserving native HTML fallback behavior.
func (ctx *RouteContext) Form(args ...any) gosx.Node {
	return server.Form(args...)
}

// ActionForm renders a POST form targeting the current route's named action.
func (ctx *RouteContext) ActionForm(name string, args ...any) gosx.Node {
	prefixed := append([]any{
		gosx.Attrs(
			gosx.Attr("method", strings.ToLower(http.MethodPost)),
			gosx.Attr("action", ctx.ActionPath(name)),
			gosx.Attr(server.NavigationFormModeAttr, "post"),
		),
	}, args...)
	return server.Form(prefixed...)
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
		safeHandle(mux, normalizePattern(extra.pattern), http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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

		safeHandle(mux, matchPattern, h)
	}

	for _, child := range route.Children {
		r.registerRoute(mux, pattern, child, layouts, middleware, errorHandler, errorLayout)
	}
}

func (r *Router) buildHandler(pattern string, route Route, layouts []LayoutFunc, errorHandler ErrorHandler, errorLayout LayoutFunc) http.HandlerFunc {
	// Param names depend only on the pattern; resolve them once at build
	// time so we don't re-parse the pattern string on every request.
	paramNames := patternParamNames(pattern)

	return func(w http.ResponseWriter, req *http.Request) {
		server.MarkObservedRequest(req, "page", pattern)
		ctx := newRouteContext(req)
		ctx.Params = extractParamsByNames(req, paramNames)

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
		if ctx.handlerErr != nil {
			r.renderError(w, ctx, layouts, errorHandler, errorLayout, ctx.handlerErr, pattern)
			return
		}
		r.renderPage(w, ctx, layouts, node, http.StatusOK)
	}
}

func (r *Router) renderPage(w http.ResponseWriter, ctx *RouteContext, layouts []LayoutFunc, node gosx.Node, defaultStatus int) {
	if ctx.StatusCode() == 0 {
		ctx.SetStatus(defaultStatus)
	}
	if runtime := ctx.RuntimeState(); runtime != nil {
		ctx.AddHead(runtime.Head())
	}

	for i := len(layouts) - 1; i >= 0; i-- {
		node = layouts[i](ctx, node)
	}

	headers := ctx.Header()
	status := ctx.StatusCode()
	cache := ctx.CacheState()
	if server.ApplyCacheHeaders(ctx.Request, headers, status, cache, r.Revalidator()) {
		server.WriteNotModified(w, headers)
		return
	}

	server.WriteHTML(w, server.HTMLResponse{
		Status:   status,
		Headers:  headers,
		Node:     node,
		Deferred: ctx.DeferredRegistry(),
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
		ctx.SetMetadata(server.Metadata{Title: server.Title{Absolute: "Not Found"}})
		node = defaultStatusBody("Page not found", "The requested page could not be found.")
	}

	r.renderPage(w, ctx, layouts, node, http.StatusNotFound)
}

func (r *Router) renderError(w http.ResponseWriter, ctx *RouteContext, layouts []LayoutFunc, errorHandler ErrorHandler, errorLayout LayoutFunc, err error, pattern string) {
	if ctx == nil {
		ctx = newRouteContext(nil)
	}
	server.MarkObservedRequest(ctx.Request, "error", pattern)
	if ctx.StatusCode() == 0 {
		ctx.SetStatus(http.StatusInternalServerError)
	}

	var node gosx.Node
	if errorHandler != nil {
		node = errorHandler(ctx, err)
	} else {
		title := http.StatusText(ctx.StatusCode())
		if title == "" {
			title = "Server Error"
		}
		ctx.SetMetadata(server.Metadata{Title: server.Title{Absolute: title}})
		node = defaultStatusBody(title, defaultErrorMessage(err, pattern))
	}

	if errorLayout != nil {
		layouts = append(append([]LayoutFunc(nil), layouts...), errorLayout)
	}
	r.renderPage(w, ctx, layouts, node, ctx.StatusCode())
}

// extractParamsByNames pulls path values for the given pre-computed param
// names. Returns nil when the route has no parameters at all — reads from
// a nil string map in Go return the zero value, so handlers calling
// ctx.Param("x") still work without a per-request map allocation.
func extractParamsByNames(req *http.Request, names []string) map[string]string {
	if len(names) == 0 {
		return nil
	}
	params := make(map[string]string, len(names))
	for _, name := range names {
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
	// Params is left nil; reads from a nil map are valid and the build-time
	// closure assigns a sized map only when the route declares parameters.
	return &RouteContext{
		Request:   req,
		PageState: *server.NewPageState(),
	}
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

// safeHandle registers a handler on the mux, recovering panics from
// conflicting patterns and re-panicking with a clear diagnostic message.
// Go 1.22+'s ServeMux panics when two patterns overlap without one being
// strictly more specific. This commonly happens when a page has actions
// (e.g. POST /blog/__actions/{action}) and a sibling [slug] directory
// creates a wildcard route (e.g. /blog/{slug}/...) that also matches
// the __actions segment.
func safeHandle(mux *http.ServeMux, pattern string, handler http.Handler) {
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprint(r)
			if strings.Contains(msg, "conflicts with") || strings.Contains(msg, "already registered") {
				panic(fmt.Sprintf(
					"gosx: route conflict registering %q: %s\n\n"+
						"This typically happens when a page with server actions sits next to a [param]\n"+
						"directory. The __actions sub-path collides with the wildcard segment.\n\n"+
						"To fix: move the page or its actions so that __actions doesn't share a path\n"+
						"level with a dynamic [param] segment. For example, nest the dynamic routes\n"+
						"under a sub-directory (e.g. /blog/posts/[slug] instead of /blog/[slug]).",
					pattern, msg,
				))
			}
			panic(r)
		}
	}()
	mux.Handle(pattern, handler)
}
