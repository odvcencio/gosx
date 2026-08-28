package server

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// TestHTMLDocumentOwnsResponsiveViewportMetaTag pins the document-shell rule:
// the shell emits one viewport. Application Head remains uninspected and
// preserved, so applications must not add shell-owned viewport, charset, or
// title nodes there.
func TestHTMLDocumentOwnsResponsiveViewportMetaTag(t *testing.T) {
	doc := HTMLDocument(&DocumentContext{
		Title: "Plain",
		Head: gosx.Fragment(
			gosx.RawHTML(`<meta name="description" content="document shell">`),
		),
		Body: gosx.El("main", gosx.Text("home")),
	})
	html := gosx.RenderHTML(doc)
	if !strings.Contains(html, `<meta name="viewport" content="width=device-width, initial-scale=1">`) {
		t.Fatalf("expected the standard viewport meta tag in %q", html)
	}
	if got := strings.Count(html, `<meta name="viewport"`); got != 1 {
		t.Fatalf("expected exactly one framework viewport, got %d in %q", got, html)
	}
}

func TestHTMLDocumentPreservesArbitraryRawHeadWithoutInspection(t *testing.T) {
	const rawHead = `<meta name="viewport" content="application escape hatch"><meta data-preserved="exact">`
	rendered := gosx.RenderHTML(HTMLDocument(&DocumentContext{
		Head: gosx.RawHTML(rawHead),
	}))

	if !strings.Contains(rendered, rawHead) {
		t.Fatalf("arbitrary RawHTML head was inspected or rewritten: %q", rendered)
	}
	if got := strings.Count(rendered, `<meta name="viewport"`); got != 2 {
		t.Fatalf("expected the framework viewport plus preserved application RawHTML viewport, got %d in %q", got, rendered)
	}
}
