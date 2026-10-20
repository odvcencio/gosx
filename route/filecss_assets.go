package route

import (
	"bytes"
	"compress/gzip"
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

	"github.com/andybalholm/brotli"

	"m31labs.dev/gosx"
	gosxcss "m31labs.dev/gosx/css"
	"m31labs.dev/gosx/internal/httpcompress"
	"m31labs.dev/gosx/server"
)

// Retain recent stylesheet versions for in-flight pages during development.
// Bound the history so repeated edits cannot grow a long-running server.
const fileCSSAssetHistory = 512

var fileCSSAssetScope = regexp.MustCompile(`^[a-f0-9]{12}$`)

type fileCSSAsset struct {
	raw    []byte
	gzip   []byte
	brotli []byte
	once   sync.Once
}

func (asset *fileCSSAsset) compress() {
	asset.once.Do(func() {
		var gz, br bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
		_, _ = zw.Write(asset.raw)
		_ = zw.Close()
		bw := brotli.NewWriterLevel(&br, brotli.BestCompression)
		_, _ = bw.Write(asset.raw)
		_ = bw.Close()
		asset.gzip, asset.brotli = gz.Bytes(), br.Bytes()
	})
}

type fileCSSAssets struct {
	prefix string
	mu     sync.RWMutex
	data   map[string]*fileCSSAsset
	files  map[string]string
	order  []string
}

func newFileCSSAssets(prefix string) *fileCSSAssets {
	return &fileCSSAssets{
		prefix: strings.TrimRight(prefix, "/") + "/",
		data:   make(map[string]*fileCSSAsset),
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
		a.data[key] = &fileCSSAsset{raw: []byte(text)}
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
	asset, ok := a.data[key]
	file := a.files[r.URL.Query().Get("source")]
	a.mu.RUnlock()
	if !ok && file != "" {
		var data []byte
		data, ok = restoreFileCSSAsset(file, key, r.URL.Query().Get("scope"))
		asset = &fileCSSAsset{raw: data}
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	data, encoding := asset.raw, ""
	if r.Header.Get("Range") == "" {
		switch {
		case httpcompress.Accepts(r.Header.Get("Accept-Encoding"), "br"):
			asset.compress()
			data, encoding = asset.brotli, "br"
		case httpcompress.Accepts(r.Header.Get("Accept-Encoding"), "gzip"):
			asset.compress()
			data, encoding = asset.gzip, "gzip"
		}
	}
	w.Header().Set("Vary", "Accept-Encoding")
	etag := strings.TrimSuffix(key, ".css")
	if encoding != "" {
		w.Header().Set("Content-Encoding", encoding)
		etag += "." + encoding
	}
	w.Header().Set("ETag", strconv.Quote(etag))
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
