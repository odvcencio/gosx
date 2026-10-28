//go:build !js || !wasm

package worklet

import (
	"bytes"
	"net/http"
	"path"
	"time"
)

// Handler serves only the versioned processor, including HEAD and ETags.
//
// Handler is a server-side route, so it is not built for GOOS=js GOARCH=wasm.
// Linking net/http into a browser module also links crypto/tls and
// crypto/x509, and a module that only needs Path, Source or the Backend types
// has no use for them. Code compiled for the browser must not call Handler.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if path.Clean("/"+r.URL.Path) != "/"+Path() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("ETag", `"`+digest+`"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, Path(), time.Time{}, bytes.NewReader(source))
	})
}
