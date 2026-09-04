# Polar: hosted checkout, server first

GoSX's first Polar integration is deliberately small: a native HTML form posts
an opaque offer ID to your Go server, `polaradapter` creates a hosted Checkout
Session with Polar's API, and the handler returns an empty `303 See Other` to a
configured Polar checkout origin. There is no browser SDK, embedded surface,
client secret, or app-authored JavaScript in this path.

## Render the form

Use the existing session token. Product IDs, customer data, pricing choices,
and provider credentials do not enter the DOM.

```go
func checkoutButton(r *http.Request) gosx.Node {
	return polarui.CheckoutForm(polarui.CheckoutFormProps{
		Action:    "/billing/polar/checkout",
		OfferID:   "pro-monthly",
		CSRFToken: session.Token(r),
	}, gosx.El("button",
		gosx.Attrs(gosx.Attr("type", "submit")),
		gosx.Text("Start checkout"),
	))
}
```

`Action` must be a root-relative path with no query, fragment, backslash, or
encoded path. `OfferID` is a bounded opaque lookup key, not a Polar product ID.
An invalid action, offer, or empty CSRF token renders a disabled non-form
fallback and copies none of those values into hidden controls.

## Configure the server adapter

Create the client with a server-only Organization Access Token (OAT), an
explicit production or sandbox environment, the application's canonical public
origin, and the exact HTTPS origin or origins your Polar environment returns in
Checkout Session URLs.

```go
polarClient, err := polaradapter.NewClient(polaradapter.ClientOptions{
	OrganizationAccessToken: os.Getenv("POLAR_ACCESS_TOKEN"),
	Environment:             polaradapter.Production,
	HTTPClient:              http.DefaultClient,
	PublicOrigin:            "https://app.example.com",
	AllowedCheckoutOrigins: []string{
		os.Getenv("POLAR_CHECKOUT_ORIGIN"),
	},
	Timeout: 8 * time.Second,
})
if err != nil {
	log.Fatal(err)
}
```

The adapter fixes the API endpoint from `Environment`; there is no base-URL
override that could receive the OAT. The supplied HTTP client is copied and API
redirects are disabled. Provider request time, response size, and form size are
bounded. Only `id` and `url` are decoded from Polar's response, and the URL must
match one configured HTTPS origin exactly with no user information or fragment.
`Client` and `ClientOptions` redact the OAT from ordinary and Go-syntax
formatting. The package does not log.

The allowed checkout origin is intentionally configuration, not a compiled
guess. Polar documents that the API returns a Checkout Session `url`, but does
not currently declare its hostname to be a stable API contract across both
environments. Record the actual origin for each environment in trusted deploy
configuration; never derive it from a request or accept a wildcard/suffix.

## Resolve offers on the server

The browser controls only the opaque offer ID. The resolver supplies typed,
allowlisted Polar inputs. It can read authenticated identity from the context;
it does not receive the HTTP request, form fields, headers, or Host.

```go
resolver := polaradapter.OfferResolverFunc(func(ctx context.Context, offerID string) (polaradapter.CheckoutIntent, error) {
	account, ok := authenticatedAccount(ctx)
	if !ok {
		return polaradapter.CheckoutIntent{}, polaradapter.ErrOfferUnavailable
	}
	switch offerID {
	case "pro-monthly":
		return polaradapter.CheckoutIntent{
			ProductIDs: []string{os.Getenv("POLAR_PRO_MONTHLY_PRODUCT_ID")},
			Customer: polaradapter.Customer{
				ExternalID: account.ID,
				Name:       account.Name,
				Email:      account.Email,
			},
			Locale:      polaradapter.LocaleEnglish,
			Billing:     polaradapter.BillingIndividual,
			SuccessPath: "/billing/success?checkout_id={CHECKOUT_ID}",
			ReturnPath:  "/pricing",
		}, nil
	default:
		return polaradapter.CheckoutIntent{}, polaradapter.ErrOfferUnavailable
	}
})
```

Product and Polar customer IDs are validated as UUIDv4 values. A Polar customer
ID and an application external customer ID are mutually exclusive. Locale and
billing intent are enums rather than open provider option strings. Success and
return targets must be root-relative; the adapter resolves them only against
`PublicOrigin`, so a hostile request `Host` cannot change Polar's callbacks.

## Mount with explicit CSRF authority

The handler cannot be constructed without a CSRF middleware. Pass the existing
GoSX `session.Manager.Protect` method and put `session.Manager.Middleware`
outside the returned handler so the session is loaded before protection runs.

```go
checkout, err := polaradapter.NewCheckoutHandler(polaradapter.CheckoutHandlerOptions{
	Client:        polarClient,
	Resolver:      resolver,
	CSRFProtector: sessions.Protect,

	// Optional. Set this only after your app has established a trusted proxy
	// boundary. polaradapter never reads Forwarded, X-Forwarded-For,
	// CF-Connecting-IP, RemoteAddr, or any other ambient source itself.
	ClientIP: trustedClientIP,
})
if err != nil {
	log.Fatal(err)
}

mux.Handle(
	"POST /billing/polar/checkout",
	sessions.Middleware(checkout),
)
```

The handler accepts only bounded `application/x-www-form-urlencoded` POSTs with
one `offer_id`, an optional single `csrf_token` for middleware validation, and
no unknown fields or query string. It resolves the offer only after CSRF passes.
Success is an empty, `no-store` 303. Resolver and provider diagnostics are never
written to the response.

## Security and fulfillment boundary

- Keep the OAT in server secret storage. Never pass it to `polarui`, a browser,
  a log field, or an error page.
- Hosted checkout adds no app-side script, frame, or `connect-src` requirement.
  For interoperable redirect handling, allow both `'self'` and every configured
  Polar checkout origin in `form-action`; browsers differ on applying that
  directive to redirects after form submission. Do not use a wildcard. For
  example: `form-action 'self' https://checkout.example.com`.
- The success redirect and Checkout Session ID are navigation/UX signals, not
  proof of payment. Grant products, subscriptions, credits, and entitlements
  only after an independently verified authoritative Polar webhook or server
  API check.
- Webhook handling and embedded checkout are intentionally outside this slice.
  Adding either requires a separate signature/lifecycle threat model rather
  than a generic option or JavaScript escape hatch.

## Polar references

- [API overview and fixed production/sandbox origins](https://polar.sh/docs/api-reference/2026-04/introduction)
- [Create Checkout Session](https://polar.sh/docs/api-reference/2026-04/checkouts/create-checkout-session)
- [Checkout API guide](https://polar.sh/docs/features/checkout/session)
- [Organization Access Token authentication](https://polar.sh/docs/integrate/authentication)
- [Sandbox isolation](https://polar.sh/docs/integrate/sandbox)
- [Checkout localization](https://polar.sh/docs/features/checkout/localization)
- [Customer IP and currency selection](https://polar.sh/docs/features/products)
- [Webhook integration](https://polar.sh/docs/integrate/webhooks/endpoints)
- [CSP `form-action` redirect behavior](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy/form-action)

These links describe Polar's current `2026-04` API at the time this adapter was
added. The documented checkout response includes a client secret and many raw
provider fields; `polaradapter` intentionally retains none of them.
