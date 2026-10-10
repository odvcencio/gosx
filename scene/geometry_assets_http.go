//go:build !js || !wasm

package scene

import (
	"bytes"
	"net/http"
	"strings"
	"time"
)

// ServeHTTP serves only a known content hash; unknown paths are never files.
func (a *GeometryAssets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, a.prefix) {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, a.prefix)
	a.mu.Lock()
	value, ok := a.geometry[name]
	a.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+strings.TrimSuffix(name, ".json")+`"`)
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(value.data))
}
