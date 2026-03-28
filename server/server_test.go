package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/buildmanifest"
	"github.com/odvcencio/gosx/engine"
)

func TestAppBasic(t *testing.T) {
	app := New()
	app.Route("/", func(r *http.Request) gosx.Node {
		return gosx.Text("home")
	})

	handler := app.Build()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !strings.Contains(w.Body.String(), "home") {
		t.Fatalf("expected 'home' in body, got %q", w.Body.String())
	}
}

func TestAppWithLayout(t *testing.T) {
	app := New()
	app.SetLayout(func(title string, body gosx.Node) gosx.Node {
		return gosx.El("html", gosx.El("body", body))
	})
	app.Route("/page", func(r *http.Request) gosx.Node {
		return gosx.Text("content")
	})

	handler := app.Build()
	req := httptest.NewRequest("GET", "/page", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "content") {
		t.Fatalf("expected 'content' in body, got %q", body)
	}
	if !strings.Contains(body, "<html>") {
		t.Fatalf("expected '<html>' in body, got %q", body)
	}
}

func TestHTMLDocument(t *testing.T) {
	doc := HTMLDocument("Test Page", gosx.Text(""), gosx.Text("hello"))
	html := gosx.RenderHTML(doc)

	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Fatal("missing doctype")
	}
	if !strings.Contains(html, "<title>Test Page</title>") {
		t.Fatalf("missing title, got %q", html)
	}
	if !strings.Contains(html, "hello") {
		t.Fatal("missing body content")
	}
}

func TestResolveListenAddrUsesPortEnv(t *testing.T) {
	prev := os.Getenv("PORT")
	t.Cleanup(func() {
		if prev == "" {
			_ = os.Unsetenv("PORT")
			return
		}
		_ = os.Setenv("PORT", prev)
	})

	if err := os.Setenv("PORT", "38177"); err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		":3000":          ":38177",
		"127.0.0.1:3000": "127.0.0.1:38177",
		"localhost:3000": "localhost:38177",
		"0.0.0.0:3000":   "0.0.0.0:38177",
		"":               ":38177",
		"localhost":      "localhost:38177",
	}

	for input, want := range cases {
		if got := resolveListenAddr(input); got != want {
			t.Fatalf("resolveListenAddr(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResolveListenAddrPrefersExplicitPortAddrEnv(t *testing.T) {
	prev := os.Getenv("PORT")
	t.Cleanup(func() {
		if prev == "" {
			_ = os.Unsetenv("PORT")
			return
		}
		_ = os.Setenv("PORT", prev)
	})

	if err := os.Setenv("PORT", "127.0.0.1:38177"); err != nil {
		t.Fatal(err)
	}

	if got := resolveListenAddr(":3000"); got != "127.0.0.1:38177" {
		t.Fatalf("resolveListenAddr(:3000) = %q, want %q", got, "127.0.0.1:38177")
	}
}

func TestAppMultipleRoutes(t *testing.T) {
	app := New()
	app.Route("/foo", func(r *http.Request) gosx.Node {
		return gosx.Text("foo-page")
	})
	app.Route("/bar", func(r *http.Request) gosx.Node {
		return gosx.Text("bar-page")
	})

	handler := app.Build()

	req := httptest.NewRequest("GET", "/foo", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "foo-page") {
		t.Fatalf("expected 'foo-page', got %q", w.Body.String())
	}

	req2 := httptest.NewRequest("GET", "/bar", nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if !strings.Contains(w2.Body.String(), "bar-page") {
		t.Fatalf("expected 'bar-page', got %q", w2.Body.String())
	}
}

func TestAppSetsRequestIDAndSecurityHeaders(t *testing.T) {
	app := New()
	app.Route("/", func(r *http.Request) gosx.Node {
		id := RequestID(r)
		return gosx.Text("request:" + id)
	})

	handler := app.Build()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	requestID := w.Header().Get("X-Request-ID")
	if requestID == "" {
		t.Fatal("expected request id header")
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected nosniff header, got %q", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("unexpected referrer policy: %q", got)
	}
	if !strings.Contains(w.Body.String(), requestID) {
		t.Fatalf("expected handler to see request id %q, got %q", requestID, w.Body.String())
	}
}

func TestAppHealthAndReadinessEndpoints(t *testing.T) {
	app := New()
	handler := app.Build()

	for _, path := range []string{"/healthz", "/readyz"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", path, w.Code)
		}
		if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
			t.Fatalf("%s: expected json content type, got %q", path, got)
		}
		if body := strings.TrimSpace(w.Body.String()); body != `{"ok":true}` {
			t.Fatalf("%s: unexpected body %q", path, body)
		}
	}
}

func TestAppRecoversFromPanics(t *testing.T) {
	app := New()
	app.SetErrorPage(func(ctx *Context, err error) gosx.Node {
		ctx.SetMetadata(Metadata{Title: "Broken"})
		return gosx.Text("custom-error:" + err.Error())
	})
	app.Route("/panic", func(r *http.Request) gosx.Node {
		panic("boom")
	})

	handler := app.Build()
	req := httptest.NewRequest("GET", "/panic", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "custom-error:boom") {
		t.Fatalf("unexpected recovery body %q", w.Body.String())
	}
	if w.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected request id header on recovered response")
	}
}

func TestAppDefaultDocumentRendersMetadataAndHead(t *testing.T) {
	app := New()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		ctx.SetMetadata(Metadata{
			Title:       "Welcome",
			Description: "Server metadata",
			Links: []LinkTag{
				{Rel: "stylesheet", Href: "/styles.css"},
			},
		})
		ctx.AddHead(gosx.El("meta", gosx.Attrs(gosx.Attr("property", "og:title"), gosx.Attr("content", "Welcome"))))
		return gosx.Text("body")
	})

	handler := app.Build()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		"<!DOCTYPE html>",
		"<title>Welcome</title>",
		`name="description"`,
		`content="Server metadata"`,
		`href="/styles.css"`,
		`rel="stylesheet"`,
		`property="og:title" content="Welcome"`,
		"body",
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
}

func TestAppInjectsRuntimeHeadForEnginePages(t *testing.T) {
	app := New()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		return ctx.Engine(engine.Config{
			Name:     "GoSXScene3D",
			Kind:     engine.KindSurface,
			JSExport: "GoSXScene3D",
			Props:    json.RawMessage(`{"width":640,"height":360}`),
		}, gosx.El("p", gosx.Text("Loading scene")))
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		`data-gosx-engine="GoSXScene3D"`,
		`gosx-manifest`,
		`/gosx/runtime.wasm`,
		`/gosx/bootstrap.js`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in runtime page body %q", snippet, body)
		}
	}
}

func TestAppInjectsBootstrapHeadForTextBlockPages(t *testing.T) {
	app := New()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		return ctx.TextBlock(TextBlockProps{
			Font:       "600 16px serif",
			LineHeight: 20,
			MaxWidth:   240,
		}, gosx.Text("hello world from gosx"))
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		`data-gosx-text-layout`,
		`data-gosx-text-layout-role="block"`,
		`data-gosx-text-layout-surface="dom"`,
		`data-gosx-text-layout-state="hint"`,
		`data-gosx-text-layout-ready="false"`,
		`data-gosx-text-layout-font="600 16px serif"`,
		`data-gosx-text-layout-line-height="20"`,
		`data-gosx-text-layout-source="hello world from gosx"`,
		`data-gosx-text-layout-line-count-hint="`,
		`data-gosx-text-layout-height-hint="`,
		`gosx-manifest`,
		`/gosx/bootstrap.js`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in text layout page body %q", snippet, body)
		}
	}
	for _, snippet := range []string{
		`data-gosx-script="wasm-exec"`,
		`/gosx/patch.js`,
	} {
		if strings.Contains(body, snippet) {
			t.Fatalf("did not expect %q in bootstrap-only text layout page body %q", snippet, body)
		}
	}
}

func TestAppServesCompatRuntimeAssetsFromSourceBuild(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "build")
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "bootstrap.js"), []byte("console.log('bootstrap');"), 0644); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.SetRuntimeRoot(root)
	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, "/gosx/bootstrap.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "no-cache") {
		t.Fatalf("expected source compat asset to disable caching, got %q", got)
	}
	if body := w.Body.String(); !strings.Contains(body, "bootstrap") {
		t.Fatalf("unexpected compat asset body %q", body)
	}
}

func TestAppServesCompatRuntimeAssetsFromBuildManifest(t *testing.T) {
	root := t.TempDir()
	assetsDir := filepath.Join(root, "assets", "runtime")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "bootstrap.3333.js"), []byte("console.log('hashed bootstrap');"), 0644); err != nil {
		t.Fatal(err)
	}
	manifest := buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			Bootstrap: buildmanifest.HashedAsset{
				File: "bootstrap.3333.js",
				Hash: "3333",
				Size: 24,
			},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.SetRuntimeRoot(root)
	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, "/gosx/bootstrap.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("expected built compat asset to be immutable, got %q", got)
	}
	if body := w.Body.String(); !strings.Contains(body, "hashed bootstrap") {
		t.Fatalf("unexpected built compat asset body %q", body)
	}
}

func TestAppServesVersionedCompatRuntimeAssetsFromBuildManifestEvenWhenSourceBuildExists(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "build")
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "bootstrap.js"), []byte("console.log('source bootstrap');"), 0644); err != nil {
		t.Fatal(err)
	}

	assetsDir := filepath.Join(root, "assets", "runtime")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "bootstrap.3333.js"), []byte("console.log('hashed bootstrap');"), 0644); err != nil {
		t.Fatal(err)
	}
	manifest := buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			Bootstrap: buildmanifest.HashedAsset{
				File: "bootstrap.3333.js",
				Hash: "3333",
				Size: 24,
			},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.SetRuntimeRoot(root)
	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, "/gosx/bootstrap.js?v=3333", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("expected versioned compat asset to be immutable, got %q", got)
	}
	if body := w.Body.String(); !strings.Contains(body, "hashed bootstrap") {
		t.Fatalf("expected versioned compat asset to serve hashed build output, got %q", body)
	}
}

func TestAppRuntimeManifestCacheReloadsWhenBuildManifestChanges(t *testing.T) {
	root := t.TempDir()
	assetsDir := filepath.Join(root, "assets", "runtime")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "bootstrap.v1.js"), []byte("console.log('v1');"), 0644); err != nil {
		t.Fatal(err)
	}
	writeRuntimeManifest(t, root, "bootstrap.v1.js")

	app := New()
	app.SetRuntimeRoot(root)
	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, "/gosx/bootstrap.js", nil)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, req)
	if body := first.Body.String(); !strings.Contains(body, "v1") {
		t.Fatalf("expected first build manifest asset, got %q", body)
	}

	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(assetsDir, "bootstrap.v2.js"), []byte("console.log('v2');"), 0644); err != nil {
		t.Fatal(err)
	}
	writeRuntimeManifest(t, root, "bootstrap.v2.js")

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/gosx/bootstrap.js", nil))
	if body := second.Body.String(); !strings.Contains(body, "v2") {
		t.Fatalf("expected refreshed build manifest asset, got %q", body)
	}
}

func TestAppServesCompatRuntimeAssetsRejectsEscapingManifestFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "secret.js"), []byte("nope"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "assets", "runtime"), 0755); err != nil {
		t.Fatal(err)
	}
	manifest := buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			Bootstrap: buildmanifest.HashedAsset{File: "../secret.js"},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.SetRuntimeRoot(root)
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/gosx/bootstrap.js", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for escaping compat asset, got %d body=%q", w.Code, w.Body.String())
	}
}

func TestAppEnableISRServesStaticExportedPages(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "static"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "static", "index.html"), []byte("<!DOCTYPE html><html><body>static home</body></html>"), 0644); err != nil {
		t.Fatal(err)
	}
	writeISRManifest(t, root, isrManifest{
		Pages: []string{"/"},
		Routes: []isrRoute{
			{Path: "/", File: "index.html"},
		},
	})

	app := New()
	app.SetRuntimeRoot(root)
	app.EnableISR()

	dynamicCalls := 0
	app.Page("GET /", func(ctx *Context) gosx.Node {
		dynamicCalls++
		return gosx.Text("dynamic home")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, req)

	if body := w.Body.String(); !strings.Contains(body, "static home") {
		t.Fatalf("expected static export body, got %q", body)
	}
	if got := w.Header().Get("X-GoSX-ISR"); got != "HIT" {
		t.Fatalf("expected ISR hit header, got %q", got)
	}
	if dynamicCalls != 0 {
		t.Fatalf("expected static page to avoid dynamic handler, got %d calls", dynamicCalls)
	}
}

func TestAppEnableISRRefreshesStalePagesInBackground(t *testing.T) {
	root := t.TempDir()
	staticDir := filepath.Join(root, "static")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(staticDir, "index.html")
	if err := os.WriteFile(target, []byte("<!DOCTYPE html><html><body>stale home</body></html>"), 0644); err != nil {
		t.Fatal(err)
	}
	staleAt := time.Now().Add(-3 * time.Minute)
	if err := os.Chtimes(target, staleAt, staleAt); err != nil {
		t.Fatal(err)
	}
	writeISRManifest(t, root, isrManifest{
		Pages: []string{"/"},
		Routes: []isrRoute{
			{Path: "/", File: "index.html", RevalidateSeconds: 1},
		},
	})

	app := New()
	app.SetRuntimeRoot(root)
	app.EnableISR()

	dynamicCalls := 0
	app.Page("GET /", func(ctx *Context) gosx.Node {
		dynamicCalls++
		return gosx.Text("fresh home")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, req)

	if body := w.Body.String(); !strings.Contains(body, "stale home") {
		t.Fatalf("expected stale page to be served first, got %q", body)
	}
	if got := w.Header().Get("X-GoSX-ISR"); got != "STALE" {
		t.Fatalf("expected ISR stale header, got %q", got)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		if data, err := os.ReadFile(target); err == nil && strings.Contains(string(data), "fresh home") && dynamicCalls > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for ISR refresh; file=%q calls=%d", readFileMaybe(target), dynamicCalls)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestAppEnableISRRespectsOnDemandTagInvalidation(t *testing.T) {
	root := t.TempDir()
	staticDir := filepath.Join(root, "static", "docs")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(staticDir, "index.html")
	if err := os.WriteFile(target, []byte("<!DOCTYPE html><html><body>cached docs</body></html>"), 0644); err != nil {
		t.Fatal(err)
	}
	writeISRManifest(t, root, isrManifest{
		Pages: []string{"/docs"},
		Routes: []isrRoute{
			{Path: "/docs", File: filepath.Join("docs", "index.html"), Tags: []string{"docs-pages"}},
		},
	})

	app := New()
	app.SetRuntimeRoot(root)
	app.EnableISR()

	dynamicCalls := 0
	app.Page("GET /docs", func(ctx *Context) gosx.Node {
		dynamicCalls++
		return gosx.Text("fresh docs")
	})

	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	handler := app.Build()
	handler.ServeHTTP(w, req)
	if got := w.Header().Get("X-GoSX-ISR"); got != "HIT" {
		t.Fatalf("expected first ISR hit, got %q", got)
	}

	app.RevalidateTag("docs-pages")

	staleReq := httptest.NewRequest(http.MethodGet, "/docs", nil)
	staleReq.Header.Set("Accept", "text/html")
	staleRes := httptest.NewRecorder()
	handler.ServeHTTP(staleRes, staleReq)
	if got := staleRes.Header().Get("X-GoSX-ISR"); got != "STALE" {
		t.Fatalf("expected stale response after tag invalidation, got %q", got)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		if data, err := os.ReadFile(target); err == nil && strings.Contains(string(data), "fresh docs") && dynamicCalls > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for tag-driven ISR refresh; file=%q calls=%d", readFileMaybe(target), dynamicCalls)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestAppEnableISRRejectsEscapingStaticFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "static"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.html"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	writeISRManifest(t, root, isrManifest{
		Pages: []string{"/"},
		Routes: []isrRoute{
			{Path: "/", File: "../secret.html"},
		},
	})

	app := New()
	app.SetRuntimeRoot(root)
	app.EnableISR()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		return gosx.Text("dynamic fallback")
	})

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	app.Build().ServeHTTP(res, req)
	if body := res.Body.String(); !strings.Contains(body, "dynamic fallback") {
		t.Fatalf("expected dynamic fallback after rejecting escaping ISR file, got %q", body)
	}
	if got := res.Header().Get("X-GoSX-ISR"); got != "" {
		t.Fatalf("did not expect ISR header after escape rejection, got %q", got)
	}
}

func writeISRManifest(t *testing.T, root string, manifest isrManifest) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "export.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func readFileMaybe(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func writeRuntimeManifest(t *testing.T, root string, bootstrapFile string) {
	t.Helper()
	manifest := buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			Bootstrap: buildmanifest.HashedAsset{
				File: bootstrapFile,
				Hash: bootstrapFile,
				Size: 1,
			},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestAppServesPublicFilesAtRoot(t *testing.T) {
	dir := t.TempDir()
	publicDir := filepath.Join(dir, "public")
	if err := os.MkdirAll(publicDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "robots.txt"), []byte("User-agent: *\n"), 0644); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.SetPublicDir(publicDir)

	handler := app.Build()
	req := httptest.NewRequest("GET", "/robots.txt", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "User-agent") {
		t.Fatalf("unexpected public file body %q", w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=0, must-revalidate" {
		t.Fatalf("unexpected public asset cache-control %q", got)
	}
}

func TestAppCustomNotFoundWinsOverRootRouteCatchall(t *testing.T) {
	app := New()
	app.Route("/", func(r *http.Request) gosx.Node {
		return gosx.Text("home")
	})
	app.SetNotFound(func(ctx *Context) gosx.Node {
		ctx.SetMetadata(Metadata{Title: "Missing"})
		return gosx.Text("missing")
	})

	handler := app.Build()
	req := httptest.NewRequest("GET", "/missing", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "missing") {
		t.Fatalf("expected custom not found page, got %q", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "home") {
		t.Fatalf("expected root route to stay exact, got %q", w.Body.String())
	}
}

func TestAppAPIProducesJSON(t *testing.T) {
	app := New()
	app.API("GET /api/status", func(ctx *Context) (any, error) {
		ctx.SetStatus(http.StatusAccepted)
		return map[string]any{"ok": true}, nil
	})

	handler := app.Build()
	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected json content type, got %q", got)
	}
	if body := strings.TrimSpace(w.Body.String()); body != `{"ok":true}` {
		t.Fatalf("unexpected api body %q", body)
	}
}

func TestAppPageCacheHeadersAndRevalidation(t *testing.T) {
	app := New()
	app.Page("GET /cached", func(ctx *Context) gosx.Node {
		ctx.Cache(CachePolicy{
			Public:               true,
			MaxAge:               30 * time.Second,
			StaleWhileRevalidate: 2 * time.Minute,
		})
		ctx.CacheTag("docs-pages")
		return gosx.Text("cached")
	})

	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, "/cached", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	cacheControl := w.Header().Get("Cache-Control")
	for _, snippet := range []string{"public", "max-age=30", "stale-while-revalidate=120"} {
		if !strings.Contains(cacheControl, snippet) {
			t.Fatalf("expected %q in cache-control %q", snippet, cacheControl)
		}
	}
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected etag in %v", w.Header())
	}

	revalidateReq := httptest.NewRequest(http.MethodGet, "/cached", nil)
	revalidateReq.Header.Set("If-None-Match", etag)
	revalidateRes := httptest.NewRecorder()
	handler.ServeHTTP(revalidateRes, revalidateReq)
	if revalidateRes.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d: %s", revalidateRes.Code, revalidateRes.Body.String())
	}

	app.RevalidateTag("docs-pages")

	invalidatedReq := httptest.NewRequest(http.MethodGet, "/cached", nil)
	invalidatedReq.Header.Set("If-None-Match", etag)
	invalidatedRes := httptest.NewRecorder()
	handler.ServeHTTP(invalidatedRes, invalidatedReq)
	if invalidatedRes.Code != http.StatusOK {
		t.Fatalf("expected 200 after revalidate, got %d: %s", invalidatedRes.Code, invalidatedRes.Body.String())
	}
	if nextETag := invalidatedRes.Header().Get("ETag"); nextETag == "" || nextETag == etag {
		t.Fatalf("expected new etag after revalidate, got %q", nextETag)
	}
}

func TestAppCacheProfileHelpers(t *testing.T) {
	app := New()
	app.Page("GET /profile", func(ctx *Context) gosx.Node {
		ctx.CacheRevalidate(20*time.Second, 2*time.Minute, "profile-pages")
		return gosx.Text("profile")
	})

	req := httptest.NewRequest(http.MethodGet, "/profile", nil)
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	cacheControl := w.Header().Get("Cache-Control")
	for _, snippet := range []string{"public", "max-age=20", "stale-while-revalidate=120"} {
		if !strings.Contains(cacheControl, snippet) {
			t.Fatalf("expected %q in cache-control %q", snippet, cacheControl)
		}
	}
	if w.Header().Get("ETag") == "" {
		t.Fatalf("expected etag in %v", w.Header())
	}
}

func TestAppAPICacheHeadersRespectPathRevalidation(t *testing.T) {
	app := New()
	app.API("GET /api/meta", func(ctx *Context) (any, error) {
		ctx.CachePublic(time.Minute)
		ctx.CacheTag("meta")
		return map[string]any{"ok": true}, nil
	})

	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, "/api/meta", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected etag in %v", w.Header())
	}

	notModifiedReq := httptest.NewRequest(http.MethodGet, "/api/meta", nil)
	notModifiedReq.Header.Set("Accept", "application/json")
	notModifiedReq.Header.Set("If-None-Match", etag)
	notModifiedRes := httptest.NewRecorder()
	handler.ServeHTTP(notModifiedRes, notModifiedReq)
	if notModifiedRes.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d: %s", notModifiedRes.Code, notModifiedRes.Body.String())
	}

	app.RevalidatePath("/api/meta")

	updatedReq := httptest.NewRequest(http.MethodGet, "/api/meta", nil)
	updatedReq.Header.Set("Accept", "application/json")
	updatedReq.Header.Set("If-None-Match", etag)
	updatedRes := httptest.NewRecorder()
	handler.ServeHTTP(updatedRes, updatedReq)
	if updatedRes.Code != http.StatusOK {
		t.Fatalf("expected 200 after path revalidate, got %d: %s", updatedRes.Code, updatedRes.Body.String())
	}
}

func TestAppObserverCapturesRouteMetadata(t *testing.T) {
	app := New()
	var events []RequestEvent
	app.UseObserver(RequestObserverFunc(func(event RequestEvent) {
		events = append(events, event)
	}))
	app.Page("GET /docs/{slug}", func(ctx *Context) gosx.Node {
		return gosx.Text("docs")
	})

	req := httptest.NewRequest(http.MethodGet, "/docs/intro", nil)
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, req)

	if len(events) != 1 {
		t.Fatalf("expected one event, got %#v", events)
	}
	event := events[0]
	if event.Kind != "page" {
		t.Fatalf("expected page kind, got %#v", event)
	}
	if event.Pattern != "GET /docs/{slug}" {
		t.Fatalf("expected route pattern, got %#v", event)
	}
	if event.Path != "/docs/intro" || event.Status != http.StatusOK {
		t.Fatalf("unexpected event %#v", event)
	}
	if event.ID == "" {
		t.Fatalf("expected request id in %#v", event)
	}
}

func TestHandlePageAppliesRouteMiddleware(t *testing.T) {
	app := New()
	app.HandlePage(PageRoute{
		Pattern: "GET /secure",
		Middleware: []Middleware{
			func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("X-Route", "page")
					next.ServeHTTP(w, r)
				})
			},
		},
		Handler: func(ctx *Context) gosx.Node {
			return gosx.Text("secure")
		},
	})

	handler := app.Build()
	req := httptest.NewRequest("GET", "/secure", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("X-Route"); got != "page" {
		t.Fatalf("expected page middleware header, got %q", got)
	}
}

func TestHandleAPIAppliesRouteMiddleware(t *testing.T) {
	app := New()
	app.HandleAPI(APIRoute{
		Pattern: "GET /api/secure",
		Middleware: []Middleware{
			func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("X-Route", "api")
					next.ServeHTTP(w, r)
				})
			},
		},
		Handler: func(ctx *Context) (any, error) {
			return map[string]bool{"ok": true}, nil
		},
	})

	handler := app.Build()
	req := httptest.NewRequest("GET", "/api/secure", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("X-Route"); got != "api" {
		t.Fatalf("expected api middleware header, got %q", got)
	}
}

func TestEnableNavigationInjectsRuntimeIntoDefaultDocument(t *testing.T) {
	app := New()
	app.EnableNavigation()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		return Link("/docs", gosx.Text("Docs"))
	})

	handler := app.Build()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "data-gosx-navigation") {
		t.Fatalf("expected navigation runtime in page, got %q", body)
	}
	if !strings.Contains(body, "data-gosx-link") {
		t.Fatalf("expected Link helper marker, got %q", body)
	}
	if !strings.Contains(body, "gosx-head-start") || !strings.Contains(body, "gosx-head-end") {
		t.Fatalf("expected managed head markers, got %q", body)
	}
}

func TestAppDeferredRegionStreamsIntoHTMLDocument(t *testing.T) {
	app := New()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		return gosx.El("main",
			ctx.Defer(
				gosx.El("p", gosx.Text("loading")),
				func() (gosx.Node, error) {
					return gosx.El("section", gosx.Text("resolved")), nil
				},
			),
		)
	})

	handler := app.Build()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		"loading",
		"resolved",
		`data-gosx-deferred`,
		`data-gosx-stream-template`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
	if strings.Contains(body, streamTailMarker) {
		t.Fatalf("expected stream tail marker to be removed, got %q", body)
	}
}

func TestAppRedirectInterpolatesPathValues(t *testing.T) {
	app := New()
	app.Redirect("GET /legacy/{slug}", "/docs/{slug}", http.StatusMovedPermanently)

	handler := app.Build()
	req := httptest.NewRequest("GET", "/legacy/getting-started", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 301, got %d", w.Code)
	}
	if got := w.Header().Get("Location"); got != "/docs/getting-started" {
		t.Fatalf("unexpected location %q", got)
	}
}

func TestAppRewriteInternallyDispatchesTargetRoute(t *testing.T) {
	app := New()
	app.Rewrite("GET /latest", "/docs/getting-started")
	app.Page("GET /docs/getting-started", func(ctx *Context) gosx.Node {
		return gosx.Text("docs-home")
	})

	handler := app.Build()
	req := httptest.NewRequest("GET", "/latest", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "docs-home") {
		t.Fatalf("unexpected rewrite body %q", body)
	}
}

func TestAppMethodMismatchStillReturns405(t *testing.T) {
	app := New()
	app.Page("GET /only-get", func(ctx *Context) gosx.Node {
		return gosx.Text("ok")
	})

	handler := app.Build()
	req := httptest.NewRequest("POST", "/only-get", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestAppRewriteCanTargetMountedHandler(t *testing.T) {
	app := New()
	app.Rewrite("GET /latest", "/docs/getting-started")

	router := http.NewServeMux()
	router.HandleFunc("GET /docs/getting-started", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("docs-home"))
	})
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "mounted-missing", http.StatusNotFound)
	})
	app.Mount("/", router)

	handler := app.Build()
	req := httptest.NewRequest("GET", "/latest", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "docs-home") {
		t.Fatalf("unexpected rewrite body %q", body)
	}
}

func TestAppPublicFilesBeatMountedCatchall(t *testing.T) {
	dir := t.TempDir()
	publicDir := filepath.Join(dir, "public")
	if err := os.MkdirAll(publicDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "docs.css"), []byte("body{}"), 0644); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.SetPublicDir(publicDir)
	app.Mount("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "mounted-catchall", http.StatusNotFound)
	}))

	handler := app.Build()
	req := httptest.NewRequest("GET", "/docs.css", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); strings.Contains(body, "mounted-catchall") || !strings.Contains(body, "body{}") {
		t.Fatalf("unexpected public asset body %q", body)
	}
}

func TestMountedHandlerPreservesFlushUnderObservers(t *testing.T) {
	app := New()
	observed := make(chan RequestEvent, 1)
	app.UseObserver(RequestObserverFunc(func(event RequestEvent) {
		observed <- event
	}))
	app.Mount("/events", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("mounted handler response writer does not implement http.Flusher")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: ping\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !w.Flushed {
		t.Fatal("expected recorder to be flushed")
	}
	if body := w.Body.String(); !strings.Contains(body, "event: ping") || !strings.Contains(body, "data: ok") {
		t.Fatalf("unexpected SSE body %q", body)
	}

	select {
	case event := <-observed:
		if event.Kind != "mount" {
			t.Fatalf("expected observed kind mount, got %q", event.Kind)
		}
		if event.Pattern != "/events" {
			t.Fatalf("expected observed pattern /events, got %q", event.Pattern)
		}
		if event.Status != http.StatusOK {
			t.Fatalf("expected observed status 200, got %d", event.Status)
		}
	default:
		t.Fatal("expected observer to receive mounted request event")
	}
}
