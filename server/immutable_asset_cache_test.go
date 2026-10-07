package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/session"
)

const immutableAssetCacheControl = "public, max-age=31536000, immutable"

// immutableCacheApp models auth middleware that reads the session on every
// request, including requests for framework assets.
func immutableCacheApp(t *testing.T) (*App, *http.Cookie, []string) {
	t.Helper()
	root := t.TempDir()
	m := session.MustNew("immutable-asset-test-secret", session.Options{})
	w := httptest.NewRecorder()
	m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session.Current(r).Set("viewer", "signed-in")
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/login", nil))
	if len(w.Result().Cookies()) != 1 {
		t.Fatal("expected session cookie")
	}
	cookie := w.Result().Cookies()[0]
	manifest := buildmanifest.Manifest{}
	var urls []string
	emit := func(bucket, name string, body []byte) buildmanifest.HashedAsset {
		t.Helper()
		hash := buildmanifest.ContentHash(body)
		ext := filepath.Ext(name)
		file := strings.TrimSuffix(name, ext) + "." + hash + ext
		fsPath := filepath.Join(root, "assets", bucket, file)
		if err := os.MkdirAll(filepath.Dir(fsPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fsPath, body, 0644); err != nil {
			t.Fatal(err)
		}
		if err := writeTestGzip(fsPath+".gz", body); err != nil {
			t.Fatal(err)
		}
		if err := writeTestBrotli(fsPath+".br", body); err != nil {
			t.Fatal(err)
		}
		urls = append(urls, buildmanifest.AssetURL("/gosx/assets", bucket, file))
		return buildmanifest.HashedAsset{File: file, Hash: hash, Size: int64(len(body))}
	}
	for _, runtime := range []struct {
		name  string
		asset *buildmanifest.HashedAsset
	}{
		{"runtime.wasm", &manifest.Runtime.WASM},
		{"runtime-islands.wasm", &manifest.Runtime.WASMIslands},
		{"wasm_exec.js", &manifest.Runtime.WASMExec},
		{"standard-go-wasm_exec.js", &manifest.Runtime.StandardGoWASMExec},
		{"bootstrap.js", &manifest.Runtime.Bootstrap},
		{"bootstrap-lite.js", &manifest.Runtime.BootstrapLite},
		{"bootstrap-runtime.js", &manifest.Runtime.BootstrapRuntime},
		{"bootstrap-feature-islands.js", &manifest.Runtime.BootstrapFeatureIslands},
		{"bootstrap-feature-engines.js", &manifest.Runtime.BootstrapFeatureEngines},
		{"bootstrap-feature-hubs.js", &manifest.Runtime.BootstrapFeatureHubs},
		{"bootstrap-feature-controllers.js", &manifest.Runtime.BootstrapFeatureControllers},
		{"bootstrap-feature-textlayout.js", &manifest.Runtime.BootstrapFeatureTextlayout},
		{"bootstrap-feature-scene3d.js", &manifest.Runtime.BootstrapFeatureScene3D},
		{"bootstrap-feature-scene3d-command.js", &manifest.Runtime.BootstrapFeatureScene3DCommand},
		{"bootstrap-feature-scene3d-hydrate.js", &manifest.Runtime.BootstrapFeatureScene3DHydrate},
		{"bootstrap-feature-scene3d-webgpu.js", &manifest.Runtime.BootstrapFeatureScene3DWebGPU},
		{"bootstrap-feature-scene3d-webgl.js", &manifest.Runtime.BootstrapFeatureScene3DWebGL},
		{"bootstrap-feature-scene3d-gltf.js", &manifest.Runtime.BootstrapFeatureScene3DGLTF},
		{"bootstrap-feature-scene3d-animation.js", &manifest.Runtime.BootstrapFeatureScene3DAnimation},
		{"bootstrap-feature-scene3d-compute.js", &manifest.Runtime.BootstrapFeatureScene3DCompute},
		{"bootstrap-feature-scene3d-decompress.js", &manifest.Runtime.BootstrapFeatureScene3DDecompress},
		{"bootstrap-feature-scene3d-walk.js", &manifest.Runtime.BootstrapFeatureScene3DWalk},
		{"bootstrap-feature-scene3d-zoom.js", &manifest.Runtime.BootstrapFeatureScene3DZoom},
		{"bootstrap-feature-scene3d-vessel.js", &manifest.Runtime.BootstrapFeatureScene3DVessel},
		{"bootstrap-feature-scene3d-ocean-query.js", &manifest.Runtime.BootstrapFeatureScene3DOceanQuery},
		{"bootstrap-feature-scene3d-instance-stream.js", &manifest.Runtime.BootstrapFeatureScene3DInstanceStream},
		{"patch.js", &manifest.Runtime.Patch},
		{"hls.min.js", &manifest.Runtime.VideoHLS},
		{"stripe-bridge.js", &manifest.Runtime.StripeBridge},
		{"relay.js", &manifest.Runtime.Relay},
	} {
		body := []byte("/* " + runtime.name + " */\n" + strings.Repeat("// runtime\n", 150))
		if strings.HasSuffix(runtime.name, ".wasm") {
			body = []byte("\x00asm\x01\x00\x00\x00")
		}
		*runtime.asset = emit("runtime", runtime.name, body)
		urls = append(urls, "/gosx/"+runtime.name, "/gosx/"+runtime.name+"?v="+runtime.asset.Hash)
	}
	manifest.Runtime.WASMVariants = make(map[string]buildmanifest.RuntimeVariantAsset)
	for _, variant := range []string{"core", "engine", "collab"} {
		asset := emit("runtime", "runtime-"+variant+".wasm", []byte("\x00asm\x01\x00\x00\x00"+variant))
		manifest.Runtime.WASMVariants[variant] = buildmanifest.RuntimeVariantAsset{HashedAsset: asset}
		urls = append(urls, "/gosx/runtime-"+variant+".wasm", "/gosx/runtime-"+variant+".wasm?v="+asset.Hash)
	}
	manifest.Islands = []buildmanifest.IslandAsset{{Name: "Counter", HashedAsset: emit("islands", "Counter.gxi", []byte("island program"))}}
	manifest.CSS = []buildmanifest.CSSAsset{{Component: "Counter", Source: "Counter.css", HashedAsset: emit("css", "Counter.css", []byte("main { color: black; }"))}}
	urls = append(urls, "/gosx/islands/Counter.gxi", "/gosx/islands/Counter.gxi?v=build", "/gosx/css/Counter.css", "/gosx/css/Counter.css?v=build")
	// Image variants and posters share the emitted-asset handler.
	imagePath := filepath.Join(root, "image.png")
	if err := writeTestPNG(imagePath, 2, 2); err != nil {
		t.Fatal(err)
	}
	imageBytes, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	emit("images", "hero.png", imageBytes)
	emit("posters", "scene.png", imageBytes)
	writeManifest(t, root, manifest)
	publicDir := filepath.Join(root, "public")
	if err := os.MkdirAll(publicDir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"app.js":       "console.log('public asset');",
		"account.html": "<main>account</main>",
		"account.json": `{"viewer":"signed-in"}`,
	} {
		if err := os.WriteFile(filepath.Join(publicDir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	urls = append(urls, "/app.js?v="+buildmanifest.ContentHash([]byte("console.log('public asset');")))
	app := New()
	app.SetRuntimeRoot(root)
	app.SetPublicDir(publicDir)
	app.SetImageDir(root)
	app.Use(m.Middleware)
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = session.Current(r).String("viewer")
			next.ServeHTTP(w, r)
		})
	})
	privatePage := func(ctx *Context) gosx.Node {
		ctx.CacheStatic()
		return gosx.El("main", gosx.Text(session.Current(ctx.Request).String("viewer")))
	}
	app.Page("GET /private", privatePage)
	app.Page("GET /gosx/assets/runtime/account.hash.js", privatePage)
	app.API("GET /data", func(ctx *Context) (any, error) {
		ctx.CacheStatic()
		return map[string]string{"viewer": session.Current(ctx.Request).String("viewer")}, nil
	})
	return app, cookie, urls
}

func TestImmutableAssetsRemainPublicAfterGlobalSessionRead(t *testing.T) {
	app, cookie, urls := immutableCacheApp(t)
	h := app.Build()
	for _, url := range urls {
		t.Run(url, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				for _, encoding := range []string{"identity", "gzip", "br"} {
					var sharedBody []byte
					for _, visitor := range []*http.Cookie{cookie, nil} {
						r := httptest.NewRequest(method, url, nil)
						r.Header.Set("Accept-Encoding", encoding)
						if visitor != nil {
							r.AddCookie(visitor)
						}
						w := httptest.NewRecorder()
						h.ServeHTTP(w, r)
						res := w.Result()
						if res.StatusCode != http.StatusOK || res.Header.Get("Cache-Control") != immutableAssetCacheControl {
							t.Fatalf("%s encoding=%s session=%t: status=%d Cache-Control=%q", method, encoding, visitor != nil, res.StatusCode, res.Header.Get("Cache-Control"))
						}
						if headerHasToken(res.Header, "Vary", "Cookie") || len(res.Cookies()) != 0 {
							t.Fatalf("session-independent asset headers: %v", res.Header)
						}
						if !headerHasToken(res.Header, "Vary", "Accept-Encoding") {
							t.Fatalf("missing encoding variance: %v", res.Header)
						}
						if method == http.MethodGet && w.Body.Len() == 0 {
							t.Fatal("empty asset body")
						}
						if visitor != nil {
							sharedBody = bytes.Clone(w.Body.Bytes())
						} else if !bytes.Equal(sharedBody, w.Body.Bytes()) {
							t.Fatal("asset bytes vary by session")
						}
					}
				}
			}
		})
	}
}

func TestImmutableAssetCacheKeepsPrivateBoundaries(t *testing.T) {
	for _, boundary := range []struct {
		name       string
		modify     func(http.ResponseWriter, *http.Request)
		wantCookie bool
	}{
		{"set-cookie", func(w http.ResponseWriter, _ *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "preference", Value: "value"})
		}, true},
		{"session-write", func(_ http.ResponseWriter, r *http.Request) { session.Current(r).Set("updated", true) }, true},
		{"vary-cookie", func(w http.ResponseWriter, _ *http.Request) { w.Header().Add("Vary", "cookie") }, false},
		{"vary-authorization", func(w http.ResponseWriter, _ *http.Request) { w.Header().Add("Vary", "Authorization") }, false},
		{"vary-session-header", func(w http.ResponseWriter, _ *http.Request) { w.Header().Add("Vary", "X-Session") }, false},
		{"vary-star", func(w http.ResponseWriter, _ *http.Request) { w.Header().Add("Vary", "*") }, false},
		{"private-policy", func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Cache-Control", "private, max-age=60") }, false},
		{"no-store-policy", func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Cache-Control", "no-store") }, false},
	} {
		t.Run(boundary.name, func(t *testing.T) {
			app, cookie, urls := immutableCacheApp(t)
			app.Use(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					boundary.modify(w, r)
					next.ServeHTTP(w, r)
				})
			})
			h := app.Build()
			for _, url := range []string{urls[0], urls[len(urls)-1]} {
				for _, visitor := range []*http.Cookie{nil, cookie} {
					r := httptest.NewRequest(http.MethodGet, url, nil)
					if visitor != nil {
						r.AddCookie(visitor)
					}
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					if got := w.Result().Header.Get("Cache-Control"); got != "private, no-store" {
						t.Fatalf("session=%t Cache-Control=%q", visitor != nil, got)
					}
					if (len(w.Result().Cookies()) != 0) != boundary.wantCookie {
						t.Fatalf("unexpected cookies: %v", w.Result().Cookies())
					}
				}
			}
		})
	}
	app, cookie, _ := immutableCacheApp(t)
	h := app.Build()
	for _, url := range []string{"/private", "/data", "/gosx/assets/runtime/account.hash.js", "/account.html?v=hash", "/account.json?v=hash", "/app.js", "/_gosx/emoji-codes.json", "/_gosx/image?src=/image.png&w=1", "/gosx/assets/runtime/missing.hash.js"} {
		t.Run(url, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, url, nil)
			r.AddCookie(cookie)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if got := w.Result().Header.Get("Cache-Control"); got != "private, no-store" {
				t.Fatalf("Cache-Control=%q", got)
			}
			if url == "/private" && !bytes.Contains(w.Body.Bytes(), []byte("signed-in")) {
				t.Fatal("private page did not render the session value")
			}
		})
	}
}

func TestImmutableAssetConditionalAndRangeResponses(t *testing.T) {
	app, cookie, urls := immutableCacheApp(t)
	h := app.Build()
	for _, url := range urls {
		t.Run(url, func(t *testing.T) {
			initial := httptest.NewRecorder()
			h.ServeHTTP(initial, httptest.NewRequest(http.MethodGet, url, nil))
			for _, tc := range []struct {
				header, value string
				status        int
				cache         string
			}{
				{"If-Modified-Since", initial.Result().Header.Get("Last-Modified"), http.StatusNotModified, immutableAssetCacheControl},
				{"Range", "bytes=0-3", http.StatusPartialContent, immutableAssetCacheControl},
				{"Range", "bytes=999999-", http.StatusRequestedRangeNotSatisfiable, "private, no-store"},
			} {
				if tc.value == "" {
					t.Fatal("asset has no Last-Modified validator")
				}
				r := httptest.NewRequest(http.MethodGet, url, nil)
				r.AddCookie(cookie)
				r.Header.Set(tc.header, tc.value)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				res := w.Result()
				if res.StatusCode != tc.status || res.Header.Get("Cache-Control") != tc.cache {
					t.Fatalf("%s=%s status=%d Cache-Control=%q", tc.header, tc.value, res.StatusCode, res.Header.Get("Cache-Control"))
				}
				if tc.status != http.StatusRequestedRangeNotSatisfiable && headerHasToken(res.Header, "Vary", "Cookie") {
					t.Fatalf("session variance on immutable response: %v", res.Header)
				}
			}
		})
	}
}

func TestSourceRuntimeDoesNotReceiveImmutableClassification(t *testing.T) {
	app, cookie, _ := immutableCacheApp(t)
	sourcePath := filepath.Join(app.effectiveRuntimeRoot(), "build", "bootstrap.js")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("// development runtime"), 0644); err != nil {
		t.Fatal(err)
	}
	h := app.Build()
	for _, tc := range []struct{ url, cache, body string }{
		{"/gosx/bootstrap.js", "private, no-store", "// development runtime"},
		{"/gosx/bootstrap.js?v=build", immutableAssetCacheControl, "/* bootstrap.js */"},
	} {
		r := httptest.NewRequest(http.MethodGet, tc.url, nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Result().Header.Get("Cache-Control") != tc.cache || !strings.Contains(w.Body.String(), tc.body) {
			t.Fatalf("%s headers=%v body=%q", tc.url, w.Result().Header, w.Body.String())
		}
	}
}

func headerHasToken(headers http.Header, name, token string) bool {
	for _, value := range headers.Values(name) {
		for _, field := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(field), token) {
				return true
			}
		}
	}
	return false
}
