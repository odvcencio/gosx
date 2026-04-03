package markdown

import (
	"net/http"
	"path"
	"strings"

	"github.com/odvcencio/gotreesitter/grammars"
)

// GrammarBlobHandler returns an HTTP handler that serves gotreesitter grammar
// blobs on demand. Mount it at /grammars/ and any browser-side WASM module can
// fetch language grammars one at a time.
//
// URL pattern: /grammars/{language}.blob (e.g. /grammars/go.blob)
func GrammarBlobHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := path.Base(r.URL.Path)

		// Security: reject path traversal attempts.
		if strings.Contains(base, "..") || strings.Contains(base, "/") || strings.Contains(base, "\\") {
			http.NotFound(w, r)
			return
		}

		lang := strings.TrimSuffix(base, ".blob")
		if lang == "" || lang == base {
			http.NotFound(w, r)
			return
		}

		data := grammars.BlobByName(lang)
		if data == nil {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write(data)
	})
}
