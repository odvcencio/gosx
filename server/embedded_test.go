package server

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/hydrate"
)

func TestAppBasePathCollidesWithInternalRoutes(t *testing.T) {
	for _, strips := range []bool{false, true} {
		t.Run(fmt.Sprintf("strips=%t", strips), func(t *testing.T) {
			app := New()
			if err := app.SetBasePath("/news", BasePathOptions{ProxyStripsPrefix: strips}); err != nil {
				t.Fatal(err)
			}
			for _, route := range []string{"/", "/news", "/news/details"} {
				app.Page(route, func(ctx *Context) gosx.Node {
					return gosx.Fragment(gosx.Text("route="+ctx.Request.URL.Path), gosx.El("a", gosx.Attrs(gosx.Attr("href", "/news?q=1#top")), gosx.Text("News")))
				})
			}
			handler := app.Build()
			for _, route := range []string{"/", "/news", "/news/details"} {
				public := "/news" + route
				upstream := public
				if strips {
					upstream = route
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, httptest.NewRequest("GET", upstream, nil))
				for _, want := range []string{"route=" + route, `href="/news/news?q=1#top"`, `"path":"` + public + `"`} {
					if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
						t.Fatalf("route %s: status=%d, missing %s in %s", route, w.Code, want, w.Body.String())
					}
				}
			}
		})
	}
}

func TestDefaultBasePathHealthProbesRequirePrefix(t *testing.T) {
	app := New()
	if err := app.SetBasePath("/news"); err != nil {
		t.Fatal(err)
	}
	handler := app.Build()
	for _, path := range []string{"/healthz", "/readyz"} {
		for _, prefix := range []string{"", "/news"} {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", prefix+path, nil))
			want := 200
			if prefix == "" {
				want = 404
			}
			if w.Code != want {
				t.Errorf("probe %s: status=%d, want %d", prefix+path, w.Code, want)
			}
		}
	}
}

func TestFrameAncestorAllowlist(t *testing.T) {
	for _, policy := range []SecurityPolicy{
		{FrameAncestors: []string{"*"}}, {FrameAncestors: []string{"https:"}}, {FrameAncestors: []string{"https://discord.com; script-src *"}},
		{FrameAncestors: []string{"https://discord.com/path"}}, {FrameAncestors: []string{"'none'", "'self'"}}, {FrameAncestors: []string{"https://*.com*"}}, {FrameAncestors: []string{"https://discord.com\u00a0"}},
		{FrameAncestors: []string{"https://discord.com"}, FrameOptions: "DENY"},
	} {
		if err := New().EnableSecurityPolicy(policy); err == nil {
			t.Errorf("accepted invalid policy: %+v", policy)
		}
	}
	for _, shared := range []bool{false, true} {
		app := New()
		sources := []string{"'self'", "https://discord.com", "https://*.discordsays.com"}
		err := app.EnableSecurityPolicy(SecurityPolicy{FrameAncestors: sources, ContentSecurityPolicy: "script-src 'nonce-{nonce}'; frame-ancestors 'none'", SharedContentSecurityPolicy: "script-src 'none'"})
		if err != nil {
			t.Fatal(err)
		}
		sources[1] = "https://evil.example"
		app.Page("/", func(ctx *Context) gosx.Node {
			if shared {
				ctx.CachePublic(time.Minute)
			}
			return gosx.Text("hello")
		})
		w := httptest.NewRecorder()
		app.Build().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		csp := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "frame-ancestors 'self' https://discord.com https://*.discordsays.com") || strings.Count(csp, "frame-ancestors") != 1 || w.Header().Get("X-Frame-Options") != "" {
			t.Fatalf("policy: %s", csp)
		}
	}
	app := New()
	app.EnableSecurityPolicy(SecurityPolicy{ReportOnly: true, ContentSecurityPolicy: "script-src 'none'"})
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Header().Get("Content-Security-Policy") != "frame-ancestors 'self'" {
		t.Fatal("report-only policy lost framing protection")
	}
}

func TestAppBasePathRenderingAndStreaming(t *testing.T) {
	for _, strips := range []bool{false, true} {
		app := New()
		if err := app.SetBasePath("/.proxy/game", BasePathOptions{ProxyStripsPrefix: strips}); err != nil {
			t.Fatal(err)
		}
		app.Page("/", func(ctx *Context) gosx.Node {
			ctx.Runtime().BindHub("room", "/ws", []hydrate.HubBinding{{Event: "update", Signal: "$state"}})
			ctx.Runtime().Engine(engine.Config{Name: "worker", Kind: engine.KindWorker, WASMPath: "/gosx/worker.wasm"}, gosx.Text(""))
			return gosx.Fragment(gosx.El("a", gosx.Attrs(gosx.Attr("href", "/next")), gosx.Text("Next")), ctx.Defer(gosx.Text("loading"), func() (gosx.Node, error) { return gosx.El("img", gosx.Attrs(gosx.Attr("src", "/image.png"))), nil }))
		})
		path := "/.proxy/game/"
		if strips {
			path = "/"
		}
		w := httptest.NewRecorder()
		app.Build().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		body := w.Body.String()
		for _, want := range []string{`href="/.proxy/game/next"`, `"path":"/.proxy/game/ws"`, `"programRef":"/.proxy/game/gosx/worker.wasm"`, `src="/.proxy/game/gosx/`, `name="gosx-base-path"`, `/.proxy/game/image.png`} {
			if !strings.Contains(body, want) {
				t.Errorf("missing %s in %s", want, body)
			}
		}
	}
}
