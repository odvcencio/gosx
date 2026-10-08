package server_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func TestMountedHeadDecoratorPreservesPageAndDocument(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "page", true: "error"}[failed], func(t *testing.T) {
			app := server.New()
			r := route.NewRouter()
			calls, layouts, bodies := 0, 0, 0
			r.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
				layouts++
				if ctx.Language() != "fr" || ctx.Header().Get("X-Decoration") != "present" {
					t.Fatal("decorator PageState changes were lost")
				}
				return server.HTMLDocument(ctx.Document("Game", body))
			})
			r.Add(route.Route{Pattern: "GET /round/{id}", Handler: func(ctx *route.RouteContext) gosx.Node {
				bodies++
				ctx.AddHead(gosx.RawHTML(`<meta name="existing" content="yes">`))
				if failed {
					panic("render failure")
				}
				return gosx.El("main", gosx.Text("round body"))
			}, ErrorHandler: func(ctx *route.RouteContext, _ error) gosx.Node {
				return gosx.El("main", gosx.Text("error body"))
			}})
			app.Mount("/", r.Build())
			app.EnableNavigation()
			app.AddHeadDecorator(func(ctx *server.Context) (gosx.Node, bool) {
				calls++
				if ctx.Pattern != "GET /round/{id}" || ctx.Request.URL.Path != "/round/42" {
					t.Fatalf("registered pattern missing: %q", ctx.Pattern)
				}
				ctx.SetLanguage("fr")
				ctx.Header().Set("X-Decoration", "present")
				ctx.BodyAttrs(gosx.Attr("data-decoration", "present"))
				ctx.AddHead(gosx.RawHTML(`<meta name="state-decoration" content="yes">`))
				return gosx.RawHTML(`<meta name="returned-decoration" content="yes">`), true
			})
			app.Build() // Rebuilding must not append the same decorators twice.
			h := app.Build()
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/round/42", nil))
			if calls != 1 || layouts != 1 || bodies != 1 {
				t.Fatalf("decorations=%d layouts=%d bodies=%d", calls, layouts, bodies)
			}
			body := w.Body.String()
			for _, marker := range []string{`<meta name="existing"`, `<meta name="state-decoration"`, `<meta name="returned-decoration"`, `<script id="gosx-document"`, `<script data-gosx-navigation="true"`, `data-decoration="present"`} {
				if strings.Count(body, marker) != 1 {
					t.Fatalf("expected one %s in document", marker)
				}
			}
			wantStatus := 200
			if failed {
				wantStatus = 500
			}
			if w.Code != wantStatus || w.Header().Get("X-Decoration") != "present" {
				t.Fatalf("status/header: %d %#v", w.Code, w.Header())
			}
		})
	}
}

func TestAppHeadDecoratorReceivesPageAndErrorPattern(t *testing.T) {
	a := server.New()
	a.Page("GET /page/{id}", func(ctx *server.Context) gosx.Node {
		if ctx.Pattern != "GET /page/{id}" {
			t.Fatal(ctx.Pattern)
		}
		if ctx.Request.PathValue("id") == "fail" {
			panic("page failure")
		}
		return gosx.Text("page")
	})
	var patterns []string
	a.AddHeadDecorator(func(ctx *server.Context) (gosx.Node, bool) {
		patterns = append(patterns, ctx.Pattern)
		return gosx.Node{}, false
	})
	h := a.Build()
	for _, path := range []string{"/page/ok", "/page/fail", "/missing"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	if strings.Join(patterns, ",") != "GET /page/{id},GET /page/{id}," {
		t.Fatal(patterns)
	}
}
