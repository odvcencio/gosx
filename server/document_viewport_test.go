package server

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// TestHTMLDocumentAlwaysEmitsResponsiveViewportMetaTag pins existing,
// pre-existing behavior: renderDocumentWithContext writes the standard
// responsive viewport meta tag into every document unconditionally. This
// was previously unproved by any test — an application that also writes
// its own viewport meta tag (for example through AddHead or directly in
// HTMLDocument's head argument) gets two tags, which is an application-side
// duplicate, not a framework gap. See gosx#236's PR discussion, which
// closed gosx#237 as filed in error on that basis.
func TestHTMLDocumentAlwaysEmitsResponsiveViewportMetaTag(t *testing.T) {
	doc := HTMLDocument("Plain", gosx.Node{}, gosx.El("main", gosx.Text("home")))
	html := gosx.RenderHTML(doc)
	if !strings.Contains(html, `<meta name="viewport" content="width=device-width, initial-scale=1">`) {
		t.Fatalf("expected the standard viewport meta tag in %q", html)
	}
}
