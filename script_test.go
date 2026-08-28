package gosx

import (
	"strings"
	"testing"
)

func TestInlineScriptCarriesNonceAndEscapesClosingTag(t *testing.T) {
	html := RenderHTML(InlineScript(`window.value = "</ScRiPt><script>pwned()</script>"`, "request-nonce"))
	if !strings.Contains(html, `<script nonce="request-nonce">`) {
		t.Fatalf("expected request nonce, got %q", html)
	}
	if strings.Contains(strings.ToLower(html), "</script><script>") {
		t.Fatalf("script source escaped a closing tag too late: %q", html)
	}
	if !strings.Contains(html, `<\/ScRiPt>`) {
		t.Fatalf("expected case-preserving closing-tag escape, got %q", html)
	}
}

func TestJSONScriptCarriesNonceAndSafeJSON(t *testing.T) {
	html := RenderHTML(JSONScript("payload", "json-nonce", map[string]string{
		"html": "</script><script>owned=false</script>",
	}))
	if !strings.Contains(html, `<script type="application/json" id="payload" nonce="json-nonce">`) {
		t.Fatalf("expected JSON script attributes, got %q", html)
	}
	if strings.Contains(strings.ToLower(html), "</script><script>") {
		t.Fatalf("JSON payload escaped a closing tag too late: %q", html)
	}
	if !strings.Contains(html, `\u003c/script\u003e`) && !strings.Contains(html, `<\/script>`) {
		t.Fatalf("expected safe JSON script payload, got %q", html)
	}
}
