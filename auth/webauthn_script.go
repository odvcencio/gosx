package auth

import (
	_ "embed"

	"m31labs.dev/gosx"
)

//go:embed webauthn_runtime.ts
var webAuthnRuntime string

// WebAuthnScript returns the built-in browser helper for WebAuthn begin/finish
// flows. It exposes `window.GoSXWebAuthn.register(...)` and
// `window.GoSXWebAuthn.authenticate(...)`, and binds elements carrying the
// data-gosx-webauthn-action declarative contract. It is nonce-free for pages
// that do not enable CSP; strict-CSP pages should use WebAuthnScriptWithNonce
// with the request's nonce.
func WebAuthnScript() gosx.Node {
	return WebAuthnScriptWithNonce("")
}

// WebAuthnScriptWithNonce renders the built-in helper with the request-owned
// CSP nonce. Prefer this form (or a request context's InlineScript helper) on
// pages that enable a strict policy.
func WebAuthnScriptWithNonce(nonce string) gosx.Node {
	return gosx.InlineScript(webAuthnRuntime, nonce, gosx.Attrs(gosx.BoolAttr("data-gosx-webauthn")))
}
