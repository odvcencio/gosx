package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// TestPageStateBodyAttrsAccumulates pins the accumulate-not-clobber rule
// AddHead already follows: two BodyAttrs calls (a layout, then a nested
// page) both reach BodyAttrsValue, in call order. See gosx#236.
func TestPageStateBodyAttrsAccumulates(t *testing.T) {
	state := NewPageState()
	state.BodyAttrs(gosx.Attr("data-gosx-heartbeat", "/api/version"))
	state.BodyAttrs(
		gosx.Attr("data-gosx-heartbeat-interval", "4s"),
		gosx.Attr("data-app-theme", "dark"),
	)

	rendered := gosx.RenderAttrs(state.BodyAttrsValue())
	for _, want := range []string{
		`data-gosx-heartbeat="/api/version"`,
		`data-gosx-heartbeat-interval="4s"`,
		`data-app-theme="dark"`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected %q in accumulated body attrs %q", want, rendered)
		}
	}
}

// TestPageStateBodyAttrsOnNilReceiverIsNoop mirrors AddHead's nil-safety —
// PageState methods tolerate a nil receiver everywhere else in this file.
func TestPageStateBodyAttrsOnNilReceiverIsNoop(t *testing.T) {
	var state *PageState
	state.BodyAttrs(gosx.Attr("data-x", "y"))
	if got := state.BodyAttrsValue(); got != nil {
		t.Fatalf("expected nil BodyAttrsValue on nil receiver, got %v", got)
	}
}

// TestAppRendersBodyLevelHeartbeatAttrsOnBody proves the gosx#236 fix end to
// end for the App-driven default document pipeline: ctx.BodyAttrs reaches
// the rendered <body> tag directly, with no wrapper div and no
// display:contents rule standing in for it.
func TestAppRendersBodyLevelHeartbeatAttrsOnBody(t *testing.T) {
	app := New()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		ctx.BodyAttrs(
			gosx.Attr(NavigationHeartbeatAttr, "/api/league/version"),
			gosx.Attr(NavigationHeartbeatIntervalAttr, "4s"),
		)
		return gosx.El("main", gosx.Text("dashboard"))
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	openTagStart := strings.Index(body, "<body")
	if openTagStart < 0 {
		t.Fatalf("expected a <body> tag in %q", body)
	}
	openTagEnd := strings.Index(body[openTagStart:], ">")
	if openTagEnd < 0 {
		t.Fatalf("expected a closed <body> opening tag in %q", body)
	}
	bodyOpenTag := body[openTagStart : openTagStart+openTagEnd+1]
	for _, want := range []string{
		`data-gosx-heartbeat="/api/league/version"`,
		`data-gosx-heartbeat-interval="4s"`,
	} {
		if !strings.Contains(bodyOpenTag, want) {
			t.Fatalf("expected %q on the <body> opening tag, got %q", want, bodyOpenTag)
		}
	}
	if strings.Contains(body, `class="gosx-heartbeat-shell"`) || strings.Contains(body, "display:contents") {
		t.Fatalf("expected no wrapper-div workaround in %q", body)
	}
}

// TestHTMLDocumentWithBodyAttrsAddsAttrsToBody proves the gosx#236 fix for
// the router.SetLayout call convention, which calls HTMLDocument directly —
// the exact shape the issue's downstream app used.
func TestHTMLDocumentWithBodyAttrsAddsAttrsToBody(t *testing.T) {
	doc := HTMLDocumentWithBodyAttrs(
		"League HQ",
		gosx.Node{},
		gosx.El("main", gosx.Text("dashboard")),
		gosx.Attrs(
			gosx.Attr(NavigationHeartbeatAttr, "/api/league/version"),
			gosx.Attr(NavigationHeartbeatIntervalAttr, "4s"),
		),
	)

	html := gosx.RenderHTML(doc)
	for _, want := range []string{
		`<body data-gosx-document-body="true" data-gosx-enhancement-layer="html" data-gosx-heartbeat="/api/league/version" data-gosx-heartbeat-interval="4s">`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in %q", want, html)
		}
	}
}

// TestDocumentBodyAttrsSharesContractWithRenderedBodyAttrs is the gosx#236
// counterpart to TestDocumentAttrsShareContractWithRenderedDocumentAttrs: a
// custom DocumentFunc built with gosx.El("body", DocumentBodyAttrs(doc), ...)
// must render the exact same body attributes documentBodyAttrs (the raw
// string path renderDocumentWithContext uses) produces, including
// app-supplied BodyAttrs.
func TestDocumentBodyAttrsSharesContractWithRenderedBodyAttrs(t *testing.T) {
	doc := &DocumentContext{
		PageID: "gosx-doc-league-hq",
		BodyAttrs: gosx.Attrs(
			gosx.Attr(NavigationHeartbeatAttr, "/api/league/version"),
			gosx.Attr(NavigationHeartbeatIntervalAttr, "4s"),
		),
	}

	bodyAttrs := documentBodyAttrs(doc)
	renderedBody := gosx.RenderHTML(gosx.El("body", DocumentBodyAttrs(doc)))

	if !strings.Contains(renderedBody, `<body`+bodyAttrs+`>`) {
		t.Fatalf("expected custom body attrs %q in %q", bodyAttrs, renderedBody)
	}
	for _, want := range []string{
		`data-gosx-document-body="true"`,
		`data-gosx-heartbeat="/api/league/version"`,
		`data-gosx-heartbeat-interval="4s"`,
	} {
		if !strings.Contains(renderedBody, want) {
			t.Fatalf("expected %q in %q", want, renderedBody)
		}
	}
}

// TestDocumentBodyAttrsEscapesValues proves attribute escaping for
// app-supplied body attrs goes through the same rules as any other
// gosx.El attribute — gosx.RenderAttrs delegates to the identical
// renderAttrHTML helper El itself uses.
func TestDocumentBodyAttrsEscapesValues(t *testing.T) {
	doc := &DocumentContext{
		BodyAttrs: gosx.Attrs(gosx.Attr("data-x", `"><script>alert(1)</script>`)),
	}
	rendered := documentBodyAttrs(doc)
	if strings.Contains(rendered, "<script>") {
		t.Fatalf("expected escaped value, got unescaped script tag: %q", rendered)
	}
	if !strings.Contains(rendered, `data-x="&#34;&gt;&lt;script&gt;`) {
		t.Fatalf("expected escaped attribute value in %q", rendered)
	}
}

// TestCustomDocumentCanReuseBodyAttrs proves a custom App.SetDocument
// function — which never calls renderDocumentWithContext — still receives
// ctx.BodyAttrs through DocumentContext.BodyAttrs.
func TestCustomDocumentCanReuseBodyAttrs(t *testing.T) {
	app := New()
	app.Page("GET /", func(ctx *Context) gosx.Node {
		ctx.BodyAttrs(gosx.Attr(NavigationHeartbeatAttr, "/api/league/version"))
		return gosx.El("main", gosx.Text("dashboard"))
	})
	app.SetDocument(func(doc *DocumentContext) gosx.Node {
		return gosx.El("html",
			gosx.El("head", gosx.El("title", gosx.Text(doc.Title))),
			gosx.El("body", DocumentBodyAttrs(doc), doc.Body),
		)
	})

	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `data-gosx-heartbeat="/api/league/version"`) {
		t.Fatalf("expected heartbeat attr in custom document output %q", body)
	}
}
