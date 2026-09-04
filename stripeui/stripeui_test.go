package stripeui

import (
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/server"
)

type testPage struct {
	runtime *server.PageRuntime
	head    []gosx.Node
}

func (page *testPage) AddHead(nodes ...gosx.Node)   { page.head = append(page.head, nodes...) }
func (page *testPage) Runtime() *server.PageRuntime { return page.runtime }

func TestHeadLoadsStripeDirectlyAndBridgeLocally(t *testing.T) {
	html := gosx.RenderHTML(Head(RuntimeConfig{Preconnect: true, BridgePath: "/assets/stripe.js"}))
	for _, snippet := range []string{
		`rel="preconnect"`,
		`href="https://js.stripe.com"`,
		`src="https://js.stripe.com/clover/stripe.js"`,
		`data-gosx-script="managed"`,
		`src="/assets/stripe.js"`,
		`data-gosx-script="lifecycle"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
	if strings.Contains(html, "eval(") {
		t.Fatalf("managed scripts must use ordinary CSP-compatible script loading: %q", html)
	}
}

func TestRequireEnablesFrameworkRuntimeSurfaceLifecycle(t *testing.T) {
	page := &testPage{runtime: server.NewPageRuntime()}
	Require(page, RuntimeConfig{})
	if !page.runtime.Summary().Bootstrap {
		t.Fatal("Stripe managed surfaces must enable the shared bootstrap lifecycle")
	}
	if len(page.head) != 1 {
		t.Fatalf("head nodes = %d, want 1 fragment", len(page.head))
	}
}

func TestRequireStillAddsAssetsWhenRuntimeIsUnavailable(t *testing.T) {
	page := &testPage{}
	Require(page, RuntimeConfig{})
	if len(page.head) != 1 {
		t.Fatalf("head nodes = %d, want 1 fragment", len(page.head))
	}
}

func TestHeadRejectsExternalBridgeOverride(t *testing.T) {
	html := gosx.RenderHTML(Head(RuntimeConfig{BridgePath: "https://evil.example/bridge.js"}))
	if !strings.Contains(html, `src="/gosx/stripe-bridge.js"`) || strings.Contains(html, "evil.example") {
		t.Fatalf("expected fail-closed local bridge path in %q", html)
	}
}

func TestHostedCheckoutFormIsNativeZeroRuntimeFallback(t *testing.T) {
	html := gosx.RenderHTML(HostedCheckoutForm(HostedCheckoutProps{
		BaseProps: BaseProps{ID: "buy", Class: "checkout"},
		Action:    "/checkout/start",
		CSRFToken: "csrf-public-token",
	}))
	for _, snippet := range []string{
		`<form`, `id="buy"`, `method="post"`, `action="/checkout/start"`,
		`name="csrf_token"`, `value="csrf-public-token"`, `>Checkout</button>`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
	for _, forbidden := range []string{
		"data-gosx-runtime-surface", "data-gosx-stripe-config", "stripe-bridge", "js.stripe.com", "publishableKey",
	} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("hosted form must remain zero-runtime; found %q in %q", forbidden, html)
		}
	}
}

func TestHostedCheckoutFormFailsClosedForInvalidAction(t *testing.T) {
	for _, action := range []string{
		"", "checkout/start", "//evil.example/start", "https://evil.example/start",
		"/checkout/start?tenant=secret", "/checkout/start#fragment", "/checkout\\start",
	} {
		html := gosx.RenderHTML(HostedCheckoutForm(HostedCheckoutProps{Action: action}))
		if strings.Contains(html, "<form") || !strings.Contains(html, "data-gosx-stripe-invalid-action") {
			t.Fatalf("action %q must render a non-submitting fallback: %q", action, html)
		}
	}
}

func TestValidateSessionActionRejectsQueriesAndCrossOriginTargets(t *testing.T) {
	for _, action := range []string{"/checkout/session", "/account/__actions/stripe-session"} {
		if err := ValidateSessionAction(action); err != nil {
			t.Fatalf("expected %q to be valid: %v", action, err)
		}
	}
	for _, action := range []string{"", "/checkout?price=1", "//evil.test/x", "https://evil.test/x", " /x", "/x#y"} {
		if err := ValidateSessionAction(action); err == nil {
			t.Fatalf("expected %q to be rejected", action)
		}
	}
}

func TestElementsSurfaceUsesActionReferenceAndRedactsSecretKeys(t *testing.T) {
	const sentinel = "SENTINEL_DO_NOT_RENDER"
	node := Elements(ElementsSurfaceProps{
		RuntimeOptions: RuntimeOptions{
			PublishableKey: "pk_test_public",
			StripeOptions: map[string]any{
				"locale":        "auto",
				"Authorization": sentinel,
			},
		},
		SessionAction: "/account/__actions/stripe-session",
		ElementsOptions: map[string]any{
			"appearance":    AppearanceFromTokens(DesignTokens{ColorPrimary: "#111111"}),
			"client_secret": sentinel,
			"nested": map[string]any{
				"headers":  map[string]string{"X-Secret": sentinel},
				"metadata": map[string]string{"clientSecret": sentinel},
			},
		},
	}, PaymentElement(ElementProps{Options: map[string]any{
		"layout": "accordion",
		"body":   sentinel,
	}}))
	html := gosx.RenderHTML(node)
	for _, snippet := range []string{
		`data-gosx-runtime-surface="stripe-elements"`,
		`data-gosx-runtime-surface-version="1"`,
		`data-gosx-fallback="server"`,
		`data-gosx-stripe-element="payment"`,
		`pk_test_public`,
		`/account/__actions/stripe-session`,
		`colorPrimary`,
		`accordion`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
	if strings.Contains(html, sentinel) || strings.Contains(html, "client_secret") || strings.Contains(html, "Authorization") {
		t.Fatalf("secret-bearing config must not enter SSR HTML: %q", html)
	}
}

func TestSecretKeyCannotMasqueradeAsPublishableKey(t *testing.T) {
	const secretKey = "sk_test_SENTINEL_DO_NOT_RENDER"
	html := gosx.RenderHTML(Elements(ElementsSurfaceProps{
		RuntimeOptions: RuntimeOptions{PublishableKey: secretKey},
		SessionAction:  "/session",
	}))
	if strings.Contains(html, secretKey) {
		t.Fatalf("secret key entered SSR HTML: %q", html)
	}
}

func TestManagedSurfacesRenderOnlyFixedActionContracts(t *testing.T) {
	embedded := gosx.RenderHTML(EmbeddedCheckout(EmbeddedCheckoutProps{
		RuntimeOptions: RuntimeOptions{PublishableKey: "pk_test_public"},
		SessionAction:  "/checkout/embedded-session",
	}))
	checkout := gosx.RenderHTML(Checkout(CheckoutProps{
		RuntimeOptions: RuntimeOptions{PublishableKey: "pk_test_public"},
		SessionAction:  "/checkout/custom-session",
	}, CheckoutPaymentElement(CheckoutElementProps{}), CheckoutConfirm(CheckoutConfirmProps{})))
	for _, item := range []struct {
		html string
		want []string
	}{
		{embedded, []string{`data-gosx-runtime-surface="stripe-embedded-checkout"`, `/checkout/embedded-session`}},
		{checkout, []string{`data-gosx-runtime-surface="stripe-checkout"`, `/checkout/custom-session`, `createPaymentElement`, `data-gosx-stripe-checkout-confirm`}},
	} {
		for _, snippet := range item.want {
			if !strings.Contains(item.html, snippet) {
				t.Fatalf("expected %q in %q", snippet, item.html)
			}
		}
	}
}

func TestInvalidManagedSessionActionIsNotSerialized(t *testing.T) {
	html := gosx.RenderHTML(Elements(ElementsSurfaceProps{
		RuntimeOptions: RuntimeOptions{PublishableKey: "pk_test_public"},
		SessionAction:  "https://evil.example/session?secret=yes",
	}))
	if strings.Contains(html, "evil.example") || strings.Contains(html, "secret=yes") {
		t.Fatalf("invalid session action leaked into HTML: %q", html)
	}
	if !strings.Contains(html, `"sessionAction":""`) {
		t.Fatalf("invalid action must leave an explicit fail-closed config: %q", html)
	}
}

func TestPublicPropsHaveNoSecretOrArbitraryRequestFields(t *testing.T) {
	types := []reflect.Type{
		reflect.TypeOf(RuntimeOptions{}),
		reflect.TypeOf(ElementsSurfaceProps{}),
		reflect.TypeOf(EmbeddedCheckoutProps{}),
		reflect.TypeOf(CheckoutProps{}),
		reflect.TypeOf(ConfirmProps{}),
	}
	for _, typ := range types {
		for index := 0; index < typ.NumField(); index++ {
			field := typ.Field(index)
			switch field.Name {
			case "ClientSecret", "ClientSecretRequest", "FetchRequest", "Headers", "Body":
				t.Fatalf("unsafe authoring field %s remains on %s", field.Name, typ.Name())
			}
		}
	}
}

func TestConfirmFormUsesBoundedConfirmationContract(t *testing.T) {
	html := gosx.RenderHTML(ConfirmForm(ConfirmProps{
		Method:     "arbitraryProviderMethod",
		ReturnPath: "https://evil.example/after",
		Redirect:   "arbitrary",
	}))
	if !strings.Contains(html, `data-gosx-stripe-confirm="confirmPayment"`) {
		t.Fatalf("expected fixed confirmation method in %q", html)
	}
	for _, forbidden := range []string{"arbitraryProviderMethod", "evil.example", `"redirect":"arbitrary"`, "clientSecret"} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("forbidden confirmation value %q in %q", forbidden, html)
		}
	}
}
