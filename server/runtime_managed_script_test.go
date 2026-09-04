package server

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestPageRuntimeManagedScriptsReceiveNonceAfterBootstrap(t *testing.T) {
	runtime := NewPageRuntime()
	runtime.EnableBootstrap()
	runtime.ManagedScript("https://js.stripe.com/clover/stripe.js", ManagedScriptOptions{
		Role: ManagedScriptRoleManaged,
	}, gosx.Attrs(gosx.BoolAttr("defer"), gosx.Attr("nonce", "caller-controlled")))
	runtime.LifecycleScript("/gosx/stripe-bridge.js", gosx.Attrs(gosx.BoolAttr("defer")))

	html := gosx.RenderHTML(runtime.HeadWithNonce("strict-csp-nonce"))
	bootstrap := strings.Index(html, `data-gosx-script="bootstrap"`)
	stripe := strings.Index(html, `src="https://js.stripe.com/clover/stripe.js"`)
	bridge := strings.Index(html, `src="/gosx/stripe-bridge.js"`)
	if bootstrap < 0 || stripe < 0 || bridge < 0 || !(bootstrap < stripe && stripe < bridge) {
		t.Fatalf("expected bootstrap, provider, bridge order in %q", html)
	}
	if got := strings.Count(html, `nonce="strict-csp-nonce"`); got != 3 {
		t.Fatalf("nonced executable tags = %d, want 3 in %q", got, html)
	}
	stripeTagStart := strings.Index(html, `<script src="https://js.stripe.com/clover/stripe.js"`)
	stripeTagEnd := strings.Index(html[stripeTagStart:], `></script>`)
	stripeTag := html[stripeTagStart : stripeTagStart+stripeTagEnd]
	if requestNonce, callerNonce := strings.Index(stripeTag, `nonce="strict-csp-nonce"`), strings.Index(stripeTag, `nonce="caller-controlled"`); requestNonce < 0 || callerNonce < 0 || requestNonce > callerNonce {
		t.Fatalf("request nonce must be the first effective nonce in %q", stripeTag)
	}
}

func TestPageRuntimeManagedScriptRegistrationIsIdempotent(t *testing.T) {
	runtime := NewPageRuntime()
	runtime.EnableBootstrap()
	for range 2 {
		runtime.ManagedScript("https://js.stripe.com/clover/stripe.js", ManagedScriptOptions{
			Role: ManagedScriptRoleManaged,
		})
		runtime.LifecycleScript("/gosx/stripe-bridge.js")
	}

	html := gosx.RenderHTML(runtime.HeadWithNonce("nonce"))
	if got := strings.Count(html, `src="https://js.stripe.com/clover/stripe.js"`); got != 1 {
		t.Fatalf("Stripe.js tags = %d, want 1 in %q", got, html)
	}
	if got := strings.Count(html, `src="/gosx/stripe-bridge.js"`); got != 1 {
		t.Fatalf("bridge tags = %d, want 1 in %q", got, html)
	}
}
