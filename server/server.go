// Package server provides the GoSX HTTP server runtime.
//
// The server renders GoSX components to HTML on each request.
// Every component renders on the server by default; no client runtime
// is required unless interactivity is explicitly requested.
package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/odvcencio/gosx"
)

// App is the GoSX server application.
type App struct {
	routes map[string]RouteHandler
	layout func(title string, body gosx.Node) gosx.Node
	mux    *http.ServeMux
}

// RouteHandler renders a page for a given request.
type RouteHandler func(r *http.Request) gosx.Node

// New creates a new GoSX server app.
func New() *App {
	return &App{
		routes: make(map[string]RouteHandler),
		mux:    http.NewServeMux(),
	}
}

// SetLayout sets a layout wrapper that receives the page title and body.
func (a *App) SetLayout(layout func(title string, body gosx.Node) gosx.Node) {
	a.layout = layout
}

// Route registers a route handler.
func (a *App) Route(pattern string, handler RouteHandler) {
	a.routes[pattern] = handler
}

// Build finalizes routes and returns an http.Handler.
func (a *App) Build() http.Handler {
	for pattern, handler := range a.routes {
		h := handler // capture
		a.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			node := h(r)

			if a.layout != nil {
				node = a.layout(pattern, node)
			}

			html := gosx.RenderHTML(node)

			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, html)
		})
	}
	return a.mux
}

// ListenAndServe starts the HTTP server.
func (a *App) ListenAndServe(addr string) error {
	handler := a.Build()
	return http.ListenAndServe(addr, handler)
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
