// Package basepath keeps public URL prefixes separate from internal route paths.
package basepath

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"os"
	"strings"

	"m31labs.dev/gosx/internal/urlpath"
)

type contextKey struct{}

// Export headers let the local static-export harness discover the public mount.
const ExportPrefixHeader = "X-GoSX-Export-Base-Path"
const ExportStripHeader = "X-GoSX-Export-Proxy-Strips-Prefix"

func FromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	value, _ := r.Context().Value(contextKey{}).(string)
	return value
}

func URL(prefix, value string) string        { return urlpath.URL(prefix, value) }
func Normalize(value string) (string, error) { return urlpath.Normalize(value) }

// Handler supplies the public prefix and optionally strips it before routing.
// A nested router inherits an existing prefix without stripping it twice.
func Handler(prefix string, proxyStripsPrefix bool, next http.Handler) http.Handler {
	if prefix == "" {
		return next
	}
	export := os.Getenv("GOSX_STATIC_EXPORT") == "1"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The local exporter connects before the deployment proxy exists. Even
		// an outside-mount 404 supplies discovery metadata in this build mode.
		if export {
			w.Header().Set(ExportPrefixHeader, prefix)
			if proxyStripsPrefix {
				w.Header().Set(ExportStripHeader, "1")
			}
		}
		if existing := FromRequest(r); existing != "" {
			if existing != prefix {
				http.Error(w, "conflicting base paths", http.StatusInternalServerError)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		req := r.Clone(context.WithValue(r.Context(), contextKey{}, prefix))
		if !proxyStripsPrefix {
			if req.URL.Path != prefix && !strings.HasPrefix(req.URL.Path, prefix+"/") {
				http.NotFound(w, r)
				return
			}
			// Reject ambiguous encoded prefix boundaries rather than decoding a second
			// time. Encoded characters in route parameters remain intact.
			escaped := req.URL.EscapedPath()
			if escaped != prefix && !strings.HasPrefix(escaped, prefix+"/") {
				http.NotFound(w, r)
				return
			}
			if req.URL.Path == prefix && (req.Method == http.MethodGet || req.Method == http.MethodHead) {
				target := prefix + "/"
				if req.URL.RawQuery != "" {
					target += "?" + req.URL.RawQuery
				}
				http.Redirect(w, r, target, http.StatusPermanentRedirect)
				return
			}
			req.URL.Path = strings.TrimPrefix(req.URL.Path, prefix)
			if req.URL.Path == "" {
				req.URL.Path = "/"
			}
			if req.URL.RawPath != "" {
				req.URL.RawPath = strings.TrimPrefix(escaped, prefix)
				if req.URL.RawPath == "" {
					req.URL.RawPath = "/"
				}
			}
			req.RequestURI = req.URL.RequestURI()
		}
		next.ServeHTTP(&responseWriter{ResponseWriter: w, prefix: prefix}, req)
	})
}

type responseWriter struct {
	http.ResponseWriter
	prefix string
	wrote  bool
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.wrote = true
	if location := w.Header().Get("Location"); location != "" {
		w.Header().Set("Location", URL(w.prefix, location))
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseWriter) Write(data []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}
func (w *responseWriter) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}
