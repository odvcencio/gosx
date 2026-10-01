package route

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"m31labs.dev/gosx"
	gosxcss "m31labs.dev/gosx/css"
	"m31labs.dev/gosx/server"
)

// Retain recent stylesheet versions for in-flight pages during development.
// Bound the history so repeated edits cannot grow a long-running server.
const fileCSSAssetHistory = 512

var fileCSSAssetScope = regexp.MustCompile(`^[a-f0-9]{12}$`)

type fileCSSAssets struct {
	prefix string
	mu     sync.RWMutex
	data   map[string][]byte
	files  map[string]string
	order  []string
}

func newFileCSSAssets(prefix string) *fileCSSAssets {
	return &fileCSSAssets{
		prefix: strings.TrimRight(prefix, "/") + "/",
		data:   make(map[string][]byte),
		files:  make(map[string]string),
	}
}

func (a *fileCSSAssets) put(text, source, path, scope string) string {
	key := fmt.Sprintf("styles.%x.css", sha256.Sum256([]byte(text)))
	a.mu.Lock()
	defer a.mu.Unlock()
	a.files[source] = path
	if _, exists := a.data[key]; !exists {
		if len(a.order) >= fileCSSAssetHistory {
			delete(a.data, a.order[0])
			a.order = a.order[1:]
		}
		a.data[key] = []byte(text)
		a.order = append(a.order, key)
	}
	query := url.Values{"source": {source}, "scope": {scope}}
	return a.prefix + key + "?" + query.Encode()
}

func (a *fileCSSAssets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, a.prefix)
	a.mu.RLock()
	data, ok := a.data[key]
	file := a.files[r.URL.Query().Get("source")]
	a.mu.RUnlock()
	if !ok && file != "" {
		data, ok = restoreFileCSSAsset(file, key, r.URL.Query().Get("scope"))
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", strconv.Quote(strings.TrimSuffix(key, ".css")))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, key, time.Time{}, bytes.NewReader(data))
}

// Restore exported CSS after a restart or directory move. Its original scope
// travels with the URL, and the content hash must match. Only registered
// sidecars can be read, never a request-selected filesystem path.
func restoreFileCSSAsset(file, key, scope string) ([]byte, bool) {
	if scope != "" && !fileCSSAssetScope.MatchString(scope) {
		return nil, false
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, false
	}
	text, _ := gosxcss.ExtractScene3DStyles(string(data))
	if scope != "" {
		text = gosxcss.ScopeCSS(text, scope)
	}
	if fmt.Sprintf("styles.%x.css", sha256.Sum256([]byte(text))) != key {
		return nil, false
	}
	return []byte(text), true
}

func externalFileCSSNode(root string, css cssFile, layer server.CSSLayer, order int, text string, assets *fileCSSAssets) gosx.Node {
	scope := ""
	if fileCSSLayerNeedsScope(layer) {
		scope = fileCSSScopeID(css.path)
	}
	return gosx.El("link", gosx.Attrs(
		gosx.Attr("rel", "stylesheet"),
		gosx.Attr("href", assets.put(text, fileCSSSource(root, css.path), css.path, scope)),
		gosx.Attr("data-gosx-file-css", filepath.Base(css.path)),
		gosx.Attr("data-gosx-css-layer", string(layer)),
		gosx.Attr("data-gosx-css-owner", server.FileStylesheetOwner(layer)),
		gosx.Attr("data-gosx-css-source", fileCSSSource(root, css.path)),
		gosx.Attr("data-gosx-css-order", strconv.Itoa(order)),
		gosx.Attr("data-gosx-css-scope", scope),
		gosx.Attr("data-gosx-file-css-scope", scope),
	))
}
