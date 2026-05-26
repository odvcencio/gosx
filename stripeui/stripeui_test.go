package stripeui

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestHeadRendersDirectStripeJSAndBridge(t *testing.T) {
	html := gosx.RenderHTML(Head(RuntimeConfig{Preconnect: true}))
	for _, snippet := range []string{
		`rel="preconnect"`,
		`href="https://js.stripe.com"`,
		`src="https://js.stripe.com/clover/stripe.js"`,
		`data-gosx-script-load="dom"`,
		`src="/gosx/stripe-bridge.js"`,
		`data-gosx-script="lifecycle"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}

func TestElementsSurfaceAndPaymentElementRenderBridgeContracts(t *testing.T) {
	node := Elements(ElementsSurfaceProps{
		RuntimeOptions: RuntimeOptions{PublishableKey: "pk_test_123"},
		ClientSecret:   "pi_secret_123",
		ElementsOptions: map[string]any{
			"appearance": AppearanceFromTokens(DesignTokens{
				ColorPrimary: "#111111",
			}),
		},
	}, PaymentElement(ElementProps{
		Options: map[string]any{"layout": "accordion"},
	}))
	html := gosx.RenderHTML(node)
	for _, snippet := range []string{
		`data-gosx-stripe-surface`,
		`data-gosx-stripe-element="payment"`,
		`pk_test_123`,
		`pi_secret_123`,
		`colorPrimary`,
		`accordion`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}

func TestEmbeddedCheckoutAndRedirectRenderOnlyTheirMounts(t *testing.T) {
	embedded := gosx.RenderHTML(EmbeddedCheckout(EmbeddedCheckoutProps{
		RuntimeOptions: RuntimeOptions{PublishableKey: "pk_test_123"},
		ClientSecretRequest: &FetchRequest{
			URL: "/api/checkout/session",
		},
	}))
	for _, snippet := range []string{
		`data-gosx-stripe-embedded-checkout`,
		`/api/checkout/session`,
	} {
		if !strings.Contains(embedded, snippet) {
			t.Fatalf("expected %q in %q", snippet, embedded)
		}
	}

	redirect := gosx.RenderHTML(RedirectCheckoutForm(RedirectCheckoutProps{
		RuntimeOptions: RuntimeOptions{PublishableKey: "pk_test_123"},
		SessionRequest: &FetchRequest{
			URL: "/api/checkout",
		},
	}))
	for _, snippet := range []string{
		`data-gosx-stripe-redirect`,
		`/api/checkout`,
		`Checkout`,
	} {
		if !strings.Contains(redirect, snippet) {
			t.Fatalf("expected %q in %q", snippet, redirect)
		}
	}
}

func TestCheckoutCustomSurfaceRenderContracts(t *testing.T) {
	node := Checkout(CheckoutProps{
		RuntimeOptions: RuntimeOptions{PublishableKey: "pk_test_123"},
		ClientSecret:   "cs_test_123",
	}, CheckoutPaymentElement(CheckoutElementProps{}), CheckoutConfirm(ConfirmProps{}))
	html := gosx.RenderHTML(node)
	for _, snippet := range []string{
		`data-gosx-stripe-checkout`,
		`data-gosx-stripe-checkout-element="payment"`,
		`createPaymentElement`,
		`data-gosx-stripe-checkout-confirm`,
		`cs_test_123`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}
