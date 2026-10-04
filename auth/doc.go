// Package auth identifies the current user and guards handlers behind that
// identity.
//
// It owns no user store and no password. A Manager wraps a session.Manager and
// keeps one thing in the session — enough to recover a User on the next
// request. Where that User comes from is the application's choice, made by
// installing a Provider or by using one of the three sign-in flows below.
//
// # The handful that matter
//
//	New(sessions, opts)   build a Manager over an existing session.Manager
//	Manager.Middleware    wrap the handler tree; Current needs it
//	Manager.Require       block anonymous visitors
//	Manager.RequireRole   the same, restricted to one role
//	RequireBearerToken    protect a handler with one static bearer token
//	Current(r)            the User for this request, and whether there is one
//	Manager.SignIn / SignOut  move a User in and out of the session
//
// Require answers in whichever form the caller asked for. A request that wants
// JSON gets 401 and {"error":"authentication required"}; anything else is
// redirected to Options.LoginPath with the original path preserved, so the
// visitor lands back where they were aiming.
//
// # Three ways to sign in, all optional
//
// Each is built from the Manager and each is independent. An application may
// use one, several, or none.
//
//	Manager.MagicLinks  emailed one-time links. Needs a MagicLinkStore and a
//	                    MagicLinkSender; NewMemoryMagicLinkStore covers
//	                    development and single-process use.
//	Manager.OAuth       third-party sign-in. GitHubProvider and GoogleProvider
//	                    are prebuilt; OAuthProvider describes any other.
//	Manager.WebAuthn    passkeys and security keys. Needs a WebAuthn store;
//	                    NewMemoryWebAuthnStore covers development.
//
// Each exposes both halves: a handler pair to mount directly
// (RequestHandler/CallbackHandler, BeginHandler/CallbackHandler,
// RegisterHandler/LoginHandler) and the underlying calls (Issue/Consume,
// Begin/Callback, BeginRegistration/FinishRegistration) for an application that
// wants to own the routes and the responses.
//
// # Safe passkey enrollment and login
//
// RegisterOptionsHandler requires an authenticated Current(r) and ignores any
// identity, Roles, or Meta in the request body. Mount both registration handlers
// behind session and auth middleware, and protect their POST requests with CSRF:
//
//	passkeys := authn.WebAuthn(WebAuthnOptions{Origin: "https://app.example"})
//	mux := http.NewServeMux()
//	mux.Handle("POST /auth/webauthn/register/options", passkeys.RegisterOptionsHandler())
//	mux.Handle("POST /auth/webauthn/register", passkeys.RegisterHandler())
//	mux.Handle("POST /auth/webauthn/login/options", passkeys.LoginOptionsHandler())
//	mux.Handle("POST /auth/webauthn/login", passkeys.LoginHandler())
//	handler := sessions.Middleware(authn.Middleware(sessions.Protect(mux)))
//
// Applications that support sign-up must explicitly set
// WebAuthnOptions.RegistrationUser. That callback must create or resolve a user
// from verified server-side enrollment state and authorize enrollment for that
// account. Looking up an existing account from a client-supplied ID or email is
// insufficient. It runs before the options body is decoded and must leave the
// body readable. Direct BeginRegistration callers have the same responsibility
// to supply a trusted, authorized User.
//
// FinishRegistration stores the credential without signing in or changing
// session privileges. An authenticated user must remain signed in as the same
// user through completion. Sign-up applications must authenticate separately
// after enrollment. Custom WebAuthnStore implementations must atomically reject
// duplicate credential IDs with ErrWebAuthnCredentialExists.
//
// Anonymous login uses discoverable credentials: login hints are ignored and
// allowCredentials is empty. New registrations require a resident key so they
// support this flow. Older non-discoverable credentials need an existing
// authenticated session for account-specific allowCredentials, or migration to
// a discoverable passkey. Authenticated callers receive credential IDs only for
// their own account. Authenticators without resident-key support cannot enroll.
//
// # Magic-link delivery
//
// Configure MagicLinkOptions.Sender before mounting RequestHandler. Without a
// sender, Send fails before issuing a token and RequestHandler returns a generic
// 500. Responses and flash state never contain the link or token, including in
// development. Issue remains a trusted application API for custom delivery;
// deliver its output through a verified channel and never echo it to the
// unauthenticated requester.
//
// # The memory stores are for development
//
// NewMemoryMagicLinkStore and NewMemoryWebAuthnStore hold their state in the
// process. Restarting drops every issued link and every registered credential,
// and a second replica does not see the first one's. Implement MagicLinkStore
// or the WebAuthn store interface against real storage before running more than
// one instance.
//
// # Ordering
//
// session.Manager.Middleware must run before auth's, which must run before any
// handler that calls Current, Require or RequireRole. Current reports false
// when the middleware did not run, which is indistinguishable from an anonymous
// visitor.
//
// # CSRF-protected mutations
//
// Authentication pages commonly mutate a session through a file action. Keep
// session.Manager.Protect in the middleware chain for those unsafe methods and
// put the request token in the form as a hidden `csrf_token` control whose
// value is the file-template `csrf.token` binding. `gosx check` reports a
// missing token when a mutating `action={actionPath("...")}` form has a
// complete, statically visible descendant tree. GET and native/external forms
// stay outside that contract, and component or expression boundaries remain
// the application's responsibility when their rendered fields cannot be
// proved at check time.
//
// # Watching what happens
//
// Manager.UseObserver installs an Observer that receives an AuthEvent.
// NewJSONObserver writes them as JSON lines. Nothing is recorded unless an
// observer is installed.
//
// SignIn and SignOut both emit, with AuthEvent.Type "sign_in" or "sign_out". A
// failure is not a separate event type — it is the same event with Success
// false and Error naming the cause, so an observer that filters on Type alone
// still sees the failures.
//
// # Static bearer tokens
//
// RequireBearerToken is a small fail-closed guard for internal endpoints and
// service-to-service routes that need one configured token but do not need a
// user session. It accepts exactly one Authorization header in the form
// "Bearer token" (the scheme is case-insensitive), compares the token using a
// constant-time SHA-256 digest, and never logs or echoes credentials. An empty
// configured token never authenticates. Set BearerOptions.HideWhenUnconfigured
// when a missing deployment secret should look like a 404; a configured route
// always answers 401 for missing, malformed, or wrong credentials and includes
// an escaped WWW-Authenticate realm challenge.
package auth
