package basepath

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutingAndRedirects(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if FromRequest(r) != "/.proxy/game" {
			t.Fatal("missing prefix")
		}
		w.Header().Set("Location", "/next?q=1#top")
		w.WriteHeader(303)
		io.WriteString(w, r.URL.EscapedPath())
	})
	for _, tc := range []struct {
		path     string
		stripped bool
		want     string
		status   int
	}{
		{"/.proxy/game/a%2Fb?q=1", false, "/a%2Fb", 303}, {"/a%2Fb", true, "/a%2Fb", 303},
		{"/.proxy/games/a", false, "404 page not found\n", 404}, {"/a", false, "404 page not found\n", 404},
		{"/.proxy%2Fgame/a", false, "404 page not found\n", 404},
	} {
		w := httptest.NewRecorder()
		Handler("/.proxy/game", tc.stripped, Handler("/.proxy/game", false, next)).ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.status || w.Body.String() != tc.want {
			t.Errorf("%s: %d %q", tc.path, w.Code, w.Body.String())
		}
		if w.Code == 303 && w.Header().Get("Location") != "/.proxy/game/next?q=1#top" {
			t.Fatal(w.Header())
		}
	}
}

func TestHTMLKeepsScriptTextAndPrefixesURLAttributes(t *testing.T) {
	src := `<!doctype html><a href="/next?q=1&amp;x=2#top">/text</a><img srcset="/a.png 1x, /b.png 2x"><form action="/save"><button formaction="/other" data-gosx-action="POST /api">Save</button></form><script nonce="abc">const url="/untouched";</script><script id="json" type="application/json">{"url":"/untouched"}</script><link href="//cdn.example/style.css">`
	got := HTML("/game", src)
	for _, want := range []string{`href="/game/next?q=1&amp;x=2#top"`, `srcset="/game/a.png 1x, /game/b.png 2x"`, `action="/game/save"`, `formaction="/game/other"`, `data-gosx-action="POST /game/api"`, `const url="/untouched";`, `{"url":"/untouched"}`, `href="//cdn.example/style.css"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if twice := HTML("/game", got); twice != got {
		t.Errorf("not idempotent: %s", twice)
	}
}

func TestMountRootRedirectPreservesQuery(t *testing.T) {
	w := httptest.NewRecorder()
	Handler("/game", false, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("noncanonical mount root dispatched") })).ServeHTTP(w, httptest.NewRequest("GET", "/game?instance=1", nil))
	if w.Code != http.StatusPermanentRedirect || w.Header().Get("Location") != "/game/?instance=1" {
		t.Fatal(w.Code, w.Header())
	}
}
