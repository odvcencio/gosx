package route

import (
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/server"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouterBasePathAndAppInheritance(t *testing.T) {
	for _, mounted := range []bool{false, true} {
		router := NewRouter()
		router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
			return server.HTMLDocument(ctx.DocumentContext(ctx.Request, "/notes/{id}", "Notes", body, false))
		})
		if !mounted {
			if err := router.SetBasePath("/.proxy/game"); err != nil {
				t.Fatal(err)
			}
		}
		router.Add(Route{Pattern: "/notes/{id}", Handler: func(ctx *RouteContext) gosx.Node {
			if ctx.Param("id") != "42" {
				t.Fatal(ctx.Params)
			}
			return gosx.El("form", gosx.Attrs(gosx.Attr("action", ctx.ActionPath("save"))), gosx.El("a", gosx.Attrs(gosx.Attr("href", "/next")), gosx.Text("Next")))
		}})
		var h http.Handler = router.Build()
		if mounted {
			app := server.New()
			app.SetBasePath("/.proxy/game")
			app.Mount("/", h)
			h = app.Build()
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/.proxy/game/notes/42", nil))
		for _, want := range []string{`action="/.proxy/game/notes/42/__actions/save"`, `href="/.proxy/game/next"`, `"path":"/.proxy/game/notes/42"`} {
			if !strings.Contains(w.Body.String(), want) {
				t.Errorf("mounted=%v missing %s: %s", mounted, want, w.Body.String())
			}
		}
	}
}

func TestBasePathManagedAndNativeActionRedirects(t *testing.T) {
	app := server.New()
	app.SetBasePath("/.proxy/game")
	registry := action.NewRegistry()
	registry.Register("save", func(ctx *action.Context) error { ctx.Redirect("/next?q=1#top"); return nil })
	app.Mount("/save", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.SetPathValue("name", "save"); registry.ServeHTTP(w, r) }))
	h := app.Build()
	for _, managed := range []bool{false, true} {
		req := httptest.NewRequest("POST", "/.proxy/game/save", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if managed {
			req.Header.Set("Accept", "application/json")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if managed {
			if !strings.Contains(w.Body.String(), `"redirect":"/.proxy/game/next?q=1#top"`) {
				t.Fatal(w.Body.String())
			}
		} else if w.Header().Get("Location") != "/.proxy/game/next?q=1#top" {
			t.Fatal(w.Header())
		}
	}
}
