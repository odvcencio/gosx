package auth

import (
	_ "embed"

	"m31labs.dev/gosx"
)

//go:embed webauthn_runtime.js
var webAuthnRuntime string

// WebAuthnScript returns the built-in browser helper for WebAuthn begin/finish
// flows. It exposes `window.GoSXWebAuthn.register(...)` and
// `window.GoSXWebAuthn.authenticate(...)`, and binds elements carrying the
// data-gosx-webauthn-action declarative contract.
func WebAuthnScript() gosx.Node {
	return gosx.RawHTML(`<script data-gosx-webauthn="true">` + webAuthnRuntime + `</script>`)
}
