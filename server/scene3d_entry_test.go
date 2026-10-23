package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx/island"
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
