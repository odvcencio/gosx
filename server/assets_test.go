package server

import (
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
)

func TestAssetURLNormalizesPublicPaths(t *testing.T) {
	cases := map[string]string{
		"styles/site.css":         "/styles/site.css",
		"/styles/site.css":        "/styles/site.css",
		"https://cdn.test/app.js": "https://cdn.test/app.js",
		"data:text/plain,hello":   "data:text/plain,hello",
	}
	for input, want := range cases {
		if got := AssetURL(input); got != want {
			t.Fatalf("%q: expected %q, got %q", input, want, got)
		}
	}
}

func TestStylesheetRendersLinkTag(t *testing.T) {
	html := gosx.RenderHTML(Stylesheet("styles/site.css", gosx.Attrs(gosx.Attr("media", "screen"))))
	for _, snippet := range []string{
		`rel="stylesheet"`,
		`href="/styles/site.css"`,
		`media="screen"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}
