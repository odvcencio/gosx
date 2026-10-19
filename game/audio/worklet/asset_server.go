//go:build !js || !wasm

package worklet

import (
	"bytes"
	"net/http"
	"path"
	"time"
)

// Handler serves only the versioned processor, including HEAD and ETags.
// This server API is excluded from browser JS/WASM builds. Mount it under a
// same-origin prefix and pass that prefix plus Path to HostJS.AddWorkletModule.
func Handler() http.Handler {
	digest := processorDigest()
	assetPath := "worklet/wasm-processor." + digest[:16] + ".js"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if path.Clean("/"+r.URL.Path) != "/"+assetPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("ETag", `"`+digest+`"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, assetPath, time.Time{}, bytes.NewReader(source))
	})
}
