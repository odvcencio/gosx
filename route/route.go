// Package route provides declarative server routing with layouts for GoSX apps.
//
// Routes map URL patterns to component handlers. Layouts wrap pages with
// shared UI. Nested layouts compose from outermost to innermost.
package route

import (
	"net/http"

	"github.com/odvcencio/gosx"
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
}

// PageHandler renders a page, receiving route context.
type PageHandler func(ctx *RouteContext) gosx.Node

// LayoutFunc wraps page content with shared layout.
type LayoutFunc func(ctx *RouteContext, content gosx.Node) gosx.Node

// Middleware runs before page handling.
type Middleware func(next http.Handler) http.Handler

// DataLoader fetches data for a route before rendering.
type DataLoader func(ctx *RouteContext) (any, error)

// RouteContext provides request context to handlers.
type RouteContext struct {
	Request    *http.Request
	Params     map[string]string
	Data       any
	parentData map[string]any
}

// Param returns a URL path parameter.
func (ctx *RouteContext) Param(name string) string {
	return ctx.Params[name]
}

// Query returns a URL query parameter.
func (ctx *RouteContext) Query(name string) string {
	return ctx.Request.URL.Query().Get(name)
}

// ParentData returns data loaded by a parent route's DataLoader.
func (ctx *RouteContext) ParentData(key string) any {
	if ctx.parentData == nil {
		return nil
	}
	return ctx.parentData[key]
}

// Router builds an http.Handler from a route tree.
type Router struct {
	routes        []Route
	defaultLayout LayoutFunc
	notFound      PageHandler
	mux           *http.ServeMux
}

// NewRouter creates a new router.
func NewRouter() *Router {
	return &Router{
		mux: http.NewServeMux(),
	}
}

// SetLayout sets the default layout for all routes.
func (r *Router) SetLayout(layout LayoutFunc) {
	r.defaultLayout = layout
}

// SetNotFound sets the 404 handler.
func (r *Router) SetNotFound(handler PageHandler) {
	r.notFound = handler
}

// Add registers routes.
func (r *Router) Add(routes ...Route) {
	r.routes = append(r.routes, routes...)
}

// Build compiles the router into an http.Handler.
func (r *Router) Build() http.Handler {
	for _, route := range r.routes {
		r.registerRoute("", route, nil, nil)
	}
	return r.mux
}

func (r *Router) registerRoute(prefix string, route Route, parentLayouts []LayoutFunc, parentMiddleware []Middleware) {
	pattern := prefix + route.Pattern

	// Collect layouts: parent layouts + this route's layout (or default)
	layouts := make([]LayoutFunc, len(parentLayouts))
	copy(layouts, parentLayouts)
	if route.Layout != nil {
		layouts = append(layouts, route.Layout)
	} else if len(parentLayouts) == 0 && r.defaultLayout != nil {
		layouts = append(layouts, r.defaultLayout)
	}

	// Collect middleware
	middleware := make([]Middleware, len(parentMiddleware))
	copy(middleware, parentMiddleware)
	middleware = append(middleware, route.Middleware...)

	if route.Handler != nil {
		handler := r.buildHandler(route, layouts)

		// Apply middleware
		var h http.Handler = handler
		for i := len(middleware) - 1; i >= 0; i-- {
			h = middleware[i](h)
		}

		r.mux.Handle(pattern, h)
	}

	// Register children
	for _, child := range route.Children {
		r.registerRoute(pattern, child, layouts, middleware)
	}
}

func (r *Router) buildHandler(route Route, layouts []LayoutFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := &RouteContext{
			Request: req,
			Params:  extractParams(req),
		}

		// Run data loader
		if route.DataLoader != nil {
			data, err := route.DataLoader(ctx)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			ctx.Data = data
		}

		// Render page
		node := route.Handler(ctx)

		// Apply layouts innermost to outermost
		for i := len(layouts) - 1; i >= 0; i-- {
			node = layouts[i](ctx, node)
		}

		html := gosx.RenderHTML(node)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
	}
}

func extractParams(req *http.Request) map[string]string {
	params := make(map[string]string)
	// Go 1.22+ path values
	if id := req.PathValue("id"); id != "" {
		params["id"] = id
	}
	if name := req.PathValue("name"); name != "" {
		params["name"] = name
	}
	if slug := req.PathValue("slug"); slug != "" {
		params["slug"] = slug
	}
	return params
}
