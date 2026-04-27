package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

const (
	edgeRequestHeader     = "X-GoSX-Edge"
	edgeRequestContextKey = contextKey("gosx.edge")
)

// Edge middleware runtime identifiers.
const (
	EdgeRuntimeWorker = "worker"
)

// EdgeMiddlewareOptions annotates middleware that is intended to run at the
// edge when a deployment adapter can bundle it there.
type EdgeMiddlewareOptions struct {
	Name    string
	Pattern string
	Runtime string
	Source  string
}

// EdgeMiddleware describes an edge-capable middleware registration.
type EdgeMiddleware struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
	Runtime string `json:"runtime"`
	Source  string `json:"source,omitempty"`
	Order   int    `json:"order"`
}

// UseEdge registers middleware and records a first-class edge deployment
// annotation for adapters that can execute it before origin fallback.
func (a *App) UseEdge(mw Middleware, opts EdgeMiddlewareOptions) {
	if mw == nil {
		return
	}
	a.edgeMiddleware = append(a.edgeMiddleware, normalizeEdgeMiddleware(opts, len(a.edgeMiddleware)))
	a.Use(mw)
}

// EdgeMiddleware returns the registered edge middleware descriptors.
func (a *App) EdgeMiddleware() []EdgeMiddleware {
	if a == nil || len(a.edgeMiddleware) == 0 {
		return nil
	}
	out := make([]EdgeMiddleware, len(a.edgeMiddleware))
	copy(out, a.edgeMiddleware)
	return out
}

// WithEdgeRequest marks a request as having passed through GoSX edge handling.
func WithEdgeRequest(r *http.Request) *http.Request {
	if r == nil {
		return nil
	}
	return r.WithContext(context.WithValue(r.Context(), edgeRequestContextKey, true))
}

// IsEdgeRequest reports whether the request came through a GoSX edge worker.
func IsEdgeRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if edge, ok := r.Context().Value(edgeRequestContextKey).(bool); ok && edge {
		return true
	}
	switch strings.TrimSpace(strings.ToLower(r.Header.Get(edgeRequestHeader))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func normalizeEdgeMiddleware(opts EdgeMiddlewareOptions, index int) EdgeMiddleware {
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "edge-middleware-" + strconv.Itoa(index+1)
	}
	pattern := strings.TrimSpace(opts.Pattern)
	if pattern == "" {
		pattern = "/*"
	}
	runtime := strings.TrimSpace(opts.Runtime)
	if runtime == "" {
		runtime = EdgeRuntimeWorker
	}
	return EdgeMiddleware{
		Name:    name,
		Pattern: pattern,
		Runtime: runtime,
		Source:  strings.TrimSpace(opts.Source),
		Order:   index,
	}
}
