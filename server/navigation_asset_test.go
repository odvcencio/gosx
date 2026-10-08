package server

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
	"m31labs.dev/gosx/session"
)

func TestNavigationAssetHashAndRepresentations(t *testing.T) {
	wantPath := fmt.Sprintf("/gosx/assets/runtime/navigation.%x.js", sha256.Sum256([]byte(runtimehost.NavigationRuntime)))
	if runtimehost.NavigationRuntimePath != wantPath {
		t.Fatalf("runtime path = %q, want %q", runtimehost.NavigationRuntimePath, wantPath)
	}
	for _, tc := range []struct {
		name, accept, encoding, method, rangeHeader string
		off                                         bool
		status                                      int
		want                                        []byte
	}{
		{name: "identity", status: 200, want: []byte(runtimehost.NavigationRuntime)},
		{name: "gzip", accept: "gzip", encoding: "gzip", status: 200, want: runtimehost.NavigationRuntimeGzip},
		{name: "brotli", accept: "br, gzip", encoding: "br", status: 200, want: runtimehost.NavigationRuntimeBrotli},
		{name: "excluded", accept: "br;q=0, gzip;q=0", status: 200, want: []byte(runtimehost.NavigationRuntime)},
		{name: "disabled", accept: "br, gzip", off: true, status: 200, want: []byte(runtimehost.NavigationRuntime)},
		{name: "head", method: "HEAD", accept: "br, gzip", status: 200},
		{name: "range", accept: "br, gzip", rangeHeader: "bytes=0-15", status: 206, want: []byte(runtimehost.NavigationRuntime[:16])},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := New()
			if tc.off {
				app.DisableCompression()
			}
			handler := app.Build()
			method := tc.method
			if method == "" {
				method = "GET"
			}
			req := httptest.NewRequest(method, wantPath, nil)
			req.Header.Set("Accept-Encoding", tc.accept)
			req.Header.Set("Range", tc.rangeHeader)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tc.status || w.Header().Get("Content-Encoding") != tc.encoding || !bytes.Equal(w.Body.Bytes(), tc.want) {
				t.Fatalf("status=%d encoding=%q bytes=%d", w.Code, w.Header().Get("Content-Encoding"), w.Body.Len())
			}
			if w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/javascript") {
				t.Fatalf("asset headers = %v", w.Header())
			}
			if !tc.off && !strings.Contains(w.Header().Get("Vary"), "Accept-Encoding") {
				t.Fatal("missing encoding Vary")
			}
			if method == "GET" && tc.rangeHeader == "" {
				req.Header.Set("If-None-Match", w.Header().Get("ETag"))
				conditional := httptest.NewRecorder()
				handler.ServeHTTP(conditional, req)
				if conditional.Code != http.StatusNotModified || conditional.Body.Len() != 0 {
					t.Fatalf("conditional status=%d bytes=%d", conditional.Code, conditional.Body.Len())
				}
			}
		})
	}
	app := New().Build()
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", strings.Replace(wantPath, "navigation.", "navigation.wrong", 1), nil))
	if w.Code != 404 {
		t.Fatalf("unknown hash status=%d", w.Code)
	}
}

func TestNavigationAssetRemainsImmutableAfterSessionRead(t *testing.T) {
	m := session.MustNew("navigation-asset-test-secret", session.Options{})
	w := httptest.NewRecorder()
	m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session.Current(r).Set("viewer", "signed-in")
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/session", nil))
	if len(w.Result().Cookies()) != 1 {
		t.Fatal("expected session cookie")
	}
	cookie := w.Result().Cookies()[0]
	app := New()
	app.Use(m.Middleware)
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if session.Current(r).String("viewer") != "signed-in" {
				t.Fatal("middleware did not read the signed-in session")
			}
			next.ServeHTTP(w, r)
		})
	})
	handler := app.Build()
	for _, tc := range []struct {
		name, method, conditional, byteRange string
		status                               int
	}{
		{name: "get", method: http.MethodGet, status: http.StatusOK},
		{name: "head", method: http.MethodHead, status: http.StatusOK},
		{name: "conditional", method: http.MethodGet, conditional: `W/"` + strings.TrimSuffix(strings.TrimPrefix(runtimehost.NavigationRuntimePath, "/gosx/assets/runtime/"), ".js") + `"`, status: http.StatusNotModified},
		{name: "range", method: http.MethodGet, byteRange: "bytes=0-15", status: http.StatusPartialContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, runtimehost.NavigationRuntimePath, nil)
			r.AddCookie(cookie)
			r.Header.Set("Accept-Encoding", "br, gzip")
			r.Header.Set("If-None-Match", tc.conditional)
			r.Header.Set("Range", tc.byteRange)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status || w.Header().Get("Cache-Control") != immutableAssetCacheControl {
				t.Fatalf("status=%d headers=%v", w.Code, w.Header())
			}
			if headerHasToken(w.Header(), "Vary", "Cookie") || len(w.Result().Cookies()) != 0 {
				t.Fatalf("session-independent asset headers=%v", w.Header())
			}
		})
	}
}

func TestNavigationScriptDefersExternalRuntimeBeforeBootstrap(t *testing.T) {
	app := New()
	app.EnableNavigation()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		ctx.SetNonce("page-nonce")
		ctx.Runtime().EnableBootstrap()
		return gosx.Text("page")
	})
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	tag := `<script data-gosx-navigation="true" nonce="page-nonce" defer crossorigin="anonymous" referrerpolicy="no-referrer" src="` + runtimehost.NavigationRuntimePath + `"></script>`
	nav := strings.Index(body, tag)
	boot := strings.Index(body, `data-gosx-script="bootstrap"`)
	if nav < 0 || boot <= nav || !strings.Contains(body, `<script defer data-gosx-script="bootstrap"`) || strings.Contains(body, runtimehost.NavigationRuntime) {
		t.Fatal("navigation and bootstrap must defer in document order with the request nonce")
	}
}

func TestNavigationAssetStaleHashRevalidatesCurrentBytes(t *testing.T) {
	stalePath := "/gosx/assets/runtime/navigation." + strings.Repeat("0", 64) + ".js"
	handler := New().Build()
	currentETag := `W/"` + strings.TrimSuffix(strings.TrimPrefix(runtimehost.NavigationRuntimePath, "/gosx/assets/runtime/"), ".js") + `"`
	for _, tc := range []struct {
		name, method, accept, encoding, conditional, byteRange string
		status                                                 int
		want                                                   []byte
	}{
		{name: "identity", status: http.StatusOK, want: []byte(runtimehost.NavigationRuntime)},
		{name: "gzip", accept: "gzip", encoding: "gzip", status: http.StatusOK, want: runtimehost.NavigationRuntimeGzip},
		{name: "brotli", accept: "br, gzip", encoding: "br", status: http.StatusOK, want: runtimehost.NavigationRuntimeBrotli},
		{name: "head", method: http.MethodHead, status: http.StatusOK},
		{name: "conditional", conditional: currentETag, status: http.StatusNotModified},
		{name: "range", byteRange: "bytes=0-15", status: http.StatusPartialContent, want: []byte(runtimehost.NavigationRuntime[:16])},
	} {
		t.Run(tc.name, func(t *testing.T) {
			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			r := httptest.NewRequest(method, stalePath, nil)
			r.Header.Set("Accept-Encoding", tc.accept)
			r.Header.Set("If-None-Match", tc.conditional)
			r.Header.Set("Range", tc.byteRange)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-cache" || w.Header().Get("ETag") != currentETag || w.Header().Get("Content-Encoding") != tc.encoding || !bytes.Equal(w.Body.Bytes(), tc.want) {
				t.Fatalf("stale asset: status=%d headers=%v bytes=%d", w.Code, w.Header(), w.Body.Len())
			}
		})
	}
	for _, hash := range []string{"", strings.Repeat("0", 63), strings.Repeat("g", 64), strings.Repeat("0", 65)} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/gosx/assets/runtime/navigation."+hash+".js", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("malformed hash %q: status=%d", hash, w.Code)
		}
	}
}
