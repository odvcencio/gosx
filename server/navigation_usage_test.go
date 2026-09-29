package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestNavigationLoadsOnlyWhenPageNeedsIt(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"static", `<main><h1>Static page</h1></main>`, false},
		{"native link", `<a href="/next" data-gosx-native>Next</a>`, false},
		{"external link", `<a href="https://external.example/next">Next</a>`, false},
		{"fragment link", `<a href="#content">Content</a>`, false},
		{"download", `<a href="/file" download>Download</a>`, false},
		{"target link", `<a href="/next" target="_blank">Next</a>`, false},
		{"native POST", `<form method="post" action="/submit"></form>`, false},
		{"false managed shorthand", `<form data-gosx-managed="false"></form>`, false},
		{"nonform shorthand", `<div data-gosx-managed></div>`, false},
		{"comment", `<!-- <a data-gosx-link href="/next"> -->`, false},
		{"script text", `<script type="application/json">{"example":"data-gosx-link"}</script>`, false},
		{"inert template", `<template><a data-gosx-link href="/next">Next</a></template>`, false},
		{"managed link", `<a data-gosx-link href="/next">Next</a>`, true},
		{"automatic link", `<a href="/next">Next</a>`, true},
		{"same origin absolute", `<a href="http://example.com/next">Next</a>`, true},
		{"managed form", `<form data-gosx-form method="post" action="/submit"></form>`, true},
		{"managed shorthand", `<form data-gosx-managed></form>`, true},
		{"automatic GET", `<form action="/search"></form>`, true},
		{"framework action", `<form method="post" action="/tasks/__actions/save"></form>`, true},
		{"GET submitter", `<form method="post" action="/save"><button formmethod="get">Preview</button></form>`, true},
		{"revalidation", `<main data-gosx-revalidate-interval="5s"></main>`, true},
		{"heartbeat", `<main data-gosx-heartbeat="POST /pulse"></main>`, true},
		{"countdown", `<time data-gosx-countdown="2026-10-01T00:00:00Z"></time>`, true},
		{"cue toggle", `<button data-gosx-cue-toggle>Mute</button>`, true},
		{"watcher", `<div data-gosx-watch="has:#target"></div>`, true},
		{"live region", `<div data-gosx-live-src="/status"></div>`, true},
		{"live hub", `<div data-gosx-live-hub="updates" data-gosx-live-on="change"></div>`, true},
		{"filter", `<input data-gosx-filter="#items">`, true},
		{"reorder", `<div data-gosx-reorder></div>`, true},
		{"transfer", `<div data-gosx-transfer></div>`, true},
		{"toast", `<div data-gosx-toast-host></div>`, true},
		{"disclosure", `<button data-gosx-disclosure-target="#dialog">Open</button>`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := New()
			app.EnableNavigation()
			app.Page("GET /", func(ctx *Context) gosx.Node { return gosx.RawHTML(tc.body) })
			res := httptest.NewRecorder()
			app.Build().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
			page := res.Body.String()
			if got := navigationScriptAttrs(t, page) != nil; got != tc.want {
				t.Fatalf("navigation script present = %v, want %v", got, tc.want)
			}
			if tc.name == "static" {
				// The document contract is inert JSON; a static page must have
				// no executable framework script or script asset URL.
				if strings.Contains(page, `src="/gosx/`) || strings.Contains(page, "application/javascript") || strings.Contains(page, "text/javascript") {
					t.Fatal("static page shipped framework JavaScript")
				}
			}
		})
	}
}

func TestCustomDocumentNavigationChecksCompleteBody(t *testing.T) {
	for _, linked := range []bool{false, true} {
		app := New()
		app.EnableNavigation()
		app.Page("GET /", func(ctx *Context) gosx.Node { return gosx.Text("Page") })
		calls := 0
		app.SetDocument(func(doc *DocumentContext) gosx.Node {
			calls++
			if linked {
				doc.Body = gosx.Fragment(Link("/next", gosx.Text("Next")), doc.Body)
			}
			return HTMLDocument(doc)
		})
		res := httptest.NewRecorder()
		app.Build().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
		if got := navigationScriptAttrs(t, res.Body.String()) != nil; got != linked {
			t.Fatalf("navigation in custom document = %v, want %v", got, linked)
		}
		if calls != 1 {
			t.Fatalf("document callback ran %d times, want once", calls)
		}
	}
}

func TestNavigationRuntimeDependenciesKeepScript(t *testing.T) {
	for _, setup := range []struct {
		name string
		fn   func(*Context)
	}{
		{"active bootstrap", func(ctx *Context) { ctx.Runtime().EnableBootstrap() }},
		{"lifecycle", func(ctx *Context) { ctx.LifecycleScript("/lifecycle.js") }},
		{"managed script", func(ctx *Context) { ctx.ManagedScript("/client.js", ManagedScriptOptions{}) }},
		{"body heartbeat", func(ctx *Context) { ctx.BodyAttrs(gosx.Attr(NavigationHeartbeatAttr, "POST /pulse")) }},
		{"head revalidation", func(ctx *Context) { ctx.AddHead(gosx.RawHTML(`<meta data-gosx-revalidate-interval="5s">`)) }},
		{"deferred content", func(ctx *Context) {
			ctx.Defer(gosx.Text("Loading"), func() (gosx.Node, error) { return Link("/next", gosx.Text("Next")), nil })
		}},
	} {
		t.Run(setup.name, func(t *testing.T) {
			app := New()
			app.EnableNavigation()
			app.Page("GET /", func(ctx *Context) gosx.Node {
				setup.fn(ctx)
				return gosx.Text("Page")
			})
			res := httptest.NewRecorder()
			app.Build().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
			if navigationScriptAttrs(t, res.Body.String()) == nil {
				t.Fatal("page dependency omitted navigation runtime")
			}
		})
	}
}

func TestNavigationStillRequiresAppOptIn(t *testing.T) {
	app := New()
	app.Page("GET /", func(ctx *Context) gosx.Node { return Link("/next", gosx.Text("Next")) })
	res := httptest.NewRecorder()
	app.Build().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
	if navigationScriptAttrs(t, res.Body.String()) != nil {
		t.Fatal("Link produced a runtime without App.EnableNavigation")
	}
	if !strings.Contains(res.Body.String(), `href="/next"`) {
		t.Fatal("unloaded navigation must keep its native link destination")
	}
}

func TestLegacyLayoutNavigationChecksCompleteBody(t *testing.T) {
	for _, linked := range []bool{false, true} {
		app := New()
		app.EnableNavigation()
		app.Page("GET /", func(ctx *Context) gosx.Node { return gosx.Text("Page") })
		app.SetLayout(func(title string, body gosx.Node) gosx.Node {
			if linked {
				body = gosx.Fragment(Link("/next", gosx.Text("Next")), body)
			}
			return gosx.El("html", gosx.El("head"), gosx.El("body", body))
		})
		res := httptest.NewRecorder()
		app.Build().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
		if got := navigationScriptAttrs(t, res.Body.String()) != nil; got != linked {
			t.Fatalf("navigation in legacy layout = %v, want %v", got, linked)
		}
	}
}
