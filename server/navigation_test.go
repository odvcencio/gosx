package server

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestNavigationScriptHasNoNonceAttribute(t *testing.T) {
	html := gosx.RenderHTML(NavigationScript())

	if !strings.Contains(html, `<script data-gosx-navigation="true">`) {
		t.Fatalf("expected un-nonced navigation script tag, got %q", html)
	}
	if strings.Contains(html, "nonce=") {
		t.Fatalf("expected no nonce attribute, got %q", html)
	}
}

func TestNavigationScriptWithNonceAttachesNonce(t *testing.T) {
	html := gosx.RenderHTML(NavigationScriptWithNonce("abc123"))

	if !strings.Contains(html, `<script data-gosx-navigation="true" nonce="abc123">`) {
		t.Fatalf("expected nonce attribute on navigation script tag, got %q", html)
	}
}

func TestNavigationScriptWithNonceEmptyMatchesNavigationScript(t *testing.T) {
	withEmpty := gosx.RenderHTML(NavigationScriptWithNonce(""))
	plain := gosx.RenderHTML(NavigationScript())

	if withEmpty != plain {
		t.Fatalf("expected NavigationScriptWithNonce(\"\") to match NavigationScript(), got %q vs %q", withEmpty, plain)
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
