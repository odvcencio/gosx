package server

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/hydrate"
)

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
