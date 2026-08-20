package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
)

func TestPageStateNonceRoundTripsAndIsNilSafe(t *testing.T) {
	var nilState *PageState
	if got := nilState.Nonce(); got != "" {
		t.Fatalf("expected empty nonce from nil PageState, got %q", got)
	}
	nilState.SetNonce("ignored")

	state := NewPageState()
	if got := state.Nonce(); got != "" {
		t.Fatalf("expected empty nonce by default, got %q", got)
	}
	state.SetNonce("abc123")
	if got := state.Nonce(); got != "abc123" {
		t.Fatalf("expected round-tripped nonce, got %q", got)
	}
}

func TestNavigationScriptWithNonceEscapesAttributeValue(t *testing.T) {
	html := gosx.RenderHTML(NavigationScriptWithNonce(`"><script>alert(1)</script>`))

	if strings.Contains(html, `nonce="">`) {
		t.Fatalf("expected nonce value to be escaped, got %q", html)
	}
	if !strings.Contains(html, `nonce="&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;"`) {
		t.Fatalf("expected escaped nonce attribute value, got %q", html)
	}
}

func TestNavigationRuntimePropagatesNonceToDynamicManagedScripts(t *testing.T) {
	html := gosx.RenderHTML(NavigationScriptWithNonce("nav-nonce"))

	// The navigation runtime ships minified (gosx#221): buildInlineAsset
	// (cmd/buildbootstrap) safely renames the local identifiers this test
	// used to match verbatim — currentDocumentNonce, applyCurrentNonce,
	// effectiveLoad — because they are function-scoped inside navigation.ts's
	// top-level IIFE. What minification never touches is a string literal or
	// a property name, so this test now looks for those: the nonce-carrying
	// selector strings and the `.nonce` assignment currentDocumentNonce/
	// applyCurrentNonce compile down to. See "Nonce propagation" in
	// client/runtime/host/navigation.ts for the readable source.
	for _, snippet := range []string{
		`script[nonce][data-gosx-navigation]`,
		`script[nonce][data-gosx-document-contract]`,
		`script[nonce][data-gosx-script]`,
		`.nonce=`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected navigation runtime to include %q in %q", snippet, html)
		}
	}
}

func TestNonceHelpersKeepEmptyNonceStable(t *testing.T) {
	if withEmpty, plain := gosx.RenderHTML(NavigationScriptWithNonce("")), gosx.RenderHTML(NavigationScript()); withEmpty != plain {
		t.Fatalf("expected empty nonce navigation script to match plain output")
	}
	if withEmpty, plain := gosx.RenderHTML(HTMLDocument(&DocumentContext{Title: "Test Page", Nonce: "", Body: gosx.Text("hello")})), gosx.RenderHTML(HTMLDocument(&DocumentContext{Title: "Test Page", Body: gosx.Text("hello")})); withEmpty != plain {
		t.Fatalf("expected empty nonce HTML document to match plain output")
	}
}

func TestDocumentContextThreadsNonceIntoDocumentContractScript(t *testing.T) {
	ctx := newContext(httptest.NewRequest(http.MethodGet, "/docs", nil))
	ctx.SetNonce("ctx-nonce")

	doc := ctx.documentContext("GET /docs", "Fallback", gosx.Text("body"), true)

	if doc.Nonce != "ctx-nonce" {
		t.Fatalf("expected DocumentContext.Nonce to carry the request nonce, got %q", doc.Nonce)
	}
	renderedHead := gosx.RenderHTML(doc.Head)
	if !strings.Contains(renderedHead, `data-gosx-document-contract nonce="ctx-nonce">`) {
		t.Fatalf("expected nonced document contract script, got %q", renderedHead)
	}
}

func TestAppThreadsPerRequestNonceToOwnedInlineAndRuntimeScripts(t *testing.T) {
	app := New()
	app.EnableNavigation()
	app.Page("GET /docs", func(ctx *Context) gosx.Node {
		ctx.SetNonce("req-nonce-1")
		ctx.Runtime().EnableBootstrap()
		ctx.SetMetadata(Metadata{Title: Title{Absolute: "Docs"}})
		return gosx.El("main", gosx.Text("hello docs"))
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		`<script data-gosx-navigation="true" nonce="req-nonce-1">`,
		`data-gosx-document-contract nonce="req-nonce-1">`,
		`data-gosx-script="bootstrap" data-gosx-bootstrap-mode="lite" src="/gosx/bootstrap-lite.js" nonce="req-nonce-1"`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
}

func TestAppThreadsNonceToFullRuntimeManifestAndBootstrapScripts(t *testing.T) {
	app := New()
	app.Page("GET /video", func(ctx *Context) gosx.Node {
		ctx.SetNonce("runtime-nonce")
		return ctx.Engine(engine.Config{
			Name: "PromoVideo",
			Kind: engine.KindVideo,
			Props: json.RawMessage(`{
				"src": "/media/promo.m3u8"
			}`),
		}, gosx.El("p", gosx.Text("Loading video")))
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/video", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		`<script id="gosx-manifest" type="application/json" nonce="runtime-nonce">`,
		`data-gosx-script="bootstrap" data-gosx-bootstrap-mode="full" src="/gosx/bootstrap-runtime.js" nonce="runtime-nonce"`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
}

func TestAppNonceIsPerRequest(t *testing.T) {
	app := New()
	app.EnableNavigation()
	app.Page("GET /docs", func(ctx *Context) gosx.Node {
		ctx.SetNonce(ctx.Request.URL.Query().Get("nonce"))
		ctx.Runtime().EnableBootstrap()
		return gosx.Text("docs")
	})

	handler := app.Build()
	render := func(target string) string {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Body.String()
	}

	first := render("/docs?nonce=first")
	second := render("/docs?nonce=second")
	if !strings.Contains(first, `nonce="first"`) || strings.Contains(first, `nonce="second"`) {
		t.Fatalf("expected first response to carry only first nonce, got %q", first)
	}
	if !strings.Contains(second, `nonce="second"`) || strings.Contains(second, `nonce="first"`) {
		t.Fatalf("expected second response to carry only second nonce, got %q", second)
	}
}
