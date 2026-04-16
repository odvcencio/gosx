package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type wrappedStatusError struct {
	status int
}

type fakeRevalidationStore struct {
	pathVersions map[string]uint64
	tagVersions  map[string]uint64
	pathCalls    []string
	tagCalls     []string
}

func (s *fakeRevalidationStore) RevalidatePath(target string) uint64 {
	s.pathCalls = append(s.pathCalls, target)
	if s.pathVersions == nil {
		s.pathVersions = make(map[string]uint64)
	}
	next := s.pathVersions[cleanCachePath(target)] + 1
	s.pathVersions[cleanCachePath(target)] = next
	return next
}

func (s *fakeRevalidationStore) RevalidateTag(tag string) uint64 {
	s.tagCalls = append(s.tagCalls, strings.TrimSpace(tag))
	if s.tagVersions == nil {
		s.tagVersions = make(map[string]uint64)
	}
	tag = strings.TrimSpace(tag)
	next := s.tagVersions[tag] + 1
	s.tagVersions[tag] = next
	return next
}

func (s *fakeRevalidationStore) PathVersion(requestPath string) uint64 {
	return s.pathVersions[cleanCachePath(requestPath)]
}

func (s *fakeRevalidationStore) TagVersion(tag string) uint64 {
	return s.tagVersions[strings.TrimSpace(tag)]
}

type fakeISRStore struct {
	artifacts map[string]ISRArtifact
	states    map[string]ISRPageState
	locks     map[string]bool
	writes    int
}

func (s *fakeISRStore) StatArtifact(staticDir, pagePath, file string) (ISRArtifactInfo, error) {
	artifact, ok := s.artifacts[s.artifactKey(staticDir, pagePath, file)]
	if !ok {
		return ISRArtifactInfo{}, ErrISRArtifactNotFound
	}
	return ISRArtifactInfo{ModTime: artifact.ModTime}, nil
}

func (s *fakeISRStore) ReadArtifact(staticDir, pagePath, file string) (ISRArtifact, error) {
	artifact, ok := s.artifacts[s.artifactKey(staticDir, pagePath, file)]
	if !ok {
		return ISRArtifact{}, ErrISRArtifactNotFound
	}
	return artifact, nil
}

func (s *fakeISRStore) WriteArtifact(staticDir, pagePath, file string, body []byte) (ISRArtifactInfo, error) {
	if s.artifacts == nil {
		s.artifacts = make(map[string]ISRArtifact)
	}
	s.writes++
	artifact := ISRArtifact{
		Body:    append([]byte(nil), body...),
		ModTime: time.Now().UTC().Truncate(time.Second),
	}
	s.artifacts[s.artifactKey(staticDir, pagePath, file)] = artifact
	return ISRArtifactInfo{ModTime: artifact.ModTime}, nil
}

func (s *fakeISRStore) LoadState(bundleRoot, pagePath string, fallbackGeneratedAt time.Time) (ISRPageState, error) {
	if s.states == nil {
		s.states = make(map[string]ISRPageState)
	}
	key := s.stateKey(bundleRoot, pagePath)
	if state, ok := s.states[key]; ok {
		return state, nil
	}
	if fallbackGeneratedAt.IsZero() {
		fallbackGeneratedAt = time.Now().UTC()
	}
	state := ISRPageState{
		GeneratedAt: fallbackGeneratedAt.UTC(),
		TagVersions: map[string]uint64{},
	}
	s.states[key] = state
	return state, nil
}

func (s *fakeISRStore) SaveState(bundleRoot, pagePath string, state ISRPageState) error {
	if s.states == nil {
		s.states = make(map[string]ISRPageState)
	}
	s.states[s.stateKey(bundleRoot, pagePath)] = state
	return nil
}

func (s *fakeISRStore) AcquireRefresh(bundleRoot, pagePath string) (ISRRefreshLease, bool, error) {
	if s.locks == nil {
		s.locks = make(map[string]bool)
	}
	key := s.stateKey(bundleRoot, pagePath)
	if s.locks[key] {
		return nil, false, nil
	}
	s.locks[key] = true
	return fakeISRLease(func() {
		delete(s.locks, key)
	}), true, nil
}

func (s *fakeISRStore) artifactKey(staticDir, pagePath, file string) string {
	return staticDir + "|" + pagePath + "|" + file
}

func (s *fakeISRStore) stateKey(bundleRoot, pagePath string) string {
	return bundleRoot + "|" + pagePath
}

type fakeISRLease func()

func (fn fakeISRLease) Release() error {
	if fn != nil {
		fn()
	}
	return nil
}

func (e wrappedStatusError) Error() string {
	return http.StatusText(e.status)
}

func (e wrappedStatusError) StatusCode() int {
	return e.status
}

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
	if !strings.Contains(body, "<html") {
		t.Fatalf("expected '<html' in body, got %q", body)
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
	for _, snippet := range []string{
		`<html data-gosx-document="true">`,
		`<body data-gosx-document-body="true" data-gosx-enhancement-layer="html">`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
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

func TestResolveListenAddrKeepsInputHostWhenPortEnvHasNoHost(t *testing.T) {
	prev := os.Getenv("PORT")
	t.Cleanup(func() {
		if prev == "" {
			_ = os.Unsetenv("PORT")
			return
		}
		_ = os.Setenv("PORT", prev)
	})

	if err := os.Setenv("PORT", ":38177"); err != nil {
		t.Fatal(err)
	}

	if got := resolveListenAddr("127.0.0.1:3000"); got != "127.0.0.1:38177" {
		t.Fatalf("resolveListenAddr(127.0.0.1:3000) = %q, want %q", got, "127.0.0.1:38177")
	}
}

func TestResolveListenAddrSupportsBracketedIPv6Host(t *testing.T) {
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

	if got := resolveListenAddr("[::1]"); got != "[::1]:38177" {
		t.Fatalf("resolveListenAddr([::1]) = %q, want %q", got, "[::1]:38177")
	}
}

func TestWantsJSONAcceptsStructuredJSONMediaTypes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		path   string
		accept string
		want   bool
	}{
		{name: "problem json", path: "/docs", accept: "application/problem+json", want: true},
		{name: "vendor json", path: "/docs", accept: "application/vnd.api+json", want: true},
		{name: "html wins", path: "/docs", accept: "application/problem+json, text/html", want: false},
		{name: "api path forces json", path: "/api/meta", accept: "text/html", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Accept", tc.accept)
			if got := wantsJSON(req); got != tc.want {
				t.Fatalf("wantsJSON(%q, %q) = %v, want %v", tc.path, tc.accept, got, tc.want)
			}
		})
	}
}

func TestErrorStatusUsesWrappedStatusCoder(t *testing.T) {
	err := errors.Join(fmt.Errorf("outer"), fmt.Errorf("wrapped: %w", wrappedStatusError{status: http.StatusTeapot}))
	if got := errorStatus(err, http.StatusBadRequest, http.StatusInternalServerError); got != http.StatusTeapot {
		t.Fatalf("errorStatus(wrapped status coder) = %d, want %d", got, http.StatusTeapot)
	}
}

func TestNormalizePatternCanonicalizesRootRoutes(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		want    string
	}{
		{pattern: "/", want: "/{$}"},
		{pattern: "GET /", want: "GET /{$}"},
		{pattern: "  POST   /  ", want: "POST /{$}"},
		{pattern: "GET /docs", want: "GET /docs"},
	} {
		if got := normalizePattern(tc.pattern); got != tc.want {
			t.Fatalf("normalizePattern(%q) = %q, want %q", tc.pattern, got, tc.want)
		}
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

func TestAppUsesCustomRevalidationStore(t *testing.T) {
	app := New()
	store := &fakeRevalidationStore{
		pathVersions: map[string]uint64{},
		tagVersions:  map[string]uint64{},
	}
	app.SetRevalidationStore(store)
	app.Page("GET /cached", func(ctx *Context) gosx.Node {
		ctx.Cache(CachePolicy{Public: true, MaxAge: time.Minute})
		ctx.CacheTag("docs-pages")
		return gosx.Text("cached")
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/cached", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected ETag, got headers %#v", w.Header())
	}

	notModifiedReq := httptest.NewRequest(http.MethodGet, "/cached", nil)
	notModifiedReq.Header.Set("If-None-Match", etag)
	notModifiedRes := httptest.NewRecorder()
	handler.ServeHTTP(notModifiedRes, notModifiedReq)
	if notModifiedRes.Code != http.StatusNotModified {
		t.Fatalf("expected 304 before store invalidation, got %d", notModifiedRes.Code)
	}

	store.pathVersions["/cached"] = 10

	updatedReq := httptest.NewRequest(http.MethodGet, "/cached", nil)
	updatedReq.Header.Set("If-None-Match", etag)
	updatedRes := httptest.NewRecorder()
	handler.ServeHTTP(updatedRes, updatedReq)
	if updatedRes.Code != http.StatusOK {
		t.Fatalf("expected 200 after store invalidation, got %d", updatedRes.Code)
	}

	app.RevalidatePath("/cached")
	app.RevalidateTag("docs-pages")
	if len(store.pathCalls) != 1 || store.pathCalls[0] != "/cached" {
		t.Fatalf("expected path revalidation call, got %#v", store.pathCalls)
	}
	if len(store.tagCalls) != 1 || store.tagCalls[0] != "docs-pages" {
		t.Fatalf("expected tag revalidation call, got %#v", store.tagCalls)
	}
	if app.RevalidationStore() != store {
		t.Fatalf("expected app revalidation store to be the custom store")
	}
}

func TestAppReadinessChecksCanFail(t *testing.T) {
	app := New()
	app.UseReadyCheck("cache", ReadyCheckFunc(func(ctx context.Context) error {
		return errors.New("redis unavailable")
	}))

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	var report ReadinessReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode readiness report: %v", err)
	}
	if report.OK {
		t.Fatalf("expected not ready report, got %+v", report)
	}
	if len(report.Checks) != 1 {
		t.Fatalf("expected one readiness check, got %+v", report)
	}
	if report.Checks[0].Name != "cache" || report.Checks[0].OK || !strings.Contains(report.Checks[0].Error, "redis unavailable") {
		t.Fatalf("unexpected readiness check %+v", report.Checks[0])
	}
}

func TestAppRecoversFromPanics(t *testing.T) {
	app := New()
	app.SetErrorPage(func(ctx *Context, err error) gosx.Node {
		ctx.SetMetadata(Metadata{Title: Title{Absolute: "Broken"}})
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
			Title:       Title{Absolute: "Welcome"},
			Description: "Server metadata",
			Links: []LinkTag{
				{Rel: "stylesheet", Href: "/styles.css", Layer: CSSLayerPage, Owner: "metadata", Source: "styles.css"},
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
		`data-gosx-css-layer="page"`,
		`data-gosx-css-owner="metadata"`,
		`data-gosx-css-source="styles.css"`,
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
			Name:  "GoSXScene3D",
			Kind:  engine.KindSurface,
			Props: json.RawMessage(`{"width":640,"height":360}`),
		}, gosx.El("p", gosx.Text("Loading scene")))
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		`data-gosx-engine="GoSXScene3D"`,
		`data-gosx-enhance="scene3d"`,
		`data-gosx-enhance-layer="runtime"`,
		`data-gosx-fallback="server"`,
		`gosx-manifest`,
		`/gosx/bootstrap.js`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in runtime page body %q", snippet, body)
		}
	}
	for _, snippet := range []string{
		`/gosx/runtime.wasm`,
		`data-gosx-script="wasm-exec"`,
	} {
		if strings.Contains(body, snippet) {
			t.Fatalf("did not expect %q in scene bootstrap page body %q", snippet, body)
		}
	}
}

func TestAppLifecycleScriptRendersWithoutBootstrap(t *testing.T) {
	app := New()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		ctx.LifecycleScript("/runtime/page-lifecycle.js")
		return gosx.Text("body")
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `src="/runtime/page-lifecycle.js"`) {
		t.Fatalf("expected lifecycle script in %q", body)
	}
	if !strings.Contains(body, `data-gosx-script="lifecycle"`) {
		t.Fatalf("expected lifecycle role in %q", body)
	}
	if strings.Contains(body, `data-gosx-script="bootstrap"`) {
		t.Fatalf("did not expect bootstrap runtime in %q", body)
	}
}

func TestAppLifecycleScriptFollowsBootstrapAssets(t *testing.T) {
	app := New()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		ctx.Runtime().EnableBootstrap()
		ctx.LifecycleScript("/runtime/page-lifecycle.js")
		return gosx.Text("body")
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	bootstrap := strings.Index(body, `data-gosx-script="bootstrap"`)
	lifecycle := strings.Index(body, `data-gosx-script="lifecycle"`)
	if bootstrap < 0 {
		t.Fatalf("expected bootstrap runtime in %q", body)
	}
	if lifecycle < 0 {
		t.Fatalf("expected lifecycle script in %q", body)
	}
	if lifecycle < bootstrap {
		t.Fatalf("expected lifecycle script after bootstrap runtime in %q", body)
	}
}

func TestAppInjectsRuntimeHeadForVideoEnginePages(t *testing.T) {
	app := New()
	app.Page("GET /video", func(ctx *Context) gosx.Node {
		return ctx.Engine(engine.Config{
			Name: "PromoVideo",
			Kind: engine.KindVideo,
			Props: json.RawMessage(`{
				"src": "/media/promo.m3u8"
			}`),
		}, gosx.El("p", gosx.Text("Loading video")))
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/video", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		`data-gosx-engine="PromoVideo"`,
		`data-gosx-enhance="video"`,
		`data-gosx-enhance-layer="runtime"`,
		`gosx-manifest`,
		`/gosx/bootstrap-runtime.js`,
		`"hlsPath":"/gosx/hls.min.js"`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in runtime page body %q", snippet, body)
		}
	}
	for _, snippet := range []string{
		`/gosx/runtime.wasm`,
		`data-gosx-script="wasm-exec"`,
		`/gosx/patch.js`,
	} {
		if strings.Contains(body, snippet) {
			t.Fatalf("did not expect %q on video engine page: %q", snippet, body)
		}
	}
}

func TestAppVideoHelperRendersManagedBaselineAndRuntimeHead(t *testing.T) {
	app := New()
	app.Page("GET /video-helper", func(ctx *Context) gosx.Node {
		return ctx.Video(VideoProps{
			Poster:   "/media/poster.jpg",
			Controls: true,
			Sources: []VideoSource{
				{Src: "/media/promo.webm", Type: "video/webm"},
				{Src: "/media/promo.mp4", Type: "video/mp4"},
			},
			SubtitleTrack: "en",
			SubtitleTracks: []VideoTrack{
				{ID: "en", Language: "en", Title: "English", Kind: "captions", Src: "/subs/en-custom.vtt"},
			},
		}, gosx.El("p", gosx.Text("Download video")))
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/video-helper", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		`data-gosx-engine="GoSXVideo"`,
		`data-gosx-engine-kind="video"`,
		`data-gosx-enhance="video"`,
		`data-gosx-enhance-layer="runtime"`,
		`<video data-gosx-video-fallback="true"`,
		`poster="/media/poster.jpg"`,
		`<source src="/media/promo.webm" type="video/webm"`,
		`<source src="/media/promo.mp4" type="video/mp4"`,
		`<track src="/subs/en-custom.vtt" kind="captions" srclang="en" label="English"`,
		`<p>Download video</p>`,
		`gosx-manifest`,
		`/gosx/bootstrap-runtime.js`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in video helper page body %q", snippet, body)
		}
	}
	for _, snippet := range []string{
		`/gosx/runtime.wasm`,
		`data-gosx-script="wasm-exec"`,
		`/gosx/patch.js`,
	} {
		if strings.Contains(body, snippet) {
			t.Fatalf("did not expect %q on video helper page: %q", snippet, body)
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
		`data-gosx-enhance="text-layout"`,
		`data-gosx-enhance-layer="bootstrap"`,
		`data-gosx-fallback="html"`,
		`data-gosx-text-layout-role="block"`,
		`data-gosx-text-layout-surface="dom"`,
		`data-gosx-text-layout-state="hint"`,
		`data-gosx-text-layout-ready="false"`,
		`data-gosx-text-layout-font="600 16px serif"`,
		`data-gosx-text-layout-line-height="20"`,
		`data-gosx-text-layout-source="hello world from gosx"`,
		`data-gosx-text-layout-line-count-hint="`,
		`data-gosx-text-layout-height-hint="`,
		`/gosx/bootstrap-lite.js`,
		`data-gosx-bootstrap-mode="lite"`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in text layout page body %q", snippet, body)
		}
	}
	for _, snippet := range []string{
		`gosx-manifest`,
		`data-gosx-script="wasm-exec"`,
		`/gosx/patch.js`,
		`/gosx/bootstrap.js`,
	} {
		if strings.Contains(body, snippet) {
			t.Fatalf("did not expect %q in bootstrap-only text layout page body %q", snippet, body)
		}
	}
}

func TestAppKeepsNativeTextBlockPagesOffBootstrapHead(t *testing.T) {
	app := New()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		return ctx.TextBlock(TextBlockProps{
			Mode:       TextBlockModeNative,
			Text:       "hello world",
			Font:       "16px monospace",
			LineHeight: 20,
			MaxWidth:   70,
		})
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		`data-gosx-text-layout-mode="native"`,
		`style="white-space: pre; font: 16px monospace; line-height: 20px; max-width: 70px"`,
		"hello\nworld",
		`"bootstrap":false`,
		`"runtime":false`,
		`"bootstrapMode":"none"`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in native text layout page body %q", snippet, body)
		}
	}
	for _, snippet := range []string{
		`data-gosx-enhance="text-layout"`,
		`/gosx/bootstrap-lite.js`,
		`data-gosx-bootstrap-mode="lite"`,
		`gosx-manifest`,
		`data-gosx-script="wasm-exec"`,
		`/gosx/bootstrap.js`,
		`/gosx/patch.js`,
	} {
		if strings.Contains(body, snippet) {
			t.Fatalf("did not expect %q in native text layout page body %q", snippet, body)
		}
	}
}

func TestAppInjectsBootstrapHeadForMotionPages(t *testing.T) {
	app := New()
	app.Page("GET /motion", func(ctx *Context) gosx.Node {
		respectReduced := false
		return ctx.Motion(MotionProps{
			Tag:                  "section",
			Preset:               MotionPresetSlideUp,
			Trigger:              MotionTriggerView,
			Duration:             360,
			Delay:                40,
			Distance:             24,
			RespectReducedMotion: &respectReduced,
		}, gosx.Text("Animated hero copy"))
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/motion", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		`data-gosx-motion`,
		`data-gosx-enhance="motion"`,
		`data-gosx-enhance-layer="bootstrap"`,
		`data-gosx-motion-preset="slide-up"`,
		`data-gosx-motion-trigger="view"`,
		`data-gosx-motion-duration="360"`,
		`data-gosx-motion-delay="40"`,
		`data-gosx-motion-distance="24"`,
		`data-gosx-motion-respect-reduced="false"`,
		`/gosx/bootstrap-lite.js`,
		`data-gosx-bootstrap-mode="lite"`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in motion page body %q", snippet, body)
		}
	}
	for _, snippet := range []string{
		`gosx-manifest`,
		`data-gosx-script="wasm-exec"`,
		`/gosx/bootstrap.js`,
		`/gosx/patch.js`,
	} {
		if strings.Contains(body, snippet) {
			t.Fatalf("did not expect %q in bootstrap-only motion page body %q", snippet, body)
		}
	}
}

func TestAppEmitsDocumentContract(t *testing.T) {
	app := New()
	app.EnableNavigation()
	app.Page("GET /docs", func(ctx *Context) gosx.Node {
		ctx.SetMetadata(Metadata{Title: Title{Absolute: "Docs"}})
		return ctx.TextBlock(TextBlockProps{
			Text: "hello docs",
		})
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		`id="gosx-document"`,
		`data-gosx-document-contract`,
		`"pattern":"GET /docs"`,
		`"path":"/docs"`,
		`"title":"Docs"`,
		`"navigation":true`,
		`"bootstrap":true`,
		`"runtime":false`,
		`"bootstrapMode":"lite"`,
		`"bootstrapPath":"/gosx/bootstrap-lite.js`,
		`"id":"gosx-doc-get-docs"`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
}

func TestAppEmitsNavigationOnlyDocumentContract(t *testing.T) {
	app := New()
	app.EnableNavigation()
	app.Page("GET /docs/forms", func(ctx *Context) gosx.Node {
		ctx.SetMetadata(Metadata{Title: Title{Absolute: "Forms"}})
		return gosx.El("main", gosx.Text("Forms"))
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/docs/forms", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		`id="gosx-document"`,
		`data-gosx-document-contract`,
		`"pattern":"GET /docs/forms"`,
		`"path":"/docs/forms"`,
		`"title":"Forms"`,
		`"navigation":true`,
		`"bootstrap":true`,
		`"runtime":false`,
		`"bootstrapMode":"none"`,
		`"id":"gosx-doc-get-docs-forms"`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
	for _, snippet := range []string{
		`data-gosx-bootstrap-mode="`,
		`"bootstrapPath":"/gosx/`,
		`"runtimePath":"/gosx/`,
		`"wasmExecPath":"/gosx/`,
		`"patchPath":"/gosx/`,
	} {
		if strings.Contains(body, snippet) {
			t.Fatalf("did not expect %q in %q", snippet, body)
		}
	}
}

func TestAppSeedsInitialNavigationDocumentState(t *testing.T) {
	app := New()
	app.EnableNavigation()
	app.Page("GET /docs/forms", func(ctx *Context) gosx.Node {
		ctx.SetMetadata(Metadata{Title: Title{Absolute: "Forms"}})
		return gosx.El("main", gosx.Text("Forms"))
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/docs/forms?tab=posting", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		`<html data-gosx-document="true" data-gosx-document-id="gosx-doc-get-docs-forms" data-gosx-document-path="/docs/forms?tab=posting" data-gosx-navigation-state="idle" data-gosx-navigation-current-path="/docs/forms">`,
		`<body data-gosx-document-body="true" data-gosx-enhancement-layer="html" data-gosx-document-id="gosx-doc-get-docs-forms" data-gosx-navigation-state="idle" data-gosx-navigation-current-path="/docs/forms">`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
}

func TestCustomDocumentCanReuseDocumentContractAttrs(t *testing.T) {
	app := New()
	app.EnableNavigation()
	app.Page("GET /docs/forms", func(ctx *Context) gosx.Node {
		ctx.SetMetadata(Metadata{Title: Title{Absolute: "Forms"}})
		return gosx.El("main", gosx.Text("Forms"))
	})

	app.SetDocument(func(doc *DocumentContext) gosx.Node {
		return gosx.El("html",
			DocumentAttrs(doc),
			gosx.El("head",
				gosx.El("title", gosx.Text(doc.Title)),
				HeadOutlet(doc.Head),
			),
			gosx.El("body",
				DocumentBodyAttrs(doc),
				doc.Body,
			),
		)
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/docs/forms?tab=posting", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		`<html data-gosx-document="true" data-gosx-document-id="gosx-doc-get-docs-forms" data-gosx-document-path="/docs/forms?tab=posting" data-gosx-navigation-state="idle" data-gosx-navigation-current-path="/docs/forms">`,
		`<body data-gosx-document-body="true" data-gosx-enhancement-layer="html" data-gosx-document-id="gosx-doc-get-docs-forms" data-gosx-navigation-state="idle" data-gosx-navigation-current-path="/docs/forms">`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
}

func TestDocumentCurrentPathNormalizesDocumentAndRequestInputs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request string
		path    string
		want    string
	}{
		{name: "absolute document url", path: "https://example.com/docs/forms?tab=posting#intro", want: "/docs/forms"},
		{name: "relative document path", path: "docs/forms?tab=posting", want: "/docs/forms"},
		{name: "query only document path", path: "?tab=posting", want: "/"},
		{name: "fragment only document path", path: "#intro", want: "/"},
		{name: "dot segments collapse", path: "/docs/../runtime/./scene?tab=posting", want: "/runtime/scene"},
		{name: "request path wins", request: "/live/request?tab=active", path: "https://example.com/docs/forms?tab=posting", want: "/live/request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := &DocumentContext{
				Path:       tc.path,
				Navigation: true,
			}
			if tc.request != "" {
				doc.Request = httptest.NewRequest(http.MethodGet, tc.request, nil)
			}

			if got := documentCurrentPath(doc); got != tc.want {
				t.Fatalf("documentCurrentPath() = %q, want %q", got, tc.want)
			}
			if attrs := documentHTMLAttrs(doc); !strings.Contains(attrs, `data-gosx-navigation-current-path="`+tc.want+`"`) {
				t.Fatalf("expected html attrs to contain normalized current path %q, got %q", tc.want, attrs)
			}
			if attrs := documentBodyAttrs(doc); !strings.Contains(attrs, `data-gosx-navigation-current-path="`+tc.want+`"`) {
				t.Fatalf("expected body attrs to contain normalized current path %q, got %q", tc.want, attrs)
			}
			rendered := gosx.RenderHTML(gosx.El("html", DocumentAttrs(doc), gosx.El("body", DocumentBodyAttrs(doc))))
			if !strings.Contains(rendered, `data-gosx-navigation-current-path="`+tc.want+`"`) {
				t.Fatalf("expected custom document attrs to contain normalized current path %q, got %q", tc.want, rendered)
			}
		})
	}
}

func TestDocumentAttrsShareContractWithRenderedDocumentAttrs(t *testing.T) {
	doc := &DocumentContext{
		Request:       httptest.NewRequest(http.MethodGet, "/docs/../runtime/scene?tab=posting", nil),
		PageID:        "gosx-doc-runtime-scene",
		Path:          "https://example.com/runtime/scene?tab=posting#intro",
		Navigation:    true,
		RuntimeActive: false,
		Runtime: PageRuntimeSummary{
			BootstrapMode: "lite",
		},
	}

	htmlAttrs := documentHTMLAttrs(doc)
	bodyAttrs := documentBodyAttrs(doc)
	renderedHTML := gosx.RenderHTML(gosx.El("html", DocumentAttrs(doc)))
	renderedBody := gosx.RenderHTML(gosx.El("body", DocumentBodyAttrs(doc)))

	if !strings.Contains(renderedHTML, `<html`+htmlAttrs+`>`) {
		t.Fatalf("expected custom html attrs %q in %q", htmlAttrs, renderedHTML)
	}
	if !strings.Contains(renderedBody, `<body`+bodyAttrs+`>`) {
		t.Fatalf("expected custom body attrs %q in %q", bodyAttrs, renderedBody)
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

func TestAppServesCompatRuntimeHLSAssetFromSourceBuild(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "build")
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "bootstrap.js"), []byte("console.log('bootstrap');"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "hls.min.js"), []byte("window.Hls = function() {};"), 0644); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.SetRuntimeRoot(root)
	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, "/gosx/hls.min.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "no-cache") {
		t.Fatalf("expected source compat asset to disable caching, got %q", got)
	}
	if body := w.Body.String(); !strings.Contains(body, "window.Hls") {
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

func TestAppServesCompatRuntimeHLSAssetFromBuildManifest(t *testing.T) {
	root := t.TempDir()
	assetsDir := filepath.Join(root, "assets", "runtime")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "hls.min.3333.js"), []byte("window.Hls = function() {};"), 0644); err != nil {
		t.Fatal(err)
	}
	manifest := buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			VideoHLS: buildmanifest.HashedAsset{
				File: "hls.min.3333.js",
				Hash: "3333",
				Size: 26,
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

	req := httptest.NewRequest(http.MethodGet, "/gosx/hls.min.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("expected built compat asset to be immutable, got %q", got)
	}
	if body := w.Body.String(); !strings.Contains(body, "window.Hls") {
		t.Fatalf("unexpected built compat asset body %q", body)
	}
}

func TestAppServesDirectBuildManifestAssets(t *testing.T) {
	root := t.TempDir()
	assetsDir := filepath.Join(root, "assets", "islands")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.json"), []byte(`{"runtime":{},"islands":[],"css":[]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "Dashboard.3333.gxi"), []byte("island program"), 0644); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.SetRuntimeRoot(root)
	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, "/gosx/assets/islands/Dashboard.3333.gxi", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("expected immutable cache control for direct build asset, got %q", got)
	}
	if body := w.Body.String(); !strings.Contains(body, "island program") {
		t.Fatalf("unexpected direct build asset body %q", body)
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
	body := w.Body.String()
	if strings.Contains(body, "nope") {
		t.Fatalf("escaping manifest path must not expose secret file, got body=%q", body)
	}
	// With no valid bootstrap on disk, the dev stub is served instead of 404.
	if w.Code != http.StatusOK {
		t.Fatalf("expected bootstrap stub (200), got %d", w.Code)
	}
	if !strings.Contains(body, "window.__gosx") {
		t.Fatalf("expected bootstrap stub content, got %q", body)
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

func TestAppEnableISRReportsBackgroundRefreshFailure(t *testing.T) {
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
		Routes: []isrRoute{
			{Path: "/", File: "index.html", RevalidateSeconds: 1},
		},
	})

	app := New()
	app.SetRuntimeRoot(root)
	app.EnableISR()
	events := make(chan OperationEvent, 1)
	app.UseOperationObserver(OperationObserverFunc(func(event OperationEvent) {
		select {
		case events <- event:
		default:
		}
	}))
	app.Page("GET /", func(ctx *Context) gosx.Node {
		ctx.SetStatus(http.StatusInternalServerError)
		return gosx.Text("broken home")
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

	select {
	case event := <-events:
		if event.Component != "isr" || event.Operation != "refresh" || event.Target != "/" {
			t.Fatalf("unexpected operation event %+v", event)
		}
		if event.Status != "error" || !strings.Contains(event.Error, "unexpected status 500") {
			t.Fatalf("expected ISR refresh failure event, got %+v", event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for ISR failure event")
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

func TestISRConfigLoadUsesDistBundleAndDefaultsRoutes(t *testing.T) {
	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	if err := os.MkdirAll(filepath.Join(dist, "static"), 0755); err != nil {
		t.Fatal(err)
	}
	writeISRManifest(t, dist, isrManifest{
		Pages: []string{"/", "docs/", " /blog/ "},
	})

	cfg := &isrConfig{}
	if !cfg.load(root) {
		t.Fatal("expected dist ISR bundle to load")
	}
	if cfg.root != dist {
		t.Fatalf("expected bundle root %q, got %q", dist, cfg.root)
	}
	if cfg.staticDir != filepath.Join(dist, "static") {
		t.Fatalf("expected static dir inside dist, got %q", cfg.staticDir)
	}

	docs, ok := cfg.lookup("/docs/")
	if !ok {
		t.Fatal("expected docs page to load from manifest pages list")
	}
	if docs.Path != "/docs" {
		t.Fatalf("expected normalized docs path, got %q", docs.Path)
	}
	if docs.File != buildmanifest.ExportFilePath("/docs") {
		t.Fatalf("expected default docs export file, got %q", docs.File)
	}

	blog, ok := cfg.lookup("/blog")
	if !ok {
		t.Fatal("expected blog page to load from manifest pages list")
	}
	if blog.File != buildmanifest.ExportFilePath("/blog") {
		t.Fatalf("expected default blog export file, got %q", blog.File)
	}
}

func TestISRConfigLoadResetsStateWhenBundleRootChanges(t *testing.T) {
	rootA := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootA, "static", "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	writeISRManifest(t, rootA, isrManifest{
		Routes: []isrRoute{
			{Path: "/docs", File: filepath.Join("docs", "index.html"), Tags: []string{"docs"}},
		},
	})

	cfg := &isrConfig{store: NewInMemoryISRStore()}
	if !cfg.load(rootA) {
		t.Fatal("expected first ISR bundle to load")
	}
	if err := cfg.store.SaveState(cfg.root, "/docs", ISRPageState{
		GeneratedAt: time.Unix(1, 0).UTC(),
		PathVersion: 99,
		TagVersions: map[string]uint64{"docs": 42},
	}); err != nil {
		t.Fatal(err)
	}

	rootB := t.TempDir()
	targetB := filepath.Join(rootB, "static", "docs", "index.html")
	if err := os.MkdirAll(filepath.Dir(targetB), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetB, []byte("<!DOCTYPE html><html><body>docs v2</body></html>"), 0644); err != nil {
		t.Fatal(err)
	}
	modTime := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	if err := os.Chtimes(targetB, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	writeISRManifest(t, rootB, isrManifest{
		Routes: []isrRoute{
			{Path: "/docs", File: filepath.Join("docs", "index.html"), Tags: []string{"docs", "docs", " "}},
		},
	})

	if !cfg.load(rootB) {
		t.Fatal("expected second ISR bundle to load")
	}
	if cfg.root != rootB {
		t.Fatalf("expected bundle root %q, got %q", rootB, cfg.root)
	}

	page, ok := cfg.lookup("/docs")
	if !ok {
		t.Fatal("expected docs page after bundle swap")
	}
	if len(page.Tags) != 1 || page.Tags[0] != "docs" {
		t.Fatalf("expected compacted tags after reload, got %#v", page.Tags)
	}
	info, err := os.Stat(targetB)
	if err != nil {
		t.Fatal(err)
	}
	state := cfg.pageState(isrArtifact{
		page:       page,
		bundleRoot: cfg.root,
		staticDir:  filepath.Join(rootB, "static"),
		store:      cfg.store,
		modTime:    info.ModTime().UTC(),
	})
	if !state.GeneratedAt.Equal(info.ModTime().UTC()) {
		t.Fatalf("expected regenerated state time %v, got %v", info.ModTime().UTC(), state.GeneratedAt)
	}
	if state.PathVersion != 0 {
		t.Fatalf("expected reset path version after bundle swap, got %d", state.PathVersion)
	}
}

func TestAppEnableISRUsesCustomStoreForArtifactsAndState(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "static"), 0755); err != nil {
		t.Fatal(err)
	}
	writeISRManifest(t, root, isrManifest{
		Routes: []isrRoute{
			{Path: "/", File: "index.html"},
		},
	})

	store := &fakeISRStore{}
	app := New()
	app.SetRuntimeRoot(root)
	app.SetISRStore(store)
	app.EnableISR()

	dynamicCalls := 0
	app.Page("GET /", func(ctx *Context) gosx.Node {
		dynamicCalls++
		return gosx.Text("generated home")
	})

	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if body := w.Body.String(); !strings.Contains(body, "generated home") {
		t.Fatalf("expected generated body on first request, got %q", body)
	}
	if got := w.Header().Get("X-GoSX-ISR"); got != "MISS" {
		t.Fatalf("expected ISR miss header, got %q", got)
	}
	if dynamicCalls != 1 || store.writes != 1 {
		t.Fatalf("expected one dynamic render and one store write, got renders=%d writes=%d", dynamicCalls, store.writes)
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/", nil)
	secondReq.Header.Set("Accept", "text/html")
	secondRes := httptest.NewRecorder()
	handler.ServeHTTP(secondRes, secondReq)

	if got := secondRes.Header().Get("X-GoSX-ISR"); got != "HIT" {
		t.Fatalf("expected ISR hit after custom-store regeneration, got %q", got)
	}
	if dynamicCalls != 1 {
		t.Fatalf("expected custom store artifact to avoid a second dynamic render, got %d", dynamicCalls)
	}
	if app.ISRStore() != store {
		t.Fatalf("expected app ISR store to be the custom store")
	}
}

func TestAppEnableISRRegeneratesMissingArtifactAsMiss(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "static"), 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "static", "index.html")
	writeISRManifest(t, root, isrManifest{
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
		return gosx.Text("generated home")
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if body := w.Body.String(); !strings.Contains(body, "generated home") {
		t.Fatalf("expected regenerated body on missing ISR artifact, got %q", body)
	}
	if got := w.Header().Get("X-GoSX-ISR"); got != "MISS" {
		t.Fatalf("expected ISR miss header, got %q", got)
	}
	if dynamicCalls != 1 {
		t.Fatalf("expected one dynamic regeneration call, got %d", dynamicCalls)
	}
	if data, err := os.ReadFile(target); err != nil || !strings.Contains(string(data), "generated home") {
		t.Fatalf("expected regenerated artifact to be written, got err=%v body=%q", err, string(data))
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/", nil)
	secondReq.Header.Set("Accept", "text/html")
	secondRes := httptest.NewRecorder()
	handler.ServeHTTP(secondRes, secondReq)
	if got := secondRes.Header().Get("X-GoSX-ISR"); got != "HIT" {
		t.Fatalf("expected ISR hit after regeneration, got %q", got)
	}
	if dynamicCalls != 1 {
		t.Fatalf("expected regenerated artifact to avoid a second dynamic call, got %d", dynamicCalls)
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
		ctx.SetMetadata(Metadata{Title: Title{Absolute: "Missing"}})
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

func TestAppPageNotModifiedSkipsDocumentRender(t *testing.T) {
	app := New()
	documentRenders := 0
	app.SetDocument(func(doc *DocumentContext) gosx.Node {
		documentRenders++
		return gosx.El("html",
			DocumentAttrs(doc),
			gosx.El("head",
				gosx.El("title", gosx.Text(doc.Title)),
				HeadOutlet(doc.Head),
			),
			gosx.El("body",
				DocumentBodyAttrs(doc),
				doc.Body,
			),
		)
	})
	app.Page("GET /cached-doc", func(ctx *Context) gosx.Node {
		ctx.CachePublic(time.Minute)
		ctx.CacheTag("cached-doc")
		return gosx.Text("cached")
	})

	handler := app.Build()

	firstReq := httptest.NewRequest(http.MethodGet, "/cached-doc", nil)
	firstRes := httptest.NewRecorder()
	handler.ServeHTTP(firstRes, firstReq)
	if firstRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", firstRes.Code, firstRes.Body.String())
	}
	if documentRenders != 1 {
		t.Fatalf("expected first request to render document once, got %d", documentRenders)
	}
	etag := firstRes.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected etag in %v", firstRes.Header())
	}

	notModifiedReq := httptest.NewRequest(http.MethodGet, "/cached-doc", nil)
	notModifiedReq.Header.Set("If-None-Match", etag)
	notModifiedRes := httptest.NewRecorder()
	handler.ServeHTTP(notModifiedRes, notModifiedReq)
	if notModifiedRes.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d: %s", notModifiedRes.Code, notModifiedRes.Body.String())
	}
	if documentRenders != 1 {
		t.Fatalf("expected conditional request to skip document render, got %d renders", documentRenders)
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
		return gosx.El("main",
			Link("/docs", gosx.Text("Docs")),
			Form(
				gosx.Attrs(
					gosx.Attr("method", http.MethodGet),
					gosx.Attr("action", "/search"),
				),
				gosx.El("input", gosx.Attrs(gosx.Attr("name", "q"), gosx.Attr("value", "docs"))),
			),
		)
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
	if !strings.Contains(body, `data-gosx-enhance="navigation"`) || !strings.Contains(body, `data-gosx-fallback="native-link"`) {
		t.Fatalf("expected Link enhancement contract, got %q", body)
	}
	if !strings.Contains(body, `data-gosx-link-current-policy="auto"`) || !strings.Contains(body, `data-gosx-link-state="idle"`) {
		t.Fatalf("expected Link policy contract, got %q", body)
	}
	if !strings.Contains(body, "data-gosx-form") || !strings.Contains(body, `data-gosx-form-state="idle"`) {
		t.Fatalf("expected Form helper marker, got %q", body)
	}
	if !strings.Contains(body, `data-gosx-enhance="form"`) || !strings.Contains(body, `data-gosx-fallback="native-form"`) {
		t.Fatalf("expected Form enhancement contract, got %q", body)
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

func TestMountAppStripsPrefixBeforeDelegating(t *testing.T) {
	child := New()
	var childSawPath string
	child.Mount("/foo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		childSawPath = r.URL.Path
		_, _ = w.Write([]byte("child-ok"))
	}))

	parent := New()
	parent.MountApp("/cobalt/example", child)

	handler := parent.Build()
	req := httptest.NewRequest(http.MethodGet, "/cobalt/example/foo", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "child-ok") {
		t.Fatalf("expected child-ok body, got %q", body)
	}
	if childSawPath != "/foo" {
		t.Fatalf("expected child to see /foo after prefix strip, got %q", childSawPath)
	}
}

func TestMountAppRootPrefixDelegatesWithoutStrippingSlash(t *testing.T) {
	child := New()
	var childSawPath string
	child.Mount("/foo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		childSawPath = r.URL.Path
		_, _ = w.Write([]byte("root-child-ok"))
	}))

	parent := New()
	parent.MountApp("/", child)

	handler := parent.Build()
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "root-child-ok") {
		t.Fatalf("expected root-child-ok body, got %q", body)
	}
	if childSawPath != "/foo" {
		t.Fatalf("expected child to see /foo for root mount, got %q", childSawPath)
	}
}

func TestMountAppIncludesRoutesRegisteredAfterMount(t *testing.T) {
	child := New()
	parent := New()
	parent.MountApp("/child", child)

	var childSawPath string
	child.Mount("/late", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		childSawPath = r.URL.Path
		_, _ = w.Write([]byte("late-child-ok"))
	}))

	handler := parent.Build()
	req := httptest.NewRequest(http.MethodGet, "/child/late", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "late-child-ok") {
		t.Fatalf("expected late-child-ok body, got %q", body)
	}
	if childSawPath != "/late" {
		t.Fatalf("expected child to see /late after prefix strip, got %q", childSawPath)
	}
}

func TestAppServesBootstrapStubWhenNoBuildExists(t *testing.T) {
	// Use a temp dir with no build artifacts at all — simulates `go run`
	// without ever running `gosx build`.
	root := t.TempDir()

	app := New()
	app.SetRuntimeRoot(root)
	handler := app.Build()

	for _, name := range []string{"bootstrap.js", "bootstrap-lite.js", "bootstrap-runtime.js"} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/gosx/"+name, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200 for %s stub, got %d", name, w.Code)
			}
			if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
				t.Fatalf("expected javascript content-type, got %q", ct)
			}
			if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
				t.Fatalf("expected no-cache, got %q", cc)
			}
			body := w.Body.String()
			if !strings.Contains(body, "window.__gosx") {
				t.Fatalf("expected stub script body containing window.__gosx, got %q", body)
			}
		})
	}
}
