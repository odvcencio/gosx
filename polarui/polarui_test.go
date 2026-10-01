package polarui

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestCheckoutFormRendersNativePOSTWithOnlyOpaqueInputs(t *testing.T) {
	html := gosx.RenderHTML(CheckoutForm(CheckoutFormProps{
		ID:        "checkout",
		Class:     "primary",
		Action:    "/billing/polar/checkout",
		OfferID:   "annual_pro:v2",
		CSRFToken: "csrf-token_123",
	}, gosx.El("button", gosx.Attrs(gosx.Attr("type", "submit")), gosx.Text("Buy now"))))
	for _, want := range []string{
		`<form`,
		`id="checkout"`,
		`class="primary"`,
		`method="post"`,
		`action="/billing/polar/checkout"`,
		`data-gosx-native`,
		`data-gosx-polar-checkout`,
		`name="offer_id"`,
		`value="annual_pro:v2"`,
		`name="csrf_token"`,
		`value="csrf-token_123"`,
		`Buy now`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in %q", want, html)
		}
	}
	for _, forbidden := range []string{"product_id", "customer_email", "access_token", "<script"} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("did not expect %q in %q", forbidden, html)
		}
	}
}

func TestCheckoutFormSuppliesAccessibleDefaultSubmit(t *testing.T) {
	html := gosx.RenderHTML(CheckoutForm(CheckoutFormProps{
		Action:    "/checkout",
		OfferID:   "starter",
		CSRFToken: "csrf",
	}))
	if !strings.Contains(html, `<button type="submit">Checkout</button>`) {
		t.Fatalf("expected default submit button in %q", html)
	}
}

func TestCheckoutFormFailsClosedWithoutRenderingSensitiveInputs(t *testing.T) {
	tests := []struct {
		name  string
		props CheckoutFormProps
	}{
		{name: "missing action", props: CheckoutFormProps{OfferID: "offer", CSRFToken: "csrf"}},
		{name: "relative action", props: CheckoutFormProps{Action: "checkout", OfferID: "offer", CSRFToken: "csrf"}},
		{name: "scheme relative action", props: CheckoutFormProps{Action: "//evil.example/checkout", OfferID: "offer", CSRFToken: "csrf"}},
		{name: "absolute action", props: CheckoutFormProps{Action: "https://evil.example/checkout", OfferID: "offer", CSRFToken: "csrf"}},
		{name: "query action", props: CheckoutFormProps{Action: "/checkout?next=/evil", OfferID: "offer", CSRFToken: "csrf"}},
		{name: "fragment action", props: CheckoutFormProps{Action: "/checkout#target", OfferID: "offer", CSRFToken: "csrf"}},
		{name: "backslash action", props: CheckoutFormProps{Action: `/checkout\evil`, OfferID: "offer", CSRFToken: "csrf"}},
		{name: "encoded action", props: CheckoutFormProps{Action: "/checkout%2fevil", OfferID: "offer", CSRFToken: "csrf"}},
		{name: "control action", props: CheckoutFormProps{Action: "/checkout\n", OfferID: "offer", CSRFToken: "csrf"}},
		{name: "missing offer", props: CheckoutFormProps{Action: "/checkout", CSRFToken: "csrf"}},
		{name: "offer with whitespace", props: CheckoutFormProps{Action: "/checkout", OfferID: "offer id", CSRFToken: "csrf"}},
		{name: "offer with slash", props: CheckoutFormProps{Action: "/checkout", OfferID: "offer/id", CSRFToken: "csrf"}},
		{name: "oversized offer", props: CheckoutFormProps{Action: "/checkout", OfferID: strings.Repeat("a", MaxOfferIDBytes+1), CSRFToken: "csrf"}},
		{name: "missing csrf", props: CheckoutFormProps{Action: "/checkout", OfferID: "offer"}},
		{name: "csrf with whitespace", props: CheckoutFormProps{Action: "/checkout", OfferID: "offer", CSRFToken: "bad token"}},
		{name: "oversized csrf", props: CheckoutFormProps{Action: "/checkout", OfferID: "offer", CSRFToken: strings.Repeat("a", MaxCSRFTokenBytes+1)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.props.ID = "fallback"
			test.props.Class = "checkout"
			html := gosx.RenderHTML(CheckoutForm(test.props,
				gosx.El("button", gosx.Attrs(gosx.Attr("type", "submit")), gosx.Text("secret child")),
			))
			for _, want := range []string{
				`<div`,
				`id="fallback"`,
				`class="checkout"`,
				`role="group"`,
				`aria-disabled="true"`,
				`data-gosx-polar-checkout-invalid`,
				`type="button"`,
				`disabled`,
				`Checkout unavailable`,
			} {
				if !strings.Contains(html, want) {
					t.Fatalf("expected %q in %q", want, html)
				}
			}
			for _, forbidden := range []string{"<form", `name="offer_id"`, `name="csrf_token"`, "secret child", test.props.OfferID, test.props.CSRFToken} {
				if forbidden != "" && strings.Contains(html, forbidden) {
					t.Fatalf("invalid fallback leaked %q in %q", forbidden, html)
				}
			}
		})
	}
}
