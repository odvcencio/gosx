# Stripe: server first, managed only when required

GoSX treats Stripe as two different integration shapes:

1. Hosted Checkout is a native HTML form. The browser POSTs to your Go server,
   your server creates the Checkout Session with Stripe's API, and the handler
   returns `303 See Other` to Stripe's Checkout URL. No Stripe.js or GoSX client
   runtime is required.
2. Elements, embedded Checkout, wallets, and 3DS are explicit managed browser
   surfaces. Server-rendered HTML contains only a publishable key and one fixed,
   same-origin session-action path. The bridge obtains the short-lived client
   secret through GoSX's scoped, abortable, CSRF-aware transport and passes it
   directly to Stripe without writing it into HTML, the DOM, telemetry, or
   lifecycle events.

## Hosted Checkout (default)

Render a zero-runtime form:

```go
func checkoutButton(r *http.Request) gosx.Node {
	return stripeui.HostedCheckoutForm(stripeui.HostedCheckoutProps{
		Action:    "/checkout/start",
		CSRFToken: session.Token(r),
	}, gosx.El("button",
		gosx.Attrs(gosx.Attr("type", "submit")),
		gosx.Text("Checkout"),
	))
}
```

Mount the GoSX app beside an app-owned handler. The handler may use
`stripe-go` or direct HTTPS; GoSX core intentionally depends on neither.

```go
app := server.New()
// Register pages on app...

sessions, err := session.New(os.Getenv("SESSION_SECRET"), session.Options{})
if err != nil {
	log.Fatal(err)
}

checkout := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	checkoutURL, err := createCheckoutSession(
		r.Context(),
		os.Getenv("STRIPE_SECRET_KEY"), // server only
		r.FormValue("price"),
	)
	if err != nil {
		http.Error(w, "checkout unavailable", http.StatusBadGateway)
		return
	}
	target, err := url.Parse(checkoutURL)
	if err != nil || target.Scheme != "https" || target.Host != "checkout.stripe.com" {
		http.Error(w, "invalid checkout destination", http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
})

mux := http.NewServeMux()
mux.Handle("POST /checkout/start", sessions.Middleware(sessions.Protect(checkout)))
mux.Handle("/", app.Build())
log.Fatal(http.ListenAndServe(":8080", mux))
```

`HostedCheckoutForm` accepts only a root-relative action without a query or
fragment. An invalid action renders a non-submitting group rather than silently
POSTing to the current page. Put product/price selection in validated POST
fields or resolve it entirely on the server; do not encode capabilities in the
action URL.

## Elements and embedded surfaces

Call `Require` only on pages that actually need Stripe's browser runtime. It
enables the GoSX bootstrap, loads Stripe.js directly from `js.stripe.com`, and
loads the local bridge as a lifecycle asset.

```go
stripeui.Require(ctx, stripeui.RuntimeConfig{Preconnect: true})

return stripeui.Elements(stripeui.ElementsSurfaceProps{
	RuntimeOptions: stripeui.RuntimeOptions{
		PublishableKey: os.Getenv("STRIPE_PUBLISHABLE_KEY"),
	},
	SessionAction: ctx.ActionPath("payment-intent"),
	ElementsOptions: map[string]any{
		"appearance": stripeui.AppearanceFromTokens(themeTokens),
	},
},
	stripeui.PaymentElement(stripeui.ElementProps{}),
	stripeui.ConfirmForm(stripeui.ConfirmProps{
		ReturnPath: "/checkout/return",
	}),
)
```

The named action creates or retrieves the Stripe object on the server and
returns only the client secret needed by the secure surface:

```go
"payment-intent": func(ctx *action.Context) error {
	clientSecret, err := createPaymentIntent(ctx.Request.Context())
	if err != nil {
		return err
	}
	return ctx.Success("", map[string]string{"clientSecret": clientSecret})
},
```

The action path is always a fixed same-origin POST target. Query strings,
fragments, scheme-relative URLs, absolute URLs, arbitrary headers, and authored
request bodies are rejected by the public Go contract and checked again by the
browser bridge. Session requests use the surface lifecycle signal, so page
replacement cancels outstanding work.

GoSX emits only four document events: `gosx:stripe:ready`,
`gosx:stripe:status`, `gosx:stripe:complete`, and `gosx:stripe:error`. Their
details contain bounded scalar identifiers/status fields. Raw Stripe events,
sessions, confirmation results, and errors are never forwarded. A client
`complete` event is UX state only: fulfill orders and grant entitlements from
verified Stripe webhooks or another authoritative server callback.

## Migration from the pre-1.0 bridge

This is an intentional API break. Silent acceptance of ignored secret-bearing
fields would be less safe than a compile error.

| Removed | Replacement |
| --- | --- |
| `FetchRequest{URL, Method, Headers, Body}` | `SessionAction: "/fixed/same-origin/path"` |
| `ClientSecret` / `ClientSecretRequest` | Return `clientSecret` from the session action |
| `RedirectCheckoutForm` | `HostedCheckoutForm` and a server-owned 303 handler |
| `RuntimeOptions.StripeJSURL` / `RuntimeConfig.StripeJSURL` | Stripe.js always loads directly from `js.stripe.com` |
| `BaseProps.Attrs` | Wrap the component, or use its explicit `ID` and `Class` |
| `ElementProps.Events` | Fixed redacted `ready/status/complete/error` events |
| `ConfirmProps.ClientSecret`, `Params`, `ConfirmParams`, `ReturnURL` | Surface session action plus `ReturnPath` and typed confirmation fields |
| `CheckoutConfirm(ConfirmProps{...})` | `CheckoutConfirm(CheckoutConfirmProps{...})` |

Treat every Stripe secret key, webhook signing secret, authorization header,
and server response as server-only material. A publishable key is the only
provider key intended for HTML.
