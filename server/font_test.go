package server

import (
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
)

func TestFontRendersPreloadAndFaceCSS(t *testing.T) {
	html := gosx.RenderHTML(Font(FontProps{
		Family: "Inter",
		Src:    "fonts/inter.woff2",
		Weight: "100 900",
	}))

	for _, snippet := range []string{
		`<link rel="preload" as="font" href="/fonts/inter.woff2" type="font/woff2" crossorigin="anonymous" />`,
		`<style data-gosx-font>`,
		`font-family:"Inter"`,
		`src:url("/fonts/inter.woff2") format("woff2")`,
		`font-weight:100 900`,
		`font-display:optional`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}

func TestFontCanSkipPreload(t *testing.T) {
	html := gosx.RenderHTML(Font(FontProps{
		Family:    "Inter",
		Src:       "/fonts/inter.woff2",
		NoPreload: true,
	}))

	if strings.Contains(html, `rel="preload"`) {
		t.Fatalf("expected no preload link, got %q", html)
	}
	if !strings.Contains(html, `@font-face`) {
		t.Fatalf("expected font-face CSS, got %q", html)
	}
}
