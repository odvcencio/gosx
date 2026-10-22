package server

import (
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/session"
)

func TestServeSceneEntryIsImmutableJSONThroughSession(t *testing.T) {
	prev := island.DefaultSceneEntryStore()
	t.Cleanup(func() { island.SetSceneEntryStore(prev) })
	store := island.NewSceneEntryStore(4, 1<<20)
	island.SetSceneEntryStore(store)
	hash := store.Put([]byte(`{"scene":{"objects":[]}}`))

	m := session.MustNew("scene-entry-test-secret", session.Options{})
	login := httptest.NewRecorder()
	m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session.Current(r).Set("viewer", "signed-in")
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/login", nil))
	if len(login.Result().Cookies()) != 1 {
		t.Fatal("expected session cookie")
	}
	sessionCookie := login.Result().Cookies()[0]

	app := New()
	app.Use(m.Middleware)
	// Model auth middleware that reads the session on every request.
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = session.Current(r).String("viewer")
			next.ServeHTTP(w, r)
		})
	})
	handler := app.Build()
	get := func(method, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		handler.ServeHTTP(w, req)
		return w
	}
	for _, cookie := range []*http.Cookie{nil, sessionCookie} {
		w := get(http.MethodGet, island.SceneEntryPath(hash), cookie)
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("cookie=%v status=%d type=%q", cookie, w.Code, w.Header().Get("Content-Type"))
		}
		if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") || strings.Contains(cc, "private") {
			t.Fatalf("cookie=%v Cache-Control = %q, want public immutable", cookie, cc)
		}
		if w.Header().Get("ETag") == "" || w.Body.String() != `{"scene":{"objects":[]}}` {
			t.Fatalf("etag=%q body=%q", w.Header().Get("ETag"), w.Body.String())
		}
	}
	for _, path := range []string{"/gosx/scene3d/0000000000000000000000000000ffff.json", "/gosx/scene3d/" + hash} {
		if w := get(http.MethodGet, path, nil); w.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", path, w.Code)
		}
	}
	// The mux cleans a traversal path into a redirect before the handler runs.
	if w := get(http.MethodGet, "/gosx/scene3d/../bootstrap.js", nil); w.Code == http.StatusOK && strings.Contains(w.Body.String(), "scene") {
		t.Fatalf("traversal served scene data: %d", w.Code)
	}
	if head := get(http.MethodHead, island.SceneEntryPath(hash), nil); head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD = %d with %d bytes", head.Code, head.Body.Len())
	}
}

func sceneEntryTestApp(t *testing.T) http.Handler {
	t.Helper()
	app := New()
	app.Page("/scene", func(ctx *Context) gosx.Node {
		cfg := scene.Props{
			Defer: &scene.Defer{},
			Graph: scene.NewGraph(scene.Model{ID: "m", Src: "/models/hero.glb"}),
		}.EngineConfig()
		cfg.MountID = "hero"
		return ctx.Runtime().Engine(cfg, gosx.Text("poster"))
	})
	return app.Build()
}

func scenePropsRef(t *testing.T, handler http.Handler, target string) string {
	t.Helper()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	m := regexp.MustCompile(`"propsRef":"([^"]+)"`).FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatalf("no propsRef in %s", w.Body.String())
	}
	return html.UnescapeString(m[1])
}

// A page rendered by one process names a props hash another process never
// stored (rolling deploy, several pods, restart). The miss re-renders the
// route named in the URL and serves the bytes when the hash matches.
func TestSceneEntryMissRefillsFromRoute(t *testing.T) {
	prev := island.DefaultSceneEntryStore()
	t.Cleanup(func() { island.SetSceneEntryStore(prev) })

	storeA := island.NewSceneEntryStore(4, 1<<20)
	island.SetSceneEntryStore(storeA)
	handler := sceneEntryTestApp(t)
	ref := scenePropsRef(t, handler, "/scene")
	if !strings.Contains(ref, "?r=") {
		t.Fatalf("propsRef must name its route: %s", ref)
	}
	hash := strings.TrimSuffix(strings.TrimPrefix(strings.SplitN(ref, "?", 2)[0], island.SceneEntryPathPrefix), ".json")
	want, ok := storeA.Get(hash)
	if !ok {
		t.Fatal("store A lost the props")
	}

	// Another process: an empty store.
	storeB := island.NewSceneEntryStore(4, 1<<20)
	island.SetSceneEntryStore(storeB)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, ref, nil))
	if w.Code != http.StatusOK || w.Body.String() != string(want) {
		t.Fatalf("refill = %d, body equal = %v", w.Code, w.Body.String() == string(want))
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("Cache-Control = %q", cc)
	}

	// A hash the route does not produce stays a 404, and is remembered.
	bad := island.SceneEntryPath("0000000000000000000000000000ffff") + "?r=%2Fscene"
	for i := 0; i < 2; i++ {
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, bad, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("wrong hash = %d, want 404", w.Code)
		}
	}
	// A route under /gosx/ is never re-rendered.
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, island.SceneEntryPath(hash)+"?r=%2Fgosx%2Fscene3d%2F"+hash+".json", nil))
	storeB2 := island.NewSceneEntryStore(4, 1<<20)
	island.SetSceneEntryStore(storeB2)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, island.SceneEntryPath(hash)+"?r=%2Fgosx%2Fx", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("internal route refill = %d, want 404", w.Code)
	}
}
