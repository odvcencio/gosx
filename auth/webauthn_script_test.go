package auth

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestWebAuthnScriptIncludesDeclarativeBinding(t *testing.T) {
	html := gosx.RenderHTML(WebAuthnScript())
	for _, want := range []string{"data-gosx-webauthn-action", "bindDeclarative", "data-gosx-webauthn-options"} {
		if !strings.Contains(html, want) {
			t.Fatalf("WebAuthn runtime missing %q", want)
		}
	}
}
