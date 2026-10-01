package route

import (
	"compress/gzip"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/perf/wire"
	"m31labs.dev/gosx/server"
)

func TestExternalFileCSSPreservesScopeAndCachesVersions(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", `package app

func Page() Node {
	return <main class="page">External CSS</main>
}
`)
	writeRouteFile(t, root, "page.css", `.page { color: seagreen; }`)
	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(ctx.Document("Styles", body))
	})
	if err := router.AddDir(root, FileRoutesOptions{ExternalCSS: "/_gosx/test-css/"}); err != nil {
		t.Fatal(err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	page := get("/").Body.String()
	linkPattern := regexp.MustCompile(`href="([^\"]+\.css[^\"]*)"`)
	link := linkPattern.FindStringSubmatch(page)
	if len(link) != 2 || strings.Contains(page, "seagreen") {
		t.Fatalf("expected CSS link without inline rules: %s", page)
	}
	link[1] = html.UnescapeString(link[1])
	if !wire.IsHashedURL(link[1]) {
		t.Fatal("stylesheet URL must satisfy the immutable asset policy")
	}
	css := get(link[1])
	if css.Code != http.StatusOK || !strings.Contains(css.Body.String(), "seagreen") ||
		!strings.Contains(css.Body.String(), "data-gosx-s=") {
		t.Fatalf("scoped stylesheet missing: %d %s", css.Code, css.Body.String())
	}
	if css.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatal("content-addressed stylesheet must be immutable")
	}
	compressedRequest := httptest.NewRequest(http.MethodGet, link[1], nil)
	compressedRequest.Header.Set("Accept-Encoding", "br, gzip")
	compressed := httptest.NewRecorder()
	handler.ServeHTTP(compressed, compressedRequest)
	if compressed.Header().Get("Content-Encoding") != "gzip" ||
		!strings.Contains(compressed.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatal("stylesheet must negotiate compression for supported clients")
	}
	reader, err := gzip.NewReader(compressed.Body)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || string(decoded) != css.Body.String() {
		t.Fatalf("compressed stylesheet differs: %v", err)
	}
	conditional := httptest.NewRequest(http.MethodGet, link[1], nil)
	conditional.Header.Set("If-None-Match", css.Header().Get("ETag"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, conditional)
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Fatalf("conditional response: %d %s", w.Code, w.Body.String())
	}
	writeRouteFile(t, root, "page.css", `.page { color: darkorange; }`)
	updated := linkPattern.FindStringSubmatch(get("/").Body.String())
	if len(updated) == 2 {
		updated[1] = html.UnescapeString(updated[1])
	}
	if len(updated) != 2 || updated[1] == link[1] || !strings.Contains(get(updated[1]).Body.String(), "darkorange") {
		t.Fatal("edited CSS must publish a new asset")
	}
	if get(link[1]).Body.String() != css.Body.String() {
		t.Fatal("in-flight pages must retain their original stylesheet")
	}
	if get(strings.Replace(link[1], ".css?", ".missing?", 1)).Code != http.StatusNotFound {
		t.Fatal("unknown assets must fail closed")
	}
}

func TestExternalFileCSSRestoresExportedScopeAfterDirectoryMove(t *testing.T) {
	oldRoot, newRoot := t.TempDir(), t.TempDir()
	for _, root := range []string{oldRoot, newRoot} {
		writeRouteFile(t, root, "page.gsx", "package app\n\nfunc Page() Node {\n return <main>Styles</main>\n}\n")
		writeRouteFile(t, root, "page.css", `.page { color: seagreen; }`)
	}
	oldAssets := newFileCSSAssets("/_gosx/test-css/")
	css := sidecarCSSFile(oldRoot + "/page.gsx")
	node, ok := fileCSSNode(oldRoot, css, server.CSSLayerPage, 0, oldAssets)
	if !ok {
		t.Fatal("CSS did not load")
	}
	match := regexp.MustCompile(`href="([^\"]+)"`).FindStringSubmatch(gosx.RenderHTML(node))
	oldURL := html.UnescapeString(match[1])
	router := NewRouter()
	if err := router.AddDir(newRoot, FileRoutesOptions{ExternalCSS: "/_gosx/test-css/"}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	router.Build().ServeHTTP(w, httptest.NewRequest(http.MethodGet, oldURL, nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), fileCSSScopeID(css.path)) {
		t.Fatalf("exported CSS scope was lost: %d %s", w.Code, w.Body.String())
	}
	for _, query := range []string{"source=../private.css", "source=page.css&scope=invalid"} {
		requestURL := strings.Split(oldURL, "?")[0] + "?" + query
		w := httptest.NewRecorder()
		router.Build().ServeHTTP(w, httptest.NewRequest(http.MethodGet, requestURL, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("invalid restoration query: %d", w.Code)
		}
	}
}

func TestExternalFileCSSBoundsEditHistory(t *testing.T) {
	assets := newFileCSSAssets("/_gosx/test-css/")
	first := assets.put("first", "page.css", "", "")
	for i := 0; i < fileCSSAssetHistory; i++ {
		assets.put(strings.Repeat("x", i+1), "page.css", "", "")
	}
	if len(assets.data) != fileCSSAssetHistory || len(assets.order) != fileCSSAssetHistory {
		t.Fatal("stylesheet history exceeded its bound")
	}
	w := httptest.NewRecorder()
	assets.ServeHTTP(w, httptest.NewRequest(http.MethodGet, first, nil))
	if w.Code != http.StatusNotFound {
		t.Fatal("the oldest edit should retire")
	}
}
