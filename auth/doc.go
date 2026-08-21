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
