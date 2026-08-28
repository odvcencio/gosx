package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/server"
)

func TestVerifyBuildAssetsAcceptsCompleteManifestAndDetectsTampering(t *testing.T) {
	root := t.TempDir()
	writeTestBuildAsset(t, root, "runtime", "runtime.js", "runtime")
	writeTestBuildAsset(t, root, "css", "site.css", "styles")
	writeTestBuildAsset(t, root, "islands", "counter.bin", "island")
	manifest := `{
		"runtime":{"bootstrap":{"file":"runtime.js","hash":"d92c6a81b2ff5009","size":7}},
		"css":[{"component":"site","file":"site.css","hash":"90a7578caf8760be","size":6}],
		"islands":[{"name":"Counter","format":"bin","file":"counter.bin","hash":"28cd2c5c15d13978","size":6}]
	}`
	if err := os.WriteFile(filepath.Join(root, "build.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyBuildAssets(root); err != nil {
		t.Fatalf("complete manifest failed readiness: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "assets", "runtime", "runtime.js"), []byte("runtimf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyBuildAssets(root); err == nil || !strings.Contains(err.Error(), "hash does not match manifest") {
		t.Fatalf("expected tampered hash failure, got %v", err)
	}
}

func TestVerifyBuildAssetsRejectsMissingAndUnsafeFiles(t *testing.T) {
	for _, test := range []struct {
		name     string
		manifest string
		want     string
	}{
		{name: "missing", manifest: `{"runtime":{"bootstrap":{"file":"missing.js","size":1}}}`, want: "missing.js"},
		{name: "unsafe", manifest: `{"runtime":{"bootstrap":{"file":"../escape.js","size":1}}}`, want: "unsafe runtime asset path"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "build.json"), []byte(test.manifest), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := verifyBuildAssets(root); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("verifyBuildAssets() = %v; want error containing %q", err, test.want)
			}
		})
	}
}

func TestConfigureProductionReadinessChecksBuildAssetsOnlyWhenStaged(t *testing.T) {
	t.Setenv("GOSX_APP_ROOT", t.TempDir())
	app := server.New()
	configureProductionReadiness(app)
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "build-assets") {
		t.Fatalf("expected staged app readiness failure, status=%d body=%q", w.Code, w.Body.String())
	}

	t.Setenv("GOSX_APP_ROOT", "")
	local := server.New()
	configureProductionReadiness(local)
	localW := httptest.NewRecorder()
	local.Build().ServeHTTP(localW, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if localW.Code != http.StatusOK {
		t.Fatalf("expected local dev readiness, status=%d body=%q", localW.Code, localW.Body.String())
	}

	t.Setenv("GOSX_APP_ROOT", t.TempDir())
	t.Setenv("GOSX_DEV", "1")
	dev := server.New()
	configureProductionReadiness(dev)
	devW := httptest.NewRecorder()
	dev.Build().ServeHTTP(devW, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if devW.Code != http.StatusOK {
		t.Fatalf("expected dev readiness to skip production bundle proof, status=%d body=%q", devW.Code, devW.Body.String())
	}
}

func TestCanonicalDocsIndexBypassesStagedISRAndPreservesSearch(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "static", "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "export.json"), []byte(`{"pages":["/docs"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "static", "docs", "index.html"), []byte("stale cached index"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := server.New()
	app.SetRuntimeRoot(root)
	app.EnableISR()
	app.Use(canonicalDocsIndex)
	app.Mount("GET /docs", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("dynamic query=" + r.URL.Query().Get("q")))
	}))
	handler := app.Build()

	index := httptest.NewRecorder()
	handler.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/docs?q=webgpu", nil))
	if index.Code != http.StatusOK || index.Body.String() != "dynamic query=webgpu" {
		t.Fatalf("staged /docs search = %d %q", index.Code, index.Body.String())
	}

	slash := httptest.NewRecorder()
	handler.ServeHTTP(slash, httptest.NewRequest(http.MethodGet, "/docs/?q=webgpu", nil))
	if slash.Code != http.StatusPermanentRedirect || slash.Header().Get("Location") != "/docs?q=webgpu" {
		t.Fatalf("slash canonicalization = %d location %q", slash.Code, slash.Header().Get("Location"))
	}
}

func TestInteractiveAndQueryBackedRoutesAreNeverPrerendered(t *testing.T) {
	for _, routeDir := range []string{
		"app/docs/auth",
		"app/docs/forms",
		"app/demos/cms",
		"app/demos/scene3d-bench",
		"app/demos/water",
	} {
		t.Run(strings.ReplaceAll(routeDir, "/", "_"), func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(routeDir, "route.config.json"))
			if err != nil {
				t.Fatal(err)
			}
			var config struct {
				Prerender *bool `json:"prerender"`
			}
			if err := json.Unmarshal(body, &config); err != nil {
				t.Fatal(err)
			}
			if config.Prerender == nil || *config.Prerender {
				t.Fatalf("%s must set prerender:false", routeDir)
			}
		})
	}
}

func TestDocsSecurityHeadersPreserveSameOriginInteractiveSurfaces(t *testing.T) {
	handler := docsSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/demos/beacon", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("wrapped status = %d", w.Code)
	}
	for name, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "SAMEORIGIN",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	} {
		if got := w.Header().Get(name); got != want {
			t.Errorf("%s = %q; want %q", name, got, want)
		}
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "object-src 'none'") || !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Fatalf("CSP = %q", csp)
	}
}

func TestDocsSecurityHeadersRejectsPublicISRBypass(t *testing.T) {
	var observed string
	handler := docsSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = r.Header.Get(isrBypassHeader)
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/docs/getting-started", nil)
	request.Header.Set(isrBypassHeader, "attacker-controlled")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if observed != "" {
		t.Fatalf("downstream observed public ISR bypass header %q", observed)
	}
}

func TestDocsIndexSetsISRBypassOnlyAfterPublicHeaderIsStripped(t *testing.T) {
	var observed string
	handler := docsSecurityHeaders(canonicalDocsIndex(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = r.Header.Get(isrBypassHeader)
		w.WriteHeader(http.StatusNoContent)
	})))
	request := httptest.NewRequest(http.MethodGet, "/docs?q=webgpu", nil)
	request.Header.Set(isrBypassHeader, "attacker-controlled")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if observed != "docs-index-query" {
		t.Fatalf("trusted docs index marker = %q", observed)
	}
}

func TestDocsSessionSecretFailsClosedForPublicHTTPS(t *testing.T) {
	if _, err := docsSessionSecret("https://docs.example.test", ""); err == nil {
		t.Fatal("public HTTPS deployment accepted an empty SESSION_SECRET")
	}
	if got, err := docsSessionSecret("http://localhost:8080", ""); err != nil || got == "" {
		t.Fatalf("local HTTP fallback = %q, %v", got, err)
	}
	if got, err := docsSessionSecret("https://docs.example.test", "strong-secret-value"); err != nil || got != "strong-secret-value" {
		t.Fatalf("explicit secret = %q, %v", got, err)
	}
}

func TestDocsRequestBodyLimitRunsBeforeMultipartParsing(t *testing.T) {
	const maxBytes = 1024
	nextCalled := false
	handler := limitDocsRequestBodies(maxBytes)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		if err := r.ParseMultipartForm(maxBytes); err == nil {
			t.Error("oversized multipart body unexpectedly parsed")
		}
	}))
	request := httptest.NewRequest(http.MethodPost, "/docs/auth", bytes.NewReader(bytes.Repeat([]byte("x"), maxBytes+1)))
	request.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if !nextCalled {
		t.Fatal("body limit did not preserve middleware flow")
	}
	if request.MultipartForm != nil {
		t.Fatal("oversized request materialized multipart data")
	}
}

func writeTestBuildAsset(t *testing.T, root, bucket, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "assets", bucket)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCurrentSiteBuildInfoReportsReleaseAndSanitizedBuildFields(t *testing.T) {
	t.Setenv("PUBLIC_URL", "https://docs.example.test/")
	t.Setenv("GOSX_DOCS_REVISION", " deadbeef ")
	t.Setenv("GOSX_DOCS_BUILT_AT", "2026-08-12T20:30:00-07:00")

	got := currentSiteBuildInfo()
	// Derive the expected version from the framework rather than pinning it.
	// The site reports "v"+gosx.Version, so a release bump used to fail this
	// test for a reason unrelated to build-info handling.
	wantVersion := "v" + gosx.Version
	if got.FrameworkVersion != wantVersion {
		t.Fatalf("frameworkVersion = %q; want %q", got.FrameworkVersion, wantVersion)
	}
	if got.Revision != "deadbeef" || got.BuiltAt != "2026-08-13T03:30:00Z" {
		t.Fatalf("unexpected normalized build info: %#v", got)
	}
	if got.PublicURL != "https://docs.example.test" {
		t.Fatalf("publicURL = %q", got.PublicURL)
	}
}

func TestSiteDocumentsAreMachineReadableAndExcludeTestRoutes(t *testing.T) {
	t.Setenv("PUBLIC_URL", "https://docs.example.test")
	app := server.New()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "public", "site.webmanifest"), []byte(`{"name":"GoSX Docs"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mountSiteDocuments(app, root)
	app.SetPublicDir(filepath.Join(root, "public"))
	handler := app.Build()

	apiRequest := httptest.NewRequest(http.MethodGet, "/api/site", nil)
	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, apiRequest)
	if apiResponse.Code != http.StatusOK {
		t.Fatalf("/api/site status = %d; body=%q", apiResponse.Code, apiResponse.Body.String())
	}
	if got := apiResponse.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("/api/site Cache-Control = %q; want no-store", got)
	}
	var info siteBuildInfo
	if err := json.NewDecoder(apiResponse.Body).Decode(&info); err != nil {
		t.Fatalf("decode /api/site: %v", err)
	}
	if info.FrameworkVersion != "v"+gosx.Version || info.Status != "ok" {
		t.Fatalf("unexpected /api/site payload: %#v", info)
	}
	probeRequest := httptest.NewRequest(http.MethodPost, "/api/site/probe", nil)
	probeResponse := httptest.NewRecorder()
	handler.ServeHTTP(probeResponse, probeRequest)
	if probeResponse.Code != http.StatusOK {
		t.Fatalf("/api/site/probe status = %d body=%q", probeResponse.Code, probeResponse.Body.String())
	}
	var probe siteActionProbe
	if err := json.NewDecoder(probeResponse.Body).Decode(&probe); err != nil {
		t.Fatalf("decode /api/site/probe: %v", err)
	}
	if !probe.OK || probe.Revision == "" {
		t.Fatalf("unexpected /api/site/probe payload: %#v", probe)
	}

	manifestRequest := httptest.NewRequest(http.MethodGet, "/site.webmanifest", nil)
	manifestResponse := httptest.NewRecorder()
	handler.ServeHTTP(manifestResponse, manifestRequest)
	if got := manifestResponse.Header().Get("Content-Type"); got != "application/manifest+json; charset=utf-8" {
		t.Fatalf("manifest content type = %q", got)
	}

	sitemapRequest := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	sitemapResponse := httptest.NewRecorder()
	handler.ServeHTTP(sitemapResponse, sitemapRequest)
	if sitemapResponse.Code != http.StatusOK {
		t.Fatalf("/sitemap.xml status = %d; body=%q", sitemapResponse.Code, sitemapResponse.Body.String())
	}
	if got := sitemapResponse.Header().Get("Content-Type"); got != "application/xml; charset=utf-8" {
		t.Fatalf("sitemap content type = %q", got)
	}
	var sitemap sitemapURLSet
	if err := xml.Unmarshal(sitemapResponse.Body.Bytes(), &sitemap); err != nil {
		t.Fatalf("decode sitemap: %v", err)
	}
	joined := sitemapResponse.Body.String()
	for _, required := range []string{"https://docs.example.test/", "https://docs.example.test/docs", "https://docs.example.test/demos/playground"} {
		if !strings.Contains(joined, required) {
			t.Errorf("sitemap missing %q", required)
		}
	}
	if strings.Contains(joined, "/test/") {
		t.Fatalf("sitemap exposes test route: %q", joined)
	}

	robotsRequest := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	robotsResponse := httptest.NewRecorder()
	handler.ServeHTTP(robotsResponse, robotsRequest)
	if body := robotsResponse.Body.String(); !strings.Contains(body, "Sitemap: https://docs.example.test/sitemap.xml") {
		t.Fatalf("unexpected robots body %q", body)
	}
}
