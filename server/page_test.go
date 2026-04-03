package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
)

func TestDocumentContextDefaultsWithoutRequest(t *testing.T) {
	var ctx *Context

	doc := ctx.documentContext("", "Fallback", gosx.Text("body"), true)

	if doc == nil {
		t.Fatal("expected document context")
	}
	if doc.Title != "Fallback" {
		t.Fatalf("expected fallback title, got %q", doc.Title)
	}
	if doc.Path != "/" {
		t.Fatalf("expected default path '/', got %q", doc.Path)
	}
	if doc.PageID != "gosx-doc-page" {
		t.Fatalf("expected default page id, got %q", doc.PageID)
	}
	if doc.Request != nil {
		t.Fatalf("expected nil request, got %#v", doc.Request)
	}
	renderedHead := gosx.RenderHTML(doc.Head)
	if renderedHead == "" || !strings.Contains(renderedHead, `data-gosx-document-contract`) {
		t.Fatalf("expected document contract head node, got %q", renderedHead)
	}
}

func TestDocumentContextUsesRequestURIForPathAndPageID(t *testing.T) {
	ctx := newContext(httptest.NewRequest(http.MethodGet, "/docs/forms?tab=posting", nil))
	ctx.SetMetadata(Metadata{Title: Title{Absolute: "Forms"}})

	doc := ctx.documentContext("GET /docs/forms", "Fallback", gosx.Text("body"), true)

	if doc.Path != "/docs/forms?tab=posting" {
		t.Fatalf("expected request uri path, got %q", doc.Path)
	}
	if doc.PageID != "gosx-doc-get-docs-forms" {
		t.Fatalf("expected page id from pattern, got %q", doc.PageID)
	}
	if doc.Title != "Forms" {
		t.Fatalf("expected metadata title, got %q", doc.Title)
	}
}

func TestLinkTagNodeNormalizesRelativeHrefAndKeepsDeterministicOrder(t *testing.T) {
	html := gosx.RenderHTML(LinkTag{
		Rel:   "stylesheet",
		Href:  "styles/site.css",
		Layer: CSSLayerPage,
	}.Node())

	const want = `<link rel="stylesheet" href="/styles/site.css" data-gosx-css-layer="page" data-gosx-css-owner="document-page" data-gosx-css-source="/styles/site.css" />`
	if html != want {
		t.Fatalf("expected %q, got %q", want, html)
	}
}

func TestLinkTagNodePreservesExternalHrefAndExplicitSource(t *testing.T) {
	html := gosx.RenderHTML(LinkTag{
		Rel:    "stylesheet preload",
		Href:   "https://cdn.example.com/app.css",
		As:     "style",
		Source: "cdn-app",
		Owner:  "metadata",
	}.Node())

	const want = `<link rel="stylesheet preload" href="https://cdn.example.com/app.css" as="style" data-gosx-css-layer="global" data-gosx-css-owner="metadata" data-gosx-css-source="cdn-app" />`
	if html != want {
		t.Fatalf("expected %q, got %q", want, html)
	}
}

func TestRenderMetaTagKeepsDeterministicOrder(t *testing.T) {
	html := gosx.RenderHTML(renderMetaTag(MetaTag{
		Name:     "description",
		Property: "og:description",
		Content:  "GoSX",
	}))

	const want = `<meta name="description" property="og:description" content="GoSX" />`
	if html != want {
		t.Fatalf("expected %q, got %q", want, html)
	}
}
