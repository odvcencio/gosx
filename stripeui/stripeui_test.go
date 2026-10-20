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
}

func (page *testPage) Runtime() *server.PageRuntime { return page.runtime }

func TestRequireLoadsNoncedStripeAssetsAfterBootstrap(t *testing.T) {
	page := &testPage{runtime: server.NewPageRuntime()}
	Require(page, RuntimeConfig{Preconnect: true, BridgePath: "/assets/stripe.js"})
	html := gosx.RenderHTML(page.runtime.HeadWithNonce("strict-csp-nonce"))
	for _, snippet := range []string{
		`rel="preconnect"`,
		`href="https://js.stripe.com"`,
		`src="https://js.stripe.com/clover/stripe.js"`,
		`data-gosx-script="managed"`,
		`src="/assets/stripe.js"`,
		`data-gosx-script="lifecycle"`,
		`nonce="strict-csp-nonce"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
	if strings.Contains(html, "eval(") {
		t.Fatalf("managed scripts must use ordinary CSP-compatible script loading: %q", html)
	}
	bootstrap := strings.Index(html, `data-gosx-script="bootstrap"`)
	stripe := strings.Index(html, `src="https://js.stripe.com/clover/stripe.js"`)
	bridge := strings.Index(html, `src="/assets/stripe.js"`)
	if bootstrap < 0 || !(bootstrap < stripe && stripe < bridge) {
		t.Fatalf("expected bootstrap, Stripe.js, bridge execution order in %q", html)
	}
	if got := strings.Count(html, `nonce="strict-csp-nonce"`); got != 3 {
		t.Fatalf("nonced executable scripts = %d, want 3 in %q", got, html)
	}
}

func TestRequireEnablesFrameworkRuntimeSurfaceLifecycle(t *testing.T) {
	page := &testPage{runtime: server.NewPageRuntime()}
	Require(page, RuntimeConfig{})
	if !page.runtime.Summary().Bootstrap {
		t.Fatal("Stripe managed surfaces must enable the shared bootstrap lifecycle")
	}
	html := gosx.RenderHTML(page.runtime.Head())
	if !strings.Contains(html, DefaultStripeJSURL) || !strings.Contains(html, DefaultBridgePath) {
		t.Fatalf("managed Stripe assets missing from runtime head: %q", html)
	}
}

func TestRequireToleratesUnavailableRuntime(t *testing.T) {
	page := &testPage{}
	Require(page, RuntimeConfig{})
}

func TestRequireIsIdempotentAndRejectsExternalBridgeOverride(t *testing.T) {
	page := &testPage{runtime: server.NewPageRuntime()}
	for range 2 {
		Require(page, RuntimeConfig{BridgePath: "https://evil.example/bridge.js"})
	}
	html := gosx.RenderHTML(page.runtime.HeadWithNonce("nonce"))
	if !strings.Contains(html, `src="/gosx/stripe-bridge.js"`) || strings.Contains(html, "evil.example") {
		t.Fatalf("expected fail-closed local bridge path in %q", html)
	}
	if strings.Count(html, DefaultStripeJSURL) != 1 || strings.Count(html, DefaultBridgePath) != 1 {
		t.Fatalf("repeated Require must emit each executable asset once: %q", html)
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
	node := Elements(ElementsSurfaceProps{
		RuntimeOptions: RuntimeOptions{PublishableKey: "pk_test_public"},
		SessionAction:  "/account/__actions/stripe-session",
		ElementsOptions: ElementsOptions{
			Appearance: AppearanceFromTokens(DesignTokens{ColorPrimary: "#111111"}),
			Loader:     LoaderAuto,
		},
	}, PaymentElement(ElementProps{Options: ElementOptions{Layout: LayoutAccordion}}))
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
	if strings.Contains(html, "client_secret") || strings.Contains(html, "clientSecret") || strings.Contains(html, "Authorization") {
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
		{checkout, []string{`data-gosx-runtime-surface="stripe-checkout"`, `/checkout/custom-session`, `data-gosx-stripe-checkout-element="payment"`, `data-gosx-stripe-checkout-confirm`}},
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
		reflect.TypeOf(ElementsOptions{}),
		reflect.TypeOf(ElementOptions{}),
		reflect.TypeOf(Appearance{}),
		reflect.TypeOf(AppearanceVariables{}),
		reflect.TypeOf(ElementsSurfaceProps{}),
		reflect.TypeOf(ElementProps{}),
		reflect.TypeOf(EmbeddedCheckoutProps{}),
		reflect.TypeOf(CheckoutProps{}),
		reflect.TypeOf(CheckoutElementProps{}),
		reflect.TypeOf(ConfirmProps{}),
	}
	for _, typ := range types {
		for index := 0; index < typ.NumField(); index++ {
			field := typ.Field(index)
			if field.Type.Kind() == reflect.Map || field.Type.Kind() == reflect.Interface {
				t.Fatalf("generic serialized field %s remains on %s", field.Name, typ.Name())
			}
			switch field.Name {
			case "ClientSecret", "ClientSecretRequest", "FetchRequest", "Headers", "Body", "Params", "Rules":
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
	for _, snippet := range []string{
		`<button`, `type="button"`, `aria-busy="false"`, `data-gosx-stripe-confirm-control`, `>Pay</button>`,
		`"method":"confirmPayment"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected explicit accessible confirm control %q in %q", snippet, html)
		}
	}
	for _, forbidden := range []string{"role=", "data-gosx-stripe-confirm=", "arbitraryProviderMethod", "evil.example", `"redirect":"arbitrary"`, "clientSecret"} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("forbidden confirmation value %q in %q", forbidden, html)
		}
	}
	alias := gosx.RenderHTML(ConfirmForm(ConfirmProps{BaseProps: BaseProps{ID: "pay"}}, gosx.Text("Purchase")))
	canonical := gosx.RenderHTML(ConfirmButton(ConfirmProps{BaseProps: BaseProps{ID: "pay"}}, gosx.Text("Purchase")))
	for name, rendered := range map[string]string{"ConfirmForm": alias, "ConfirmButton": canonical} {
		for _, snippet := range []string{`id="pay"`, `data-gosx-stripe-confirm-control`, `>Purchase</button>`} {
			if !strings.Contains(rendered, snippet) {
				t.Fatalf("%s must render the canonical confirmation control %q in %q", name, snippet, rendered)
			}
		}
		if strings.Contains(rendered, "role=") {
			t.Fatalf("%s must not render a delegated wrapper: %q", name, rendered)
		}
	}
}
