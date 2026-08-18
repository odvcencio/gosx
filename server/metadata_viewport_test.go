package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// TestResolveViewportDefaultsWhenUnset pins DefaultViewport as the resolved
// value when Metadata.Viewport is empty or whitespace-only. See gosx#237.
func TestResolveViewportDefaultsWhenUnset(t *testing.T) {
	for _, value := range []string{"", "   "} {
		if got := resolveViewport(value); got != DefaultViewport {
			t.Fatalf("resolveViewport(%q) = %q, want %q", value, got, DefaultViewport)
		}
	}
}

// TestResolveViewportHonorsOverride proves a page can request non-standard
// viewport content, for example a fixed-scale canvas page.
func TestResolveViewportHonorsOverride(t *testing.T) {
	const custom = "width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no"
	if got := resolveViewport(custom); got != custom {
		t.Fatalf("resolveViewport(%q) = %q, want unchanged", custom, got)
	}
}

// TestMetadataHeadRendersDefaultViewport proves Metadata.Head — the entry
// point ctx.Head() uses, reached by the legacy-layout and custom-document
// paths that never touch renderDocumentWithContext — always carries a
// viewport tag, not just the App-default document pipeline.
func TestMetadataHeadRendersDefaultViewport(t *testing.T) {
	head := gosx.RenderHTML(Metadata{}.Head())
	if !strings.Contains(head, `<meta name="viewport" content="`+DefaultViewport+`" />`) {
		t.Fatalf("expected default viewport meta tag in %q", head)
	}
}

// TestMetadataHeadRendersViewportOverride proves the typed field overrides
// the default for callers reaching Metadata.Head() directly.
func TestMetadataHeadRendersViewportOverride(t *testing.T) {
	const custom = "width=device-width, initial-scale=1, maximum-scale=1"
	head := gosx.RenderHTML(Metadata{Viewport: custom}.Head())
	if !strings.Contains(head, `<meta name="viewport" content="`+custom+`" />`) {
		t.Fatalf("expected overridden viewport meta tag in %q", head)
	}
	if strings.Count(head, `<meta name="viewport"`) != 1 {
		t.Fatalf("expected exactly one viewport tag in %q", head)
	}
}

// TestAppDefaultDocumentEmitsDefaultViewportOnce proves the App-driven
// default document pipeline emits exactly one viewport tag when the page
// sets no metadata at all.
func TestAppDefaultDocumentEmitsDefaultViewportOnce(t *testing.T) {
	app := New()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		return gosx.El("main", gosx.Text("home"))
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if got := strings.Count(body, `<meta name="viewport"`); got != 1 {
		t.Fatalf("expected exactly one viewport tag, got %d in %q", got, body)
	}
	if !strings.Contains(body, `content="`+DefaultViewport+`"`) {
		t.Fatalf("expected default viewport content in %q", body)
	}
}

// TestAppDefaultDocumentHonorsMetadataViewportOverride proves a page's
// Metadata.Viewport reaches the App-driven default document, replacing the
// default content rather than adding to it.
func TestAppDefaultDocumentHonorsMetadataViewportOverride(t *testing.T) {
	const custom = "width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no"
	app := New()
	app.Page("GET /canvas", func(ctx *Context) gosx.Node {
		ctx.SetMetadata(Metadata{Viewport: custom})
		return gosx.El("main", gosx.Text("canvas"))
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/canvas", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if got := strings.Count(body, `<meta name="viewport"`); got != 1 {
		t.Fatalf("expected exactly one viewport tag, got %d in %q", got, body)
	}
	if !strings.Contains(body, `content="`+custom+`"`) {
		t.Fatalf("expected overridden viewport content in %q", body)
	}
}

// TestAppDefaultDocumentDedupesHandWrittenViewportFromAddHead proves the
// backward-compatibility requirement from gosx#237: a page that has not
// migrated off a hand-written AddHead(gosx.El("meta", ...viewport...)) call
// still gets exactly one viewport tag, not two, once the framework starts
// resolving its own default.
func TestAppDefaultDocumentDedupesHandWrittenViewportFromAddHead(t *testing.T) {
	app := New()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		ctx.AddHead(gosx.El("meta", gosx.Attrs(
			gosx.Attr("name", "viewport"),
			gosx.Attr("content", "width=device-width, initial-scale=1, maximum-scale=1"),
		)))
		return gosx.El("main", gosx.Text("home"))
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if got := strings.Count(body, `<meta name="viewport"`); got != 1 {
		t.Fatalf("expected exactly one viewport tag, got %d in %q", got, body)
	}
	if !strings.Contains(body, `content="width=device-width, initial-scale=1, maximum-scale=1"`) {
		t.Fatalf("expected the hand-written viewport content to survive in %q", body)
	}
}

// TestHTMLDocumentDedupesHandWrittenViewportInHeadArgument proves the same
// backward-compatibility rule for the free-function call convention
// (router.SetLayout callbacks calling HTMLDocument directly, as
// examples/counter and examples/hotswap did before gosx#237): a raw
// viewport meta tag passed directly in the head argument is not duplicated.
func TestHTMLDocumentDedupesHandWrittenViewportInHeadArgument(t *testing.T) {
	doc := HTMLDocument(
		"Counter",
		gosx.RawHTML(`<meta name="viewport" content="width=device-width, initial-scale=1">`),
		gosx.El("main", gosx.Text("count")),
	)
	html := gosx.RenderHTML(doc)
	if got := strings.Count(html, `<meta name="viewport"`); got != 1 {
		t.Fatalf("expected exactly one viewport tag, got %d in %q", got, html)
	}
}

// TestHTMLDocumentDefaultsViewportWithNoMetadataInPlay proves the plain
// three-argument HTMLDocument call — which carries no *Context and no
// Metadata at all — still gets the default viewport tag.
func TestHTMLDocumentDefaultsViewportWithNoMetadataInPlay(t *testing.T) {
	doc := HTMLDocument("Plain", gosx.Node{}, gosx.El("main", gosx.Text("home")))
	html := gosx.RenderHTML(doc)
	if !strings.Contains(html, `<meta name="viewport" content="`+DefaultViewport+`">`) {
		t.Fatalf("expected default viewport meta tag in %q", html)
	}
}

// TestCustomDocumentPathGetsViewportThroughHead proves a custom
// App.SetDocument function — which never calls renderDocumentWithContext —
// still gets a viewport tag, because it flows through doc.Head (built from
// ctx.Head(), i.e. Metadata.Head()) rather than the hardcoded fallback.
func TestCustomDocumentPathGetsViewportThroughHead(t *testing.T) {
	app := New()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		return gosx.El("main", gosx.Text("home"))
	})
	app.SetDocument(func(doc *DocumentContext) gosx.Node {
		return gosx.El("html",
			gosx.El("head", gosx.El("title", gosx.Text(doc.Title)), HeadOutlet(doc.Head)),
			gosx.El("body", doc.Body),
		)
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `<meta name="viewport" content="`+DefaultViewport+`" />`) {
		t.Fatalf("expected default viewport meta tag in custom document output %q", body)
	}
}
