// Package server provides the GoSX HTTP server runtime.
//
// The server renders GoSX components to HTML on each request.
// Every component renders on the server by default; no client runtime
// is required unless interactivity is explicitly requested.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

// App is the GoSX server application.
type App struct {
	routes     map[string]RouteHandler
	layout     func(title string, body gosx.Node) gosx.Node
	mux        *http.ServeMux
	middleware []Middleware
}

// RouteHandler renders a page for a given request.
type RouteHandler func(r *http.Request) gosx.Node

// New creates a new GoSX server app.
func New() *App {
	app := &App{
		routes: make(map[string]RouteHandler),
		mux:    http.NewServeMux(),
	}
	app.middleware = []Middleware{
		requestIDMiddleware(),
		securityHeadersMiddleware(),
		recoveryMiddleware(),
	}
	return app
}

// SetLayout sets a layout wrapper that receives the page title and body.
func (a *App) SetLayout(layout func(title string, body gosx.Node) gosx.Node) {
	a.layout = layout
}

// Route registers a route handler.
func (a *App) Route(pattern string, handler RouteHandler) {
	a.routes[pattern] = handler
}

// Use appends middleware to the handler chain.
func (a *App) Use(mw Middleware) {
	if mw == nil {
		return
	}
	a.middleware = append(a.middleware, mw)
}

// Build finalizes routes and returns an http.Handler.
func (a *App) Build() http.Handler {
	mux := http.NewServeMux()
	if _, exists := a.routes["/healthz"]; !exists {
		mux.HandleFunc("/healthz", healthHandler)
	}
	if _, exists := a.routes["/readyz"]; !exists {
		mux.HandleFunc("/readyz", healthHandler)
	}

	for pattern, handler := range a.routes {
		h := handler // capture
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			node := h(r)

			if a.layout != nil {
				node = a.layout(pattern, node)
			}

			html := gosx.RenderHTML(node)

			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, html)
		})
	}
	a.mux = mux
	return a.wrap(mux)
}

// ListenAndServe starts the HTTP server.
func (a *App) ListenAndServe(addr string) error {
	handler := a.Build()
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      45 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return srv.ListenAndServe()
}

// HTMLDocument wraps content in a full HTML5 document.
func HTMLDocument(title string, head gosx.Node, body gosx.Node) gosx.Node {
	return gosx.RawHTML(renderDocument(title, head, body))
}

func renderDocument(title string, head gosx.Node, body gosx.Node) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html>\n<head>\n")
	b.WriteString("<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", title)
	if !head.IsZero() {
		b.WriteString(gosx.RenderHTML(head))
	}
	b.WriteString("\n</head>\n<body>\n")
	b.WriteString(gosx.RenderHTML(body))
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
	return wrapped
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
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
				if recover() == nil {
					return
				}
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func nextRequestID() string {
	seq := atomic.AddUint64(&requestSeq, 1)
	return fmt.Sprintf("gosx-%d-%d", time.Now().UnixNano(), seq)
}
