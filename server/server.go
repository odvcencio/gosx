// Package server provides the GoSX HTTP server runtime.
//
// The server renders GoSX components to HTML on each request.
// Every component renders on the server by default; no client runtime
// is required unless interactivity is explicitly requested.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/odvcencio/gosx"
)

type Middleware func(http.Handler) http.Handler

type contextKey string

const requestIDHeader = "X-Request-ID"

const requestIDContextKey contextKey = "gosx.request_id"

var requestSeq uint64

type registeredPageRoute struct {
	pattern    string
	handler    PageHandler
	middleware []Middleware
}

type registeredAPIRoute struct {
	pattern    string
	handler    APIHandler
	middleware []Middleware
}

type registeredRedirectRoute struct {
	pattern     string
	destination string
	status      int
}

type registeredRewriteRoute struct {
	pattern     string
	destination string
}

type registeredMountedRoute struct {
	pattern string
	handler http.Handler
}

type statusCoder interface {
	StatusCode() int
}

// App is the GoSX server application.
type App struct {
	pageRoutes  map[string]registeredPageRoute
	apiRoutes   map[string]registeredAPIRoute
	layout      func(title string, body gosx.Node) gosx.Node
	document    DocumentFunc
	mux         *http.ServeMux
	middleware  []Middleware
	notFound    PageHandler
	errorPage   ErrorHandler
	publicDir   string
	imageDir    string
	runtimeRoot string
	runtimeMeta *runtimeManifestCache
	isr         *isrConfig
	isrStore    ISRStore
	navigation  bool
	observers   []RequestObserver
	readyChecks []namedReadyCheck
	redirects   map[string]registeredRedirectRoute
	rewrites    map[string]registeredRewriteRoute
	mounts      map[string]registeredMountedRoute
	revalidator *Revalidator
	operations  []OperationObserver
}

// New creates a new GoSX server app.
func New() *App {
	app := &App{
		pageRoutes:  make(map[string]registeredPageRoute),
		apiRoutes:   make(map[string]registeredAPIRoute),
		redirects:   make(map[string]registeredRedirectRoute),
		rewrites:    make(map[string]registeredRewriteRoute),
		mounts:      make(map[string]registeredMountedRoute),
		mux:         http.NewServeMux(),
		publicDir:   "public",
		revalidator: NewRevalidator(),
	}
	app.middleware = []Middleware{
		requestIDMiddleware(),
		securityHeadersMiddleware(),
		recoveryMiddleware(),
	}
	return app
}

// SetLayout sets a legacy layout wrapper that receives the page title and body.
// If SetDocument is also configured, the document renderer takes precedence.
func (a *App) SetLayout(layout func(title string, body gosx.Node) gosx.Node) {
	a.layout = layout
}

// SetDocument sets a full-document renderer for page responses.
func (a *App) SetDocument(document DocumentFunc) {
	a.document = document
}

// SetNotFound sets the 404 page handler.
func (a *App) SetNotFound(handler PageHandler) {
	a.notFound = handler
}

// SetErrorPage sets the 500 page handler.
func (a *App) SetErrorPage(handler ErrorHandler) {
	a.errorPage = handler
}

// SetPublicDir sets the public asset directory served at the site root.
// An empty directory disables automatic public asset serving.
func (a *App) SetPublicDir(dir string) {
	a.publicDir = dir
}

// SetRevalidator replaces the app-wide revalidator used for
// automatic ETags and explicit path/tag invalidation.
func (a *App) SetRevalidator(revalidator *Revalidator) {
	if revalidator == nil {
		revalidator = NewRevalidator()
	}
	a.revalidator = revalidator
}

// SetRevalidationStore replaces the app-wide revalidation/version store used
// for automatic ETags and explicit path/tag invalidation.
func (a *App) SetRevalidationStore(store RevalidationStore) {
	if a == nil {
		return
	}
	if a.revalidator == nil {
		a.revalidator = NewRevalidatorWithStore(store)
		return
	}
	a.revalidator.SetStore(store)
}

// SetISRStore replaces the ISR artifact/state store used by incremental static
// regeneration. A nil store restores the default local filesystem + in-memory
// implementation.
func (a *App) SetISRStore(store ISRStore) {
	if a == nil {
		return
	}
	if store == nil {
		store = NewInMemoryISRStore()
	}
	a.isrStore = store
	if a.isr != nil {
		a.isr.store = store
	}
}

// Revalidator returns the app-wide revalidator.
func (a *App) Revalidator() *Revalidator {
	if a.revalidator == nil {
		a.revalidator = NewRevalidator()
	}
	return a.revalidator
}

// RevalidatePath invalidates cache validators for the provided path prefix.
func (a *App) RevalidatePath(target string) uint64 {
	return a.Revalidator().RevalidatePath(target)
}

// RevalidateTag invalidates cache validators for the provided tag.
func (a *App) RevalidateTag(tag string) uint64 {
	return a.Revalidator().RevalidateTag(tag)
}

// RevalidationStore returns the app-wide store backing the revalidator.
func (a *App) RevalidationStore() RevalidationStore {
	if a == nil {
		return nil
	}
	return a.Revalidator().Store()
}

// ISRStore returns the store backing ISR state and artifacts.
func (a *App) ISRStore() ISRStore {
	if a == nil {
		return nil
	}
	if a.isrStore == nil {
		a.isrStore = NewInMemoryISRStore()
	}
	return a.isrStore
}

// SetImageDir sets the source directory used by the built-in image optimizer.
// If unset, the optimizer reads from the configured public directory.
func (a *App) SetImageDir(dir string) {
	a.imageDir = dir
}

// Route registers a page route using the legacy request-only handler signature.
func (a *App) Route(pattern string, handler func(r *http.Request) gosx.Node) {
	a.Page(pattern, func(ctx *Context) gosx.Node {
		return handler(ctx.Request)
	})
}

// Page registers an HTML page route.
func (a *App) Page(pattern string, handler PageHandler) {
	a.HandlePage(PageRoute{
		Pattern: pattern,
		Handler: handler,
	})
}

// HandlePage registers an HTML page route with optional route middleware.
func (a *App) HandlePage(route PageRoute) {
	if route.Handler == nil {
		return
	}
	matchPattern := normalizePattern(route.Pattern)
	a.pageRoutes[matchPattern] = registeredPageRoute{
		pattern:    route.Pattern,
		handler:    route.Handler,
		middleware: append([]Middleware(nil), route.Middleware...),
	}
}

// API registers a JSON API route.
func (a *App) API(pattern string, handler APIHandler) {
	a.HandleAPI(APIRoute{
		Pattern: pattern,
		Handler: handler,
	})
}

// HandleAPI registers a JSON API route with optional route middleware.
func (a *App) HandleAPI(route APIRoute) {
	if route.Handler == nil {
		return
	}
	matchPattern := normalizePattern(route.Pattern)
	a.apiRoutes[matchPattern] = registeredAPIRoute{
		pattern:    route.Pattern,
		handler:    route.Handler,
		middleware: append([]Middleware(nil), route.Middleware...),
	}
}

// EnableNavigation injects the built-in client-side page navigation runtime
// into document/head-aware responses.
func (a *App) EnableNavigation() {
	a.navigation = true
}

// Redirect registers a redirect rule. The pattern uses the same syntax as page
// routes, and path values such as `{slug}` may be referenced in the destination.
func (a *App) Redirect(pattern, destination string, status int) {
	a.HandleRedirect(RedirectRoute{
		Pattern:     pattern,
		Destination: destination,
		Status:      status,
	})
}

// HandleRedirect registers a redirect rule.
func (a *App) HandleRedirect(route RedirectRoute) {
	if strings.TrimSpace(route.Pattern) == "" || strings.TrimSpace(route.Destination) == "" {
		return
	}
	if route.Status == 0 {
		route.Status = http.StatusTemporaryRedirect
	}
	matchPattern := normalizePattern(route.Pattern)
	a.redirects[matchPattern] = registeredRedirectRoute{
		pattern:     route.Pattern,
		destination: route.Destination,
		status:      route.Status,
	}
}

// Rewrite registers an internal rewrite rule. The destination may reference
// path values such as `{slug}` captured from the pattern.
func (a *App) Rewrite(pattern, destination string) {
	a.HandleRewrite(RewriteRoute{
		Pattern:     pattern,
		Destination: destination,
	})
}

// HandleRewrite registers an internal rewrite rule.
func (a *App) HandleRewrite(route RewriteRoute) {
	if strings.TrimSpace(route.Pattern) == "" || strings.TrimSpace(route.Destination) == "" {
		return
	}
	matchPattern := normalizePattern(route.Pattern)
	a.rewrites[matchPattern] = registeredRewriteRoute{
		pattern:     route.Pattern,
		destination: route.Destination,
	}
}

// Mount registers an arbitrary HTTP handler under the given pattern.
func (a *App) Mount(pattern string, handler http.Handler) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || handler == nil {
		return
	}
	a.mounts[pattern] = registeredMountedRoute{
		pattern: pattern,
		handler: handler,
	}
}

// MountApp mounts a child GoSX App at a path prefix. The child app's handler
// is wrapped with http.StripPrefix so routes registered on the child (which
// are rooted at "/") receive requests with the prefix removed. Requests to the
// bare prefix without a trailing slash are redirected to prefix+"/" by the
// underlying http.ServeMux.
//
// Example:
//
//	parent := server.New()
//	child := server.New()
//	// ... configure child routes ...
//	parent.MountApp("/cobalt/example", child)
//
// With the mount above, a request to /cobalt/example/programs reaches the
// child's handler as /programs.
func (a *App) MountApp(prefix string, child *App) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || child == nil {
		return
	}
	prefix = "/" + strings.Trim(prefix, "/")
	a.Mount(prefix+"/", http.StripPrefix(prefix, child.Build()))
}

// EnableGzip adds gzip compression middleware. It compresses all responses
// when the client advertises gzip support, skipping WebSocket upgrades and
// pre-compressed responses. Call this before Use() calls that write responses.
func (a *App) EnableGzip() {
	a.Use(GzipMiddleware())
}

// Use appends middleware to the handler chain.
func (a *App) Use(mw Middleware) {
	if mw == nil {
		return
	}
	a.middleware = append(a.middleware, mw)
}

// UseObserver appends a request observer to the supported app extension surface.
func (a *App) UseObserver(observer RequestObserver) {
	if observer == nil {
		return
	}
	a.observers = append(a.observers, observer)
}

// UseOperationObserver appends a non-request operational observer to the app.
func (a *App) UseOperationObserver(observer OperationObserver) {
	if observer == nil {
		return
	}
	a.operations = append(a.operations, observer)
}

// UseReadyCheck appends a named readiness check evaluated by `/readyz`.
func (a *App) UseReadyCheck(name string, check ReadyCheck) {
	if check == nil {
		return
	}
	a.readyChecks = append(a.readyChecks, namedReadyCheck{
		name:  normalizeReadyCheckName(name),
		check: check,
	})
}

// Build finalizes routes and returns an http.Handler.
// preloadGrammarBlob loads an app-staged GoSX grammar blob when present.
// The gosx library also embeds a default blob, so dist/ is now just an override
// and deployment convenience.
func (a *App) preloadGrammarBlob() {
	root := a.effectiveRuntimeRoot()
	if root == "" {
		return
	}
	blobPath := filepath.Join(root, "gosx-grammar.blob")
	data, err := os.ReadFile(blobPath)
	if err != nil {
		return // no blob — will generate at runtime (slow but works)
	}
	if err := gosx.SetGrammarBlob(data); err != nil {
		log.Printf("[gosx] failed to load grammar blob: %v", err)
		return
	}
	log.Printf("[gosx] grammar blob loaded (%d bytes) — fast .gsx compilation enabled", len(data))
}

func (a *App) Build() http.Handler {
	a.preloadGrammarBlob()
	mux := http.NewServeMux()
	redirectMux := http.NewServeMux()
	rewriteMux := http.NewServeMux()
	mountMux := http.NewServeMux()
	a.registerBuiltinRoutes(mux)
	a.registerPageRoutes(mux)
	a.registerAPIRoutes(mux)
	a.registerRedirectRoutes(redirectMux)
	a.registerMountRoutes(mountMux)

	var dispatch func(w http.ResponseWriter, r *http.Request, allowRewrite bool)

	a.mux = mux
	dispatch = a.buildDispatcher(mux, redirectMux, rewriteMux, mountMux)
	a.registerRewriteRoutes(rewriteMux, dispatch)

	return a.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.maybeServeISR(w, r, dispatch) {
			return
		}
		dispatch(w, r, true)
	}))
}

func (a *App) registerBuiltinRoutes(mux *http.ServeMux) {
	if !a.hasRoute("/healthz") {
		mux.HandleFunc("/healthz", healthHandler)
	}
	if !a.hasRoute("/readyz") {
		mux.HandleFunc("/readyz", a.readyHandler)
	}
	if !a.hasRoute("GET /gosx/") {
		mux.Handle("GET /gosx/", http.HandlerFunc(a.serveRuntimeAsset))
	}
	if imageDir := a.effectiveImageDir(); imageDir != "" && !a.hasRoute(defaultImageEndpoint) {
		mux.Handle("GET "+defaultImageEndpoint, ImageHandler(imageDir))
	}
	if !a.hasRoute("GET /_gosx/emoji-codes.json") {
		mux.Handle("GET /_gosx/emoji-codes.json", EmojiCodesHandler())
	}
}

func (a *App) registerPageRoutes(mux *http.ServeMux) {
	for matchPattern, route := range a.pageRoutes {
		mux.Handle(matchPattern, chainMiddleware(a.pageRouteHandler(route), route.middleware))
	}
}

func (a *App) pageRouteHandler(route registeredPageRoute) http.Handler {
	pattern := route.pattern
	handler := route.handler
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		MarkObservedRequest(r, "page", pattern)
		defer func() {
			if recovered := recover(); recovered != nil {
				a.renderError(w, r, panicError(recovered))
			}
		}()
		ctx := newContext(r)
		ctx.cache = NewCacheState()
		node := handler(ctx)
		a.renderPage(w, ctx, pattern, node, "GoSX")
	})
}

func (a *App) registerAPIRoutes(mux *http.ServeMux) {
	for matchPattern, route := range a.apiRoutes {
		mux.Handle(matchPattern, chainMiddleware(a.apiRouteHandler(route), route.middleware))
	}
}

func (a *App) apiRouteHandler(route registeredAPIRoute) http.Handler {
	pattern := route.pattern
	handler := route.handler
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		MarkObservedRequest(r, "api", pattern)
		defer func() {
			if recovered := recover(); recovered != nil {
				writeJSONError(w, http.StatusInternalServerError, panicError(recovered), nil)
			}
		}()
		ctx := newContext(r)
		ctx.cache = NewCacheState()
		payload, err := handler(ctx)
		if err != nil {
			writeJSONError(w, errorStatus(err, ctx.status, http.StatusInternalServerError), err, ctx.headers)
			return
		}
		status := statusWithDefault(ctx.status, payload)
		if ApplyCacheHeaders(r, ctx.headers, status, ctx.cache, a.Revalidator()) {
			WriteNotModified(w, ctx.headers)
			return
		}
		writeJSON(w, status, payload, ctx.headers)
	})
}

func (a *App) registerRedirectRoutes(mux *http.ServeMux) {
	for matchPattern, route := range a.redirects {
		pattern := route.pattern
		destination := route.destination
		status := route.status
		mux.HandleFunc(matchPattern, func(w http.ResponseWriter, r *http.Request) {
			MarkObservedRequest(r, "redirect", pattern)
			http.Redirect(w, r, expandPatternValues(r, destination), status)
		})
	}
}

func (a *App) registerMountRoutes(mux *http.ServeMux) {
	for _, route := range a.mounts {
		pattern := route.pattern
		handler := route.handler
		mux.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			MarkObservedRequest(r, "mount", pattern)
			handler.ServeHTTP(w, r)
		}))
	}
}

func (a *App) buildDispatcher(mux, redirectMux, rewriteMux, mountMux *http.ServeMux) func(http.ResponseWriter, *http.Request, bool) {
	return func(w http.ResponseWriter, r *http.Request, allowRewrite bool) {
		if redirectHandled(redirectMux, w, r) {
			return
		}
		if routeHandled(mux, w, r) {
			return
		}
		if allowRewrite && len(a.rewrites) > 0 && rewriteHandled(rewriteMux, w, r) {
			return
		}
		if a.servePublic(w, r) {
			return
		}
		if mountHandled(mountMux, w, r) {
			return
		}
		a.renderNotFound(w, r)
	}
}

func (a *App) registerRewriteRoutes(mux *http.ServeMux, dispatch func(http.ResponseWriter, *http.Request, bool)) {
	for matchPattern, route := range a.rewrites {
		pattern := route.pattern
		destination := route.destination
		mux.HandleFunc(matchPattern, func(w http.ResponseWriter, r *http.Request) {
			MarkObservedRequest(r, "rewrite", pattern)
			dispatch(w, rewriteRequest(r, expandPatternValues(r, destination)), false)
		})
	}
}

// ListenAndServe starts the HTTP server.
func (a *App) ListenAndServe(addr string) error {
	handler := a.Build()
	srv := &http.Server{
		Addr:              resolveListenAddr(addr),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      45 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return srv.ListenAndServe()
}

func resolveListenAddr(addr string) string {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		return addr
	}
	if host, parsedPort, ok := explicitListenHostPort(port); ok {
		return net.JoinHostPort(host, parsedPort)
	}
	return listenAddrWithPort(addr, port)
}

func explicitListenHostPort(value string) (string, string, bool) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil || strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "" {
		return "", "", false
	}
	return host, port, true
}

func listenAddrWithPort(addr, port string) string {
	port = normalizedListenPort(port)
	if port == "" {
		return addr
	}
	if host, ok := listenAddrHost(addr); ok {
		return net.JoinHostPort(host, port)
	}
	return ":" + port
}

func normalizedListenPort(value string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), ":"))
}

func listenAddrHost(addr string) (string, bool) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", false
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host, true
	}
	if strings.HasPrefix(addr, "[") && strings.HasSuffix(addr, "]") {
		host := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(addr, "["), "]"))
		return host, host != ""
	}
	if strings.Contains(addr, ":") {
		return "", false
	}
	return addr, true
}

// HTMLDocument wraps content in a full HTML5 document.
func HTMLDocument(title string, head gosx.Node, body gosx.Node) gosx.Node {
	return gosx.RawHTML(renderDocument(title, head, body))
}

func renderDocument(title string, head gosx.Node, body gosx.Node) string {
	return renderDocumentWithContext(&DocumentContext{
		Title: title,
		Head:  head,
		Body:  body,
	})
}

func renderDocumentWithContext(doc *DocumentContext) string {
	title := ""
	head := gosx.Text("")
	body := gosx.Text("")
	if doc != nil {
		title = doc.Title
		head = doc.Head
		body = doc.Body
	}
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html")
	b.WriteString(documentHTMLAttrs(doc))
	b.WriteString(">\n<head>\n")
	b.WriteString("<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", title)
	b.WriteString(gosx.RenderHTML(HeadOutlet(head)))
	b.WriteString("\n</head>\n<body")
	b.WriteString(documentBodyAttrs(doc))
	b.WriteString(">\n")
	b.WriteString(gosx.RenderHTML(body))
	b.WriteString("\n")
	b.WriteString(streamTailMarker)
	b.WriteString("\n</body>\n</html>")
	return b.String()
}

// RequestID returns the per-request ID assigned by the default middleware.
func RequestID(r *http.Request) string {
	if r == nil {
		return ""
	}
	if id, ok := r.Context().Value(requestIDContextKey).(string); ok {
		return id
	}
	return r.Header.Get(requestIDHeader)
}

func (a *App) wrap(handler http.Handler) http.Handler {
	wrapped := handler
	for i := len(a.middleware) - 1; i >= 0; i-- {
		wrapped = a.middleware[i](wrapped)
	}
	if len(a.observers) > 0 {
		wrapped = ObserveHandler(wrapped, append([]RequestObserver(nil), a.observers...))
	}
	return wrapped
}

func (a *App) observeOperation(event OperationEvent) {
	if len(a.operations) == 0 {
		return
	}
	for _, observer := range a.operations {
		if observer != nil {
			observer.ObserveOperation(event)
		}
	}
}

func (a *App) renderPage(w http.ResponseWriter, ctx *Context, pattern string, body gosx.Node, defaultTitle string) {
	ctx = ensurePageContext(ctx)
	if a.pageNotModified(ctx) {
		WriteNotModified(w, ctx.headers)
		return
	}
	renderedPage := a.renderPageNode(ctx, pattern, body, defaultTitle)

	WriteHTML(w, HTMLResponse{
		Status:   ctx.status,
		Headers:  ctx.headers,
		Node:     renderedPage,
		Deferred: ctx.deferred,
	})
}

func ensurePageContext(ctx *Context) *Context {
	if ctx == nil {
		ctx = newContext(nil)
	}
	if ctx.status == 0 {
		ctx.status = http.StatusOK
	}
	return ctx
}

func (a *App) pageNotModified(ctx *Context) bool {
	return ApplyCacheHeaders(ctx.Request, ctx.headers, ctx.status, ctx.cache, a.Revalidator())
}

func (a *App) decoratePageContext(ctx *Context) {
	if ctx.runtime != nil {
		ctx.AddHead(ctx.runtime.Head())
	}
	if a.navigation {
		ctx.AddHead(NavigationScript())
	}
}

func (a *App) renderPageNode(ctx *Context, pattern string, body gosx.Node, defaultTitle string) gosx.Node {
	// Render the body once up front so islands/engines/hubs register with the
	// page runtime before we finalize managed head assets.
	bodyHTML := gosx.RenderHTML(body)
	renderedBody := gosx.RawHTML(bodyHTML)
	a.decoratePageContext(ctx)
	doc := ctx.documentContext(pattern, defaultTitle, renderedBody, a.navigation)
	switch {
	case a.document != nil:
		return a.document(doc)
	case a.layout != nil:
		return HTMLDocument(pageTitle(ctx, pattern, defaultTitle), ctx.Head(), renderedBody)
	default:
		return gosx.RawHTML(renderDocumentWithContext(doc))
	}
}

func pageTitle(ctx *Context, pattern string, defaultTitle string) string {
	if ctx != nil {
		if title := resolveTitle(ctx.metadata.Title); title != "" {
			return title
		}
	}
	return fallbackTitle(pattern, defaultTitle)
}

func (a *App) renderNotFound(w http.ResponseWriter, r *http.Request) {
	MarkObservedRequest(r, "not_found", "")
	if wantsJSON(r) {
		writeJSONError(w, http.StatusNotFound, fmt.Errorf("not found"), nil)
		return
	}

	ctx := newContext(r)
	ctx.SetStatus(http.StatusNotFound)

	var node gosx.Node
	if a.notFound != nil {
		node = a.notFound(ctx)
	} else {
		ctx.SetMetadata(Metadata{Title: Title{Absolute: "Not Found"}})
		node = defaultStatusBody("Page not found", "The requested page could not be found.")
	}
	a.renderPage(w, ctx, "", node, "Not Found")
}

func (a *App) renderError(w http.ResponseWriter, r *http.Request, err error) {
	MarkObservedRequest(r, "error", "")
	if wantsJSON(r) {
		writeJSONError(w, errorStatus(err, 0, http.StatusInternalServerError), err, nil)
		return
	}

	ctx := newContext(r)
	ctx.SetStatus(errorStatus(err, 0, http.StatusInternalServerError))

	var node gosx.Node
	if a.errorPage != nil {
		node = a.errorPage(ctx, err)
	} else {
		title := http.StatusText(ctx.status)
		if title == "" {
			title = "Server Error"
		}
		ctx.SetMetadata(Metadata{Title: Title{Absolute: title}})
		node = defaultStatusBody(title, defaultErrorMessage(err, r))
	}
	a.renderPage(w, ctx, "", node, http.StatusText(ctx.status))
}

func (a *App) servePublic(w http.ResponseWriter, r *http.Request) bool {
	if a.publicDir == "" {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}

	cleanPath := path.Clean("/" + r.URL.Path)
	if cleanPath == "/" {
		return false
	}

	name := strings.TrimPrefix(cleanPath, "/")
	fsPath := filepath.Join(a.publicDir, filepath.FromSlash(name))
	info, err := os.Stat(fsPath)
	if err != nil || info.IsDir() {
		return false
	}

	MarkObservedRequest(r, "public", cleanPath)
	w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	http.ServeFile(w, r, fsPath)
	return true
}

func (a *App) hasRoute(pattern string) bool {
	matchPattern := normalizePattern(pattern)
	if _, ok := a.pageRoutes[matchPattern]; ok {
		return true
	}
	_, ok := a.apiRoutes[matchPattern]
	return ok
}

func (a *App) effectiveImageDir() string {
	if strings.TrimSpace(a.imageDir) != "" {
		return a.imageDir
	}
	return a.publicDir
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (a *App) readyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	report := ReadinessReport{OK: true}
	for _, entry := range a.readyChecks {
		if entry.check == nil {
			continue
		}
		result := ReadinessCheckResult{
			Name: normalizeReadyCheckName(entry.name),
			OK:   true,
		}
		if err := entry.check.CheckReady(r.Context()); err != nil {
			report.OK = false
			result.OK = false
			result.Error = err.Error()
		}
		report.Checks = append(report.Checks, result)
	}
	if !report.OK {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(report)
}

func requestIDMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(requestIDHeader)
			if id == "" {
				id = nextRequestID()
			}
			w.Header().Set(requestIDHeader, id)
			ctx := context.WithValue(r.Context(), requestIDContextKey, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func securityHeadersMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			next.ServeHTTP(w, r)
		})
	}
}

func recoveryMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					writePanic(w, r, recovered)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func nextRequestID() string {
	seq := atomic.AddUint64(&requestSeq, 1)
	return fmt.Sprintf("gosx-%d-%d", time.Now().UnixNano(), seq)
}

func statusWithDefault(status int, payload any) int {
	if status != 0 {
		return status
	}
	if payload == nil {
		return http.StatusNoContent
	}
	return http.StatusOK
}

func errorStatus(err error, fallback int, defaultStatus int) int {
	if status := statusFromError(err); status != 0 {
		return status
	}
	return firstNonZeroStatus(fallback, defaultStatus)
}

func statusFromError(err error) int {
	if err == nil {
		return 0
	}
	var sc statusCoder
	if errors.As(err, &sc) {
		return sc.StatusCode()
	}
	return 0
}

func firstNonZeroStatus(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func writeJSON(w http.ResponseWriter, status int, payload any, headers http.Header) {
	copyHeaders(w.Header(), headers)
	if payload == nil {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, err error, headers http.Header) {
	message := http.StatusText(status)
	if err != nil && err.Error() != "" {
		message = err.Error()
	}
	writeJSON(w, status, map[string]any{
		"error": message,
	}, headers)
}

func writePanic(w http.ResponseWriter, r *http.Request, recovered any) {
	message := http.StatusText(http.StatusInternalServerError)
	if wantsJSON(r) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": message}, nil)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.Error(w, message, http.StatusInternalServerError)
}

func wantsJSON(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		return true
	}
	return jsonAcceptedMediaType(r.Header.Get("Accept")) && !htmlAcceptedMediaType(r.Header.Get("Accept"))
}

func jsonAcceptedMediaType(accept string) bool {
	for _, mediaType := range acceptMediaTypes(accept) {
		if mediaType == "application/json" || strings.HasSuffix(mediaType, "+json") {
			return true
		}
	}
	return false
}

func htmlAcceptedMediaType(accept string) bool {
	for _, mediaType := range acceptMediaTypes(accept) {
		if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
			return true
		}
	}
	return false
}

func acceptMediaTypes(accept string) []string {
	parts := strings.Split(accept, ",")
	mediaTypes := make([]string, 0, len(parts))
	for _, part := range parts {
		mediaType, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(part)), ";")
		mediaType = strings.TrimSpace(mediaType)
		if mediaType == "" {
			continue
		}
		mediaTypes = append(mediaTypes, mediaType)
	}
	return mediaTypes
}

func defaultStatusBody(title string, message string) gosx.Node {
	return gosx.El("main", gosx.Attrs(gosx.Attr("style", "font-family: sans-serif; max-width: 40rem; margin: 4rem auto; padding: 0 1.5rem; line-height: 1.5")),
		gosx.El("h1", gosx.Text(title)),
		gosx.El("p", gosx.Text(message)),
	)
}

func defaultErrorMessage(err error, r *http.Request) string {
	requestID := RequestID(r)
	if requestID == "" {
		return "The server encountered an unexpected error."
	}
	return fmt.Sprintf("The server encountered an unexpected error. Request ID: %s", requestID)
}

func panicError(recovered any) error {
	if err, ok := recovered.(error); ok {
		return err
	}
	return fmt.Errorf("%v", recovered)
}

func fallbackTitle(pattern, defaultTitle string) string {
	if title := displayPattern(pattern); title != "" {
		return title
	}
	return defaultTitle
}

func normalizePattern(pattern string) string {
	fields := strings.Fields(pattern)
	if normalized, ok := normalizedRootPattern(fields); ok {
		return normalized
	}
	return strings.TrimSpace(pattern)
}

func normalizedRootPattern(fields []string) (string, bool) {
	if len(fields) == 1 && fields[0] == "/" {
		return "/{$}", true
	}
	if len(fields) == 2 && fields[1] == "/" {
		return fields[0] + " /{$}", true
	}
	return "", false
}

var pathParamPattern = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

func expandPatternValues(r *http.Request, target string) string {
	if r == nil || target == "" {
		return target
	}
	return pathParamPattern.ReplaceAllStringFunc(target, func(match string) string {
		submatch := pathParamPattern.FindStringSubmatch(match)
		if len(submatch) != 2 {
			return match
		}
		value := r.PathValue(submatch[1])
		if value == "" {
			return match
		}
		return value
	})
}

func rewriteRequest(r *http.Request, destination string) *http.Request {
	if r == nil {
		return nil
	}
	clone := r.Clone(r.Context())
	if destination == "" {
		return clone
	}
	parsed, err := url.Parse(destination)
	if err != nil {
		clone.URL.Path = destination
		clone.URL.RawPath = destination
		clone.RequestURI = destination
		return clone
	}
	if parsed.Path != "" {
		clone.URL.Path = parsed.Path
		clone.URL.RawPath = parsed.EscapedPath()
	}
	clone.URL.RawQuery = parsed.RawQuery
	if parsed.Fragment != "" {
		clone.URL.Fragment = parsed.Fragment
	}
	requestURI := clone.URL.Path
	if clone.URL.RawQuery != "" {
		requestURI += "?" + clone.URL.RawQuery
	}
	clone.RequestURI = requestURI
	return clone
}

func displayPattern(pattern string) string {
	fields := strings.Fields(pattern)
	switch len(fields) {
	case 0:
		return ""
	case 1:
		return strings.ReplaceAll(fields[0], "{$}", "")
	default:
		return strings.ReplaceAll(fields[len(fields)-1], "{$}", "")
	}
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func chainMiddleware(handler http.Handler, middleware []Middleware) http.Handler {
	wrapped := handler
	for i := len(middleware) - 1; i >= 0; i-- {
		if middleware[i] == nil {
			continue
		}
		wrapped = middleware[i](wrapped)
	}
	return wrapped
}

func routeHandled(mux *http.ServeMux, w http.ResponseWriter, r *http.Request) bool {
	rec := &interceptResponseWriter{header: make(http.Header)}
	mux.ServeHTTP(rec, r)
	switch rec.statusCode {
	case 0, http.StatusNotFound:
		return false
	default:
		rec.commit(w)
		return true
	}
}

func mountHandled(mux *http.ServeMux, w http.ResponseWriter, r *http.Request) bool {
	handler, pattern := mux.Handler(r)
	if pattern == "" || handler == nil {
		return false
	}
	handler.ServeHTTP(w, r)
	return true
}

func redirectHandled(mux *http.ServeMux, w http.ResponseWriter, r *http.Request) bool {
	rec := &interceptResponseWriter{header: make(http.Header)}
	mux.ServeHTTP(rec, r)
	switch {
	case rec.statusCode >= 300 && rec.statusCode < 400:
		rec.commit(w)
		return true
	default:
		return false
	}
}

func rewriteHandled(mux *http.ServeMux, w http.ResponseWriter, r *http.Request) bool {
	rec := &interceptResponseWriter{header: make(http.Header)}
	mux.ServeHTTP(rec, r)
	switch rec.statusCode {
	case 0, http.StatusNotFound, http.StatusMethodNotAllowed:
		return false
	default:
		rec.commit(w)
		return true
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
