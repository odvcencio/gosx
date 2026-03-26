package auth

import (
	_ "embed"

	"github.com/odvcencio/gosx"
)

//go:embed webauthn_runtime.js
var webAuthnRuntime string

// WebAuthnScript returns the built-in browser helper for WebAuthn begin/finish
// flows. It exposes `window.GoSXWebAuthn.register(...)` and
// `window.GoSXWebAuthn.authenticate(...)`.
func WebAuthnScript() gosx.Node {
	return gosx.RawHTML(`<script data-gosx-webauthn="true">` + webAuthnRuntime + `</script>`)
}
